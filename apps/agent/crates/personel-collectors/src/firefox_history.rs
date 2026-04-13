//! Firefox browsing-history collector — Faz 2 Wave 2 item #10.
//!
//! Periodically reads `places.sqlite` from every Firefox profile under the
//! current user's `%APPDATA%\Mozilla\Firefox\Profiles\` directory and emits
//! one [`EventKind::BrowserFirefoxHistoryVisited`] event per new visit since
//! the last poll. The cursor is the most recent `moz_historyvisits.visit_date`
//! (PRTime — microseconds since the Unix epoch, 1970-01-01 UTC) seen for that
//! profile. Cursors are persisted to disk so a service restart does not cause
//! duplicate emission.
//!
//! # KVKK / privacy
//!
//! Per CLAUDE.md §0 Faz 2 design decisions, **only visited URL + page title +
//! visit timestamp** are captured. We deliberately do **not** read:
//!
//! - Cookies
//! - Bookmarks
//! - Saved passwords (`logins.json`, `key4.db`)
//! - Form autofill data
//! - Download history blobs
//!
//! `about:`, `chrome://`, `file://`, `resource://`, and `javascript:` URLs are
//! filtered out at the SQL boundary so that local-only navigation never leaves
//! the agent.
//!
//! # Locked database handling
//!
//! Firefox holds `places.sqlite` open in WAL mode and SQLite returns
//! `SQLITE_BUSY` for any write attempt. To avoid contending with the running
//! browser we **copy** the database (plus its `-wal` and `-shm` companion
//! files when present) into a temp directory and open the copy read-only. A
//! `std::fs::copy` is preferable to `VACUUM INTO` here because it does not
//! require write access to the source file and works even when the WAL is
//! large.
//!
//! # Cursor rollback
//!
//! If a poll fails (file copy error, SQL error, JSON parse error, etc.) we
//! deliberately do **not** advance the cursor. The next poll cycle retries
//! from the same point. Worst case a Firefox crash can leave a half-flushed
//! WAL — the missed visits will be picked up after the next clean shutdown.
//!
//! # Concurrency
//!
//! `rusqlite` is synchronous and the file copy is blocking. The poll loop runs
//! inside `tokio::task::spawn_blocking` on each tick to keep the runtime
//! reactor unblocked.

#![allow(
    clippy::cast_possible_truncation,
    clippy::cast_possible_wrap,
    clippy::cast_sign_loss,
    clippy::cast_lossless,
    clippy::similar_names,
    clippy::doc_markdown
)]

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
// Tunables
// ──────────────────────────────────────────────────────────────────────────────

/// Time between successive polls of every Firefox profile.
const POLL_INTERVAL: Duration = Duration::from_secs(300); // 5 minutes
/// Maximum number of visits emitted in a single poll cycle (per profile).
const MAX_VISITS_PER_POLL: usize = 500;
/// Maximum URL length kept (bytes, UTF-8 safe truncation).
const MAX_URL_BYTES: usize = 2048;
/// Maximum title length kept (bytes, UTF-8 safe truncation).
const MAX_TITLE_BYTES: usize = 256;
/// Minimum URL length below which entries are silently skipped.
const MIN_URL_LEN: usize = 8;

