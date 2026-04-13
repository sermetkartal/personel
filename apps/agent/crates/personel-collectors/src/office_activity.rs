//! Microsoft Office recent-files (MRU) collector.
//!
//! Polls the Office user-MRU registry every two minutes and emits one
//! `office.recent_file_opened` event per *new* MRU entry observed since the
//! last poll. This is a deliberately cheap observation strategy: we never
//! attach to Office processes, never speak COM, never read document bodies,
//! never touch anything outside the per-user `HKCU\Software\Microsoft\Office`
//! hive.
//!
//! # KVKK note (kişisel veri kapsamı)
//!
//! The file path itself is treated as content under the Personel event
//! taxonomy (see `personel_core::event` `OfficeRecentFileOpened` kind, which
//! is classified as **content** for retention/RBAC purposes). The collector
//! deliberately captures **only**:
//!
//!   * Office product (word / excel / powerpoint)
//!   * Office major version that hosted the MRU entry (16.0 / 15.0 / 14.0)
//!   * Absolute file path as written in the registry by Office
//!   * `last_opened` timestamp (FILETIME → RFC 3339 UTC)
//!
//! It does NOT read the file, hash it, capture title/author metadata,
//! enumerate embedded objects, or parse the modified-time field — all of
//! that lives behind the same registry key but would expand the privacy
//! footprint without enriching the SOC monitoring use case (file CRUD is
//! covered by the dedicated `file_system` ETW collector).
//!
//! # Why polling and not RegNotifyChangeKeyValue
//!
//! `RegNotifyChangeKeyValue` requires a dedicated thread blocked on a Win32
//! event object per watched key. With six product/version combinations and a
//! variable number of `LiveId_*` subkeys per product, we would burn 10–20
//! threads to avoid a 120-second poll. MRU entries are not latency-sensitive
//! security signals (SOC alerting fires on the upstream ETW file open, not
//! the cleanup-time MRU write), so periodic enumeration is the right
//! trade-off.
//!
//! # Cursor file
//!
//! `%PROGRAMDATA%\Personel\agent\office_activity.state` stores
//! `{ "<product>_<version>_<filetime_hex>": true }` — one entry per
//! `(product, version, last_opened_filetime)` tuple already emitted. On the
//! first run we baseline the cursor without emitting events; otherwise the
//! agent would flood the queue with the user's entire historical MRU on
//! first install. Subsequent ticks emit only entries whose FILETIME is
//! absent from the cursor map.

use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

/// Polling cadence — once every two minutes per the Faz 2 brief.
const POLL_INTERVAL: Duration = Duration::from_secs(120);

/// Max events emitted in a single poll cycle. If a user opened more than this
/// many distinct documents inside one poll window (or the agent was offline
/// for hours and now sees a long backlog) we emit the most recent N and
/// silently advance the cursor past the rest. The skip is logged at warn so
/// SOC operators can correlate gaps.
const MAX_EVENTS_PER_POLL: usize = 200;

/// Office major versions to enumerate. 16.0 covers Office 2016 / 2019 / 2021
/// / Microsoft 365 (all share the same `16.0` registry root), 15.0 is Office
/// 2013, 14.0 is Office 2010. Older versions are deliberately skipped.
const OFFICE_VERSIONS: &[&str] = &["16.0", "15.0", "14.0"];

/// Office products with a Word / Excel / PowerPoint MRU layout. Other Office
/// apps (Outlook, OneNote, Visio, Project) use different schemas and would
/// need their own parser.
const PRODUCTS: &[Product] = &[Product::Word, Product::Excel, Product::PowerPoint];

/// Maximum file path length we will record. Longer values are truncated. The
/// NTFS limit is 32_767 wchars; in practice Office MRU entries cap around
/// 260 chars but malware authors love writing weirdly long paths.
const MAX_PATH_BYTES: usize = 1024;

// ──────────────────────────────────────────────────────────────────────────────
// Product enum
// ──────────────────────────────────────────────────────────────────────────────

/// Office document product backing an MRU registry layout.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub enum Product {
    /// Microsoft Word.
    Word,
    /// Microsoft Excel.
    Excel,
    /// Microsoft PowerPoint.
    PowerPoint,
}

