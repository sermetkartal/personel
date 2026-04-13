//! Outlook email metadata collector (Faz 2 Wave 2 — roadmap item #12).
//!
//! # KVKK boundary
//!
//! Personel's locked design (`CLAUDE.md` §0, "Tasarım kararları" #2):
//!
//! > **Email: sender/recipient/subject/timestamp, NO body.**
//!
//! This collector NEVER reads, parses, decodes, decompresses, decrypts, or
//! enqueues any email body, HTML body, attachment, or full RFC 5322 header
//! block. The Phase 1 scaffold below operates exclusively on PST/OST
//! **file-level** metadata (path, size, modification time). It physically
//! cannot leak email content because it never opens the binary store at
//! all. The Phase 2 MAPI path described in the TODO below is REQUIRED to
//! preserve the same boundary at field-extraction time — see the explicit
//! field allow-list in the Phase 2 plan.
//!
//! # Phase 1 (this scaffold)
//!
//! Outlook stores its mail in `.pst` (archived) and `.ost` (cached
//! Exchange/M365 mailbox) files under `%LOCALAPPDATA%\Microsoft\Outlook\`
//! and `%USERPROFILE%\Documents\Outlook Files\`. We poll these files every
//! 2 minutes, compare against the previous size recorded in
//! `%PROGRAMDATA%\Personel\agent\email_stores.state`, and emit one
//! `email.metadata_observed` event per store that grew. The event is a
//! pure activity heartbeat — it tells the backend "N bytes of email
//! activity happened on store S during window W" without exposing any
//! message-level fact.
//!
//! Stores smaller than 1 MB are skipped (newly-created empty .ost files).
//! At most one event per poll cycle per store is emitted (rate-limit).
//!
//! # Phase 2 plan (TODO for the next agent picking this up)
//!
//! Two viable real-message paths exist; we deliberately scaffolded so a
//! future agent can plug either one in WITHOUT reshaping the surrounding
//! code:
//!
//! 1. **MAPI COM event subscription** (preferred for `.ost` cached mode):
//!    - Add a `windows` crate feature gate for `Win32_System_Com` and
//!      `Win32_System_Com_Marshal`, then bind to the Outlook Object Model
//!      (CLSID `0006F03A-0000-0000-C000-000000000046`,
//!      ProgID `Outlook.Application`).
//!    - From the `Application` interface, fetch
//!      `Application.GetNamespace("MAPI")`, then iterate
//!      `Namespace.Stores`. For each `Store`, attach a sink to
//!      `Items.ItemAdd` and `Items.ItemChange` on the
//!      `Inbox` / `Sent Items` default folders (use
//!      `Store.GetDefaultFolder(olFolderInbox=6, olFolderSentMail=5)`).
//!    - In the event handler, read ONLY these properties off the
//!      `MailItem` (KVKK boundary — the allow-list below is exhaustive):
//!         * `SenderEmailAddress`              → `sender_email`
//!         * `Recipients` (loop, `.Address`)   → `recipient_emails: [..]`
//!         * `Subject` (truncate to 256 bytes) → `subject`
//!         * `SentOn` / `ReceivedTime`         → `sent_at` (RFC3339)
//!         * folder name → derive `direction` ("inbound" if Inbox,
//!           "outbound" if Sent Items)
//!    - **NEVER** touch `Body`, `HTMLBody`, `Attachments`,
//!      `PropertyAccessor.GetProperty(PR_TRANSPORT_MESSAGE_HEADERS)`,
//!      `ReplyTo`, `CC`, or `BCC`. A CI lint should grep this file for
//!      those identifiers and fail the build if present outside the
//!      "forbidden" comment block.
//!    - COM apartment: spawn a dedicated OS thread, init STA via
//!      `CoInitializeEx(COINIT_APARTMENTTHREADED)`, run a message pump
//!      with `GetMessage` / `DispatchMessage` so DCOM event dispatch
//!      works. Do NOT use this from a tokio worker thread directly.
//!    - Outlook must be running. If not (`CoCreateInstance` returns
//!      `REGDB_E_CLASSNOTREG` or the process isn't found), fall back to
//!      this scaffold's PST/OST size-delta polling — the two modes are
//!      complementary, not exclusive.
//!
//! 2. **Offline PST parsing via `libpst`** (fallback for `.pst` archives
//!    when Outlook isn't running):
//!    - Vendor `libpst` (LGPL, C library) or use the pure-Rust `pst`
//!      crate when it stabilises. License vetting REQUIRED before adding.
//!    - Walk the PST node tree and read message envelopes only
//!      (PR_SENDER_EMAIL_ADDRESS_W = 0x0C1F001F,
//!      PR_DISPLAY_TO_W = 0x0E04001F,
//!      PR_SUBJECT_W = 0x0037001F,
//!      PR_MESSAGE_DELIVERY_TIME = 0x0E060040). Skip PR_BODY (0x1000001F)
//!      and PR_ATTACH_DATA_BIN (0x37010102) entirely.
//!    - Only re-process messages whose internal PST modification rank is
//!      newer than the per-store cursor stored alongside `last_size` in
//!      `email_stores.state` (extend the JSON map to `{size, last_msg_id}`).
//!
//! Both paths must keep emitting the Phase 1 heartbeat events alongside
//! the new per-message events, so the coverage signal "email activity
//! occurred" remains robust even if MAPI dispatch silently drops events.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Poll Outlook stores every 2 minutes (KVKK-friendly low frequency; mail
/// activity is bursty enough that a finer cadence would not improve signal).
const POLL_INTERVAL: Duration = Duration::from_secs(120);

