//! Browser URL / page-title extraction collector.
//!
//! Polls the foreground window every 1 second. When the foreground process is
//! a known browser AND the window title carries one of the browser-specific
//! suffixes (e.g. ` - Google Chrome`, ` — Mozilla Firefox`), the page title
//! portion is sliced out. If that slice happens to look like a URL it is
//! captured separately as a `suspected_url`.
//!
//! # KVKK note (content class)
//!
//! Page titles are classified as **content** by [`personel_core::event`] —
//! they may contain personal information embedded by the underlying website.
//! This collector emits the title verbatim; downstream `SensitivityGuard` in
//! the enricher (ADR 0013) is responsible for redaction / drop decisions.
//! Nothing here writes to disk outside the encrypted offline queue.
//!
//! # Why no `regex` dependency
//!
//! The "looks like a URL" heuristic is intentionally tiny and case-folded:
//! starts-with `http://` / `https://`, OR contains a dot plus no whitespace
//! plus at least 4 characters total. Adding the full `regex` crate just for
//! this one predicate would inflate the agent binary by ~250 KB for zero
//! correctness gain — browsers virtually never put a literal URL in the
//! window title unless a user has explicitly toggled "Show full URLs", and
//! the heuristic only needs to be good enough to catch the obvious cases.
//!
//! # Dedup
//!
//! Within the poll loop we keep the last `(pid, page_title)` pair. Repeats
//! within the 5-second debounce window are suppressed even if the loop
//! re-observes them. This gives us natural anti-flapping for animated tab
//! titles (e.g. notification counters in Gmail like `(3) Inbox`).
//!
//! # Platform support
//!
//! Uses the `personel_platform::input::foreground_window_info` facade. On
//! non-Windows platforms the facade returns `AgentError::Unsupported`; the
//! collector then parks and reports healthy (non-blocking for dev builds).
//! Process-name lookup is done via `sysinfo`, which is cross-platform.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use sysinfo::{Pid, ProcessRefreshKind, RefreshKind, System};

// Throttle full process-table refresh: scanning all processes every poll
// (1 Hz) burns CPU on large workstations. The foreground process is almost
// always already known from the previous tick, so we only do a global
// refresh when the pid is missing from the cache.
const REFRESH_FALLBACK: Duration = Duration::from_secs(15);
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

/// Poll interval between foreground-window probes.
const POLL_INTERVAL: Duration = Duration::from_millis(1000);

/// Debounce window for `(pid, page_title)` pairs.
const DEDUP_WINDOW: Duration = Duration::from_secs(5);

/// Minimum page-title length (after suffix strip) to be worth emitting.
const MIN_PAGE_TITLE_LEN: usize = 3;

/// Browser identity used in the emitted payload.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Browser {
    Chrome,
    Edge,
    Firefox,
    Brave,
}

impl Browser {
    fn as_str(self) -> &'static str {
        match self {
            Browser::Chrome => "chrome",
            Browser::Edge => "edge",
            Browser::Firefox => "firefox",
            Browser::Brave => "brave",
        }
    }
}

/// Known browser process-name → window-title-suffix table.
///
/// The order matters when a browser ships multiple historical suffixes —
/// the first match wins. Suffixes are matched against the raw window title
/// with no case folding (browsers keep their suffixes consistent).
struct BrowserMatch {
    browser: Browser,
    process_names: &'static [&'static str],
    suffixes: &'static [&'static str],
}

const BROWSERS: &[BrowserMatch] = &[
    BrowserMatch {
        browser: Browser::Chrome,
        process_names: &["chrome.exe", "chrome", "google chrome"],
        suffixes: &[" - Google Chrome"],
    },
    BrowserMatch {
        browser: Browser::Edge,
        process_names: &["msedge.exe", "msedge", "microsoft edge"],
        // Edge sometimes inserts " and N more pages" between the title and
        // the browser suffix when multiple tabs share a window group.
        suffixes: &[" - Microsoft Edge", " — Microsoft Edge"],
    },
    BrowserMatch {
        browser: Browser::Firefox,
        process_names: &["firefox.exe", "firefox"],
        // Firefox uses the EM DASH (U+2014) on Windows and a regular hyphen
        // on some Linux builds. We accept both for forward-compat.
        suffixes: &[" — Mozilla Firefox", " - Mozilla Firefox"],
    },
    BrowserMatch {
        browser: Browser::Brave,
        process_names: &["brave.exe", "brave"],
        suffixes: &[" - Brave"],
    },
];

