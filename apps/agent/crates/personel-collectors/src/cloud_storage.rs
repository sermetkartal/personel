//! Cloud storage local sync folder watcher.
//!
//! Monitors local OneDrive / Dropbox / Google Drive / iCloud / Box sync
//! roots via Win32 [`ReadDirectoryChangesW`] and emits one
//! [`EventKind::CloudStorageSyncEvent`] per file create / modify / delete /
//! rename observed inside any discovered root.
//!
//! # KVKK / privacy posture
//!
//! Per the locked Faz 2 design decision in `CLAUDE.md` §0:
//!
//! > "Cloud storage: lokal sync klasör watch only, NO cloud API OAuth"
//!
//! This collector does **not** call any cloud provider REST API, never
//! reads file contents, and never hashes files. Hashing of sensitive files
//! is the responsibility of [`crate::file_system`]; this collector emits
//! complementary metadata about *which provider* a change came from, so
//! analysts can correlate "user dropped a doc into OneDrive" with the
//! kernel-file ETW stream.
//!
//! Only file metadata (provider tag, relative path, optional size) leaves
//! the endpoint. Absolute paths are included so that joins against the
//! file_system stream are deterministic.
//!
//! # Discovery
//!
//! Probed locations (each, if it exists, becomes a separate watcher):
//!
//! | Provider                  | Path                                                 |
//! |---------------------------|------------------------------------------------------|
//! | OneDrive Personal         | `%USERPROFILE%\OneDrive`                             |
//! | OneDrive Personal (moved) | `HKCU\Software\Microsoft\OneDrive\Accounts\Personal\UserFolder` |
//! | OneDrive for Business     | any `%USERPROFILE%\OneDrive - *` directory + `Accounts\BusinessN\UserFolder` |
//! | Dropbox                   | `%USERPROFILE%\Dropbox`                              |
//! | Google Drive (mirror)     | `%USERPROFILE%\Google Drive`, `%USERPROFILE%\GoogleDrive` |
//! | Google Drive (stream)     | `G:\My Drive`, `H:\My Drive` (drive letter probe)    |
//! | iCloud Drive              | `%USERPROFILE%\iCloudDrive`                          |
//! | Box                       | `%USERPROFILE%\Box`                                  |
//!
//! When zero providers are installed (e.g. a fresh VM) the collector still
//! starts cleanly, reports `healthy: true`, and simply produces no events.
//!
//! # Watcher model
//!
//! One dedicated `std::thread` per discovered root, each running a blocking
//! [`ReadDirectoryChangesW`] loop with `bWatchSubtree = TRUE`. Notifications
//! are bridged into a tokio aggregation task via an
//! [`tokio::sync::mpsc::UnboundedSender`]. The aggregator applies the
//! filter rules and a 3-second per-path coalescing window, then enqueues
//! the resulting events through the standard [`crate::CollectorCtx`]
//! queue handle.
//!
//! Permission-denied or other start-up failures on an individual root are
//! logged at `warn!` level and the offending root is skipped — the
//! remaining watchers continue uninterrupted.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use async_trait::async_trait;
use tokio::sync::{mpsc, oneshot};
use tracing::{debug, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Provider taxonomy
// ──────────────────────────────────────────────────────────────────────────────

/// Local sync provider this collector knows how to detect.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Provider {
    /// OneDrive Personal (consumer account).
    OneDrivePersonal,
    /// OneDrive for Business / Office 365.
    OneDriveBusiness,
    /// Dropbox.
    Dropbox,
    /// Google Drive in mirror mode (full local copy under a normal folder).
    GoogleDriveMirror,
    /// Google Drive for Desktop in stream mode (virtual drive letter).
    GoogleDriveStream,
    /// Apple iCloud Drive on Windows.
    ICloud,
    /// Box (Box Drive / Box Sync).
    Box,
}

impl Provider {
    /// Returns the lowercase tag used in the JSON payload.
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::OneDrivePersonal => "onedrive_personal",
            Self::OneDriveBusiness => "onedrive_business",
            Self::Dropbox => "dropbox",
            Self::GoogleDriveMirror => "google_drive",
            Self::GoogleDriveStream => "google_drive",
            Self::ICloud => "icloud",
            Self::Box => "box",
        }
    }
}