impl Product {
    /// Lower-case product token for the JSON payload.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Word => "word",
            Self::Excel => "excel",
            Self::PowerPoint => "powerpoint",
        }
    }

    /// Registry subkey segment under `Software\Microsoft\Office\<ver>\<seg>`.
    #[must_use]
    pub const fn registry_segment(self) -> &'static str {
        match self {
            Self::Word => "Word",
            Self::Excel => "Excel",
            Self::PowerPoint => "PowerPoint",
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Cursor state
// ──────────────────────────────────────────────────────────────────────────────

/// Persistent dedup cursor. We store hex-encoded FILETIME stamps keyed by
/// `<product>_<version>_<filetime_hex>` as a BTreeSet so the on-disk file
/// stays diff-stable across runs (helpful when an operator inspects it).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct CursorState {
    /// Set of `(product, version, filetime)` tuples already emitted.
    seen: BTreeSet<String>,
    /// Whether the collector has completed its first baseline pass. On the
    /// first poll we populate `seen` *without* emitting events (so the agent
    /// doesn't flood the queue with historical opens) and flip this flag.
    baselined: bool,
}

impl CursorState {
    fn key(product: Product, version: &str, filetime: u64) -> String {
        format!("{}_{}_{:016x}", product.as_str(), version, filetime)
    }

    fn contains(&self, product: Product, version: &str, filetime: u64) -> bool {
        self.seen.contains(&Self::key(product, version, filetime))
    }

    fn insert(&mut self, product: Product, version: &str, filetime: u64) {
        self.seen.insert(Self::key(product, version, filetime));
    }
}

/// Returns the on-disk state file path under `%PROGRAMDATA%`. Falls back to
/// the system temp directory if `PROGRAMDATA` is unset (Windows dev builds
/// running under unusual session contexts; non-Windows test harnesses).
fn state_file_path() -> PathBuf {
    let base = std::env::var("PROGRAMDATA")
        .map(PathBuf::from)
        .unwrap_or_else(|_| std::env::temp_dir());
    base.join("Personel").join("agent").join("office_activity.state")
}

fn load_state() -> CursorState {
    let path = state_file_path();
    match fs::read(&path) {
        Ok(bytes) => serde_json::from_slice::<CursorState>(&bytes).unwrap_or_else(|e| {
            warn!(
                error = %e,
                path = %path.display(),
                "office_activity: state file corrupt — re-baselining"
            );
            CursorState::default()
        }),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => CursorState::default(),
        Err(e) => {
            warn!(
                error = %e,
                path = %path.display(),
                "office_activity: state file unreadable — re-baselining"
            );
            CursorState::default()
        }
    }
}