/// Skip stores smaller than 1 MB — these are newly-provisioned empty .ost
/// files that Outlook creates on first sign-in.
const MIN_STORE_BYTES: u64 = 1024 * 1024;

/// Outlook email metadata collector.
///
/// See module-level docs for the strict KVKK boundary this collector
/// upholds.
#[derive(Default)]
pub struct EmailMetadataCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl EmailMetadataCollector {
    /// Creates a new [`EmailMetadataCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for EmailMetadataCollector {
    fn name(&self) -> &'static str {
        "email_metadata"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["email.metadata_observed"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            // Load persistent store state. Failures here are non-fatal —
            // we just start with an empty map and treat the first poll as
            // a baseline (no events on the very first cycle).
            let state_path = state_file_path();
            let mut state = load_state(&state_path).unwrap_or_default();
            let mut first_cycle = true;

            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            info!("email_metadata collector: started (Phase 1 PST/OST size-delta scaffold)");

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        healthy.store(true, Ordering::Relaxed);
                        let stores = discover_outlook_stores();

                        if stores.is_empty() {
                            debug!("email_metadata: no Outlook PST/OST stores found");
                            continue;
                        }

                        let mut updated = false;
                        for store in stores {
                            let key = store.to_string_lossy().into_owned();
                            let last_size = state.get(&key).copied();
                            let Some(delta) = probe_store(&store, last_size) else {
                                continue;
                            };

                            // First cycle: just record baseline, do NOT
                            // emit phantom events for stores that grew
                            // before the agent existed.
                            if !first_cycle && delta.size_delta_bytes > 0 {
                                emit_event(&ctx, &delta, &events, &drops);
                            }

                            state.insert(key, delta.current_size_bytes);
                            updated = true;
                        }

                        if updated {
                            if let Err(e) = save_state(&state_path, &state) {
                                warn!(error = %e, "email_metadata: state save failed");
                            }
                        }

                        first_cycle = false;
                    }
                    _ = &mut stop_rx => {
                        info!("email_metadata collector: stop requested");
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
// Store discovery
// ──────────────────────────────────────────────────────────────────────────────

/// Probed delta for a single PST/OST file at the current poll cycle.
struct StoreDelta {
    path: PathBuf,
    current_size_bytes: u64,
    size_delta_bytes: i64,
}

/// Locates Outlook PST/OST stores in the two canonical locations.
///
/// Returns an empty vector on non-Windows hosts, missing directories, or
/// permission errors. Never panics; never propagates I/O errors.
fn discover_outlook_stores() -> Vec<PathBuf> {
    let mut found = Vec::new();
    for dir in candidate_dirs() {
        scan_dir(&dir, &mut found);
    }
    found
}

fn candidate_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();

    if let Ok(local_app_data) = std::env::var("LOCALAPPDATA") {
        dirs.push(PathBuf::from(local_app_data).join("Microsoft").join("Outlook"));
    }
    if let Ok(user_profile) = std::env::var("USERPROFILE") {
        dirs.push(PathBuf::from(user_profile).join("Documents").join("Outlook Files"));
    }

    dirs
}

fn scan_dir(dir: &Path, out: &mut Vec<PathBuf>) {
    let Ok(read_dir) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in read_dir.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let Some(ext) = path.extension().and_then(|s| s.to_str()) else {
            continue;
        };
        let lower = ext.to_ascii_lowercase();
        if lower == "pst" || lower == "ost" {
            out.push(path);
        }
    }
}

