//! Chromium-family browser history collector (Chrome, Edge, Brave).
//!
//! Polls each installed Chromium-family browser's per-profile `History`
//! SQLite database every 5 minutes, joins the `visits` and `urls` tables,
//! and emits one `browser.history_visited` event per *new* visit since the
//! last poll. Cursors are persisted per `(browser, profile)` pair in
//! `%PROGRAMDATA%\Personel\agent\browser_history.state` so we never re-emit
//! the same visit across agent restarts.
//!
//! # KVKK note (kişisel veri kapsamı)
//!
//! This collector deliberately captures **only** URL + page title +
//! visit timestamp + transition type. It NEVER reads cookies, saved
//! passwords, autofill / form data, bookmarks, download manifests, or
//! POST bodies — they live in different tables of the same database file
//! and our SQL query is hand-bounded to `urls` + `visits`.
//!
//! Per Faz 2 design lock (CLAUDE.md §0): browser history is "visited URL
//! and title only, no bookmark / cookie / password". This is an
//! aydınlatma metni-disclosed monitoring scope — a wider crawl would
//! breach KVKK m.5 amaçla bağlı (purpose limitation) and require a fresh
//! DPIA + worker notice cycle.
//!
//! # Locked database handling
//!
//! Chromium holds an exclusive lock on `History` while the browser is
//! running, so a direct `rusqlite::Connection::open` would race / fail.
//! We side-step the lock by `std::fs::copy`'ing the database to a temp
//! file in `%TEMP%\personel-bh-<n>.db` first and opening *that* read-only.
//! This is the same approach used by every forensic browser history tool
//! and is officially supported by the SQLite documentation as long as the
//! source DB is not actively being checkpointed during the copy.

use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use rusqlite::{Connection, OpenFlags};
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

/// Polling cadence — once every five minutes, matching the Faz 2 brief.
const POLL_INTERVAL: Duration = Duration::from_secs(300);

/// Hard cap on events emitted per single poll cycle. If a profile produced
/// more than this many new visits since the last cursor (heavy browsing
/// session, long agent downtime, forensic dump) we emit the most recent
/// `MAX_EVENTS_PER_POLL` and advance the cursor past the rest. The skip is
/// logged at warn so SOC operators can correlate gaps.
const MAX_EVENTS_PER_POLL: usize = 500;

/// Maximum URL length we will record. Longer URLs are truncated. A 2 KB
/// ceiling matches IIS/Chrome practical limits and prevents 1 MB Base64
/// data URIs from blowing the queue.
const MAX_URL_BYTES: usize = 2048;

/// Maximum page title length. Titles in the wild can be megabytes (single
/// page apps that put their entire DOM into `document.title`).
const MAX_TITLE_BYTES: usize = 256;

/// Minimum URL length to consider — anything shorter cannot encode a real
/// scheme + host and is almost certainly a probe / about: leftover.
const MIN_URL_LEN: usize = 8;

/// WebKit epoch offset: WebKit timestamps are microseconds since
/// 1601-01-01T00:00:00Z; Unix is seconds since 1970-01-01T00:00:00Z. The
/// gap is exactly 11_644_473_600 seconds.
const WEBKIT_EPOCH_OFFSET_SECS: i64 = 11_644_473_600;

// ──────────────────────────────────────────────────────────────────────────────
// Browser enum
// ──────────────────────────────────────────────────────────────────────────────

/// Supported Chromium-family browsers. Adding a new one is a one-line job
/// in [`Browser::installed_root`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Browser {
    /// Google Chrome stable channel.
    Chrome,
    /// Microsoft Edge (Chromium-based).
    Edge,
    /// Brave Browser.
    Brave,
}

impl Browser {
    /// All browsers we attempt to discover at startup.
    const ALL: &'static [Browser] = &[Browser::Chrome, Browser::Edge, Browser::Brave];

