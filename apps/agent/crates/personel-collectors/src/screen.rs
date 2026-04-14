//! Screenshot capture collector (DXGI Desktop Duplication).
//!
//! Faz 3 overhaul (items #21–#28): the collector is rewritten around a small
//! set of pure helpers (encoding, preprocessing, dedup, sensitivity, clicks)
//! so the capture loop stays thin and every decision is individually
//! testable on macOS/Linux.
//!
//! # What this collector does
//!
//! On each tick (adaptive, see #22) it checks the sensitivity guard (#23),
//! grabs the primary monitor via
//! [`personel_os::capture::DxgiCapture`], optionally runs OCR preprocessing
//! (#26), encodes as WebP (#24), computes a SHA-256 of the raw RGB and skips
//! emission if the frame is identical to the previous one for that monitor
//! (#25), optionally encrypts under the PE-DEK when DLP is enabled (#27),
//! attaches any recent mouse clicks (#28) and enqueues a single
//! `screenshot.captured` event as a JSON payload.
//!
//! # Multi-monitor (Faz 3 #21)
//!
//! This implementation **enumerates all DXGI outputs** via
//! [`personel_os::capture::enumerate_outputs`] and emits one
//! `monitor_count` metric on startup. Actual capture remains **primary-only**
//! for Phase 1; per-monitor duplication sessions are a Phase 3.1 item that
//! requires holding multiple `IDXGIOutputDuplication` handles on independent
//! threads. Each emitted event still carries a `monitor_index`, the
//! enumerated device name, and the desktop bounds so the enricher can
//! attribute frames correctly once Phase 3.1 lands.
//!
//! # Adaptive frequency (Faz 3 #22)
//!
//! Before each tick we read `GetLastInputInfo` synchronously via
//! [`personel_platform::input::last_input_idle_ms`]. If the user has been
//! idle for longer than 60 s we sleep for a short **idle interval**
//! (default 30 s) and check for a new frame — this catches meeting screens
//! and alerts that appear while the user is away. Otherwise we sleep for
//! the **active interval** read from
//! `policy.screenshot.interval_seconds` (default 300 s, minimum 10 s).
//!
//! # Sensitivity guard (Faz 3 #23)
//!
//! Three layers, all short-circuit:
//!
//! 1. **Hard-coded process name list** (see [`HARDCODED_EXCLUDE_EXES`]) —
//!    password managers, RDP client, KeePass family. These cannot be
//!    overridden by policy: a misconfigured bundle can never lift this guard.
//!    This is the KVKK m.6 / ADR 0013 defence-in-depth anchor.
//! 2. **Hard-coded title substring list** (see
//!    [`HARDCODED_EXCLUDE_TITLE_SUBSTRINGS`]) — "private browsing",
//!    "incognito", "inprivate", "gizli pencere", "password manager",
//!    "2fa", "recovery codes". Case-insensitive, UTF-8 substring match.
//! 3. **Policy-driven `screenshot.exclude_apps`** — customer-configurable
//!    additional process / title substrings. Applied on top of the
//!    hard-coded list, never replacing it.
//!
//! Skipped frames are logged at INFO and the `drops` counter is incremented
//! without enqueuing anything.
//!
//! # Delta / dedup (Faz 3 #25)
//!
//! After every successful capture the SHA-256 of the raw **BGRA** bytes is
//! computed and stored keyed by `monitor_index`. If the next tick produces
//! a frame whose hash matches the previous one we skip emission entirely.
//! This is full-frame dedup, NOT region-based delta — the latter requires a
//! diff algorithm and dirty-region encoding scheme which is deferred to
//! Phase 4. Full-frame dedup catches the biggest real-world win (locked /
//! idle screens) without touching the wire format.
//!
//! # OCR preprocessing (Faz 3 #26)
//!
//! When `policy.screenshot.ocr_optimised == true` (NOT a proto field today;
//! reserved in the PolicyView OCR gate and defaulting to `false` until the
//! proto tag lands) the BGRA frame is converted to grayscale via luminance
//! `Y = 0.299·R + 0.587·G + 0.114·B`, its histogram is stretched to
//! `[0..255]`, and the result is WebP-encoded as a single-channel image.
//! This is currently controlled by a compile-time constant
//! ([`OCR_MODE_DEFAULT`]) because the proto field is not yet wired; the
//! preprocessing function is fully exposed for unit tests either way.
//!
//! # PE-DEK at-rest encryption (Faz 3 #27, ADR 0013 gated)
//!
//! Phase 1 default is **plaintext WebP**: the payload carries
//! `"dlp_enabled": false` and the encoded bytes as base16. When and only when
//! `ctx.pe_dek.is_some()` (meaning the DLP opt-in ceremony produced a
//! per-endpoint DEK) the WebP bytes are passed through
//! [`personel_crypto::envelope::encrypt`], which generates a fresh random
//! 12-byte nonce and appends the GCM tag. The payload then carries
//! `"dlp_enabled": true`, `"ciphertext"`, `"nonce"`, `"aead_tag_appended":
//! true` and `"key_version"` (fixed to `1` until the DEK rotation schedule
//! is wired into the keystore).
//!
//! **Critical**: the `encrypt_if_dlp` helper is the sole entry point for
//! deciding plaintext vs ciphertext. No code path below that function should
//! ever emit plaintext when `pe_dek.is_some()`. The unit test
//! `pe_dek_gating_chooses_correct_branch` pins this invariant.
//!
//! # Click-aware capture (Faz 3 #28)
//!
//! A dedicated std::thread message pump installs a `WH_MOUSE_LL` low-level
//! hook and pushes `(x, y, timestamp_ms_since_unix_epoch)` into a bounded
//! 16-slot ring buffer on every `WM_LBUTTONDOWN`. Each capture drains the
//! ring buffer into the event payload (`recent_clicks: [...]`) and clears
//! it. Hook installation failures are logged at WARN and the collector
//! continues emitting events **without** the click list — click awareness
//! is a best-effort enrichment, never load-bearing.
//!
//! Crop-around-click is **not** performed in the agent; the enricher can
//! use the click coordinates for server-side cropping when desired.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full DXGI capture loop described above.
//! Other platforms: the collector starts, logs once that DXGI is
//! unavailable, and parks until stopped so cross-platform `cargo check`
//! passes. All pure helpers (sensitivity guard, dedup, preprocess, click
//! ring buffer, WebP encoder wiring) live outside the Windows cfg and are
//! fully covered by unit tests that run on every platform.
//!
//! # Error handling (Windows)
//!
//! | Error             | Action                                            |
//! |-------------------|---------------------------------------------------|
//! | `FrameTimeout`    | Skip tick; healthy stays `true`                   |
//! | `AccessLost`      | Call `reopen()`; retry on next tick               |
//! | `DeviceRemoved`   | Reconstruct `DxgiCapture`; wait 5 s before retry  |
//! | Other fatal error | Log `error!`; healthy → `false`; keep running     |

use std::collections::{HashMap, VecDeque};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use async_trait::async_trait;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Public collector type
// ──────────────────────────────────────────────────────────────────────────────

/// Screenshot capture collector (DXGI-based on Windows; no-op on others).
#[derive(Default)]
pub struct ScreenCollector {
    healthy:          Arc<AtomicBool>,
    events:           Arc<AtomicU64>,
    drops:            Arc<AtomicU64>,
    /// Number of frames skipped because their raw-pixel SHA-256 matched the
    /// previous frame for the same monitor. Exposed via `health()` status.
    frames_deduped:   Arc<AtomicU64>,
    /// Click ring buffer shared with the LL mouse hook thread.
    clicks:           Arc<Mutex<VecDeque<ClickEvent>>>,
    /// Faz 4 #33 — set to `true` while screen capture is suspended due to a
    /// low battery + on-battery condition. Surfaced via `health().status`.
    battery_suspended: Arc<AtomicBool>,
}