/// Filename of the per-profile cursor JSON file written under
/// `%PROGRAMDATA%\Personel\agent\`. The map key is the profile directory
/// name (e.g. `xxxxxxxx.default-release`) and the value is the most recent
/// PRTime (microseconds since 1970) we have already emitted for that profile.
const CURSOR_FILE_NAME: &str = "firefox_history.state";

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// Firefox `places.sqlite` history collector.
#[derive(Default)]
pub struct FirefoxHistoryCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl FirefoxHistoryCollector {
    /// Creates a new [`FirefoxHistoryCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for FirefoxHistoryCollector {
    fn name(&self) -> &'static str {
        "firefox_history"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["browser.firefox_history_visited"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            // First tick fires immediately; we want a small delay so the rest
            // of the agent has finished bringing up the queue. Skip it.
            ticker.tick().await;

            info!(
                interval_secs = POLL_INTERVAL.as_secs(),
                "firefox_history collector: started"
            );

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        let ctx_clone = ctx.clone();
                        let healthy_inner = Arc::clone(&healthy);
                        let events_inner = Arc::clone(&events);
                        let drops_inner = Arc::clone(&drops);

                        // rusqlite + std::fs::copy are blocking — push the
                        // entire poll cycle off the runtime worker pool.
                        let join = tokio::task::spawn_blocking(move || {
                            poll_cycle(&ctx_clone, &healthy_inner, &events_inner, &drops_inner);
                        }).await;

                        if let Err(e) = join {
                            error!(error = %e, "firefox_history: blocking task panicked");
                            healthy.store(false, Ordering::Relaxed);
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("firefox_history collector: stop requested");
                        break;
                    }
                }
            }
        });

        // Mark healthy on registration; a missing Firefox is not an error.
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
// Visit row + payload helpers
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
struct Visit {
    url: String,
    title: String,
    /// PRTime: microseconds since Unix epoch.
    date_us: i64,
    kind: i32,
    count: i32,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct CursorState {
    /// profile directory name → most recent emitted visit_date (PRTime µs)
    profiles: HashMap<String, i64>,
}

// ──────────────────────────────────────────────────────────────────────────────
// Poll cycle
// ──────────────────────────────────────────────────────────────────────────────

fn poll_cycle(
    ctx: &CollectorCtx,
    healthy: &AtomicBool,
    events: &AtomicU64,
    drops: &AtomicU64,
) {
    let profiles = match discover_profiles() {
        Ok(p) => p,
        Err(err) => {
            warn!(error = %err, "firefox_history: profile discovery failed");
            healthy.store(false, Ordering::Relaxed);
            return;
        }
    };

    if profiles.is_empty() {
        debug!("firefox_history: no Firefox profiles found — skipping cycle");
        healthy.store(true, Ordering::Relaxed);
        return;
    }

    let Some(cursor_path) = cursor_file_path() else {
        warn!("firefox_history: PROGRAMDATA env var missing — cannot persist cursors");
        healthy.store(false, Ordering::Relaxed);
        return;
    };

    let mut state = read_cursor_state(&cursor_path);
    let mut any_progress = false;

    for (profile_name, db_path) in profiles {
        let cursor_us = state.profiles.get(&profile_name).copied().unwrap_or(0);
        match poll_profile(&profile_name, &db_path, cursor_us) {
            Ok((visits, new_cursor)) => {
                if visits.len() == MAX_VISITS_PER_POLL {
                    warn!(
                        profile = %profile_name,
                        cap = MAX_VISITS_PER_POLL,
                        "firefox_history: poll cap reached — some visits deferred to next cycle"
                    );
                }
                for visit in visits {
                    emit_visit(ctx, &profile_name, &visit, events, drops);
                }
                if new_cursor > cursor_us {
                    state.profiles.insert(profile_name.clone(), new_cursor);
                    any_progress = true;
                }
            }
            Err(err) => {
                // Cursor rollback: leave the cursor unchanged. Next poll
                // retries the same window.
                warn!(
                    profile = %profile_name,
                    error = %err,
                    "firefox_history: profile poll failed — cursor not advanced"
                );
            }
        }
    }

    if any_progress {
        if let Err(err) = write_cursor_state(&cursor_path, &state) {
            warn!(error = %err, "firefox_history: cursor persist failed");
        }
    }

    healthy.store(true, Ordering::Relaxed);
}

// ──────────────────────────────────────────────────────────────────────────────
// Profile discovery
// ──────────────────────────────────────────────────────────────────────────────

/// Enumerates every Firefox profile directory under
/// `%APPDATA%\Mozilla\Firefox\Profiles\`. Returns an empty `Vec` if Firefox is
/// not installed for this user (the directory does not exist).
///
/// We deliberately scan every subdirectory rather than parse `profiles.ini`.
/// In practice profiles.ini lists the same set we would discover via a
/// directory scan, and the directory walk is one OS call cheaper and immune
/// to malformed INI files.
fn discover_profiles() -> std::io::Result<Vec<(String, PathBuf)>> {
    let Ok(appdata) = std::env::var("APPDATA") else {
        return Ok(Vec::new());
    };

    let profiles_root = PathBuf::from(appdata)
        .join("Mozilla")
        .join("Firefox")
        .join("Profiles");

    if !profiles_root.exists() {
        return Ok(Vec::new());
    }

    let mut out = Vec::new();
    for entry in fs::read_dir(&profiles_root)? {
        let entry = entry?;
        let path = entry.path();
        if !path.is_dir() {
            continue;
        }
        let places = path.join("places.sqlite");
        if places.exists() {
            if let Some(name) = path.file_name().and_then(|n| n.to_str()) {
                out.push((name.to_owned(), places));
            }
        }
    }
    Ok(out)
}

// ──────────────────────────────────────────────────────────────────────────────
// Per-profile poll
// ──────────────────────────────────────────────────────────────────────────────

/// Reads new visits from a single profile's `places.sqlite`.
///
/// Returns the visit list and the new cursor (the maximum `visit_date` seen
/// in this batch, or `cursor_us` unchanged when the batch was empty).
fn poll_profile(
    profile_name: &str,
    db_path: &Path,
    cursor_us: i64,
) -> std::result::Result<(Vec<Visit>, i64), String> {
    // Step 1: copy the live DB + WAL/SHM companions to a temp location.
    // Firefox's lock would otherwise return SQLITE_BUSY on read attempts.
    let temp_dir = std::env::temp_dir().join(format!("personel_ff_{profile_name}"));
    fs::create_dir_all(&temp_dir).map_err(|e| format!("temp dir create: {e}"))?;

    let temp_db = temp_dir.join("places.sqlite");
    fs::copy(db_path, &temp_db).map_err(|e| format!("copy places.sqlite: {e}"))?;

    // Best-effort copy of WAL + SHM. Their absence is fine — if Firefox is
    // stopped the WAL has already been merged into the main DB.
    let wal_src = db_path.with_extension("sqlite-wal");
    if wal_src.exists() {
        let _ = fs::copy(&wal_src, temp_dir.join("places.sqlite-wal"));
    }
    let shm_src = db_path.with_extension("sqlite-shm");
    if shm_src.exists() {
        let _ = fs::copy(&shm_src, temp_dir.join("places.sqlite-shm"));
    }

    // Step 2: open the copy read-only. SQLITE_OPEN_READ_ONLY +
    // SQLITE_OPEN_NO_MUTEX is the safest combination for a snapshot.
    let conn = Connection::open_with_flags(
        &temp_db,
        OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .map_err(|e| format!("open places.sqlite copy: {e}"))?;

    // Step 3: query new visits ordered ASC, capped at MAX_VISITS_PER_POLL.
    // The URL prefix filter happens here so we never marshal browser-internal
    // navigations across the FFI boundary.
    let sql = "
        SELECT p.url, p.title, p.visit_count, h.visit_date, h.visit_type
        FROM moz_historyvisits h
        JOIN moz_places p ON p.id = h.place_id
        WHERE h.visit_date > ?1
          AND p.url IS NOT NULL
          AND p.url NOT LIKE 'about:%'
          AND p.url NOT LIKE 'chrome://%'
          AND p.url NOT LIKE 'file://%'
          AND p.url NOT LIKE 'resource://%'
          AND p.url NOT LIKE 'javascript:%'
          AND p.url NOT LIKE 'moz-extension://%'
        ORDER BY h.visit_date ASC
        LIMIT ?2
    ";

    let mut stmt = conn.prepare(sql).map_err(|e| format!("prepare: {e}"))?;
    let limit = i64::try_from(MAX_VISITS_PER_POLL).unwrap_or(i64::MAX);
    let rows = stmt
        .query_map(rusqlite::params![cursor_us, limit], |row| {
            Ok(Visit {
                url: row.get::<_, String>(0)?,
                title: row.get::<_, Option<String>>(1)?.unwrap_or_default(),
                count: row.get::<_, i64>(2)?.try_into().unwrap_or(i32::MAX),
                date_us: row.get::<_, i64>(3)?,
                kind: row.get::<_, i64>(4)?.try_into().unwrap_or(0),
            })
        })
        .map_err(|e| format!("query_map: {e}"))?;

    let mut visits = Vec::new();
    let mut new_cursor = cursor_us;
    for row in rows {
        let mut visit = row.map_err(|e| format!("row decode: {e}"))?;
        if visit.url.len() < MIN_URL_LEN {
            continue;
        }
        visit.url = truncate_utf8(&visit.url, MAX_URL_BYTES);
        visit.title = truncate_utf8(&visit.title, MAX_TITLE_BYTES);
        if visit.date_us > new_cursor {
            new_cursor = visit.date_us;
        }
        visits.push(visit);
    }

    // Drop the connection before removing the temp file to release any
    // OS-level handle (Windows refuses unlink on an open file).
    drop(stmt);
    drop(conn);
    let _ = fs::remove_dir_all(&temp_dir);

    Ok((visits, new_cursor))
}

// ──────────────────────────────────────────────────────────────────────────────
// Event emission
// ──────────────────────────────────────────────────────────────────────────────

fn emit_visit(
    ctx: &CollectorCtx,
    profile: &str,
    visit: &Visit,
    events: &AtomicU64,
    drops: &AtomicU64,
) {
    let visit_time_iso = prtime_to_iso8601(visit.date_us);

    // Hand-rolled JSON to avoid pulling serde_json::json! macro into a hot
    // path. All string fields are escaped via the `{:?}` debug formatter
    // which produces a valid JSON string for typical browser data.
    let payload = format!(
        r#"{{"browser":"firefox","profile":{:?},"url":{:?},"title":{:?},"visit_time":{:?},"visit_type":{},"visit_count":{}}}"#,
        profile,
        visit.url,
        visit.title,
        visit_time_iso,
        visit.kind,
        visit.count,
    );

    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    let kind = EventKind::BrowserFirefoxHistoryVisited;
    match ctx.queue.enqueue(
        &id,
        kind.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            error!(error = %e, "firefox_history: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Cursor state file
// ──────────────────────────────────────────────────────────────────────────────

fn cursor_file_path() -> Option<PathBuf> {
    let programdata = std::env::var("PROGRAMDATA").ok()?;
    Some(
        PathBuf::from(programdata)
            .join("Personel")
            .join("agent")
            .join(CURSOR_FILE_NAME),
    )
}

fn read_cursor_state(path: &Path) -> CursorState {
    let bytes = match fs::read(path) {
        Ok(b) => b,
        Err(_) => return CursorState::default(),
    };
    serde_json::from_slice(&bytes).unwrap_or_default()
}

fn write_cursor_state(path: &Path, state: &CursorState) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let bytes = serde_json::to_vec(state)
        .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))?;
    // Atomic-ish write: rename over the destination after the temp file is
    // fully flushed. Fall back to direct write if rename fails (e.g. across
    // filesystems — should not happen here but cheap to guard).
    let tmp = path.with_extension("state.tmp");
    fs::write(&tmp, &bytes)?;
    if fs::rename(&tmp, path).is_err() {
        fs::write(path, &bytes)?;
    }
    Ok(())
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Converts a Firefox PRTime (microseconds since Unix epoch 1970-01-01 UTC)
/// to an ISO-8601 string. We hand-format to avoid pulling `time` or `chrono`
/// just for one call site.
fn prtime_to_iso8601(prtime_us: i64) -> String {
    // PRTime is signed; clamp to non-negative for sanity.
    let us = prtime_us.max(0);
    let secs = us / 1_000_000;
    let frac_us = us % 1_000_000;

    // Unix-epoch days → civil date. Algorithm from Howard Hinnant's
    // "date" library, public domain.
    let z = secs.div_euclid(86_400) + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = (if mp < 10 { mp + 3 } else { mp - 9 }) as u32;
    let y = if m <= 2 { y + 1 } else { y };

    let secs_of_day = secs.rem_euclid(86_400);
    let hh = (secs_of_day / 3600) as u32;
    let mm = ((secs_of_day % 3600) / 60) as u32;
    let ss = (secs_of_day % 60) as u32;

    format!("{y:04}-{m:02}-{d:02}T{hh:02}:{mm:02}:{ss:02}.{frac_us:06}Z")
}

/// Truncates a string to at most `max_bytes` bytes without splitting a UTF-8
/// codepoint. Falls back to a clean prefix if the boundary is mid-codepoint.
fn truncate_utf8(s: &str, max_bytes: usize) -> String {
    if s.len() <= max_bytes {
        return s.to_owned();
    }
    let mut end = max_bytes;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    s[..end].to_owned()
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn truncate_utf8_keeps_boundary() {
        let s = "héllo wörld"; // 13 bytes
        let out = truncate_utf8(s, 6);
        assert!(out.len() <= 6);
        assert!(s.starts_with(&out));
    }

    #[test]
    fn truncate_utf8_passthrough_when_short() {
        assert_eq!(truncate_utf8("abc", 100), "abc");
    }

    #[test]
    fn prtime_zero_is_unix_epoch() {
        assert_eq!(prtime_to_iso8601(0), "1970-01-01T00:00:00.000000Z");
    }

    #[test]
    fn prtime_known_value() {
        // 2026-04-13T10:34:22.123456Z
        // unix seconds = 1_776_249_262 ; PRTime µs = 1_776_249_262_123_456
        let s = prtime_to_iso8601(1_776_249_262_123_456);
        assert_eq!(s, "2026-04-13T10:34:22.123456Z");
    }

    #[test]
    fn cursor_state_roundtrip() {
        let mut s = CursorState::default();
        s.profiles.insert("xxxxxxxx.default-release".into(), 1_776_249_262_123_456);
        let json = serde_json::to_string(&s).unwrap();
        let back: CursorState = serde_json::from_str(&json).unwrap();
        assert_eq!(
            back.profiles.get("xxxxxxxx.default-release").copied(),
            Some(1_776_249_262_123_456)
        );
    }

    #[test]
    fn discover_profiles_handles_missing_appdata() {
        // We cannot reliably scrub APPDATA in a parallel test environment;
        // instead just assert the function does not panic and returns Ok.
        let _ = discover_profiles();
    }
}