/// Sensitive-tab markers we never want to emit. Compared case-insensitively
/// against the post-suffix-strip page title.
const SKIP_TITLES: &[&str] = &[
    "new tab",
    "yeni sekme",
    "untitled",
    "adsız",
    "about:blank",
];

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// Browser URL / page-title extraction collector.
#[derive(Default)]
pub struct WindowUrlExtractionCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl WindowUrlExtractionCollector {
    /// Creates a new [`WindowUrlExtractionCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for WindowUrlExtractionCollector {
    fn name(&self) -> &'static str {
        "window_url_extraction"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["browser.url_extracted"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            // System for pid → process-name lookup. We start with a fresh
            // process table and re-refresh either when the foreground pid is
            // unknown to us, or every REFRESH_FALLBACK as a safety net for
            // long-running parent pids whose process record may have rotated
            // (very rare but possible after pid wraparound on busy hosts).
            let mut sys = System::new_with_specifics(
                RefreshKind::new().with_processes(ProcessRefreshKind::new()),
            );
            sys.refresh_processes();

            let mut last_pid: u32 = 0;
            let mut last_title: String = String::new();
            let mut last_emit_nanos: i64 = 0;
            let mut last_refresh = std::time::Instant::now();

            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            info!("window_url_extraction collector: started");

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        match personel_platform::input::foreground_window_info() {
                            Ok(info) => {
                                healthy.store(true, Ordering::Relaxed);

                                if info.title.is_empty() || info.pid == 0 {
                                    continue;
                                }

                                // Lazy refresh: only re-scan the process table
                                // if the foreground pid is unknown or we passed
                                // the fallback interval.
                                let pid = Pid::from_u32(info.pid);
                                let need_refresh = sys.process(pid).is_none()
                                    || last_refresh.elapsed() >= REFRESH_FALLBACK;
                                if need_refresh {
                                    sys.refresh_processes();
                                    last_refresh = std::time::Instant::now();
                                }
                                let process_name = sys
                                    .process(pid)
                                    .map(|p| p.name().to_string())
                                    .unwrap_or_default();

                                let Some(browser) = match_browser(&process_name) else {
                                    continue;
                                };

                                let Some((_, page_title)) = strip_browser_suffix(&info.title) else {
                                    // Process is a browser but title doesn't carry a
                                    // recognised suffix → skip (e.g. settings dialog).
                                    continue;
                                };

                                if !is_emittable(&page_title) {
                                    continue;
                                }

                                // Dedup: same (pid, title) within DEDUP_WINDOW = drop.
                                let now = ctx.clock.now_unix_nanos();
                                if info.pid == last_pid
                                    && page_title == last_title
                                    && (now - last_emit_nanos) >= 0
                                    && (now - last_emit_nanos)
                                        < i64::try_from(DEDUP_WINDOW.as_nanos()).unwrap_or(i64::MAX)
                                {
                                    continue;
                                }

                                let suspected_url = if looks_like_url(&page_title) {
                                    Some(page_title.clone())
                                } else {
                                    None
                                };

                                debug!(
                                    browser = browser.as_str(),
                                    pid = info.pid,
                                    page_title = %page_title,
                                    has_url = suspected_url.is_some(),
                                    "browser title observed"
                                );

                                let payload = build_payload(
                                    browser,
                                    &process_name,
                                    info.pid,
                                    &page_title,
                                    suspected_url.as_deref(),
                                );

                                emit_event(&ctx, &payload, &events, &drops);

                                last_pid = info.pid;
                                last_title = page_title;
                                last_emit_nanos = now;
                            }
                            Err(personel_core::error::AgentError::Unsupported { .. }) => {
                                healthy.store(true, Ordering::Relaxed);
                                info!(
                                    "window_url_extraction: platform unsupported — parking"
                                );
                                let _ = stop_rx.await;
                                break;
                            }
                            Err(e) => {
                                warn!(
                                    "window_url_extraction: foreground_window_info error: {e}"
                                );
                                healthy.store(false, Ordering::Relaxed);
                            }
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("window_url_extraction collector: stop requested");
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
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Matches a process executable name against the browser table. Comparison
/// is case-insensitive on the basename only.
fn match_browser(process_name: &str) -> Option<Browser> {
    if process_name.is_empty() {
        return None;
    }
    let lower = process_name.to_ascii_lowercase();
    for entry in BROWSERS {
        for &candidate in entry.process_names {
            if lower == candidate || lower.ends_with(candidate) {
                return Some(entry.browser);
            }
        }
    }
    None
}

/// Strips a known browser suffix from a window title and returns the
/// remaining "page title" portion together with the matching browser.
///
/// Also strips Chromium's `" and N more pages"` interjection that Edge and
/// Chrome sometimes put between the page title and the browser name.
fn strip_browser_suffix(title: &str) -> Option<(Browser, String)> {
    for entry in BROWSERS {
        for &suffix in entry.suffixes {
            if let Some(stripped) = title.strip_suffix(suffix) {
                let cleaned = strip_more_pages_marker(stripped).trim().to_string();
                if cleaned.is_empty() {
                    return None;
                }
                return Some((entry.browser, cleaned));
            }
        }
    }
    None
}

/// Removes Chromium's `" and N more pages"` / `" and N more page"` marker
/// from the tail of `s`. Returns `s` unchanged if no marker is present.
fn strip_more_pages_marker(s: &str) -> &str {
    // Look for " and " followed by a digit and "more page".
    let needle = " and ";
    let Some(pos) = s.rfind(needle) else { return s };
    let tail = &s[pos + needle.len()..];
    // Tail must start with a digit, contain "more page", and have nothing
    // surprising afterwards.
    let mut chars = tail.chars();
    let Some(first) = chars.next() else { return s };
    if !first.is_ascii_digit() {
        return s;
    }
    if tail.contains("more page") {
        return &s[..pos];
    }
    s
}

/// Cheap "looks like a URL" heuristic. Avoids pulling the `regex` crate.
///
/// Returns true when the input either begins with `http://` / `https://`,
/// or has the shape `<word>.<word>` with no ASCII whitespace and at least
/// 4 visible characters.
fn looks_like_url(s: &str) -> bool {
    let trimmed = s.trim();
    if trimmed.len() < 4 {
        return false;
    }
    let lower = trimmed.to_ascii_lowercase();
    if lower.starts_with("http://") || lower.starts_with("https://") {
        return true;
    }
    // No whitespace allowed, must contain at least one dot, dot must not be
    // first or last character.
    if trimmed.chars().any(char::is_whitespace) {
        return false;
    }
    let Some(dot_idx) = trimmed.find('.') else { return false };
    if dot_idx == 0 || dot_idx == trimmed.len() - 1 {
        return false;
    }
    // Must contain at least one alphanumeric on each side of the dot.
    let (left, right) = trimmed.split_at(dot_idx);
    let right = &right[1..];
    left.chars().any(|c| c.is_ascii_alphanumeric())
        && right.chars().any(|c| c.is_ascii_alphanumeric())
}

/// Returns `true` if a title is worth emitting (passes length + skip-list).
fn is_emittable(page_title: &str) -> bool {
    if page_title.chars().count() < MIN_PAGE_TITLE_LEN {
        return false;
    }
    let lower = page_title.to_ascii_lowercase();
    for skip in SKIP_TITLES {
        if lower == *skip || lower.ends_with(skip) {
            return false;
        }
    }
    true
}

/// Builds a JSON payload string. Uses `serde_json::to_string` for correct
/// escaping of all UTF-8 / control characters in arbitrary page titles.
fn build_payload(
    browser: Browser,
    process_name: &str,
    pid: u32,
    page_title: &str,
    suspected_url: Option<&str>,
) -> String {
    // Hand-built serde_json::Value avoids deriving Serialize on a struct just
    // for one local payload site.
    let value = serde_json::json!({
        "browser": browser.as_str(),
        "process_name": process_name,
        "pid": pid,
        "page_title": page_title,
        "suspected_url": suspected_url,
    });
    serde_json::to_string(&value).unwrap_or_else(|_| String::from("{}"))
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
        EventKind::BrowserUrlExtracted.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            error!(error = %e, "window_url_extraction: queue error");
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
    fn match_browser_chrome_basenames() {
        assert_eq!(match_browser("chrome.exe"), Some(Browser::Chrome));
        assert_eq!(match_browser("CHROME.EXE"), Some(Browser::Chrome));
        assert_eq!(match_browser("/usr/bin/chrome"), Some(Browser::Chrome));
    }

    #[test]
    fn match_browser_edge_firefox_brave() {
        assert_eq!(match_browser("msedge.exe"), Some(Browser::Edge));
        assert_eq!(match_browser("firefox.exe"), Some(Browser::Firefox));
        assert_eq!(match_browser("brave.exe"), Some(Browser::Brave));
    }

    #[test]
    fn match_browser_unknown() {
        assert_eq!(match_browser("notepad.exe"), None);
        assert_eq!(match_browser("explorer.exe"), None);
        assert_eq!(match_browser(""), None);
    }

    #[test]
    fn strip_chrome_suffix() {
        let (b, t) = strip_browser_suffix("Example Domain - Google Chrome").unwrap();
        assert_eq!(b, Browser::Chrome);
        assert_eq!(t, "Example Domain");
    }

    #[test]
    fn strip_edge_suffix_em_dash() {
        let (b, t) =
            strip_browser_suffix("GitHub: Where everything happens — Microsoft Edge")
                .unwrap();
        assert_eq!(b, Browser::Edge);
        assert_eq!(t, "GitHub: Where everything happens");
    }

    #[test]
    fn strip_firefox_em_dash_suffix() {
        let (b, t) =
            strip_browser_suffix("Mozilla — Mozilla Firefox").unwrap();
        assert_eq!(b, Browser::Firefox);
        assert_eq!(t, "Mozilla");
    }

    #[test]
    fn strip_brave_suffix() {
        let (b, t) = strip_browser_suffix("Privacy First - Brave").unwrap();
        assert_eq!(b, Browser::Brave);
        assert_eq!(t, "Privacy First");
    }

    #[test]
    fn strip_turkish_utf8_title() {
        let (b, t) = strip_browser_suffix("Şirket Portalı - Google Chrome").unwrap();
        assert_eq!(b, Browser::Chrome);
        assert_eq!(t, "Şirket Portalı");
    }

    #[test]
    fn strip_handles_more_pages_marker_chrome() {
        let (b, t) = strip_browser_suffix(
            "Inbox (5) - mail@example.com and 3 more pages - Google Chrome",
        )
        .unwrap();
        assert_eq!(b, Browser::Chrome);
        assert_eq!(t, "Inbox (5) - mail@example.com");
    }

    #[test]
    fn strip_handles_more_pages_marker_edge() {
        let (b, t) = strip_browser_suffix(
            "Outlook and 7 more pages - Microsoft Edge",
        )
        .unwrap();
        assert_eq!(b, Browser::Edge);
        assert_eq!(t, "Outlook");
    }

    #[test]
    fn strip_no_match_returns_none() {
        assert!(strip_browser_suffix("Just a random window title").is_none());
        assert!(strip_browser_suffix("Notepad++ v8.6").is_none());
    }

    #[test]
    fn strip_empty_payload_after_suffix_returns_none() {
        // Edge case: title is *just* the suffix.
        assert!(strip_browser_suffix(" - Google Chrome").is_none());
    }

    #[test]
    fn looks_like_url_positive() {
        assert!(looks_like_url("https://example.com"));
        assert!(looks_like_url("HTTP://Example.COM/path"));
        assert!(looks_like_url("github.com/user/repo"));
        assert!(looks_like_url("example.org"));
        assert!(looks_like_url("sub.domain.tld"));
    }

    #[test]
    fn looks_like_url_negative() {
        assert!(!looks_like_url("Untitled"));
        assert!(!looks_like_url("Example Page"));
        assert!(!looks_like_url("Şirket Portalı"));
        assert!(!looks_like_url("a.b")); // too short
        assert!(!looks_like_url(".com")); // dot at start
        assert!(!looks_like_url("example.")); // dot at end
        assert!(!looks_like_url("hello world")); // whitespace
        assert!(!looks_like_url(""));
        assert!(!looks_like_url("nothing-here"));
    }

    #[test]
    fn is_emittable_skips_short_and_blank_pages() {
        assert!(!is_emittable(""));
        assert!(!is_emittable("ab"));
        assert!(!is_emittable("New Tab"));
        assert!(!is_emittable("Yeni Sekme"));
        assert!(!is_emittable("about:blank"));
        assert!(!is_emittable("Untitled"));
        assert!(!is_emittable("Adsız"));
    }

    #[test]
    fn is_emittable_passes_normal_titles() {
        assert!(is_emittable("Example Domain"));
        assert!(is_emittable("Şirket Portalı"));
        assert!(is_emittable("GitHub - Where everything happens"));
    }

    #[test]
    fn build_payload_shape_with_url() {
        let s = build_payload(
            Browser::Chrome,
            "chrome.exe",
            1234,
            "github.com/user/repo",
            Some("github.com/user/repo"),
        );
        assert!(s.contains("\"browser\":\"chrome\""));
        assert!(s.contains("\"process_name\":\"chrome.exe\""));
        assert!(s.contains("\"pid\":1234"));
        assert!(s.contains("\"page_title\":\"github.com/user/repo\""));
        assert!(s.contains("\"suspected_url\":\"github.com/user/repo\""));
    }

    #[test]
    fn build_payload_shape_without_url() {
        let s = build_payload(
            Browser::Firefox,
            "firefox.exe",
            42,
            "Mozilla",
            None,
        );
        assert!(s.contains("\"browser\":\"firefox\""));
        assert!(s.contains("\"suspected_url\":null"));
    }

    #[test]
    fn build_payload_escapes_unicode_correctly() {
        let s = build_payload(
            Browser::Chrome,
            "chrome.exe",
            1,
            "Şirket \"Portalı\"",
            None,
        );
        // serde_json will escape the embedded quotes; just sanity check it
        // produced valid JSON by round-tripping.
        let v: serde_json::Value = serde_json::from_str(&s).expect("valid json");
        assert_eq!(v["page_title"], "Şirket \"Portalı\"");
    }

    /// Models the dedup decision the poll loop makes for a sequence of
    /// observations. Asserts that within DEDUP_WINDOW the same (pid, title)
    /// pair is suppressed and a different pair always emits.
    #[test]
    fn dedup_logic_suppresses_repeats() {
        fn should_emit(
            last_pid: u32,
            last_title: &str,
            last_nanos: i64,
            now_nanos: i64,
            new_pid: u32,
            new_title: &str,
        ) -> bool {
            if new_pid == last_pid
                && new_title == last_title
                && (now_nanos - last_nanos) >= 0
                && (now_nanos - last_nanos)
                    < i64::try_from(DEDUP_WINDOW.as_nanos()).unwrap_or(i64::MAX)
            {
                return false;
            }
            true
        }

        // First observation always emits.
        assert!(should_emit(0, "", 0, 1_000_000_000, 100, "Example"));

        // Repeat 1s later → suppressed.
        assert!(!should_emit(
            100,
            "Example",
            1_000_000_000,
            2_000_000_000,
            100,
            "Example"
        ));

        // Repeat 6s later → emits (window expired).
        assert!(should_emit(
            100,
            "Example",
            1_000_000_000,
            7_000_000_000,
            100,
            "Example"
        ));

        // Different title at same pid → emits.
        assert!(should_emit(
            100,
            "Example",
            1_000_000_000,
            1_500_000_000,
            100,
            "Other"
        ));

        // Different pid same title → emits.
        assert!(should_emit(
            100,
            "Example",
            1_000_000_000,
            1_500_000_000,
            200,
            "Example"
        ));
    }
}