fn save_state(state: &CursorState) {
    let path = state_file_path();
    if let Some(parent) = path.parent() {
        if let Err(e) = fs::create_dir_all(parent) {
            warn!(
                error = %e,
                path = %parent.display(),
                "office_activity: cannot create state dir — cursor will not persist"
            );
            return;
        }
    }
    match serde_json::to_vec_pretty(state) {
        Ok(bytes) => {
            if let Err(e) = fs::write(&path, bytes) {
                warn!(
                    error = %e,
                    path = %path.display(),
                    "office_activity: state write failed — cursor will not persist"
                );
            }
        }
        Err(e) => {
            warn!(error = %e, "office_activity: state serialise failed");
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// MRU entry parser (platform-agnostic)
// ──────────────────────────────────────────────────────────────────────────────

/// One parsed MRU registry value.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct MruEntry {
    /// 64-bit Windows FILETIME — 100 ns ticks since 1601-01-01 UTC.
    pub last_opened: u64,
    /// Absolute file path as written by Office.
    pub file_path: PathBuf,
}

/// Parses an Office MRU registry value of the form:
///
/// ```text
/// [F00000000][T01D8FA12345678AB][O01D8FA12345600AB]*C:\Users\me\report.docx
/// ```
///
/// We extract the 16-hex-digit value inside the `[T...]` group as the
/// FILETIME and treat everything after the final `*` as the path. The
/// `[O...]` modify-time and `[F...]` flag bits are intentionally ignored.
///
/// Returns `None` for any malformed value: missing brackets, non-hex digits,
/// missing `*`, empty path. Office occasionally writes synthetic entries
/// with paths like `Recovered_Document` or empty `*` payloads — those should
/// not produce events.
pub(crate) fn parse_mru_value(raw: &str) -> Option<MruEntry> {
    // Split off the path. A real MRU value always contains exactly one '*'
    // separator between the closing ']' of the bracketed metadata block and
    // the file path. We anchor on the LAST ']' first so that paths
    // containing literal asterisks (legal on UNC share names) survive.
    let last_bracket = raw.rfind(']')?;
    let after_brackets = &raw[last_bracket + 1..];
    let star_offset = after_brackets.find('*')?;
    let star = last_bracket + 1 + star_offset;
    let (head, path_part) = raw.split_at(star);
    // path_part starts with the '*' — skip it.
    let path_str = path_part.get(1..)?.trim();
    if path_str.is_empty() {
        return None;
    }
    if path_str.len() > MAX_PATH_BYTES {
        // Truncate giant paths but keep the entry — we still want the event.
        // The truncation is logged at the call site.
    }

    // Find the [T...] group inside head. Office may reorder F/T/O groups in
    // theory; in practice T always follows F, but we search rather than
    // assume positional offsets.
    let t_open = head.find("[T")?;
    let after_t = &head[t_open + 2..];
    let t_close = after_t.find(']')?;
    let hex = &after_t[..t_close];
    if hex.is_empty() || hex.len() > 16 {
        return None;
    }
    let filetime = u64::from_str_radix(hex, 16).ok()?;
    if filetime == 0 {
        return None;
    }

    let trimmed = if path_str.len() > MAX_PATH_BYTES {
        // Cut on a char boundary — file paths are valid UTF-8 by Rust contract.
        let mut end = MAX_PATH_BYTES;
        while !path_str.is_char_boundary(end) {
            end -= 1;
        }
        &path_str[..end]
    } else {
        path_str
    };

    Some(MruEntry { last_opened: filetime, file_path: PathBuf::from(trimmed) })
}

// ──────────────────────────────────────────────────────────────────────────────
// FILETIME → RFC 3339 UTC
// ──────────────────────────────────────────────────────────────────────────────

/// Windows FILETIME epoch is 1601-01-01T00:00:00Z. Unix epoch is
/// 1970-01-01T00:00:00Z. The gap is exactly 11_644_473_600 seconds.
const FILETIME_EPOCH_OFFSET_SECS: i64 = 11_644_473_600;

/// Converts a Windows FILETIME (100 ns ticks since 1601-01-01) to an RFC 3339
/// UTC timestamp string. The seconds are floored — we drop sub-second
/// precision because MRU resolution in practice rounds to the nearest second
/// anyway.
pub(crate) fn filetime_to_rfc3339(filetime: u64) -> String {
    // 100 ns ticks → seconds.
    let secs_since_1601 = (filetime / 10_000_000) as i64;
    let unix_secs = secs_since_1601 - FILETIME_EPOCH_OFFSET_SECS;
    if unix_secs < 0 {
        // Pathological — pre-1970 timestamp. Clamp at epoch to keep the
        // event payload valid.
        return "1970-01-01T00:00:00Z".to_string();
    }

    // Algorithm: civil-from-days, public-domain (Howard Hinnant). Avoids
    // pulling in `chrono` for one timestamp formatter.
    let days = unix_secs / 86_400;
    let secs_of_day = unix_secs % 86_400;
    let h = secs_of_day / 3600;
    let m = (secs_of_day % 3600) / 60;
    let s = secs_of_day % 60;

    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m_civ = if mp < 10 { mp + 3 } else { mp - 9 };
    let y_civ = y + i64::from(m_civ <= 2);

    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        y_civ, m_civ, d, h, m, s
    )
}

// ──────────────────────────────────────────────────────────────────────────────
// Registry enumeration (Windows only)
// ──────────────────────────────────────────────────────────────────────────────

/// One MRU root key — i.e. one specific `File MRU` registry key whose values
/// we want to dump. We collect all roots first, then iterate.
#[derive(Debug, Clone)]
struct MruRoot {
    product: Product,
    version: &'static str,
    /// Subkey path under `HKEY_CURRENT_USER`.
    subkey: String,
}

#[cfg(target_os = "windows")]
fn enumerate_mru_roots() -> Vec<MruRoot> {
    use windows::core::PCWSTR;
    use windows::Win32::System::Registry::{
        RegCloseKey, RegEnumKeyExW, RegOpenKeyExW, HKEY, HKEY_CURRENT_USER, KEY_READ,
    };

    let mut roots: Vec<MruRoot> = Vec::new();

    for &version in OFFICE_VERSIONS {
        for &product in PRODUCTS {
            let base = format!(
                r"Software\Microsoft\Office\{}\{}",
                version,
                product.registry_segment()
            );

            // 1. Direct File MRU (non-LiveId fallback).
            let direct = format!(r"{}\File MRU", base);
            if hkcu_subkey_exists(&direct) {
                roots.push(MruRoot {
                    product,
                    version,
                    subkey: direct,
                });
            }

            // 2. User MRU\LiveId_*\File MRU. Enumerate User MRU subkeys.
            let user_mru = format!(r"{}\User MRU", base);
            let user_mru_wide: Vec<u16> =
                user_mru.encode_utf16().chain(std::iter::once(0)).collect();
            let mut hkey = HKEY::default();
            let open = unsafe {
                RegOpenKeyExW(
                    HKEY_CURRENT_USER,
                    PCWSTR::from_raw(user_mru_wide.as_ptr()),
                    0,
                    KEY_READ,
                    &mut hkey,
                )
            };
            if open.is_err() {
                continue;
            }

            // Enumerate subkeys of User MRU. Each is typically named
            // "LiveId_<hex>" or "AD_<guid>"; we accept anything and let
            // the per-key open below filter empties.
            let mut idx: u32 = 0;
            loop {
                let mut name_buf = [0u16; 256];
                let mut name_len: u32 = name_buf.len() as u32;
                let rc = unsafe {
                    RegEnumKeyExW(
                        hkey,
                        idx,
                        windows::core::PWSTR::from_raw(name_buf.as_mut_ptr()),
                        &mut name_len,
                        None,
                        windows::core::PWSTR::null(),
                        None,
                        None,
                    )
                };
                if rc.is_err() {
                    break;
                }
                let name = String::from_utf16_lossy(&name_buf[..name_len as usize]);
                let candidate = format!(r"{}\{}\File MRU", user_mru, name);
                if hkcu_subkey_exists(&candidate) {
                    roots.push(MruRoot {
                        product,
                        version,
                        subkey: candidate,
                    });
                }
                idx += 1;
            }

            unsafe {
                let _ = RegCloseKey(hkey);
            }
        }
    }

    debug!(count = roots.len(), "office_activity: enumerated MRU roots");
    roots
}

#[cfg(not(target_os = "windows"))]
fn enumerate_mru_roots() -> Vec<MruRoot> {
    Vec::new()
}

#[cfg(target_os = "windows")]
fn hkcu_subkey_exists(subkey: &str) -> bool {
    use windows::core::PCWSTR;
    use windows::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, HKEY, HKEY_CURRENT_USER, KEY_READ,
    };

    let wide: Vec<u16> = subkey.encode_utf16().chain(std::iter::once(0)).collect();
    let mut hkey = HKEY::default();
    let rc = unsafe {
        RegOpenKeyExW(
            HKEY_CURRENT_USER,
            PCWSTR::from_raw(wide.as_ptr()),
            0,
            KEY_READ,
            &mut hkey,
        )
    };
    if rc.is_ok() {
        unsafe {
            let _ = RegCloseKey(hkey);
        }
        true
    } else {
        false
    }
}