/// Kind of file system change observed inside a sync root.
#[derive(Debug, Clone, Copy)]
enum ChangeKind {
    Create,
    Modify,
    Delete,
    Rename,
}

impl ChangeKind {
    fn as_str(self) -> &'static str {
        match self {
            Self::Create => "create",
            Self::Modify => "modify",
            Self::Delete => "delete",
            Self::Rename => "rename",
        }
    }
}

/// One file-level change handed off from the per-root watcher thread to the
/// async aggregation task.
#[derive(Debug)]
struct FileChange {
    provider: Provider,
    root: PathBuf,
    relative: PathBuf,
    kind: ChangeKind,
    observed_at: Instant,
}

// ──────────────────────────────────────────────────────────────────────────────
// Collector type
// ──────────────────────────────────────────────────────────────────────────────

/// Cloud storage local sync folder watcher.
#[derive(Default)]
pub struct CloudStorageCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl CloudStorageCollector {
    /// Creates a new [`CloudStorageCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for CloudStorageCollector {
    fn name(&self) -> &'static str {
        "cloud_storage"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["cloud.storage_sync_event"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            let roots = discover_roots();
            if roots.is_empty() {
                info!("cloud_storage: no providers detected — parking (healthy)");
                healthy.store(true, Ordering::Relaxed);
                let _ = stop_rx.await;
                return;
            }
            for (provider, root) in &roots {
                info!(provider = provider.as_str(), root = %root.display(), "cloud_storage: watching root");
            }

            let (tx, mut rx) = mpsc::unbounded_channel::<FileChange>();

            // Spawn one OS thread per root. Each thread owns its own kill
            // flag stored in its FileChange channel: when the aggregator's
            // tx is dropped (collector shutdown), the next ReadDirectoryChangesW
            // returning will fail to send and the thread will exit.
            #[cfg(target_os = "windows")]
            for (provider, root) in roots.into_iter() {
                let tx_clone = tx.clone();
                std::thread::Builder::new()
                    .name(format!("personel-cloud-{}", provider.as_str()))
                    .spawn(move || {
                        windows_impl::watch_loop(provider, root, tx_clone);
                    })
                    .map(|_| ())
                    .unwrap_or_else(|e| warn!("cloud_storage: thread spawn failed: {e}"));
            }
            #[cfg(not(target_os = "windows"))]
            {
                let _ = (tx, &roots);
                info!("cloud_storage: ReadDirectoryChangesW not supported on this platform — parking");
            }

            // Drop our own clone so the channel closes when all watcher
            // threads exit.
            drop(tx);

            healthy.store(true, Ordering::Relaxed);

            // Per-path coalescing map. Key is the absolute path string.
            // Value is the Instant at which we last enqueued an event for it.
            let mut last_emit: HashMap<String, Instant> = HashMap::new();
            const COALESCE_WINDOW: Duration = Duration::from_secs(3);
            const GC_INTERVAL: Duration = Duration::from_secs(60);
            let mut last_gc = Instant::now();

            loop {
                tokio::select! {
                    maybe = rx.recv() => {
                        let Some(change) = maybe else {
                            debug!("cloud_storage: all watcher threads exited");
                            break;
                        };
                        let absolute = change.root.join(&change.relative);
                        let key = absolute.to_string_lossy().into_owned();

                        if should_skip(&change.relative) {
                            continue;
                        }

                        // Coalesce same-path bursts within COALESCE_WINDOW.
                        if let Some(prev) = last_emit.get(&key) {
                            if change.observed_at.duration_since(*prev) < COALESCE_WINDOW {
                                continue;
                            }
                        }
                        last_emit.insert(key, change.observed_at);

                        let size_bytes = std::fs::metadata(&absolute)
                            .ok()
                            .and_then(|m| if m.is_file() { Some(m.len()) } else { None });

                        let payload = build_payload(
                            change.provider,
                            change.kind,
                            &change.relative,
                            &absolute,
                            size_bytes,
                            ctx.clock.now_unix_nanos(),
                        );
                        emit_event(&ctx, &payload, &events, &drops);

                        // Periodic GC to bound the coalesce map.
                        if last_gc.elapsed() >= GC_INTERVAL {
                            last_emit.retain(|_, ts| ts.elapsed() < COALESCE_WINDOW * 4);
                            last_gc = Instant::now();
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("cloud_storage: stop requested");
                        break;
                    }
                }
            }
        });