/// Reads the current size of a store and computes the delta vs. the last
/// recorded size. Returns `None` if the file is unreadable, smaller than
/// the [`MIN_STORE_BYTES`] floor, or unchanged.
fn probe_store(path: &Path, last_size: Option<u64>) -> Option<StoreDelta> {
    let meta = std::fs::metadata(path).ok()?;
    let current = meta.len();
    if current < MIN_STORE_BYTES {
        return None;
    }

    let delta = match last_size {
        Some(prev) if prev == current => 0_i64,
        Some(prev) => i64::try_from(current).unwrap_or(i64::MAX)
            - i64::try_from(prev).unwrap_or(0),
        None => 0, // baseline cycle
    };

    Some(StoreDelta {
        path: path.to_path_buf(),
        current_size_bytes: current,
        size_delta_bytes: delta,
    })
}

// ──────────────────────────────────────────────────────────────────────────────
// State persistence
// ──────────────────────────────────────────────────────────────────────────────

fn state_file_path() -> PathBuf {
    let base = std::env::var("PROGRAMDATA")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from("C:\\ProgramData"));
    base.join("Personel").join("agent").join("email_stores.state")
}

fn load_state(path: &Path) -> Option<BTreeMap<String, u64>> {
    let bytes = std::fs::read(path).ok()?;
    serde_json::from_slice(&bytes).ok()
}

fn save_state(path: &Path, state: &BTreeMap<String, u64>) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let bytes = serde_json::to_vec(state)
        .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;
    std::fs::write(path, bytes)
}

// ──────────────────────────────────────────────────────────────────────────────
// Event emission
// ──────────────────────────────────────────────────────────────────────────────