/// Reads every `REG_SZ` value in the given HKCU subkey, returning a sorted
/// map keyed by value name (so iteration order is deterministic across
/// runs, which matters for test snapshots).
#[cfg(target_os = "windows")]
fn read_mru_values(subkey: &str) -> BTreeMap<String, String> {
    use windows::core::{PCWSTR, PWSTR};
    use windows::Win32::System::Registry::{
        RegCloseKey, RegEnumValueW, RegOpenKeyExW, HKEY, HKEY_CURRENT_USER, KEY_READ, REG_SZ,
        REG_VALUE_TYPE,
    };

    let mut out: BTreeMap<String, String> = BTreeMap::new();

    let wide: Vec<u16> = subkey.encode_utf16().chain(std::iter::once(0)).collect();
    let mut hkey = HKEY::default();
    let open = unsafe {
        RegOpenKeyExW(
            HKEY_CURRENT_USER,
            PCWSTR::from_raw(wide.as_ptr()),
            0,
            KEY_READ,
            &mut hkey,
        )
    };
    if open.is_err() {
        return out;
    }

    let mut idx: u32 = 0;
    loop {
        let mut name_buf = [0u16; 16_384];
        let mut name_len: u32 = name_buf.len() as u32;
        // Office MRU values cap around 1 KB but we provision generously.
        let mut data_buf = vec![0u8; 8 * 1024];
        let mut data_len: u32 = data_buf.len() as u32;
        // Type is returned as a raw DWORD pointer; we wrap into REG_VALUE_TYPE
        // after the call.
        let mut value_type_raw: u32 = 0;

        let rc = unsafe {
            RegEnumValueW(
                hkey,
                idx,
                PWSTR::from_raw(name_buf.as_mut_ptr()),
                &mut name_len,
                None,
                Some(&mut value_type_raw),
                Some(data_buf.as_mut_ptr()),
                Some(&mut data_len),
            )
        };
        if rc.is_err() {
            break;
        }
        idx += 1;

        if REG_VALUE_TYPE(value_type_raw) != REG_SZ {
            continue;
        }

        let name = String::from_utf16_lossy(&name_buf[..name_len as usize]);
        // Only process Item N values — Office stores other metadata
        // (Max Display, Max Pinned) under the same key.
        if !name.starts_with("Item ") {
            continue;
        }

        // Convert the data bytes (UTF-16LE, possibly NUL-terminated) to String.
        let len_u16 = (data_len as usize) / 2;
        if len_u16 == 0 {
            continue;
        }
        let u16_slice: Vec<u16> = data_buf[..len_u16 * 2]
            .chunks_exact(2)
            .map(|c| u16::from_le_bytes([c[0], c[1]]))
            .collect();
        let trimmed: Vec<u16> = u16_slice.into_iter().take_while(|&c| c != 0).collect();
        let value = String::from_utf16_lossy(&trimmed);
        if value.is_empty() {
            continue;
        }
        out.insert(name, value);
    }

    unsafe {
        let _ = RegCloseKey(hkey);
    }
    out
}