        self.healthy.store(true, Ordering::Relaxed);
        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers shared across platforms
// ──────────────────────────────────────────────────────────────────────────────

/// Skip list: temp/lock/sidecar files that providers create as part of
/// their own sync bookkeeping. Filtering these prevents the firehose
/// effect during normal "save" operations in Office and similar tools.
fn should_skip(rel: &Path) -> bool {
    // Skip any path component that is a known cache directory.
    for component in rel.components() {
        let s = component.as_os_str().to_string_lossy();
        if matches!(
            s.as_ref(),
            ".dropbox" | ".dropbox.cache" | "Personal Vault" | ".tmp.drivedownload"
        ) {
            return true;
        }
    }

    let Some(name) = rel.file_name().map(|n| n.to_string_lossy().into_owned()) else {
        return true;
    };

    if name.starts_with('.') || name.starts_with("~$") || name.starts_with(".~lock.") {
        return true;
    }
    let lower = name.to_ascii_lowercase();
    if lower.ends_with(".tmp")
        || lower.ends_with(".~tmp")
        || lower.ends_with("~.tmp")
        || lower.ends_with(".partial")
        || lower.ends_with(".downloading")
        || lower.ends_with(".crdownload")
    {
        return true;
    }
    false
}

/// Builds the JSON payload string for a single sync event.
fn build_payload(
    provider: Provider,
    kind: ChangeKind,
    relative: &Path,
    absolute: &Path,
    size_bytes: Option<u64>,
    now_nanos: i64,
) -> String {
    let rel_s = relative.to_string_lossy();
    let abs_s = absolute.to_string_lossy();
    let size_s = match size_bytes {
        Some(v) => v.to_string(),
        None => "null".to_string(),
    };
    // RFC3339-ish ISO timestamp from nanos. We avoid pulling chrono — convert
    // via the time crate which is already a workspace dep.
    let ts = format_rfc3339(now_nanos);
    format!(
        r#"{{"provider":"{}","kind":"{}","path":{:?},"absolute_path":{:?},"size_bytes":{},"timestamp":"{}"}}"#,
        provider.as_str(),
        kind.as_str(),
        rel_s,
        abs_s,
        size_s,
        ts,
    )
}

fn format_rfc3339(unix_nanos: i64) -> String {
    // The `time` crate is in workspace deps but not yet imported by this
    // crate. We compute RFC3339 manually for the Z-suffixed UTC form.
    // unix_nanos may be negative pre-1970; clamp to 0 for safety.
    let nanos = unix_nanos.max(0) as u128;
    let secs = (nanos / 1_000_000_000) as i64;
    let sub_ns = (nanos % 1_000_000_000) as u32;

    // Days since epoch and time-of-day.
    let days = secs.div_euclid(86_400);
    let tod = secs.rem_euclid(86_400) as u32;
    let hh = tod / 3600;
    let mm = (tod % 3600) / 60;
    let ss = tod % 60;

    // Civil-from-days (Howard Hinnant's date algorithm).
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };

    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:09}Z",
        y, m, d, hh, mm, ss, sub_ns
    )
}