fn emit_event(
    ctx: &CollectorCtx,
    delta: &StoreDelta,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();

    // RFC3339-ish detected_at derived from wall clock seconds. We avoid
    // pulling in `chrono` for a single timestamp string — the backend
    // already has the `now` nanos field on the envelope; this string is
    // purely for human-readable audit context inside the JSON payload.
    let detected_at = system_time_to_rfc3339_seconds(SystemTime::now());

    let payload = format!(
        r#"{{"provider":"outlook","store_path":{store_path},"store_size_bytes":{size},"size_delta_bytes":{delta},"detected_at":"{ts}"}}"#,
        store_path = json_string(&delta.path.to_string_lossy()),
        size = delta.current_size_bytes,
        delta = delta.size_delta_bytes,
        ts = detected_at,
    );

    match ctx.queue.enqueue(
        &id,
        EventKind::EmailMetadataObserved.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
            debug!(
                store = %delta.path.display(),
                delta = delta.size_delta_bytes,
                "email_metadata: enqueued size-delta heartbeat"
            );
        }
        Err(e) => {
            error!(error = %e, "email_metadata: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}

/// Minimal JSON string escaper — wraps the input in quotes and escapes the
/// six characters that are illegal raw inside a JSON string. We avoid
/// `serde_json::to_string` here because the rest of this file's payload
/// builder is also `format!`-based for parity with `window_title.rs`.
fn json_string(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for ch in s.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

/// Formats a `SystemTime` as `YYYY-MM-DDTHH:MM:SSZ` (UTC, second precision).
/// Pure stdlib — avoids pulling `chrono` into this scaffold.
fn system_time_to_rfc3339_seconds(t: SystemTime) -> String {
    let secs = t.duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    let (y, mo, d, h, mi, s) = unix_to_civil(secs);
    format!("{y:04}-{mo:02}-{d:02}T{h:02}:{mi:02}:{s:02}Z")
}

/// Howard Hinnant's days-from-civil algorithm (public domain).
/// Converts a Unix timestamp (UTC seconds) to (Y, M, D, h, m, s).
fn unix_to_civil(secs: u64) -> (i32, u32, u32, u32, u32, u32) {
    let days = (secs / 86_400) as i64;
    let sod = secs % 86_400;
    let h = (sod / 3600) as u32;
    let mi = ((sod % 3600) / 60) as u32;
    let s = (sod % 60) as u32;

    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    let y = if m <= 2 { y + 1 } else { y };

    (y as i32, m, d, h, mi, s)
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn json_string_escapes_quotes_and_backslash() {
        assert_eq!(json_string("a\"b\\c"), r#""a\"b\\c""#);
    }

    #[test]
    fn json_string_escapes_control_chars() {
        assert_eq!(json_string("\n\r\t"), r#""\n\r\t""#);
    }

    #[test]
    fn unix_to_civil_known_epoch() {
        // 2026-04-13 00:00:00 UTC = 1_776_124_800
        let (y, mo, d, h, mi, s) = unix_to_civil(1_776_124_800);
        assert_eq!((y, mo, d, h, mi, s), (2026, 4, 13, 0, 0, 0));
    }

    #[test]
    fn rfc3339_format_shape() {
        let out = system_time_to_rfc3339_seconds(UNIX_EPOCH + Duration::from_secs(1_776_124_800));
        assert_eq!(out, "2026-04-13T00:00:00Z");
    }

    #[test]
    fn probe_store_below_floor_returns_none() {
        // Use a tempfile-equivalent: write a small file in the target dir.
        let dir = std::env::temp_dir();
        let path = dir.join("personel_email_test_small.ost");
        std::fs::write(&path, b"tiny").unwrap();
        assert!(probe_store(&path, None).is_none());
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn probe_store_baseline_returns_zero_delta() {
        let dir = std::env::temp_dir();
        let path = dir.join("personel_email_test_baseline.ost");
        let payload = vec![0u8; (MIN_STORE_BYTES + 1) as usize];
        std::fs::write(&path, &payload).unwrap();
        let d = probe_store(&path, None).expect("probe should succeed");
        assert_eq!(d.size_delta_bytes, 0);
        assert_eq!(d.current_size_bytes, payload.len() as u64);
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn probe_store_growth_returns_positive_delta() {
        let dir = std::env::temp_dir();
        let path = dir.join("personel_email_test_grow.ost");
        let payload = vec![0u8; (MIN_STORE_BYTES + 1024) as usize];
        std::fs::write(&path, &payload).unwrap();
        let d = probe_store(&path, Some(MIN_STORE_BYTES)).expect("probe should succeed");
        assert_eq!(d.size_delta_bytes, 1024);
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn state_roundtrip() {
        let dir = std::env::temp_dir();
        let path = dir.join("personel_email_test_state.json");
        let mut m = BTreeMap::new();
        m.insert("C:\\foo.ost".to_string(), 12345_u64);
        m.insert("C:\\bar.pst".to_string(), 67890_u64);
        save_state(&path, &m).unwrap();
        let loaded = load_state(&path).expect("load");
        assert_eq!(loaded.get("C:\\foo.ost"), Some(&12345));
        assert_eq!(loaded.get("C:\\bar.pst"), Some(&67890));
        let _ = std::fs::remove_file(&path);
    }
}