#[cfg(not(target_os = "windows"))]
fn read_mru_values(_subkey: &str) -> BTreeMap<String, String> {
    BTreeMap::new()
}

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// Office recent-files MRU collector.
#[derive(Default)]
pub struct OfficeActivityCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl OfficeActivityCollector {
    /// Creates a new [`OfficeActivityCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for OfficeActivityCollector {
    fn name(&self) -> &'static str {
        "office_activity"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["office.recent_file_opened"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            let mut state = load_state();
            // Flag captured at task start. `baselined=false` means the next
            // (i.e. first) tick should populate `state.seen` without emitting.
            info!(
                baselined = state.baselined,
                seen = state.seen.len(),
                "office_activity: started"
            );

            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        let baseline = !state.baselined;
                        match poll_once(&ctx, &mut state, baseline, &events, &drops) {
                            Ok(emitted) => {
                                healthy.store(true, Ordering::Relaxed);
                                if baseline {
                                    state.baselined = true;
                                    info!(
                                        baseline_count = state.seen.len(),
                                        "office_activity: baseline complete"
                                    );
                                } else if emitted > 0 {
                                    debug!(emitted, "office_activity: poll cycle emitted");
                                }
                                save_state(&state);
                            }
                            Err(e) => {
                                warn!(error = %e, "office_activity: poll error");
                                healthy.store(false, Ordering::Relaxed);
                            }
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("office_activity: stop requested");
                        break;
                    }
                }
            }
        });

        // Default to healthy at registration time. A missing Office install
        // is a perfectly valid state — we report healthy with zero events.
        self.healthy.store(true, Ordering::Relaxed);
        Ok(CollectorHandle {
            name: self.name(),
            task,
            stop_tx,
        })
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
// Poll loop
// ──────────────────────────────────────────────────────────────────────────────