    /// Short, stable identifier used in event payloads and the on-disk
    /// state file. Do NOT change without a state-file migration.
    fn as_str(self) -> &'static str {
        match self {
            Browser::Chrome => "chrome",
            Browser::Edge => "edge",
            Browser::Brave => "brave",
        }
    }

    /// Returns the path to the `User Data` root for this browser, given
    /// `%LOCALAPPDATA%`. Profiles are subdirectories of this root.
    fn installed_root(self, local_appdata: &Path) -> PathBuf {
        match self {
            Browser::Chrome => local_appdata.join("Google").join("Chrome").join("User Data"),
            Browser::Edge => local_appdata.join("Microsoft").join("Edge").join("User Data"),
            Browser::Brave => local_appdata
                .join("BraveSoftware")
                .join("Brave-Browser")
                .join("User Data"),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Visit record + state file
// ──────────────────────────────────────────────────────────────────────────────

/// A single row from the `visits` join `urls` query.
#[derive(Debug, Clone)]
struct Visit {
    url: String,
    title: String,
    /// Same instant as the source `visits.visit_time` row in Unix nanos.
    unix_nanos: i64,
    transition: i64,
    visit_count: i64,
}

/// Persisted state file. Keys are `"<browser>_<profile>"`, values are the
/// largest WebKit `visit_time` seen so far for that pair. We persist the
/// raw WebKit value (not Unix nanos) so the cursor can be compared cheaply
/// against `visits.visit_time` in the next SQL query without conversion.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct CursorState {
    #[serde(flatten)]
    cursors: HashMap<String, i64>,
}

impl CursorState {
    fn key(browser: Browser, profile: &str) -> String {
        format!("{}_{}", browser.as_str(), profile)
    }

    fn get(&self, browser: Browser, profile: &str) -> i64 {
        self.cursors.get(&Self::key(browser, profile)).copied().unwrap_or(0)
    }

    fn set(&mut self, browser: Browser, profile: &str, value: i64) {
        self.cursors.insert(Self::key(browser, profile), value);
    }
}

/// Returns the on-disk state file path under `%PROGRAMDATA%`. Falls back
/// to the system temp directory if `PROGRAMDATA` is unset (extremely
/// unusual on Windows; a sane default keeps tests + non-Windows dev
/// builds working without panicking).
fn state_file_path() -> PathBuf {
    let base = std::env::var("PROGRAMDATA")
        .map(PathBuf::from)
        .unwrap_or_else(|_| std::env::temp_dir());
    base.join("Personel").join("agent").join("browser_history.state")
}

fn load_state() -> CursorState {
    let path = state_file_path();
    match fs::read(&path) {
        Ok(bytes) => serde_json::from_slice::<CursorState>(&bytes).unwrap_or_else(|e| {
            warn!(
                error = %e,
                path = %path.display(),
                "browser_history: state file corrupt — starting from zero cursor"
            );
            CursorState::default()
        }),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => CursorState::default(),
        Err(e) => {
            warn!(
                error = %e,
                path = %path.display(),
                "browser_history: state file unreadable — starting from zero cursor"
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
                "browser_history: cannot create state dir — cursor will not persist"
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
                    "browser_history: state file write failed — cursor will not persist"
                );
            }
        }
        Err(e) => {
            // serde_json::to_vec_pretty on a HashMap<String,i64> realistically
            // never fails, but treat it as a soft warning anyway.
            warn!(error = %e, "browser_history: state file serialise failed");
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Profile discovery
// ──────────────────────────────────────────────────────────────────────────────

/// Enumerates every `(Browser, profile_db_path)` we should poll. A profile
/// is any directory under `User Data\` named `Default` or starting with
/// `Profile ` whose `History` file exists. Missing browsers / missing
/// `User Data` directories are silently skipped — most endpoints will
/// only have one or two of these installed.
fn discover_profiles() -> Vec<(Browser, String, PathBuf)> {
    let local_appdata = match std::env::var("LOCALAPPDATA") {
        Ok(v) => PathBuf::from(v),
        Err(_) => {
            debug!("browser_history: LOCALAPPDATA unset — no profiles to discover");
            return Vec::new();
        }
    };

    let mut out = Vec::new();
    for browser in Browser::ALL {
        let root = browser.installed_root(&local_appdata);
        let entries = match fs::read_dir(&root) {
            Ok(e) => e,
            Err(_) => continue, // browser not installed; perfectly fine
        };
        for entry in entries.flatten() {
            let Ok(file_type) = entry.file_type() else { continue };
            if !file_type.is_dir() {
                continue;
            }
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            let is_profile = name_str == "Default" || name_str.starts_with("Profile ");
            if !is_profile {
                continue;
            }
            let history_path = entry.path().join("History");
            if history_path.is_file() {
                out.push((*browser, name_str.into_owned(), history_path));
            }
        }
    }
    out
}

// ──────────────────────────────────────────────────────────────────────────────
// SQLite polling
// ──────────────────────────────────────────────────────────────────────────────

/// Converts a WebKit microsecond timestamp to Unix nanoseconds. Returns
/// 0 for timestamps before the Unix epoch (which would indicate a corrupt
/// history DB rather than a legitimate visit).
fn webkit_to_unix_nanos(webkit_us: i64) -> i64 {
    let unix_us = webkit_us.saturating_sub(WEBKIT_EPOCH_OFFSET_SECS * 1_000_000);
    if unix_us <= 0 {
        return 0;
    }
    unix_us.saturating_mul(1_000)
}

/// True for `chrome://`, `edge://`, `brave://`, `about:`, `file://` URLs
/// — these are local browser internals we deliberately skip.
fn is_internal_url(url: &str) -> bool {
    let lower = url.trim_start().to_ascii_lowercase();
    lower.starts_with("chrome://")
        || lower.starts_with("edge://")
        || lower.starts_with("brave://")
        || lower.starts_with("about:")
        || lower.starts_with("file://")
        || lower.starts_with("chrome-extension://")
        || lower.starts_with("edge-extension://")
        || lower.starts_with("devtools://")
}

/// Truncates `s` to at most `max` bytes without splitting a UTF-8 code
/// point. We walk back from `max` until we hit a char boundary.
fn truncate_utf8(s: &str, max: usize) -> String {
    if s.len() <= max {
        return s.to_string();
    }
    let mut end = max;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    s[..end].to_string()
}

/// Copy the locked `History` database to a temp file and return the
/// destination path. The caller is responsible for deleting it after the
/// SQLite connection is closed.
fn copy_locked_db(src: &Path) -> std::io::Result<PathBuf> {
    let mut dest = std::env::temp_dir();
    // Unique-per-process-per-source path: PID + nanos avoids two
    // concurrent collectors clobbering each other (defensive — there's
    // only one BrowserHistoryCollector instance, but cheap insurance).
    let tag = format!(
        "personel-bh-{}-{}.db",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0)
    );
    dest.push(tag);
    fs::copy(src, &dest)?;
    Ok(dest)
}

/// Polls one (browser, profile) database. Returns the new visits sorted
/// **ascending** by `visit_time` plus the new high-water cursor that
/// should be persisted on success.
fn poll_profile(
    browser: Browser,
    db_path: &Path,
    cursor: i64,
) -> std::result::Result<(Vec<Visit>, i64), String> {
    // Step 1: copy the locked DB to a safe temp file.
    let tmp = copy_locked_db(db_path).map_err(|e| format!("copy_locked_db: {e}"))?;

    // Use a guard so the temp file is removed even if the SQL query
    // panics or the connection drop ordering is surprising.
    struct TempFileGuard(PathBuf);
    impl Drop for TempFileGuard {
        fn drop(&mut self) {
            let _ = fs::remove_file(&self.0);
        }
    }
    let guard = TempFileGuard(tmp.clone());

    // Step 2: open it strictly read-only. We do NOT need a WAL or shared
    // cache here — the DB is a single-writer copy, so the simplest open
    // flags suffice.
    let conn = Connection::open_with_flags(
        &tmp,
        OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .map_err(|e| format!("sqlite open: {e}"))?;

    // Step 3: query visits + urls join above the cursor. We over-fetch
    // by 1 row past `MAX_EVENTS_PER_POLL` so the truncation branch can
    // detect "there were more than the cap".
    //
    // The SELECT list is hand-bounded to columns we are explicitly
    // allowed to read per KVKK. If a future Chrome version renames a
    // column, the query fails loudly at this line — not silently in a
    // downstream consumer.
    let sql = "
        SELECT
            urls.url,
            urls.title,
            urls.visit_count,
            visits.visit_time,
            visits.transition
        FROM visits
        INNER JOIN urls ON urls.id = visits.url
        WHERE visits.visit_time > ?1
        ORDER BY visits.visit_time ASC
        LIMIT ?2
    ";

    let cap = i64::try_from(MAX_EVENTS_PER_POLL + 1).unwrap_or(i64::MAX);
    let mut stmt = conn.prepare(sql).map_err(|e| format!("sqlite prepare: {e}"))?;
    let rows = stmt
        .query_map(rusqlite::params![cursor, cap], |row| {
            Ok((
                row.get::<_, String>(0)?,         // url
                row.get::<_, Option<String>>(1)?, // title
                row.get::<_, i64>(2)?,            // visit_count
                row.get::<_, i64>(3)?,            // visit_time (WebKit us)
                row.get::<_, i64>(4)?,            // transition
            ))
        })
        .map_err(|e| format!("sqlite query: {e}"))?;

    let mut visits = Vec::new();
    let mut max_seen = cursor;
    for row in rows {
        let (url, title, visit_count, webkit_us, transition) =
            row.map_err(|e| format!("sqlite row: {e}"))?;

        if webkit_us > max_seen {
            max_seen = webkit_us;
        }

        if url.len() < MIN_URL_LEN || is_internal_url(&url) {
            continue;
        }

        let url = truncate_utf8(&url, MAX_URL_BYTES);
        let title = truncate_utf8(title.as_deref().unwrap_or(""), MAX_TITLE_BYTES);
        let unix_nanos = webkit_to_unix_nanos(webkit_us);
        if unix_nanos == 0 {
            // Garbage row — historic visit_time before 1970 means the DB
            // is corrupt for that row. Skip it but still advance cursor.
            continue;
        }

        visits.push(Visit {
            url,
            title,
            unix_nanos,
            transition,
            visit_count,
        });
    }

    drop(stmt);
    drop(conn);
    drop(guard); // Removes the temp file.

    // Apply rate-limit. Because the query was ASC ordered we keep the
    // newest tail of the result set when over the cap.
    if visits.len() > MAX_EVENTS_PER_POLL {
        let skip = visits.len() - MAX_EVENTS_PER_POLL;
        warn!(
            browser = browser.as_str(),
            skip,
            cap = MAX_EVENTS_PER_POLL,
            "browser_history: per-poll cap exceeded — older visits dropped"
        );
        visits.drain(..skip);
    }

    Ok((visits, max_seen))
}

// ──────────────────────────────────────────────────────────────────────────────
// Event payload formatting
// ──────────────────────────────────────────────────────────────────────────────

/// Formats a Unix nanosecond timestamp as RFC3339 with microsecond
/// precision (matches the brief's example payload). Done by hand to
/// avoid pulling chrono into the collectors crate.
fn format_rfc3339_micros(unix_nanos: i64) -> String {
    // Use std `SystemTime` + naive arithmetic. We intentionally do not
    // panic on negative inputs (already filtered upstream).
    let secs = unix_nanos / 1_000_000_000;
    let nanos_part = (unix_nanos % 1_000_000_000) as u32;
    let micros = nanos_part / 1_000;

    // Civil time conversion. Algorithm from Howard Hinnant's "date"
    // paper, public domain.
    let z = secs.div_euclid(86_400) + 719_468;
    let secs_of_day = secs.rem_euclid(86_400);
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64; // [0, 146096]
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };

    let hour = secs_of_day / 3600;
    let minute = (secs_of_day % 3600) / 60;
    let second = secs_of_day % 60;

    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:06}Z",
        y, m, d, hour, minute, second, micros
    )
}