fn emit_event(
    ctx: &CollectorCtx,
    payload: &str,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::CloudStorageSyncEvent.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            tracing::error!(error = %e, "cloud_storage: queue enqueue failed");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Discovery
// ──────────────────────────────────────────────────────────────────────────────

/// Enumerates all installed sync providers on this machine.
///
/// The returned list contains one entry per *root* — a provider with two
/// configured Business accounts contributes two entries.
pub fn discover_roots() -> Vec<(Provider, PathBuf)> {
    let mut out: Vec<(Provider, PathBuf)> = Vec::new();

    let user_profile = std::env::var_os("USERPROFILE").map(PathBuf::from);

    if let Some(home) = user_profile.as_ref() {
        // Simple fixed locations.
        let simple: &[(Provider, &str)] = &[
            (Provider::OneDrivePersonal, "OneDrive"),
            (Provider::Dropbox, "Dropbox"),
            (Provider::GoogleDriveMirror, "Google Drive"),
            (Provider::GoogleDriveMirror, "GoogleDrive"),
            (Provider::ICloud, "iCloudDrive"),
            (Provider::Box, "Box"),
        ];
        for (p, sub) in simple {
            let candidate = home.join(sub);
            if candidate.is_dir() {
                push_unique(&mut out, *p, candidate);
            }
        }

        // OneDrive for Business sibling folders: any directory matching
        // "OneDrive - *" directly under %USERPROFILE%.
        if let Ok(rd) = std::fs::read_dir(home) {
            for ent in rd.flatten() {
                let name = ent.file_name();
                let n = name.to_string_lossy();
                if n.starts_with("OneDrive - ") {
                    let path = ent.path();
                    if path.is_dir() {
                        push_unique(&mut out, Provider::OneDriveBusiness, path);
                    }
                }
            }
        }
    }

    // Google Drive for Desktop stream-mode drive letters. Drive for Desktop
    // exposes a virtual drive (default G:) with a "My Drive" subfolder.
    for letter in ['G', 'H'] {
        let candidate = PathBuf::from(format!("{letter}:\\My Drive"));
        if candidate.is_dir() {
            push_unique(&mut out, Provider::GoogleDriveStream, candidate);
        }
    }

    // Registry probes: OneDrive UserFolder overrides. The collector reads
    // HKCU\Software\Microsoft\OneDrive\Accounts\{Personal,BusinessN}\UserFolder
    // and adds any path that exists and is not already in `out`.
    #[cfg(target_os = "windows")]
    {
        let probes: &[(Provider, &str)] = &[
            (Provider::OneDrivePersonal, "Software\\Microsoft\\OneDrive\\Accounts\\Personal"),
            (Provider::OneDriveBusiness, "Software\\Microsoft\\OneDrive\\Accounts\\Business1"),
            (Provider::OneDriveBusiness, "Software\\Microsoft\\OneDrive\\Accounts\\Business2"),
            (Provider::OneDriveBusiness, "Software\\Microsoft\\OneDrive\\Accounts\\Business3"),
            (Provider::OneDriveBusiness, "Software\\Microsoft\\OneDrive\\Accounts\\Business4"),
        ];
        for (p, key) in probes {
            if let Some(folder) = windows_impl::read_hkcu_string(key, "UserFolder") {
                let candidate = PathBuf::from(&folder);
                if candidate.is_dir() {
                    push_unique(&mut out, *p, candidate);
                }
            }
        }
    }

    out
}

fn push_unique(out: &mut Vec<(Provider, PathBuf)>, p: Provider, path: PathBuf) {
    let canon = std::fs::canonicalize(&path).unwrap_or(path);
    if !out.iter().any(|(_, existing)| existing == &canon) {
        out.push((p, canon));
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows_impl {
    use std::ffi::OsString;
    use std::os::windows::ffi::{OsStrExt, OsStringExt};
    use std::path::PathBuf;
    use std::time::Instant;

    use tokio::sync::mpsc;
    use tracing::{debug, warn};

    use windows::core::PCWSTR;
    use windows::Win32::Foundation::{BOOL, CloseHandle, ERROR_OPERATION_ABORTED, HANDLE};
    use windows::Win32::Storage::FileSystem::{
        CreateFileW, ReadDirectoryChangesW, FILE_FLAG_BACKUP_SEMANTICS, FILE_LIST_DIRECTORY,
        FILE_NOTIFY_CHANGE_DIR_NAME, FILE_NOTIFY_CHANGE_FILE_NAME, FILE_NOTIFY_CHANGE_LAST_WRITE,
        FILE_NOTIFY_CHANGE_SIZE, FILE_SHARE_DELETE, FILE_SHARE_READ, FILE_SHARE_WRITE,
        OPEN_EXISTING,
    };
    use windows::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, RegQueryValueExW, HKEY, HKEY_CURRENT_USER, KEY_READ, REG_SZ,
        REG_VALUE_TYPE,
    };

    use super::{ChangeKind, FileChange, Provider};

    // FILE_NOTIFY_INFORMATION action codes (winnt.h).
    const FILE_ACTION_ADDED: u32 = 0x00000001;
    const FILE_ACTION_REMOVED: u32 = 0x00000002;
    const FILE_ACTION_MODIFIED: u32 = 0x00000003;
    const FILE_ACTION_RENAMED_OLD_NAME: u32 = 0x00000004;
    const FILE_ACTION_RENAMED_NEW_NAME: u32 = 0x00000005;

    /// Synchronous ReadDirectoryChangesW loop for one root. Runs on a
    /// dedicated OS thread spawned by the collector. Exits when the
    /// channel send returns `Err` (aggregator dropped the receiver) or
    /// when ReadDirectoryChangesW reports an unrecoverable error.
    pub fn watch_loop(provider: Provider, root: PathBuf, tx: mpsc::UnboundedSender<FileChange>) {
        // Open the directory handle. FILE_FLAG_BACKUP_SEMANTICS is required
        // to obtain a HANDLE on a directory object. We open without
        // FILE_FLAG_OVERLAPPED so ReadDirectoryChangesW is fully synchronous
        // and we can pass `lpoverlapped = None`.
        let wide: Vec<u16> = root.as_os_str().encode_wide().chain(std::iter::once(0)).collect();
        let handle = unsafe {
            CreateFileW(
                PCWSTR::from_raw(wide.as_ptr()),
                FILE_LIST_DIRECTORY.0,
                FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
                None,
                OPEN_EXISTING,
                FILE_FLAG_BACKUP_SEMANTICS,
                HANDLE::default(),
            )
        };
        let h = match handle {
            Ok(h) if !h.is_invalid() => h,
            Ok(_) => {
                warn!(
                    provider = provider.as_str(),
                    root = %root.display(),
                    "cloud_storage: CreateFileW returned invalid handle — skipping root"
                );
                return;
            }
            Err(e) => {
                warn!(
                    provider = provider.as_str(),
                    root = %root.display(),
                    error = %e,
                    "cloud_storage: CreateFileW failed (permission denied?) — skipping root"
                );
                return;
            }
        };

        // 64 KiB notification buffer — Microsoft recommends staying under
        // this for network shares but we operate on local paths.
        let mut buf = vec![0u8; 64 * 1024];
        let notify_filter = FILE_NOTIFY_CHANGE_FILE_NAME
            | FILE_NOTIFY_CHANGE_DIR_NAME
            | FILE_NOTIFY_CHANGE_LAST_WRITE
            | FILE_NOTIFY_CHANGE_SIZE;

        loop {
            let mut bytes_returned: u32 = 0;
            let rc = unsafe {
                ReadDirectoryChangesW(
                    h,
                    buf.as_mut_ptr().cast(),
                    buf.len() as u32,
                    BOOL(1), // bWatchSubtree = TRUE
                    notify_filter,
                    Some(&mut bytes_returned),
                    None, // synchronous mode
                    None,
                )
            };
            if let Err(e) = rc {
                // ERROR_OPERATION_ABORTED maps to HRESULT 0x80070000 | 995.
                let hr = e.code().0 as u32;
                let is_abort = hr & 0xFFFF == ERROR_OPERATION_ABORTED.0;
                if is_abort {
                    debug!(provider = provider.as_str(), "cloud_storage: watch aborted");
                } else {
                    warn!(
                        provider = provider.as_str(),
                        root = %root.display(),
                        error = %e,
                        "cloud_storage: ReadDirectoryChangesW failed — exiting watcher"
                    );
                }
                break;
            }
            if bytes_returned == 0 {
                // Buffer overflow: notifications were lost. Continue with
                // the next call to resume monitoring.
                debug!(provider = provider.as_str(), "cloud_storage: notification buffer overflow");
                continue;
            }

            // Parse the FILE_NOTIFY_INFORMATION linked list out of `buf`.
            let mut offset: usize = 0;
            let now = Instant::now();
            loop {
                if offset + 12 > buf.len() {
                    break;
                }
                // Layout (FILE_NOTIFY_INFORMATION):
                //   DWORD NextEntryOffset; DWORD Action; DWORD FileNameLength;
                //   WCHAR FileName[1];
                let next = u32::from_le_bytes(
                    buf[offset..offset + 4].try_into().unwrap_or([0; 4]),
                ) as usize;
                let action = u32::from_le_bytes(
                    buf[offset + 4..offset + 8].try_into().unwrap_or([0; 4]),
                );
                let name_len_bytes = u32::from_le_bytes(
                    buf[offset + 8..offset + 12].try_into().unwrap_or([0; 4]),
                ) as usize;

                let name_start = offset + 12;
                let name_end = name_start + name_len_bytes;
                if name_end > buf.len() {
                    break;
                }
                let name_u16: Vec<u16> = buf[name_start..name_end]
                    .chunks_exact(2)
                    .map(|c| u16::from_le_bytes([c[0], c[1]]))
                    .collect();
                let rel = PathBuf::from(OsString::from_wide(&name_u16));

                let kind = match action {
                    FILE_ACTION_ADDED => Some(ChangeKind::Create),
                    FILE_ACTION_REMOVED => Some(ChangeKind::Delete),
                    FILE_ACTION_MODIFIED => Some(ChangeKind::Modify),
                    FILE_ACTION_RENAMED_OLD_NAME | FILE_ACTION_RENAMED_NEW_NAME => {
                        Some(ChangeKind::Rename)
                    }
                    _ => None,
                };
                if let Some(k) = kind {
                    let change = FileChange {
                        provider,
                        root: root.clone(),
                        relative: rel,
                        kind: k,
                        observed_at: now,
                    };
                    if tx.send(change).is_err() {
                        // Aggregator gone — collector shutting down.
                        debug!(provider = provider.as_str(), "cloud_storage: aggregator closed");
                        unsafe {
                            let _ = CloseHandle(h);
                        }
                        return;
                    }
                }

                if next == 0 {
                    break;
                }
                offset += next;
            }
        }

        unsafe {
            let _ = CloseHandle(h);
        }
    }

    /// Reads a REG_SZ value from HKEY_CURRENT_USER. Returns `None` on any
    /// failure (key missing, value missing, wrong type, decode error).
    pub fn read_hkcu_string(subkey: &str, value_name: &str) -> Option<String> {
        let subkey_wide: Vec<u16> = subkey.encode_utf16().chain(std::iter::once(0)).collect();
        let value_wide: Vec<u16> = value_name.encode_utf16().chain(std::iter::once(0)).collect();

        let mut hkey = HKEY::default();
        let open = unsafe {
            RegOpenKeyExW(
                HKEY_CURRENT_USER,
                PCWSTR::from_raw(subkey_wide.as_ptr()),
                0,
                KEY_READ,
                &mut hkey,
            )
        };
        if open.is_err() {
            return None;
        }

        // First call to discover the required buffer size.
        let mut probe_type = REG_VALUE_TYPE(0);
        let mut data_size: u32 = 0;
        let probe = unsafe {
            RegQueryValueExW(
                hkey,
                PCWSTR::from_raw(value_wide.as_ptr()),
                None,
                Some(&mut probe_type),
                None,
                Some(&mut data_size),
            )
        };
        if probe.is_err() || data_size == 0 {
            unsafe {
                let _ = RegCloseKey(hkey);
            }
            return None;
        }

        let mut buf = vec![0u8; data_size as usize];
        let mut out_type = REG_VALUE_TYPE(0);
        let read = unsafe {
            RegQueryValueExW(
                hkey,
                PCWSTR::from_raw(value_wide.as_ptr()),
                None,
                Some(&mut out_type),
                Some(buf.as_mut_ptr()),
                Some(&mut data_size),
            )
        };
        unsafe {
            let _ = RegCloseKey(hkey);
        }
        if read.is_err() || out_type != REG_SZ {
            return None;
        }

        // Convert UTF-16LE bytes (possibly NUL-terminated) to String.
        let len_u16 = (data_size as usize) / 2;
        let u16_slice: Vec<u16> = buf[..len_u16 * 2]
            .chunks_exact(2)
            .map(|c| u16::from_le_bytes([c[0], c[1]]))
            .collect();
        let trimmed: Vec<u16> = u16_slice.into_iter().take_while(|&c| c != 0).collect();
        String::from_utf16(&trimmed).ok()
    }
}

// Non-windows stub for the registry helper used by discover_roots().
#[cfg(not(target_os = "windows"))]
mod windows_impl {
    pub fn read_hkcu_string(_subkey: &str, _value_name: &str) -> Option<String> {
        None
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn skip_lock_and_temp_files() {
        assert!(should_skip(&PathBuf::from(".~lock.report.docx#")));
        assert!(should_skip(&PathBuf::from("~$report.docx")));
        assert!(should_skip(&PathBuf::from("docs/.hidden")));
        assert!(should_skip(&PathBuf::from("downloads/install.crdownload")));
        assert!(should_skip(&PathBuf::from("file.PARTIAL")));
        assert!(should_skip(&PathBuf::from(".dropbox.cache/abc")));
        assert!(should_skip(&PathBuf::from(".dropbox/abc")));
        assert!(should_skip(&PathBuf::from("Personal Vault/secret.txt")));
        assert!(!should_skip(&PathBuf::from("docs/report.pdf")));
        assert!(!should_skip(&PathBuf::from("photo.jpg")));
    }

    #[test]
    fn provider_tag_strings() {
        assert_eq!(Provider::OneDrivePersonal.as_str(), "onedrive_personal");
        assert_eq!(Provider::OneDriveBusiness.as_str(), "onedrive_business");
        assert_eq!(Provider::Dropbox.as_str(), "dropbox");
        assert_eq!(Provider::GoogleDriveMirror.as_str(), "google_drive");
        assert_eq!(Provider::GoogleDriveStream.as_str(), "google_drive");
        assert_eq!(Provider::ICloud.as_str(), "icloud");
        assert_eq!(Provider::Box.as_str(), "box");
    }

    #[test]
    fn payload_shape() {
        let payload = build_payload(
            Provider::Dropbox,
            ChangeKind::Create,
            &PathBuf::from("docs/report.pdf"),
            &PathBuf::from("C:\\Users\\kartal\\Dropbox\\docs\\report.pdf"),
            Some(123_456),
            1_700_000_000_000_000_000,
        );
        assert!(payload.contains(r#""provider":"dropbox""#));
        assert!(payload.contains(r#""kind":"create""#));
        assert!(payload.contains(r#""size_bytes":123456"#));
        assert!(payload.contains(r#""path":"#));
        assert!(payload.contains(r#""absolute_path":"#));
        assert!(payload.contains(r#""timestamp":"2023-"#));
    }

    #[test]
    fn payload_null_size() {
        let payload = build_payload(
            Provider::OneDrivePersonal,
            ChangeKind::Delete,
            &PathBuf::from("gone.txt"),
            &PathBuf::from("C:\\x\\gone.txt"),
            None,
            1_700_000_000_000_000_000,
        );
        assert!(payload.contains(r#""size_bytes":null"#));
        assert!(payload.contains(r#""kind":"delete""#));
    }

    #[test]
    fn rfc3339_round_number() {
        // 2023-11-14T22:13:20Z corresponds to 1700000000 unix seconds.
        let s = format_rfc3339(1_700_000_000_000_000_000);
        assert!(s.starts_with("2023-11-14T22:13:20"));
        assert!(s.ends_with("Z"));
    }
}