/// Runs one poll cycle. When `baseline` is true the cursor is updated but no
/// events are emitted (used on the first run to avoid a flood of historical
/// MRU entries). Returns the number of events successfully enqueued.
fn poll_once(
    ctx: &CollectorCtx,
    state: &mut CursorState,
    baseline: bool,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) -> Result<usize> {
    let roots = enumerate_mru_roots();
    if roots.is_empty() {
        debug!("office_activity: no Office MRU roots present");
        return Ok(0);
    }

    // Collect every (root, parsed entry) pair across all roots so we can
    // sort by recency and apply the per-cycle cap globally rather than
    // per-root (a SOC-relevant document is most likely the most recent
    // overall, regardless of which Office app opened it).
    let mut candidates: Vec<(MruRoot, MruEntry)> = Vec::new();
    for root in roots {
        let values = read_mru_values(&root.subkey);
        for (_name, raw) in values {
            let Some(entry) = parse_mru_value(&raw) else {
                continue;
            };
            if state.contains(root.product, root.version, entry.last_opened) {
                continue;
            }
            candidates.push((root.clone(), entry));
        }
    }

    // Newest first.
    candidates.sort_by(|a, b| b.1.last_opened.cmp(&a.1.last_opened));

    let total_new = candidates.len();
    let truncated = total_new > MAX_EVENTS_PER_POLL;
    if truncated {
        warn!(
            total_new,
            cap = MAX_EVENTS_PER_POLL,
            "office_activity: backlog exceeds cap — emitting newest only, advancing cursor past rest"
        );
    }

    let mut emitted = 0usize;
    for (i, (root, entry)) in candidates.into_iter().enumerate() {
        // Always advance the cursor so we don't loop forever on a single
        // pathological entry.
        state.insert(root.product, root.version, entry.last_opened);

        if baseline {
            continue;
        }
        if i >= MAX_EVENTS_PER_POLL {
            continue;
        }
        if emit_event(ctx, &root, &entry, events, drops) {
            emitted += 1;
        }
    }

    Ok(emitted)
}

fn emit_event(
    ctx: &CollectorCtx,
    root: &MruRoot,
    entry: &MruEntry,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) -> bool {
    let payload = build_payload(root, entry);
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::OfficeRecentFileOpened.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
            true
        }
        Err(e) => {
            error!(error = %e, "office_activity: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
            false
        }
    }
}