fn build_payload(browser: Browser, profile: &str, v: &Visit) -> String {
    // serde_json::to_string would also work, but we already format string
    // values inline elsewhere in this crate (see window_title.rs) and a
    // hand-built JSON keeps the dep surface unchanged. We *do* call into
    // serde_json::to_string for the URL + title to get correct escaping.
    let url_json = serde_json::to_string(&v.url).unwrap_or_else(|_| "\"\"".to_string());
    let title_json = serde_json::to_string(&v.title).unwrap_or_else(|_| "\"\"".to_string());
    let profile_json = serde_json::to_string(profile).unwrap_or_else(|_| "\"\"".to_string());
    let ts = format_rfc3339_micros(v.unix_nanos);

    format!(
        r#"{{"browser":"{}","profile":{},"url":{},"title":{},"visit_time":"{}","transition":{},"visit_count":{}}}"#,
        browser.as_str(),
        profile_json,
        url_json,
        title_json,
        ts,
        v.transition,
        v.visit_count,
    )
}

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// Chromium-family browser history polling collector.
#[derive(Default)]
pub struct BrowserHistoryCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl BrowserHistoryCollector {
    /// Creates a new [`BrowserHistoryCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for BrowserHistoryCollector {
    fn name(&self) -> &'static str {
        "browser_history"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["browser.history_visited"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            healthy.store(true, Ordering::Relaxed);
            info!("browser_history collector: started (poll = 5m)");

            // First tick fires immediately; subsequent ticks every 5m.
            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        let h = healthy.clone();
                        let ev = events.clone();
                        let dr = drops.clone();
                        let ctx_clone = ctx.clone();

                        // rusqlite + std::fs are blocking; offload.
                        let join = tokio::task::spawn_blocking(move || {
                            poll_all_profiles(&ctx_clone, &h, &ev, &dr);
                        }).await;

                        if let Err(e) = join {
                            error!(error = %e, "browser_history: blocking task panicked");
                            healthy.store(false, Ordering::Relaxed);
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("browser_history collector: stop requested");
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

/// Runs one full poll cycle across every discovered profile. Synchronous
/// (called from within `spawn_blocking`).
fn poll_all_profiles(
    ctx: &CollectorCtx,
    healthy: &Arc<AtomicBool>,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let profiles = discover_profiles();
    if profiles.is_empty() {
        debug!("browser_history: no Chromium profiles discovered this cycle");
        return;
    }

    let mut state = load_state();
    let mut any_progress = false;

    for (browser, profile, db_path) in profiles {
        let cursor = state.get(browser, &profile);
        match poll_profile(browser, &db_path, cursor) {
            Ok((visits, new_cursor)) => {
                debug!(
                    browser = browser.as_str(),
                    profile = %profile,
                    cursor,
                    new_cursor,
                    new_visits = visits.len(),
                    "browser_history: profile poll OK"
                );

                for v in &visits {
                    let payload = build_payload(browser, &profile, v);
                    enqueue(ctx, &payload, v.unix_nanos, events, drops);
                }

                if new_cursor > cursor {
                    state.set(browser, &profile, new_cursor);
                    any_progress = true;
                }
                healthy.store(true, Ordering::Relaxed);
            }
            Err(e) => {
                // Don't roll the cursor back — we'll retry next cycle.
                warn!(
                    browser = browser.as_str(),
                    profile = %profile,
                    db = %db_path.display(),
                    error = %e,
                    "browser_history: profile poll failed (will retry)"
                );
                // A single profile failing doesn't make the whole
                // collector unhealthy — treat it as transient.
            }
        }
    }

    if any_progress {
        save_state(&state);
    }
}

fn enqueue(
    ctx: &CollectorCtx,
    payload: &str,
    occurred_unix_nanos: i64,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::BrowserHistoryVisited.as_str(),
        Priority::Low,
        occurred_unix_nanos,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            error!(error = %e, "browser_history: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn webkit_epoch_conversion_known_values() {
        // 1970-01-01T00:00:00Z = WebKit 11_644_473_600_000_000 us
        assert_eq!(webkit_to_unix_nanos(11_644_473_600_000_000), 0);
        // 1970-01-01T00:00:01Z = +1_000_000 us
        assert_eq!(webkit_to_unix_nanos(11_644_473_601_000_000), 1_000_000_000);
        // Pre-Unix epoch → 0
        assert_eq!(webkit_to_unix_nanos(0), 0);
        assert_eq!(webkit_to_unix_nanos(11_644_473_500_000_000), 0);
    }

    #[test]
    fn truncate_utf8_respects_char_boundary() {
        let s = "günaydın";
        let t = truncate_utf8(s, 5);
        // Must still be valid UTF-8.
        assert!(t.len() <= 5);
        assert!(std::str::from_utf8(t.as_bytes()).is_ok());
    }

    #[test]
    fn truncate_utf8_short_input_unchanged() {
        assert_eq!(truncate_utf8("hello", 100), "hello");
    }

    #[test]
    fn internal_urls_are_skipped() {
        assert!(is_internal_url("chrome://settings"));
        assert!(is_internal_url("edge://flags"));
        assert!(is_internal_url("brave://wallet"));
        assert!(is_internal_url("about:blank"));
        assert!(is_internal_url("file:///C:/Users"));
        assert!(is_internal_url("chrome-extension://abc/foo.html"));
        assert!(!is_internal_url("https://example.com/"));
        assert!(!is_internal_url("http://example.com/"));
    }

    #[test]
    fn cursor_state_round_trip() {
        let mut s = CursorState::default();
        s.set(Browser::Chrome, "Default", 1234);
        s.set(Browser::Edge, "Profile 1", 5678);
        let bytes = serde_json::to_vec(&s).unwrap();
        let back: CursorState = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(back.get(Browser::Chrome, "Default"), 1234);
        assert_eq!(back.get(Browser::Edge, "Profile 1"), 5678);
        assert_eq!(back.get(Browser::Brave, "Default"), 0);
    }

    #[test]
    fn build_payload_shape() {
        let v = Visit {
            url: "https://example.com/path".to_string(),
            title: "Example \"quoted\" Domain".to_string(),
            unix_nanos: 1_744_415_288_888_888_000,
            transition: 7,
            visit_count: 3,
        };
        let payload = build_payload(Browser::Chrome, "Default", &v);
        // It must parse as JSON.
        let parsed: serde_json::Value = serde_json::from_str(&payload).unwrap();
        assert_eq!(parsed["browser"], "chrome");
        assert_eq!(parsed["profile"], "Default");
        assert_eq!(parsed["url"], "https://example.com/path");
        assert_eq!(parsed["title"], "Example \"quoted\" Domain");
        assert_eq!(parsed["transition"], 7);
        assert_eq!(parsed["visit_count"], 3);
        assert!(parsed["visit_time"].as_str().unwrap().ends_with("Z"));
    }

    #[test]
    fn rfc3339_format_known_value() {
        // 2026-04-13T10:34:22.123456Z
        // Unix seconds = 1_775_644_462; nanos = 123_456_000
        let ts = 1_775_644_462i64 * 1_000_000_000 + 123_456_000;
        let s = format_rfc3339_micros(ts);
        assert_eq!(s, "2026-04-13T10:34:22.123456Z");
    }
}