impl ScreenCollector {
    /// Creates a new [`ScreenCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ScreenCollector {
    fn name(&self) -> &'static str {
        "screen"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["screenshot.captured"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy           = Arc::clone(&self.healthy);
        let events            = Arc::clone(&self.events);
        let drops             = Arc::clone(&self.drops);
        let frames_deduped    = Arc::clone(&self.frames_deduped);
        let clicks            = Arc::clone(&self.clicks);
        let battery_suspended = Arc::clone(&self.battery_suspended);

        // DXGI requires a dedicated OS thread; run the whole capture loop on
        // `spawn_blocking` so we never touch DXGI from an async context.
        let task = tokio::task::spawn_blocking(move || {
            run_capture_loop(
                ctx,
                healthy,
                events,
                drops,
                frames_deduped,
                clicks,
                battery_suspended,
                stop_rx,
            );
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {
        // Loop reads live policy via ctx.policy() on every tick.
    }

    fn health(&self) -> HealthSnapshot {
        let healthy = self.healthy.load(Ordering::Relaxed);
        let deduped = self.frames_deduped.swap(0, Ordering::Relaxed);
        let battery_suspended = self.battery_suspended.load(Ordering::Relaxed);
        HealthSnapshot {
            healthy,
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last:  self.drops.swap(0, Ordering::Relaxed),
            status: if !healthy {
                "DXGI capture unhealthy".into()
            } else if battery_suspended {
                // Operational reason for the silence — not a health failure.
                "captures_suspended_battery_low".into()
            } else if deduped > 0 {
                format!("frames_skipped_identical={deduped}")
            } else {
                String::new()
            },
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Hard-coded sensitivity exclusion (Faz 3 #23)  — KVKK m.6 / ADR 0013 anchor
// ──────────────────────────────────────────────────────────────────────────────

/// Hard-coded process executables that MUST NEVER be captured regardless of
/// policy. Matched case-insensitively as a suffix of the executable file
/// name. KVKK m.6 defence-in-depth: a misconfigured or malicious policy
/// bundle cannot remove entries from this list.
pub(crate) const HARDCODED_EXCLUDE_EXES: &[&str] = &[
    "keepass.exe",
    "keepassxc.exe",
    "1password.exe",
    "bitwarden.exe",
    "lastpass.exe",
    "dashlane.exe",
    "enpass.exe",
    "roboform.exe",
    // RDP client — capturing it would double-record the remote desktop that
    // is presumably already captured on the remote side.
    "mstsc.exe",
];

/// Hard-coded window title substrings that trigger a skip.
/// Matched case-insensitively; any substring match anywhere in the title
/// suppresses the frame.
pub(crate) const HARDCODED_EXCLUDE_TITLE_SUBSTRINGS: &[&str] = &[
    "private browsing",
    "incognito",
    "inprivate",
    "gizli pencere",
    "password manager",
    "2fa",
    "recovery codes",
];

/// Returns `true` if the frame must be suppressed based on the hard-coded
/// lists plus the policy-driven `exclude_apps`. See [`HARDCODED_EXCLUDE_EXES`]
/// and [`HARDCODED_EXCLUDE_TITLE_SUBSTRINGS`].
pub(crate) fn should_skip_for_sensitivity(
    exe_name: &str,
    window_title: &str,
    policy_exclude_apps: &[String],
) -> bool {
    let exe_lower   = exe_name.to_lowercase();
    let title_lower = window_title.to_lowercase();

    // (1) Hard-coded exe — cannot be lifted by policy.
    if HARDCODED_EXCLUDE_EXES
        .iter()
        .any(|banned| exe_lower.ends_with(banned) || exe_lower == *banned)
    {
        return true;
    }

    // (2) Hard-coded title substring — cannot be lifted by policy.
    if HARDCODED_EXCLUDE_TITLE_SUBSTRINGS
        .iter()
        .any(|needle| title_lower.contains(needle))
    {
        return true;
    }

    // (3) Policy-driven extra excludes (additive).
    if !policy_exclude_apps.is_empty() {
        return policy_exclude_apps.iter().any(|app| {
            let a = app.to_lowercase();
            !a.is_empty() && (exe_lower.contains(&a) || title_lower.contains(&a))
        });
    }

    false
}

// ──────────────────────────────────────────────────────────────────────────────
// Adaptive frequency (Faz 3 #22)
// ──────────────────────────────────────────────────────────────────────────────

/// Threshold above which we treat the endpoint as idle and use the short
/// idle tick interval.
pub(crate) const IDLE_THRESHOLD_MS: u64 = 60_000;

/// Tick interval used when the user has been idle for more than
/// [`IDLE_THRESHOLD_MS`].
pub(crate) const IDLE_TICK_SECS: u64 = 30;

/// Chooses the next tick sleep duration. Pure so it can be unit-tested.
pub(crate) fn next_tick_delay_secs(idle_ms: u64, policy_active_secs: u64) -> u64 {
    if idle_ms > IDLE_THRESHOLD_MS {
        IDLE_TICK_SECS
    } else {
        policy_active_secs.max(10)
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Battery aware (Faz 4 #33)
// ──────────────────────────────────────────────────────────────────────────────

/// Threshold at or below which captures are suspended while on battery.
pub(crate) const BATTERY_LOW_PERCENT: u8 = 20;

/// Time-to-live for the cached `(on_battery, percent)` reading. The Win32
/// `GetSystemPowerStatus` call is cheap but spamming it every tick adds zero
/// value: battery levels move on the order of seconds, not milliseconds.
pub(crate) const BATTERY_CACHE_TTL: Duration = Duration::from_secs(30);

/// Cached power status reading + last-poll timestamp.
#[derive(Debug, Clone, Copy)]
pub(crate) struct BatteryCache {
    /// `true` when on battery (AC line offline).
    pub on_battery: bool,
    /// Battery percentage 0..=100. `100` is used when no battery is present
    /// (desktop PC) so the low-battery branch is never taken.
    pub percent:    u8,
    /// Wall-clock instant of the last successful `GetSystemPowerStatus` call.
    pub last_poll:  Instant,
}

impl BatteryCache {
    fn fresh() -> Self {
        let (on_battery, percent) = read_battery_state();
        Self { on_battery, percent, last_poll: Instant::now() }
    }
}

static BATTERY_CACHE: OnceLock<Mutex<BatteryCache>> = OnceLock::new();

/// Reads the current power state from the OS.
///
/// Returns `(on_battery, percent)`. On non-Windows this is a stub that
/// reports `(false, 100)` so the battery-skip branch is never taken on dev
/// builds — the screen capture loop is Windows-only anyway.
#[cfg(target_os = "windows")]
fn read_battery_state() -> (bool, u8) {
    use windows::Win32::System::Power::{GetSystemPowerStatus, SYSTEM_POWER_STATUS};
    let mut sps = SYSTEM_POWER_STATUS::default();
    // SAFETY: pointer valid for one struct.
    let res = unsafe { GetSystemPowerStatus(&mut sps) };
    if res.is_err() {
        // On API failure assume AC + full battery — never spuriously suspend.
        return (false, 100);
    }
    // ACLineStatus: 0 = offline (on battery), 1 = online (AC), 255 = unknown.
    let on_battery = sps.ACLineStatus == 0;
    // BatteryFlag bit 128 = no battery present (desktop) → treat as full.
    let no_battery = sps.BatteryFlag & 128 != 0 || sps.BatteryFlag == 255;
    if no_battery {
        return (false, 100);
    }
    // BatteryLifePercent: 0..100, 255 = unknown → assume full.
    let percent = if sps.BatteryLifePercent <= 100 {
        sps.BatteryLifePercent
    } else {
        100
    };
    (on_battery, percent)
}

#[cfg(not(target_os = "windows"))]
fn read_battery_state() -> (bool, u8) {
    (false, 100)
}

/// Pure decision helper, fully testable: returns `Some("battery_low")` when
/// the cached reading shows we are on battery and at or below
/// [`BATTERY_LOW_PERCENT`], `None` otherwise.
pub(crate) fn battery_skip_reason(cache: BatteryCache) -> Option<&'static str> {
    if cache.on_battery && cache.percent <= BATTERY_LOW_PERCENT {
        Some("battery_low")
    } else {
        None
    }
}

/// Checks the battery skip condition with a cached 30 s poll cadence.
/// Returns `Some("battery_low")` if captures should be suspended this tick.
///
/// Safe on every platform: on non-Windows the underlying read returns
/// `(false, 100)` which can never trip the threshold.
pub(crate) fn should_skip_for_battery() -> Option<&'static str> {
    let cell = BATTERY_CACHE.get_or_init(|| Mutex::new(BatteryCache::fresh()));
    let mut guard = cell.lock().expect("battery cache poisoned");
    if guard.last_poll.elapsed() >= BATTERY_CACHE_TTL {
        let (on_battery, percent) = read_battery_state();
        guard.on_battery = on_battery;
        guard.percent    = percent;
        guard.last_poll  = Instant::now();
    }
    battery_skip_reason(*guard)
}

// ──────────────────────────────────────────────────────────────────────────────
// Game mode detection (Faz 4 #34)
// ──────────────────────────────────────────────────────────────────────────────

/// Reduced tick interval used while a full-screen exclusive application
/// (game, video player, slideshow) holds the foreground. 30 minutes.
pub(crate) const GAME_MODE_TICK_SECS: u64 = 30 * 60;

/// Pure helper testable on every platform: returns `true` if the foreground
/// window covers the entire primary monitor AND has the typical full-screen
/// exclusive style (`WS_POPUP` set, `WS_BORDER` clear). Also returns `true`
/// when the foreground process executable matches a short list of known
/// fullscreen players (`wmplayer.exe`, `vlc.exe`, `mpc-hc.exe`,
/// `powerpnt.exe` in slideshow context).
pub(crate) fn is_game_mode_for(
    win_left: i32,
    win_top: i32,
    win_right: i32,
    win_bottom: i32,
    primary_w: i32,
    primary_h: i32,
    style_popup: bool,
    style_border: bool,
    exe_name: &str,
) -> bool {
    let exe_lower = exe_name.to_lowercase();
    const KNOWN_FULLSCREEN_EXES: &[&str] = &[
        "wmplayer.exe",
        "vlc.exe",
        "mpc-hc.exe",
        "mpc-hc64.exe",
    ];
    if KNOWN_FULLSCREEN_EXES
        .iter()
        .any(|known| exe_lower.ends_with(known) || exe_lower == *known)
    {
        return true;
    }

    // PowerPoint in slideshow mode — title-driven detection happens in the
    // caller via the popup/border style heuristic since the slideshow window
    // class is `screenClass` with WS_POPUP set; falling through to the rect
    // check below catches it.

    let covers_primary = win_left <= 0
        && win_top <= 0
        && win_right >= primary_w
        && win_bottom >= primary_h;

    covers_primary && style_popup && !style_border
}

/// Detects whether the current foreground application is running in a
/// full-screen exclusive mode that should suppress aggressive screen
/// capture. Always returns `false` on non-Windows.
#[cfg(target_os = "windows")]
fn detect_game_mode() -> bool {
    use windows::Win32::Foundation::{HWND, RECT};
    use windows::Win32::UI::WindowsAndMessaging::{
        GetSystemMetrics, GetWindowLongPtrW, GetWindowRect, GWL_STYLE, SM_CXSCREEN, SM_CYSCREEN,
        WS_BORDER, WS_POPUP,
    };

    let fg = match personel_platform::input::foreground_window_info() {
        Ok(fg) if fg.hwnd != 0 => fg,
        _ => return false,
    };
    let exe = exe_name_for_pid(fg.pid);

    // SAFETY: hwnd is the foreground window we just queried; if it has been
    // destroyed in the racing instant the API returns an error and we
    // bail out without dereferencing anything.
    let hwnd = HWND(fg.hwnd as isize);
    let mut rect = RECT::default();
    let rect_ok = unsafe { GetWindowRect(hwnd, &mut rect).is_ok() };
    if !rect_ok {
        return false;
    }

    let primary_w = unsafe { GetSystemMetrics(SM_CXSCREEN) };
    let primary_h = unsafe { GetSystemMetrics(SM_CYSCREEN) };
    if primary_w <= 0 || primary_h <= 0 {
        return false;
    }

    // GetWindowLongPtrW returns 0 on failure; we treat 0 as "no style flags
    // set" which is harmless — both popup and border bits will be `false`
    // and the rect-only branch will gate the decision.
    let style_bits = unsafe { GetWindowLongPtrW(hwnd, GWL_STYLE) } as u32;
    let style_popup  = (style_bits & WS_POPUP.0) != 0;
    let style_border = (style_bits & WS_BORDER.0) != 0;

    is_game_mode_for(
        rect.left,
        rect.top,
        rect.right,
        rect.bottom,
        primary_w,
        primary_h,
        style_popup,
        style_border,
        &exe,
    )
}

#[cfg(not(target_os = "windows"))]
fn detect_game_mode() -> bool {
    false
}

/// Wrapper around [`next_tick_delay_secs`] that stretches the chosen
/// interval to [`GAME_MODE_TICK_SECS`] when a full-screen exclusive
/// application is detected and the would-be tick is shorter than that.
/// Pure so the whole branching matrix can be unit-tested.
pub(crate) fn next_tick_delay_secs_with_game_mode(
    idle_ms: u64,
    policy_active_secs: u64,
    game_mode: bool,
) -> u64 {
    let base = next_tick_delay_secs(idle_ms, policy_active_secs);
    if game_mode && base < GAME_MODE_TICK_SECS {
        GAME_MODE_TICK_SECS
    } else {
        base
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Click ring buffer (Faz 3 #28)
// ──────────────────────────────────────────────────────────────────────────────

/// Maximum number of click events kept in the ring buffer.
pub(crate) const CLICK_RING_CAPACITY: usize = 16;

/// A single left-click event captured by the `WH_MOUSE_LL` hook.
#[derive(Debug, Clone, Copy)]
pub struct ClickEvent {
    /// Screen X coordinate (virtual desktop pixels).
    pub x: i32,
    /// Screen Y coordinate.
    pub y: i32,
    /// Timestamp in **milliseconds since Unix epoch**.
    pub ts_ms: i64,
}

/// Pushes a click into a bounded ring buffer, evicting the oldest entry if
/// the buffer is already at capacity.
pub(crate) fn click_ring_push(ring: &mut VecDeque<ClickEvent>, click: ClickEvent) {
    if ring.len() >= CLICK_RING_CAPACITY {
        ring.pop_front();
    }
    ring.push_back(click);
}

/// Drains the ring buffer and returns its contents as a Vec.
pub(crate) fn click_ring_drain(ring: &mut VecDeque<ClickEvent>) -> Vec<ClickEvent> {
    ring.drain(..).collect()
}

// ──────────────────────────────────────────────────────────────────────────────
// OCR preprocessing (Faz 3 #26)
// ──────────────────────────────────────────────────────────────────────────────

/// Default for OCR preprocessing when the proto field is not wired yet.
/// Keeps Phase 1 traffic color-preserving.
pub(crate) const OCR_MODE_DEFAULT: bool = false;

/// Converts a BGRA frame to grayscale + histogram-stretched single-channel
/// bytes suitable for OCR. Returns `Vec<u8>` of length `width * height`.
///
/// Luminance formula: `Y = 0.299·R + 0.587·G + 0.114·B` (Rec. 601).
/// The histogram is then linearly stretched so the darkest luma becomes 0
/// and the brightest luma becomes 255. If the frame is flat (`min == max`)
/// the raw luminance is returned unchanged to avoid a divide-by-zero.
pub(crate) fn preprocess_for_ocr(bgra: &[u8], width: u32, height: u32) -> Vec<u8> {
    let pixel_count = (width as usize) * (height as usize);
    let mut out = Vec::with_capacity(pixel_count);

    let mut min_y: u8 = 255;
    let mut max_y: u8 = 0;

    // First pass: luminance + min/max.
    for chunk in bgra.chunks_exact(4).take(pixel_count) {
        let b = chunk[0] as f32;
        let g = chunk[1] as f32;
        let r = chunk[2] as f32;
        let y = (0.114 * b + 0.587 * g + 0.299 * r).round() as i32;
        let y = y.clamp(0, 255) as u8;
        if y < min_y { min_y = y; }
        if y > max_y { max_y = y; }
        out.push(y);
    }

    // Second pass: histogram stretch.
    if max_y > min_y {
        let span = f32::from(max_y - min_y);
        for pixel in &mut out {
            let stretched = ((f32::from(*pixel - min_y)) * 255.0 / span).round() as i32;
            *pixel = stretched.clamp(0, 255) as u8;
        }
    }

    out
}

// ──────────────────────────────────────────────────────────────────────────────
// Dedup hash (Faz 3 #25)
// ──────────────────────────────────────────────────────────────────────────────

/// SHA-256 of the raw BGRA bytes. 32 bytes. Collisions are cryptographically
/// improbable so a match is treated as a true identical-frame signal.
pub(crate) fn raw_frame_hash(bgra: &[u8]) -> [u8; 32] {
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(bgra);
    let out = h.finalize();
    let mut arr = [0u8; 32];
    arr.copy_from_slice(&out);
    arr
}

// ──────────────────────────────────────────────────────────────────────────────
// WebP encoding (Faz 3 #24)
// ──────────────────────────────────────────────────────────────────────────────

/// Encodes BGRA pixels as lossy WebP at the given quality (1–100).
/// Falls back to None on encoder failure so the caller can use JPEG.
///
/// The `webp` crate wants RGBA input so we perform the BGRA→RGBA swap in a
/// single pass. We intentionally do NOT drop the alpha channel: keeping it
/// lets the encoder use its 4-channel fast path, and alpha is always 0xFF
/// for DXGI desktop surfaces so it compresses to essentially nothing.
pub(crate) fn encode_webp_color(
    bgra: &[u8],
    width: u32,
    height: u32,
    quality: f32,
) -> Option<Vec<u8>> {
    let pixel_count = (width as usize) * (height as usize);
    if bgra.len() < pixel_count * 4 {
        return None;
    }
    let mut rgba = Vec::with_capacity(pixel_count * 4);
    for chunk in bgra.chunks_exact(4).take(pixel_count) {
        rgba.push(chunk[2]); // R
        rgba.push(chunk[1]); // G
        rgba.push(chunk[0]); // B
        rgba.push(chunk[3]); // A
    }

    // `webp::Encoder::from_rgba` panics on invalid size, so guard above.
    let encoder = webp::Encoder::from_rgba(&rgba, width, height);
    let memory = encoder.encode(quality.clamp(1.0, 100.0));
    Some(memory.to_vec())
}

/// Encodes grayscale (single-channel, one byte per pixel) as WebP by first
/// expanding to RGBA (R=G=B=Y, A=0xFF). True single-channel WebP is not
/// exposed by the `webp` crate; the expand-to-RGBA step is still ~4x
/// smaller than the source BGRA buffer.
pub(crate) fn encode_webp_grayscale(
    luma: &[u8],
    width: u32,
    height: u32,
    quality: f32,
) -> Option<Vec<u8>> {
    let pixel_count = (width as usize) * (height as usize);
    if luma.len() < pixel_count {
        return None;
    }
    let mut rgba = Vec::with_capacity(pixel_count * 4);
    for &y in luma.iter().take(pixel_count) {
        rgba.push(y);
        rgba.push(y);
        rgba.push(y);
        rgba.push(0xFF);
    }
    let encoder = webp::Encoder::from_rgba(&rgba, width, height);
    let memory = encoder.encode(quality.clamp(1.0, 100.0));
    Some(memory.to_vec())
}

// ──────────────────────────────────────────────────────────────────────────────
// PE-DEK encryption gate (Faz 3 #27, ADR 0013)
// ──────────────────────────────────────────────────────────────────────────────

/// Result of the PE-DEK gate. Either plaintext WebP or an encrypted envelope.
pub(crate) enum EncryptedPayload {
    /// Plaintext — default Phase 1 behaviour, `dlp_enabled=false`.
    Plain(Vec<u8>),
    /// AES-256-GCM ciphertext + nonce produced from the PE-DEK.
    Sealed {
        /// Ciphertext with the 16-byte GCM tag appended.
        ciphertext:  Vec<u8>,
        /// Random 12-byte GCM nonce.
        nonce:       [u8; 12],
        /// Key version; fixed to 1 until rotation is wired into the keystore.
        key_version: u32,
    },
}

/// Faz 3 #27 ADR 0013 gate: when `pe_dek.is_some()` the WebP bytes are
/// encrypted with AES-256-GCM under the per-endpoint DEK. When it is
/// `None` (default Phase 1) the plaintext passes through unchanged.
pub(crate) fn encrypt_if_dlp(
    pe_dek: Option<&personel_crypto::Aes256Key>,
    webp: Vec<u8>,
) -> EncryptedPayload {
    match pe_dek {
        None => EncryptedPayload::Plain(webp),
        Some(key) => {
            // Empty AAD: the enricher does not yet support screenshot AAD.
            // TODO (Phase 3.1): bind endpoint_id + seq via canonical AAD
            //   matching `build_keystroke_aad`.
            match personel_crypto::envelope::encrypt(key, Vec::new(), &webp) {
                Ok(env) => EncryptedPayload::Sealed {
                    ciphertext:  env.ciphertext,
                    nonce:       env.nonce,
                    key_version: 1,
                },
                Err(e) => {
                    // Crypto failure is a hard no-fail zone: we MUST NOT fall
                    // back to plaintext when DLP is enabled. Drop the frame
                    // and return an empty sealed payload so the caller knows
                    // to skip the enqueue.
                    error!(error = %e, "screen: PE-DEK encrypt failed — dropping frame");
                    EncryptedPayload::Sealed {
                        ciphertext:  Vec::new(),
                        nonce:       [0u8; 12],
                        key_version: 1,
                    }
                }
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// JSON payload shaping
// ──────────────────────────────────────────────────────────────────────────────

/// Builds the `screenshot.captured` JSON payload.
///
/// Kept as a free function so unit tests can snapshot the exact shape
/// without needing a live collector context.
#[allow(clippy::too_many_arguments)]
pub(crate) fn build_screenshot_payload(
    payload: &EncryptedPayload,
    format: &str,
    width: u32,
    height: u32,
    monitor_index: u32,
    monitor_name: &str,
    bounds_left: i32,
    bounds_top: i32,
    ocr_preprocessed: bool,
    recent_clicks: &[ClickEvent],
) -> serde_json::Value {
    let clicks_json: Vec<serde_json::Value> = recent_clicks
        .iter()
        .map(|c| {
            serde_json::json!({
                "x":     c.x,
                "y":     c.y,
                "ts_ms": c.ts_ms,
            })
        })
        .collect();

    let (dlp_enabled, data_field_name, data_hex, extra) = match payload {
        EncryptedPayload::Plain(bytes) => {
            (false, "data", hex::encode(bytes), serde_json::Value::Null)
        }
        EncryptedPayload::Sealed { ciphertext, nonce, key_version } => (
            true,
            "ciphertext",
            hex::encode(ciphertext),
            serde_json::json!({
                "nonce":             hex::encode(nonce),
                "aead_tag_appended": true,
                "key_version":       *key_version,
            }),
        ),
    };

    let mut obj = serde_json::json!({
        "format":            format,
        "width":             width,
        "height":            height,
        "monitor_index":     monitor_index,
        "monitor_name":      monitor_name,
        "bounds_left":       bounds_left,
        "bounds_top":        bounds_top,
        "ocr_preprocessed":  ocr_preprocessed,
        "dlp_enabled":       dlp_enabled,
        data_field_name:     data_hex,
        "recent_clicks":     clicks_json,
    });

    if let serde_json::Value::Object(ref mut map) = obj {
        if let serde_json::Value::Object(extra_map) = extra {
            for (k, v) in extra_map {
                map.insert(k, v);
            }
        }
    }
    obj
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

#[allow(clippy::too_many_arguments)]
fn run_capture_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    frames_deduped: Arc<AtomicU64>,
    clicks: Arc<Mutex<VecDeque<ClickEvent>>>,
    battery_suspended: Arc<AtomicBool>,
    #[allow(unused_mut)] mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    run_windows(
        ctx,
        healthy,
        events,
        drops,
        frames_deduped,
        clicks,
        battery_suspended,
        stop_rx,
    );

    #[cfg(not(target_os = "windows"))]
    {
        info!("screen collector: DXGI not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = (ctx, events, drops, frames_deduped, clicks, battery_suspended);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
#[allow(clippy::too_many_arguments)]
fn run_windows(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    frames_deduped: Arc<AtomicU64>,
    clicks: Arc<Mutex<VecDeque<ClickEvent>>>,
    battery_suspended: Arc<AtomicBool>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    info!("screen collector: starting (DXGI)");
    healthy.store(true, Ordering::Relaxed);

    // ── Multi-monitor enumeration (Faz 3 #21) ─────────────────────────────────
    let monitors = match personel_os::capture::enumerate_outputs() {
        Ok(m) => m,
        Err(e) => {
            warn!(error = %e, "screen: enumerate_outputs failed — assuming 1 monitor");
            Vec::new()
        }
    };
    if monitors.is_empty() {
        info!("screen: monitor_count unknown; capturing primary (index 0)");
    } else {
        info!(
            monitor_count = monitors.len(),
            "screen: enumerated DXGI outputs (Phase 1 captures primary only; \
             Phase 3.1 will open per-monitor duplication sessions)"
        );
        for m in &monitors {
            info!(
                index       = m.index,
                device_name = %m.device_name,
                width       = m.width,
                height      = m.height,
                bounds_left = m.bounds_left,
                bounds_top  = m.bounds_top,
                attached    = m.attached,
                "screen: monitor"
            );
        }
    }
    let primary = monitors.first().cloned();

    // ── Install mouse hook for click awareness (#28) ──────────────────────────
    let _hook_handle = match install_mouse_hook(Arc::clone(&clicks)) {
        Ok(h) => Some(h),
        Err(e) => {
            warn!(error = %e, "screen: mouse hook install failed — recent_clicks will be empty");
            None
        }
    };

    let quality = ctx.policy().screenshot.quality_percent as u8;
    let capture = match personel_os::capture::DxgiCapture::open(0, quality) {
        Ok(c) => c,
        Err(e) => {
            error!(error = %e, "screen: failed to open DXGI — will retry every 30 s");
            healthy.store(false, Ordering::Relaxed);
            loop {
                std::thread::sleep(Duration::from_secs(30));
                if stop_rx.try_recv().is_ok() {
                    return;
                }
                let q = ctx.policy().screenshot.quality_percent as u8;
                match personel_os::capture::DxgiCapture::open(0, q) {
                    Ok(c) => {
                        healthy.store(true, Ordering::Relaxed);
                        return capture_loop(
                            c,
                            ctx,
                            healthy,
                            events,
                            drops,
                            frames_deduped,
                            clicks,
                            battery_suspended,
                            primary,
                            stop_rx,
                        );
                    }
                    Err(e2) => error!(error = %e2, "screen: DXGI retry failed"),
                }
            }
        }
    };

    capture_loop(
        capture,
        ctx,
        healthy,
        events,
        drops,
        frames_deduped,
        clicks,
        battery_suspended,
        primary,
        stop_rx,
    );
}

#[cfg(target_os = "windows")]
#[allow(clippy::too_many_arguments)]
fn capture_loop(
    mut capture: personel_os::capture::DxgiCapture,
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    frames_deduped: Arc<AtomicU64>,
    clicks: Arc<Mutex<VecDeque<ClickEvent>>>,
    battery_suspended: Arc<AtomicBool>,
    primary_monitor: Option<personel_os::capture::MonitorInfo>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    use personel_core::error::AgentError;

    let mut last_frame_hash_per_monitor: HashMap<u32, [u8; 32]> = HashMap::new();
    // First-tick fires immediately (not after policy_secs) so operators can see
    // a screenshot.captured event within a few seconds of agent startup.
    let mut first_tick = true;
    let mut tick_counter: u64 = 0;

    loop {
        if stop_rx.try_recv().is_ok() {
            info!("screen collector: stop received");
            return;
        }

        // ── Adaptive interval sleep (#22) + game mode stretch (#34) ───────
        let policy = ctx.policy();
        let policy_secs = u64::from(policy.screenshot.interval_seconds).max(10);
        drop(policy);

        let idle_ms = personel_platform::input::last_input_idle_ms().unwrap_or(0);
        let game_mode = detect_game_mode();
        let delay_secs =
            next_tick_delay_secs_with_game_mode(idle_ms, policy_secs, game_mode);

        if first_tick {
            info!(
                policy_secs,
                idle_ms,
                "screen: first tick — capturing immediately (skipping initial interval sleep)"
            );
            first_tick = false;
        } else {
            if game_mode {
                debug!(
                    delay_secs,
                    "screen: game/full-screen mode detected — stretching tick"
                );
            }
            info!(
                delay_secs, idle_ms, game_mode, policy_secs,
                "screen: tick sleep starting"
            );
            for _ in 0..delay_secs {
                std::thread::sleep(Duration::from_secs(1));
                if stop_rx.try_recv().is_ok() {
                    info!("screen collector: stop received during interval sleep");
                    return;
                }
            }
        }

        tick_counter += 1;
        info!(tick = tick_counter, idle_ms, "screen: tick fired");

        // ── Sensitivity guard (#23) — KVKK m.6 / ADR 0013 anchor.
        // MUST run before the operational battery skip so a sensitive
        // foreground app is never silently bypassed by the energy heuristic.
        let policy = ctx.policy();
        let exclude_apps = policy.screenshot.exclude_apps.clone();
        drop(policy);

        if let Ok(fg) = personel_platform::input::foreground_window_info() {
            let exe = exe_name_for_pid(fg.pid);
            if should_skip_for_sensitivity(&exe, &fg.title, &exclude_apps) {
                info!(exe = %exe, title = %fg.title, "screen: skip — sensitivity guard");
                drops.fetch_add(1, Ordering::Relaxed);
                continue;
            }
            debug!(exe = %exe, title = %fg.title, "screen: sensitivity gate passed");
        }

        // ── Battery aware (#33) ───────────────────────────────────────────
        // Operational skip — runs AFTER the KVKK sensitivity guard so a
        // misconfigured battery state can never bypass the m.6 anchor.
        // Skip frames are NOT counted into `frames_deduped`; this is a
        // distinct operational category surfaced via `health().status`.
        if let Some(reason) = should_skip_for_battery() {
            if !battery_suspended.swap(true, Ordering::Relaxed) {
                info!(reason, "screen: suspending captures — battery low on battery power");
            }
            // Sleep an extra interval to avoid spinning while suspended.
            for _ in 0..30 {
                std::thread::sleep(Duration::from_secs(1));
                if stop_rx.try_recv().is_ok() {
                    return;
                }
            }
            continue;
        } else if battery_suspended.swap(false, Ordering::Relaxed) {
            info!("screen: resuming captures — AC restored or battery recovered");
        }

        // ── Grab a raw BGRA frame ─────────────────────────────────────────
        // DXGI Desktop Duplication AcquireNextFrame returns DXGI_ERROR_WAIT_TIMEOUT
        // (→ CollectorRuntime "frame timeout") if nothing on the desktop has
        // changed in the last 100 ms. On a static desktop (VM, console-mode
        // test, user AFK) this is EXTREMELY common and previously caused the
        // whole tick to be skipped for policy_secs (default 300 s!) — zero
        // events ever reached the queue.
        //
        // Fix: retry up to 20 × 250 ms = 5 s within one tick before giving up.
        // This gives the GPU compositor plenty of chances to present a frame
        // (cursor blink, clock tick, window redraw) without spinning.
        info!("screen: capture attempt (with up to 20 × 250 ms retries on frame timeout)");
        let mut frame_opt = None;
        let mut retries_used = 0u32;
        for attempt in 0..20u32 {
            match capture.capture_frame() {
                Ok(f) => {
                    frame_opt = Some(f);
                    retries_used = attempt;
                    break;
                }
                Err(AgentError::CollectorRuntime { ref reason, .. })
                    if reason.contains("frame timeout") =>
                {
                    debug!(attempt, "screen: frame timeout — retrying");
                    std::thread::sleep(Duration::from_millis(250));
                    if stop_rx.try_recv().is_ok() {
                        return;
                    }
                    continue;
                }
                Err(AgentError::CollectorRuntime { ref reason, .. })
                    if reason.contains("access lost") =>
                {
                    warn!("screen: access lost — reopening duplication output");
                    if let Err(e) = capture.reopen() {
                        error!(error = %e, "screen: reopen failed");
                        healthy.store(false, Ordering::Relaxed);
                    }
                    break;
                }
                Err(AgentError::CollectorRuntime { ref reason, .. })
                    if reason.contains("device removed") =>
                {
                    error!("screen: D3D11 device removed — sleeping 5 s then reconstructing");
                    healthy.store(false, Ordering::Relaxed);
                    std::thread::sleep(Duration::from_secs(5));
                    if stop_rx.try_recv().is_ok() {
                        return;
                    }
                    let q = ctx.policy().screenshot.quality_percent as u8;
                    match personel_os::capture::DxgiCapture::open(0, q) {
                        Ok(new_cap) => {
                            capture = new_cap;
                            healthy.store(true, Ordering::Relaxed);
                            info!("screen: DXGI reconstructed");
                        }
                        Err(e) => error!(error = %e, "screen: DXGI reconstruction failed"),
                    }
                    break;
                }
                Err(e) => {
                    error!(error = %e, "screen: unexpected capture error");
                    healthy.store(false, Ordering::Relaxed);
                    break;
                }
            }
        }
        let frame = match frame_opt {
            Some(f) => {
                info!(
                    retries = retries_used,
                    width = f.width,
                    height = f.height,
                    monitor = f.monitor_index,
                    "screen: captured frame"
                );
                f
            }
            None => {
                info!("screen: capture abandoned this tick (timeouts exhausted or transient error) — will retry next tick");
                drops.fetch_add(1, Ordering::Relaxed);
                continue;
            }
        };

        // ── Dedup hash (#25) ──────────────────────────────────────────────
        let hash = raw_frame_hash(&frame.pixels);
        let monitor_idx = frame.monitor_index;
        if let Some(prev) = last_frame_hash_per_monitor.get(&monitor_idx) {
            if *prev == hash {
                info!(monitor_idx, "screen: identical frame — skipping (dedup)");
                frames_deduped.fetch_add(1, Ordering::Relaxed);
                continue;
            }
        }
        last_frame_hash_per_monitor.insert(monitor_idx, hash);
        info!(monitor_idx, bytes = frame.pixels.len(), "screen: frame accepted, encoding");

        // ── Optional OCR preprocess (#26) + WebP encode (#24) ─────────────
        let quality = ctx.policy().screenshot.quality_percent as f32;
        let (encoded_opt, format, ocr_pre) = if OCR_MODE_DEFAULT {
            let luma = preprocess_for_ocr(&frame.pixels, frame.width, frame.height);
            (encode_webp_grayscale(&luma, frame.width, frame.height, quality), "webp-gray", true)
        } else {
            (encode_webp_color(&frame.pixels, frame.width, frame.height, quality), "webp", false)
        };

        let (final_bytes, final_format) = match encoded_opt {
            Some(bytes) => (bytes, format),
            None => {
                // WebP failed — fall back to JPEG via the existing encoder.
                warn!("screen: WebP encode failed — falling back to JPEG");
                match personel_os::capture::DxgiCapture::encode_jpeg(
                    &frame.pixels,
                    frame.width,
                    frame.height,
                    quality.clamp(1.0, 100.0) as u8,
                ) {
                    Ok(jpeg) => (jpeg, "jpeg"),
                    Err(e) => {
                        error!(error = %e, "screen: JPEG fallback failed — dropping frame");
                        drops.fetch_add(1, Ordering::Relaxed);
                        continue;
                    }
                }
            }
        };

        // ── PE-DEK gate (#27, ADR 0013) ───────────────────────────────────
        let payload = encrypt_if_dlp(ctx.pe_dek.as_deref(), final_bytes);

        // Refuse to enqueue an empty sealed payload (crypto fail path).
        if let EncryptedPayload::Sealed { ref ciphertext, .. } = payload {
            if ciphertext.is_empty() {
                drops.fetch_add(1, Ordering::Relaxed);
                continue;
            }
        }

        // ── Click snapshot (#28) + payload assembly ───────────────────────
        let recent_clicks = {
            let mut ring = clicks.lock().expect("click ring buffer poisoned");
            click_ring_drain(&mut ring)
        };

        let (monitor_name, bounds_left, bounds_top) = match &primary_monitor {
            Some(m) => (m.device_name.clone(), m.bounds_left, m.bounds_top),
            None => (String::new(), 0, 0),
        };

        let json = build_screenshot_payload(
            &payload,
            final_format,
            frame.width,
            frame.height,
            monitor_idx,
            &monitor_name,
            bounds_left,
            bounds_top,
            ocr_pre,
            &recent_clicks,
        );

        enqueue_screenshot(&ctx, json.to_string().into_bytes(), &events);
        healthy.store(true, Ordering::Relaxed);
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Queue helpers
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
fn enqueue_screenshot(ctx: &CollectorCtx, payload: Vec<u8>, events: &Arc<AtomicU64>) {
    let now = ctx.clock.now_unix_nanos();
    let id  = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::ScreenshotCaptured.as_str(),
        Priority::Normal,
        now,
        now,
        &payload,
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
            info!(bytes = payload.len(), "screen: screenshot enqueued");
        }
        Err(e) => {
            warn!(error = %e, "screen: queue error — dropping screenshot");
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Process helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Returns the base executable name for a PID, or an empty string.
#[cfg(target_os = "windows")]
fn exe_name_for_pid(pid: u32) -> String {
    if pid == 0 {
        return String::new();
    }
    use sysinfo::{Pid, System};
    let mut sys = System::new();
    sys.refresh_processes();
    sys.process(Pid::from_u32(pid))
        .and_then(|p| p.exe())
        .and_then(|p| p.file_name())
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default()
}

// ──────────────────────────────────────────────────────────────────────────────
// Mouse hook (Faz 3 #28)
// ──────────────────────────────────────────────────────────────────────────────

/// Guard returned by [`install_mouse_hook`]. Dropping it signals the hook
/// thread to exit via a `PostThreadMessageW(WM_QUIT)`.
#[cfg(target_os = "windows")]
pub(crate) struct MouseHookGuard {
    thread_id: u32,
    join:      Option<std::thread::JoinHandle<()>>,
}

#[cfg(target_os = "windows")]
impl Drop for MouseHookGuard {
    fn drop(&mut self) {
        use windows::Win32::UI::WindowsAndMessaging::{PostThreadMessageW, WM_QUIT};
        use windows::Win32::Foundation::{LPARAM, WPARAM};
        if self.thread_id != 0 {
            unsafe {
                let _ = PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0));
            }
        }
        if let Some(h) = self.join.take() {
            let _ = h.join();
        }
    }
}

/// Installs a `WH_MOUSE_LL` hook on a dedicated OS thread and pushes
/// left-click events into the shared ring buffer.
///
/// The hook thread owns a thread-local `static` that holds an `Arc` clone
/// of the ring buffer so the C callback can find it without parameter
/// passing. Writes are guarded by a standard Mutex; the callback is already
/// serialised by the OS (one low-level hook thread per process) so
/// contention is trivial.
#[cfg(target_os = "windows")]
fn install_mouse_hook(
    ring: Arc<Mutex<VecDeque<ClickEvent>>>,
) -> Result<MouseHookGuard> {
    use std::cell::RefCell;
    use std::sync::mpsc;
    use windows::Win32::Foundation::{LPARAM, LRESULT, WPARAM};
    use windows::Win32::System::Threading::GetCurrentThreadId;
    use windows::Win32::UI::WindowsAndMessaging::{
        CallNextHookEx, DispatchMessageW, GetMessageW, SetWindowsHookExW, TranslateMessage,
        UnhookWindowsHookEx, HHOOK, MSG, MSLLHOOKSTRUCT, WH_MOUSE_LL, WM_LBUTTONDOWN,
    };

    thread_local! {
        static RING_TLS: RefCell<Option<Arc<Mutex<VecDeque<ClickEvent>>>>> =
            const { RefCell::new(None) };
        static HOOK_TLS: RefCell<HHOOK> = const { RefCell::new(HHOOK(0)) };
    }

    unsafe extern "system" fn mouse_hook_proc(
        ncode: i32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        // HC_ACTION == 0. For any negative ncode we must CallNextHookEx.
        if ncode >= 0 && wparam.0 as u32 == WM_LBUTTONDOWN {
            // lparam is MSLLHOOKSTRUCT*.
            let info = lparam.0 as *const MSLLHOOKSTRUCT;
            if !info.is_null() {
                let pt_x = (*info).pt.x;
                let pt_y = (*info).pt.y;
                let ts_ms = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map(|d| d.as_millis() as i64)
                    .unwrap_or(0);
                let click = ClickEvent { x: pt_x, y: pt_y, ts_ms };
                RING_TLS.with(|cell| {
                    if let Some(ring_arc) = cell.borrow().as_ref() {
                        if let Ok(mut r) = ring_arc.lock() {
                            click_ring_push(&mut r, click);
                        }
                    }
                });
            }
        }
        // Always chain.
        let h = HOOK_TLS.with(|cell| *cell.borrow());
        CallNextHookEx(h, ncode, wparam, lparam)
    }

    let (ready_tx, ready_rx) = mpsc::sync_channel::<std::result::Result<u32, String>>(1);
    let join = std::thread::Builder::new()
        .name("personel-screen-mousehook".into())
        .spawn(move || {
            // SAFETY: entire body runs on a dedicated OS thread. SetWindowsHookEx
            // requires a message pump on the same thread, which this provides.
            // The callback only reads pointer-sized fields of MSLLHOOKSTRUCT
            // (public C-ABI struct) and pushes into a safely-wrapped Mutex.
            unsafe {
                let tid = GetCurrentThreadId();
                RING_TLS.with(|cell| *cell.borrow_mut() = Some(ring));
                let hook = match SetWindowsHookExW(WH_MOUSE_LL, Some(mouse_hook_proc), None, 0) {
                    Ok(h) => h,
                    Err(e) => {
                        let _ = ready_tx.send(Err(format!("SetWindowsHookExW failed: {e}")));
                        RING_TLS.with(|cell| *cell.borrow_mut() = None);
                        return;
                    }
                };
                HOOK_TLS.with(|cell| *cell.borrow_mut() = hook);
                let _ = ready_tx.send(Ok(tid));

                // Message pump — blocks until WM_QUIT is posted by the guard.
                let mut msg = MSG::default();
                while GetMessageW(&mut msg, None, 0, 0).as_bool() {
                    let _ = TranslateMessage(&msg);
                    DispatchMessageW(&msg);
                }

                let _ = UnhookWindowsHookEx(hook);
                HOOK_TLS.with(|cell| *cell.borrow_mut() = HHOOK(0));
                RING_TLS.with(|cell| *cell.borrow_mut() = None);
            }
        })
        .map_err(|e| personel_core::error::AgentError::CollectorStart {
            name: "screen",
            reason: format!("spawn mouse hook thread: {e}"),
        })?;

    let tid = ready_rx
        .recv_timeout(std::time::Duration::from_secs(2))
        .map_err(|_| personel_core::error::AgentError::CollectorStart {
            name:   "screen",
            reason: "mouse hook ready timeout".into(),
        })?
        .map_err(|e| personel_core::error::AgentError::CollectorStart {
            name:   "screen",
            reason: e,
        })?;

    Ok(MouseHookGuard { thread_id: tid, join: Some(join) })
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests (cross-platform — all helpers above are pure)
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    // ── #23 sensitivity guard ────────────────────────────────────────────

    #[test]
    fn sensitivity_skips_hardcoded_password_manager() {
        assert!(should_skip_for_sensitivity("KeePass.exe", "Database.kdbx", &[]));
        assert!(should_skip_for_sensitivity("keepassxc.exe", "", &[]));
        assert!(should_skip_for_sensitivity("1Password.exe", "", &[]));
    }

    #[test]
    fn sensitivity_skips_full_path_suffix() {
        assert!(should_skip_for_sensitivity(
            "C:\\Users\\kartal\\AppData\\Local\\Bitwarden\\bitwarden.exe",
            "",
            &[],
        ));
    }

    #[test]
    fn sensitivity_skips_rdp_client() {
        assert!(should_skip_for_sensitivity("mstsc.exe", "Remote Desktop Connection", &[]));
    }

    #[test]
    fn sensitivity_skips_incognito_titles() {
        assert!(should_skip_for_sensitivity("chrome.exe", "Acme — Incognito", &[]));
        assert!(should_skip_for_sensitivity("msedge.exe", "New InPrivate Window - Edge", &[]));
        assert!(should_skip_for_sensitivity("firefox.exe", "Example (Private Browsing)", &[]));
        assert!(should_skip_for_sensitivity("firefox.exe", "Örnek - Gizli Pencere", &[]));
        assert!(should_skip_for_sensitivity("anything.exe", "Scan the 2fa code", &[]));
    }

    #[test]
    fn sensitivity_respects_policy_excludes() {
        let policy = vec!["banking.exe".to_string(), "hr-portal".to_string()];
        assert!(should_skip_for_sensitivity("QnB_Banking.exe", "", &policy));
        assert!(should_skip_for_sensitivity("chrome.exe", "Acme HR-Portal", &policy));
    }

    #[test]
    fn sensitivity_allows_normal_foreground() {
        assert!(!should_skip_for_sensitivity("chrome.exe", "GitHub - Acme/Repo", &[]));
        assert!(!should_skip_for_sensitivity("notepad.exe", "Untitled - Notepad", &[]));
    }

    #[test]
    fn sensitivity_empty_policy_does_not_match_empty_needle() {
        // Safety check: empty policy entry must not trigger a universal skip.
        let policy = vec![String::new()];
        assert!(!should_skip_for_sensitivity("chrome.exe", "x", &policy));
    }

    // ── #22 adaptive frequency ───────────────────────────────────────────

    #[test]
    fn adaptive_idle_uses_short_tick() {
        assert_eq!(next_tick_delay_secs(120_000, 300), IDLE_TICK_SECS);
        assert_eq!(next_tick_delay_secs(60_001, 300), IDLE_TICK_SECS);
    }

    #[test]
    fn adaptive_active_uses_policy_interval() {
        assert_eq!(next_tick_delay_secs(0, 300), 300);
        assert_eq!(next_tick_delay_secs(59_000, 180), 180);
    }

    #[test]
    fn adaptive_enforces_minimum_10s() {
        assert_eq!(next_tick_delay_secs(0, 1), 10);
        assert_eq!(next_tick_delay_secs(0, 9), 10);
    }

    // ── #33 battery aware ────────────────────────────────────────────────

    fn mk_battery_cache(on_battery: bool, percent: u8) -> BatteryCache {
        BatteryCache {
            on_battery,
            percent,
            last_poll: Instant::now(),
        }
    }

    #[test]
    fn battery_skip_only_when_on_battery_and_low() {
        // On AC at any percent → no skip.
        assert_eq!(battery_skip_reason(mk_battery_cache(false, 5)), None);
        assert_eq!(battery_skip_reason(mk_battery_cache(false, 100)), None);
        // On battery above threshold → no skip.
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 50)), None);
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 21)), None);
    }

    #[test]
    fn battery_skip_at_threshold_boundary() {
        // 20% on battery is the inclusive boundary.
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 20)), Some("battery_low"));
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 19)), Some("battery_low"));
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 1)),  Some("battery_low"));
        assert_eq!(battery_skip_reason(mk_battery_cache(true, 0)),  Some("battery_low"));
    }

    #[test]
    fn battery_skip_runtime_helper_is_safe_on_all_platforms() {
        // On non-Windows the cached read is (false, 100) and must never trip.
        // On Windows hitting GetSystemPowerStatus on a CI machine is also fine.
        // Either way the call must not panic.
        let _ = should_skip_for_battery();
    }

    // ── #34 game mode detection ──────────────────────────────────────────

    #[test]
    fn game_mode_true_for_fullscreen_popup_no_border() {
        // 1920x1080 popup window with no border, exact primary coverage.
        assert!(is_game_mode_for(0, 0, 1920, 1080, 1920, 1080, true, false, "game.exe"));
    }

    #[test]
    fn game_mode_true_when_window_extends_past_primary() {
        // Some games over-extend the rect by 1px on each side.
        assert!(is_game_mode_for(-1, -1, 1921, 1081, 1920, 1080, true, false, "game.exe"));
    }

    #[test]
    fn game_mode_false_for_normal_windowed_app() {
        // Centred 800x600 normal window with border on a 1920x1080 monitor.
        assert!(!is_game_mode_for(560, 240, 1360, 840, 1920, 1080, false, true, "notepad.exe"));
    }

    #[test]
    fn game_mode_false_for_maximized_with_border() {
        // Maximised but still has WS_BORDER → not exclusive fullscreen.
        assert!(!is_game_mode_for(0, 0, 1920, 1080, 1920, 1080, true, true, "chrome.exe"));
    }

    #[test]
    fn game_mode_false_for_popup_smaller_than_primary() {
        // Popup style + no border but only covers half the screen.
        assert!(!is_game_mode_for(0, 0, 960, 540, 1920, 1080, true, false, "tooltip.exe"));
    }

    #[test]
    fn game_mode_true_for_known_fullscreen_player_regardless_of_rect() {
        assert!(is_game_mode_for(100, 100, 200, 200, 1920, 1080, false, true, "vlc.exe"));
        assert!(is_game_mode_for(100, 100, 200, 200, 1920, 1080, false, true, "C:\\PROGRA~1\\VLC\\vlc.exe"));
        assert!(is_game_mode_for(0, 0, 0, 0, 1920, 1080, false, false, "wmplayer.exe"));
        assert!(is_game_mode_for(0, 0, 0, 0, 1920, 1080, false, false, "mpc-hc64.exe"));
    }

    #[test]
    fn game_mode_tick_stretches_to_30min() {
        // Active, normal policy 5 min → would tick at 300, game mode → 1800.
        assert_eq!(
            next_tick_delay_secs_with_game_mode(0, 300, true),
            GAME_MODE_TICK_SECS
        );
        // Idle, would tick at 30 → game mode also stretches to 1800.
        assert_eq!(
            next_tick_delay_secs_with_game_mode(120_000, 300, true),
            GAME_MODE_TICK_SECS
        );
    }

    #[test]
    fn game_mode_tick_passthrough_when_off() {
        // Game mode off → behave exactly like next_tick_delay_secs.
        assert_eq!(next_tick_delay_secs_with_game_mode(0, 300, false), 300);
        assert_eq!(next_tick_delay_secs_with_game_mode(120_000, 300, false), IDLE_TICK_SECS);
    }

    #[test]
    fn game_mode_tick_does_not_shorten_longer_intervals() {
        // If the policy already specifies a longer interval (e.g. 1 hour),
        // game mode must NOT shorten it.
        assert_eq!(
            next_tick_delay_secs_with_game_mode(0, 7200, true),
            7200
        );
    }

    // ── #28 click ring buffer ────────────────────────────────────────────

    fn mk_click(i: i32) -> ClickEvent {
        ClickEvent { x: i, y: i * 2, ts_ms: i64::from(i) * 1_000 }
    }

    #[test]
    fn click_ring_push_within_capacity() {
        let mut ring = VecDeque::new();
        for i in 0..4 {
            click_ring_push(&mut ring, mk_click(i));
        }
        assert_eq!(ring.len(), 4);
        let drained = click_ring_drain(&mut ring);
        assert_eq!(drained.len(), 4);
        assert_eq!(drained[0].x, 0);
        assert_eq!(drained[3].x, 3);
        assert!(ring.is_empty());
    }

    #[test]
    fn click_ring_evicts_oldest_at_capacity() {
        let mut ring = VecDeque::new();
        for i in 0..(CLICK_RING_CAPACITY as i32 + 4) {
            click_ring_push(&mut ring, mk_click(i));
        }
        assert_eq!(ring.len(), CLICK_RING_CAPACITY);
        // First entry must now be click #4 (0..3 evicted).
        assert_eq!(ring.front().unwrap().x, 4);
        assert_eq!(
            ring.back().unwrap().x,
            CLICK_RING_CAPACITY as i32 + 3,
        );
    }

    // ── #25 dedup hash ───────────────────────────────────────────────────

    #[test]
    fn dedup_hash_equal_for_identical_frames() {
        let a = vec![0x42u8; 1024];
        let b = a.clone();
        assert_eq!(raw_frame_hash(&a), raw_frame_hash(&b));
    }

    #[test]
    fn dedup_hash_differs_on_single_byte_change() {
        let a = vec![0x42u8; 1024];
        let mut b = a.clone();
        b[500] ^= 0xFF;
        assert_ne!(raw_frame_hash(&a), raw_frame_hash(&b));
    }

    // ── #26 OCR preprocess ───────────────────────────────────────────────

    #[test]
    fn preprocess_flat_frame_returns_unchanged_luma() {
        // BGRA = (100, 100, 100, 255) — flat mid-gray.
        let frame: Vec<u8> = (0..16).flat_map(|_| [100, 100, 100, 255]).collect();
        let out = preprocess_for_ocr(&frame, 4, 4);
        assert_eq!(out.len(), 16);
        // Luminance: 0.299*100 + 0.587*100 + 0.114*100 = 100.
        assert!(out.iter().all(|&p| p == 100));
    }

    #[test]
    fn preprocess_histogram_stretch_expands_range() {
        // Two halves: dark (0,0,0,255) + bright (200,200,200,255).
        let mut frame = Vec::with_capacity(64);
        for _ in 0..8 { frame.extend_from_slice(&[0, 0, 0, 255]); }
        for _ in 0..8 { frame.extend_from_slice(&[200, 200, 200, 255]); }
        let out = preprocess_for_ocr(&frame, 4, 4);
        assert_eq!(out.len(), 16);
        // First 8 pixels must stretch to 0, last 8 to 255.
        assert!(out.iter().take(8).all(|&p| p == 0));
        assert!(out.iter().skip(8).all(|&p| p == 255));
    }

    #[test]
    fn preprocess_uses_luminance_weights() {
        // Pure red (BGRA = 0,0,255,255). Y = 0.299*255 = 76.245.
        let frame: Vec<u8> = (0..4).flat_map(|_| [0, 0, 255, 255]).collect();
        let out = preprocess_for_ocr(&frame, 2, 2);
        // Flat → histogram stretch noop → luma ≈ 76.
        assert_eq!(out.len(), 4);
        assert!(out.iter().all(|&p| (75..=77).contains(&p)));
    }

    // ── #27 PE-DEK gate ──────────────────────────────────────────────────

    #[test]
    fn pe_dek_gating_chooses_plain_when_none() {
        let payload = encrypt_if_dlp(None, b"hello".to_vec());
        match payload {
            EncryptedPayload::Plain(bytes) => assert_eq!(bytes, b"hello"),
            EncryptedPayload::Sealed { .. } => panic!("expected Plain"),
        }
    }

    #[test]
    fn pe_dek_gating_chooses_correct_branch() {
        use zeroize::Zeroizing;
        let key: personel_crypto::Aes256Key = Zeroizing::new([0xABu8; 32]);
        let payload = encrypt_if_dlp(Some(&key), b"secret-webp".to_vec());
        match payload {
            EncryptedPayload::Sealed { ciphertext, nonce, key_version } => {
                assert_ne!(ciphertext, b"secret-webp");
                assert_eq!(ciphertext.len(), b"secret-webp".len() + 16);
                assert_ne!(nonce, [0u8; 12]);
                assert_eq!(key_version, 1);
            }
            EncryptedPayload::Plain(_) => panic!("expected Sealed when pe_dek is Some"),
        }
    }

    // ── payload shape snapshot ───────────────────────────────────────────

    #[test]
    fn payload_shape_plain_webp() {
        let p = EncryptedPayload::Plain(vec![0xAA, 0xBB]);
        let json = build_screenshot_payload(
            &p, "webp", 1920, 1080, 0, "\\\\.\\DISPLAY1", 0, 0, false, &[],
        );
        assert_eq!(json["format"], "webp");
        assert_eq!(json["width"], 1920);
        assert_eq!(json["height"], 1080);
        assert_eq!(json["monitor_index"], 0);
        assert_eq!(json["dlp_enabled"], false);
        assert_eq!(json["data"], "aabb");
        assert!(json.get("ciphertext").is_none());
        assert_eq!(json["recent_clicks"].as_array().unwrap().len(), 0);
    }

    #[test]
    fn payload_shape_sealed_webp() {
        let p = EncryptedPayload::Sealed {
            ciphertext:  vec![0x11, 0x22, 0x33],
            nonce:       [1; 12],
            key_version: 1,
        };
        let click = ClickEvent { x: 100, y: 200, ts_ms: 1_700_000_000_000 };
        let json = build_screenshot_payload(
            &p, "webp", 800, 600, 1, "\\\\.\\DISPLAY2", -800, 0, false, &[click],
        );
        assert_eq!(json["format"], "webp");
        assert_eq!(json["dlp_enabled"], true);
        assert_eq!(json["ciphertext"], "112233");
        assert!(json.get("data").is_none());
        assert_eq!(json["aead_tag_appended"], true);
        assert_eq!(json["key_version"], 1);
        assert_eq!(json["nonce"].as_str().unwrap().len(), 24);
        assert_eq!(json["bounds_left"], -800);
        let clicks_arr = json["recent_clicks"].as_array().unwrap();
        assert_eq!(clicks_arr.len(), 1);
        assert_eq!(clicks_arr[0]["x"], 100);
        assert_eq!(clicks_arr[0]["ts_ms"], 1_700_000_000_000_i64);
    }

    // ── #24 WebP encoder smoke ───────────────────────────────────────────

    #[test]
    fn webp_color_encodes_and_is_smaller_than_source() {
        // 32x32 BGRA solid blue frame — highly compressible.
        let pixel_count = 32 * 32;
        let src: Vec<u8> = (0..pixel_count).flat_map(|_| [0xFF, 0, 0, 0xFF]).collect();
        let encoded = encode_webp_color(&src, 32, 32, 75.0).expect("encode");
        assert!(!encoded.is_empty(), "non-empty WebP output");
        assert!(encoded.len() < src.len(), "solid blue must compress smaller than raw BGRA");
    }

    #[test]
    fn webp_grayscale_encodes() {
        let src: Vec<u8> = vec![128u8; 32 * 32];
        let encoded = encode_webp_grayscale(&src, 32, 32, 75.0).expect("encode");
        assert!(!encoded.is_empty());
    }
}