/// Builds the JSON payload by hand to avoid pulling in `serde_json` for one
/// struct. Field order matches the brief.
fn build_payload(root: &MruRoot, entry: &MruEntry) -> String {
    // Use serde_json::to_string on a small struct so escaping (backslashes,
    // unicode, embedded quotes) is correct on all paths.
    #[derive(Serialize)]
    struct Payload<'a> {
        product: &'a str,
        file_path: String,
        last_opened: String,
        office_version: &'a str,
    }
    let p = Payload {
        product: root.product.as_str(),
        file_path: entry.file_path.to_string_lossy().into_owned(),
        last_opened: filetime_to_rfc3339(entry.last_opened),
        office_version: root.version,
    };
    serde_json::to_string(&p)
        .unwrap_or_else(|_| "{\"product\":\"unknown\",\"error\":\"serialise\"}".to_string())
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests (platform-agnostic — no registry access)
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_mru_value_typical_word_entry() {
        // Real-world Office 365 Word MRU value shape.
        let raw = "[F00000000][T01D8FA12345678AB][O01D8FA12345600AB]*C:\\Users\\kartal\\Documents\\report.docx";
        let entry = parse_mru_value(raw).expect("must parse");
        assert_eq!(entry.last_opened, 0x01D8_FA12_3456_78AB);
        assert_eq!(
            entry.file_path,
            PathBuf::from("C:\\Users\\kartal\\Documents\\report.docx")
        );
    }

    #[test]
    fn parse_mru_value_unc_path_with_embedded_asterisk_uses_rfind() {
        // UNC share names can legally contain '*'; rfind ensures we still
        // split off the trailing path component correctly.
        let raw = "[F00000000][T01D8FA0000000001][O01D8FA0000000001]*\\\\fileserver\\share*odd\\book.xlsx";
        let entry = parse_mru_value(raw).expect("must parse");
        assert_eq!(entry.last_opened, 0x01D8_FA00_0000_0001);
        assert_eq!(entry.file_path.to_string_lossy(), "\\\\fileserver\\share*odd\\book.xlsx");
    }

    #[test]
    fn parse_mru_value_rejects_missing_t_group() {
        let raw = "[F00000000]*C:\\foo.docx";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_rejects_zero_filetime() {
        let raw = "[F00000000][T0000000000000000][O00]*C:\\foo.docx";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_rejects_missing_path_separator() {
        let raw = "[F00000000][T01D8FA12345678AB][O01D8FA12345600AB]";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_rejects_empty_path() {
        let raw = "[F00000000][T01D8FA12345678AB][O01D8FA12345600AB]*";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_rejects_non_hex_filetime() {
        let raw = "[F00000000][TXYZ12345678ABCD][O00]*C:\\foo.docx";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_rejects_oversized_filetime_hex() {
        // 17 hex digits — overflows u64 representation, must reject.
        let raw = "[F00000000][T11111111111111111][O00]*C:\\foo.docx";
        assert!(parse_mru_value(raw).is_none());
    }

    #[test]
    fn parse_mru_value_truncates_oversized_path() {
        let long = "C:\\".to_string() + &"a".repeat(2 * MAX_PATH_BYTES);
        let raw = format!("[F00000000][T01D8FA12345678AB][O00]*{long}");
        let entry = parse_mru_value(&raw).expect("must parse");
        assert!(entry.file_path.to_string_lossy().len() <= MAX_PATH_BYTES);
    }

    #[test]
    fn filetime_to_rfc3339_known_value() {
        // 2021-01-01T00:00:00Z = unix 1_609_459_200 = filetime
        // (1_609_459_200 + 11_644_473_600) * 10_000_000.
        let ft = (1_609_459_200_u64 + 11_644_473_600_u64) * 10_000_000;
        assert_eq!(filetime_to_rfc3339(ft), "2021-01-01T00:00:00Z");
    }

    #[test]
    fn filetime_to_rfc3339_unix_epoch() {
        // Exactly the unix epoch.
        let ft = 11_644_473_600_u64 * 10_000_000;
        assert_eq!(filetime_to_rfc3339(ft), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn filetime_to_rfc3339_pre_unix_clamps_to_epoch() {
        // 1601-01-01 itself.
        assert_eq!(filetime_to_rfc3339(1), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn filetime_to_rfc3339_leap_year_boundary() {
        // 2024-02-29T12:00:00Z = unix 1_709_208_000 (leap year sanity).
        let ft = (1_709_208_000_u64 + 11_644_473_600_u64) * 10_000_000;
        assert_eq!(filetime_to_rfc3339(ft), "2024-02-29T12:00:00Z");
    }

    #[test]
    fn filetime_to_rfc3339_recent() {
        // 2026-04-13T10:34:22Z = unix 1_776_076_462.
        let ft = (1_776_076_462_u64 + 11_644_473_600_u64) * 10_000_000;
        assert_eq!(filetime_to_rfc3339(ft), "2026-04-13T10:34:22Z");
    }

    #[test]
    fn cursor_roundtrip() {
        let mut s = CursorState::default();
        s.insert(Product::Word, "16.0", 0x01D8_FA12_3456_78AB);
        s.insert(Product::Excel, "16.0", 0x01D8_FA12_3456_78CD);
        s.baselined = true;

        let bytes = serde_json::to_vec(&s).unwrap();
        let back: CursorState = serde_json::from_slice(&bytes).unwrap();

        assert!(back.baselined);
        assert!(back.contains(Product::Word, "16.0", 0x01D8_FA12_3456_78AB));
        assert!(back.contains(Product::Excel, "16.0", 0x01D8_FA12_3456_78CD));
        assert!(!back.contains(Product::PowerPoint, "16.0", 0x01D8_FA12_3456_78AB));
        // Cross-version should not collide.
        assert!(!back.contains(Product::Word, "15.0", 0x01D8_FA12_3456_78AB));
    }

    #[test]
    fn cursor_key_is_stable() {
        // Keys are content-addressed by hex and must not depend on
        // hash randomness. Lower-case hex, zero-padded to 16 digits.
        let k1 = CursorState::key(Product::Word, "16.0", 0x01D8_FA12_3456_78AB);
        let k2 = CursorState::key(Product::Word, "16.0", 0x01D8_FA12_3456_78AB);
        assert_eq!(k1, k2);
        assert_eq!(k1, "word_16.0_01d8fa12345678ab");
        assert_eq!(
            CursorState::key(Product::Excel, "15.0", 1),
            "excel_15.0_0000000000000001"
        );
    }

    #[test]
    fn build_payload_shape() {
        let root = MruRoot {
            product: Product::Word,
            version: "16.0",
            subkey: String::new(),
        };
        let entry = MruEntry {
            last_opened: (1_776_076_462_u64 + 11_644_473_600_u64) * 10_000_000,
            file_path: PathBuf::from("C:\\Users\\kartal\\rapor.docx"),
        };
        let json = build_payload(&root, &entry);
        // Spot-check fields rather than full string match (escape rules
        // around backslashes can vary across serde versions).
        assert!(json.contains(r#""product":"word""#));
        assert!(json.contains(r#""office_version":"16.0""#));
        assert!(json.contains(r#""last_opened":"2026-04-13T10:34:22Z""#));
        assert!(json.contains("rapor.docx"));
    }
}
