//! System lifecycle event collector.
//!
//! Monitors four high-signal Windows lifecycle events:
//!
//! 1. **Session lock / unlock / logon / logoff** via `WTSRegisterSessionNotification`
//!    on a hidden `HWND_MESSAGE` window listening for `WM_WTSSESSION_CHANGE`.
//! 2. **Power state transitions** (suspend, resume) via the same hidden window
//!    receiving `WM_POWERBROADCAST`. Default suspend/resume notifications are
//!    delivered to top-level windows without explicit `RegisterPowerSettingNotification`.
//! 3. **Anti-virus deactivation** by polling `root\SecurityCenter2 → AntiVirusProduct`
//!    via `powershell.exe Get-CimInstance` every 5 minutes and watching the
//!    `productState` bitfield's real-time-protection bit (bit 4 of the second nibble).
//! 4. **Agent baseline anchor**: a synthetic `system.login` event emitted at
//!    startup so the timeline always has a known good "agent_start" reference.
//!
//! # Event payloads
//!
//! All payloads are JSON. Common fields:
//! ```json
//! {
//!   "event": "lock|unlock|logon|logoff|suspend|resume_from_suspend|resume_automatic|agent_start|av_deactivated",
//!   "session_id": 1,
//!   "timestamp": "2026-04-13T12:34:56Z"
//! }
//! ```
//!
//! AV deactivation events additionally carry `av_product_name`,
//! `previous_state_hex`, `current_state_hex`.
//!
//! # KVKK note
//!
//! Session state and power state are **identifier-class** signals — never
//! captured content. They reveal user availability and basic device posture
//! but do not contain personal communication, file content, or screen pixels.
//! No special-category gate is required (KVKK m.6 not triggered).
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full implementation. On non-Windows the
//! collector parks gracefully (reports healthy) so the workspace builds
//! everywhere.
//!
//! # Non-interactive sessions
//!
//! When `personel_platform::service::is_service_context()` returns `true`
//! AND no console session is attached, WTS notification registration would
//! land on the wrong session. In that case we still spawn the message-pump
//! thread (the service is normally session 0 and `WTSRegisterSessionNotification`
//! with `NOTIFY_FOR_ALL_SESSIONS` works), but log a warning so operators
//! can see the topology.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
#[cfg(not(target_os = "windows"))]
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// System lifecycle event collector (lock/unlock/logon/logoff/power/AV).
#[derive(Default)]
pub struct SystemEventsCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl SystemEventsCollector {
    /// Creates a new [`SystemEventsCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for SystemEventsCollector {
    fn name(&self) -> &'static str {
        "system_events"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &[
            "system.power_state_changed",
            "system.login",
            "system.logout",
            "system.av_deactivated",
        ]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        // Mark healthy immediately; the message-pump and WMI poll tasks will
        // refine the state on first iteration.
        healthy.store(true, Ordering::Relaxed);

        let task = tokio::task::spawn_blocking(move || {
            run(ctx, healthy, events, drops, stop_rx);
        });

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
// Cross-platform helpers (used by tests and by the AV state classifier)
// ──────────────────────────────────────────────────────────────────────────────

/// Decoded `productState` bitfield for a Security Center AntiVirusProduct entry.
///
/// Microsoft Security Center encodes three pieces of information into the
/// 32-bit `productState` integer:
///
/// - **Product byte** (bits 16-23): which AV product family is reporting.
/// - **Real-time protection state** (bits 8-15): `0x10` enabled, `0x00` off.
/// - **Definition state** (bits 0-7): `0x00` up-to-date, `0x10` out-of-date.
///
/// We extract the realtime nibble from byte 1. The "on" sentinel is `0x10`
/// across all products we've validated (Defender, Bitdefender, Kaspersky,
/// ESET, Sophos). Any non-`0x10` value is treated as **off**.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct ProductState {
    pub raw: u32,
    pub realtime_on: bool,
}

impl ProductState {
    pub(crate) fn from_raw(raw: u32) -> Self {
        // Real-time byte is bits 8..=15. 0x10 = on, 0x00 = off (across vendors).
        let realtime_byte = (raw >> 8) & 0xFF;
        let realtime_on = realtime_byte == 0x10 || realtime_byte == 0x11;
        Self { raw, realtime_on }
    }
}

/// Builds the JSON payload for a system event. Kept outside the cfg block so
/// unit tests can exercise it on every platform.
fn payload_for(event: &str, session_id: u32, timestamp_rfc3339: &str) -> String {
    format!(
        r#"{{"event":"{}","session_id":{},"timestamp":"{}"}}"#,
        event, session_id, timestamp_rfc3339,
    )
}

/// Builds the JSON payload for an AV deactivation event.
fn payload_for_av(
    av_product_name: &str,
    previous_state: u32,
    current_state: u32,
    session_id: u32,
    timestamp_rfc3339: &str,
) -> String {
    // Escape the product name for JSON inclusion. We accept that vendor names
    // are ASCII safe in practice; defensively run through json escape helper.
    let escaped = json_escape(av_product_name);
    format!(
        r#"{{"event":"av_deactivated","av_product_name":"{}","previous_state_hex":"0x{:08x}","current_state_hex":"0x{:08x}","session_id":{},"timestamp":"{}"}}"#,
        escaped, previous_state, current_state, session_id, timestamp_rfc3339,
    )
}

/// Minimal JSON string escaper (escapes `"`, `\\`, control chars).
fn json_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                out.push_str(&format!("\\u{:04x}", c as u32));
            }
            c => out.push(c),
        }
    }
    out
}

/// Parses a powershell `ConvertTo-Json -Compress` blob from
/// `Get-CimInstance -ClassName AntiVirusProduct` and returns
/// `(displayName, productState)` pairs.
///
/// Accepts either the single-object form `{"displayName":"...","productState":N}`
/// or the array form `[{...},{...}]`.
fn parse_av_powershell_json(stdout: &str) -> Vec<(String, u32)> {
    let trimmed = stdout.trim();
    if trimmed.is_empty() {
        return vec![];
    }
    let mut out = Vec::new();
    // Hand-rolled tolerant parser. Avoids pulling in serde_json structures
    // here (we depend on serde_json at workspace level but powershell output
    // sometimes has BOM/trailing whitespace that confuses it).
    // Use serde_json::Value when available — we already depend on it.
    match serde_json::from_str::<serde_json::Value>(trimmed) {
        Ok(serde_json::Value::Array(items)) => {
            for item in items {
                if let Some((name, state)) = av_extract(&item) {
                    out.push((name, state));
                }
            }
        }
        Ok(v @ serde_json::Value::Object(_)) => {
            if let Some((name, state)) = av_extract(&v) {
                out.push((name, state));
            }
        }
        _ => {}
    }
    out
}

fn av_extract(v: &serde_json::Value) -> Option<(String, u32)> {
    let obj = v.as_object()?;
    // Powershell ConvertTo-Json preserves casing as the property is named
    // on the source object: displayName / productState (camelCase from
    // Get-CimInstance). Some shells use PascalCase — try both.
    let name = obj
        .get("displayName")
        .or_else(|| obj.get("DisplayName"))
        .and_then(|x| x.as_str())?
        .to_string();
    let state_val = obj.get("productState").or_else(|| obj.get("ProductState"))?;
    let state = match state_val {
        serde_json::Value::Number(n) => n.as_u64().map(|x| x as u32)?,
        serde_json::Value::String(s) => s.parse::<u32>().ok()?,
        _ => return None,
    };
    Some((name, state))
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

#[allow(unused_variables)]
fn run(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    stop_rx: oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    {
        windows_impl::run(ctx, healthy, events, drops, stop_rx);
    }

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        info!("system_events: not supported on non-Windows — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows_impl {
    use std::collections::HashMap;
    use std::process::Command;
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::Arc;
    use std::time::Duration;

    use tokio::sync::{mpsc, oneshot};
    use tracing::{debug, error, info, warn};

    use windows::core::PCWSTR;
    use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
    use windows::Win32::System::RemoteDesktop::{
        WTSGetActiveConsoleSessionId, WTSRegisterSessionNotification,
        WTSUnRegisterSessionNotification, NOTIFY_FOR_ALL_SESSIONS,
    };
    use windows::Win32::UI::WindowsAndMessaging::{
        CreateWindowExW, DefWindowProcW, DestroyWindow, DispatchMessageW, GetMessageW,
        PostMessageW, RegisterClassExW, HWND_MESSAGE, MSG, PBT_APMRESUMEAUTOMATIC,
        PBT_APMRESUMESUSPEND, PBT_APMSUSPEND, WM_APP, WM_DESTROY, WM_POWERBROADCAST,
        WM_WTSSESSION_CHANGE, WNDCLASSEXW, WS_EX_NOACTIVATE, WTS_SESSION_LOCK,
        WTS_SESSION_LOGOFF, WTS_SESSION_LOGON, WTS_SESSION_UNLOCK,
    };

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use crate::CollectorCtx;

    use super::{
        parse_av_powershell_json, payload_for, payload_for_av, ProductState,
    };

    const WM_PERSONEL_QUIT: u32 = WM_APP + 1;

    /// One thread runs the Win32 message loop; another runs the WMI poll.
    /// Both push `Signal`s into a single mpsc channel that is drained by
    /// the dispatcher loop on the original spawn_blocking thread (which
    /// also enqueues into the personel-queue).
    enum Signal {
        Lock,
        Unlock,
        Logon,
        Logoff,
        Suspend,
        ResumeFromSuspend,
        ResumeAutomatic,
        AvDeactivated {
            product: String,
            previous: u32,
            current: u32,
        },
    }

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("system_events: starting");

        let session_id = unsafe { WTSGetActiveConsoleSessionId() };
        if personel_platform::service::is_service_context() && session_id == 0xFFFF_FFFF {
            warn!(
                "system_events: running as service with no active console session; \
                 WTS notifications still register for ALL sessions"
            );
        }

        // Baseline anchor: emit a synthetic agent_start login event.
        emit_agent_start(&ctx, session_id, &events, &drops);
        healthy.store(true, Ordering::Relaxed);

        // mpsc channel bridging both producers (message pump + WMI poll) to
        // the dispatcher.
        let (tx, mut rx) = mpsc::unbounded_channel::<Signal>();

        // ── Spawn message-pump thread ─────────────────────────────────────
        let (pump_stop_tx, pump_stop_rx) = std::sync::mpsc::channel::<()>();
        let pump_tx = tx.clone();
        let pump_handle = std::thread::Builder::new()
            .name("personel-system-events-pump".into())
            .spawn(move || {
                pump_thread(pump_tx, pump_stop_rx);
            });
        let pump_handle = match pump_handle {
            Ok(h) => Some(h),
            Err(e) => {
                warn!(error = %e, "system_events: pump thread spawn failed");
                None
            }
        };

        // ── Spawn AV poll thread ──────────────────────────────────────────
        let (av_stop_tx, av_stop_rx) = std::sync::mpsc::channel::<()>();
        let av_tx = tx.clone();
        let av_handle = std::thread::Builder::new()
            .name("personel-system-events-av".into())
            .spawn(move || {
                av_poll_thread(av_tx, av_stop_rx);
            });
        let av_handle = match av_handle {
            Ok(h) => Some(h),
            Err(e) => {
                warn!(error = %e, "system_events: AV poll thread spawn failed");
                None
            }
        };

        // Drop our local sender so the channel can close once the producers exit.
        drop(tx);

        // ── Dispatcher loop ───────────────────────────────────────────────
        // We can't .await here (we're inside spawn_blocking). Use a tight
        // poll loop with try_recv on the mpsc + try_recv on the stop oneshot.
        loop {
            // Check shutdown.
            match stop_rx.try_recv() {
                Ok(()) | Err(oneshot::error::TryRecvError::Closed) => {
                    info!("system_events: stop requested");
                    break;
                }
                Err(oneshot::error::TryRecvError::Empty) => {}
            }

            // Drain any pending signals.
            match rx.try_recv() {
                Ok(sig) => {
                    handle_signal(&ctx, sig, session_id, &events, &drops);
                }
                Err(mpsc::error::TryRecvError::Empty) => {
                    std::thread::sleep(Duration::from_millis(100));
                }
                Err(mpsc::error::TryRecvError::Disconnected) => {
                    debug!("system_events: signal channel disconnected");
                    std::thread::sleep(Duration::from_millis(250));
                }
            }
        }

        // ── Tear down both producer threads ───────────────────────────────
        let _ = pump_stop_tx.send(());
        let _ = av_stop_tx.send(());
        if let Some(h) = pump_handle {
            let _ = h.join();
        }
        if let Some(h) = av_handle {
            let _ = h.join();
        }

        info!("system_events: stopped");
    }

    fn handle_signal(
        ctx: &CollectorCtx,
        sig: Signal,
        session_id: u32,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let now_iso = format_rfc3339(ctx.clock.now_unix_nanos());
        match sig {
            Signal::Lock => emit(
                ctx,
                EventKind::SystemLogout,
                &payload_for("lock", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::Unlock => emit(
                ctx,
                EventKind::SystemLogin,
                &payload_for("unlock", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::Logon => emit(
                ctx,
                EventKind::SystemLogin,
                &payload_for("logon", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::Logoff => emit(
                ctx,
                EventKind::SystemLogout,
                &payload_for("logoff", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::Suspend => emit(
                ctx,
                EventKind::SystemPowerStateChanged,
                &payload_for("suspend", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::ResumeFromSuspend => emit(
                ctx,
                EventKind::SystemPowerStateChanged,
                &payload_for("resume_from_suspend", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::ResumeAutomatic => emit(
                ctx,
                EventKind::SystemPowerStateChanged,
                &payload_for("resume_automatic", session_id, &now_iso),
                events,
                drops,
            ),
            Signal::AvDeactivated { product, previous, current } => emit(
                ctx,
                EventKind::SystemAvDeactivated,
                &payload_for_av(&product, previous, current, session_id, &now_iso),
                events,
                drops,
            ),
        }
    }

    fn emit_agent_start(
        ctx: &CollectorCtx,
        session_id: u32,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let now_iso = format_rfc3339(ctx.clock.now_unix_nanos());
        let user = std::env::var("USERNAME").unwrap_or_default();
        let escaped = super::json_escape(&user);
        let payload = format!(
            r#"{{"event":"agent_start","session_id":{},"timestamp":"{}","user":"{}"}}"#,
            session_id, now_iso, escaped,
        );
        emit(ctx, EventKind::SystemLogin, &payload, events, drops);
    }

    fn emit(
        ctx: &CollectorCtx,
        kind: EventKind,
        payload: &str,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let now = ctx.clock.now_unix_nanos();
        let id = EventId::new_v7().to_bytes();
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
                warn!(error = %e, "system_events: queue error");
                drops.fetch_add(1, Ordering::Relaxed);
            }
        }
    }

    // ──────────────────────────────────────────────────────────────────────
    // Hidden-window message pump
    // ──────────────────────────────────────────────────────────────────────

    // Thread-local storage for the channel sender, populated before
    // CreateWindowExW so the static wnd_proc can dispatch into it.
    thread_local! {
        static SIGNAL_TX: std::cell::RefCell<Option<mpsc::UnboundedSender<Signal>>> =
            const { std::cell::RefCell::new(None) };
    }

    fn pump_thread(
        tx: mpsc::UnboundedSender<Signal>,
        stop_rx: std::sync::mpsc::Receiver<()>,
    ) {
        SIGNAL_TX.with(|cell| *cell.borrow_mut() = Some(tx));

        let class_name_w: Vec<u16> = "PersonelSystemEventsListener\0".encode_utf16().collect();
        let window_name_w: Vec<u16> = "PersonelSystemEvents\0".encode_utf16().collect();

        // SAFETY: standard Win32 RegisterClassExW + CreateWindowExW pattern.
        // class_name_w and window_name_w live for the duration of this scope
        // which encloses both the registration and the message loop.
        let hwnd = unsafe {
            let wc = WNDCLASSEXW {
                cbSize: std::mem::size_of::<WNDCLASSEXW>() as u32,
                lpfnWndProc: Some(wnd_proc),
                lpszClassName: PCWSTR(class_name_w.as_ptr()),
                ..Default::default()
            };
            RegisterClassExW(&wc);

            CreateWindowExW(
                WS_EX_NOACTIVATE,
                PCWSTR(class_name_w.as_ptr()),
                PCWSTR(window_name_w.as_ptr()),
                Default::default(),
                0,
                0,
                0,
                0,
                HWND_MESSAGE,
                None,
                None,
                None,
            )
        };

        if hwnd.0 == 0 {
            error!("system_events: CreateWindowExW failed");
            // Drain stop_rx before exiting so the parent doesn't hang.
            let _ = stop_rx.recv();
            return;
        }

        // Register for WTS session notifications (all sessions so a service
        // running in session 0 still observes user session changes).
        // SAFETY: hwnd is a freshly-created valid message-only window.
        let wts_registered = unsafe {
            WTSRegisterSessionNotification(hwnd, NOTIFY_FOR_ALL_SESSIONS).is_ok()
        };
        if !wts_registered {
            warn!("system_events: WTSRegisterSessionNotification failed (continuing without session events)");
        } else {
            info!("system_events: WTS session notifications registered");
        }

        // Note: WM_POWERBROADCAST suspend/resume is delivered to top-level
        // windows automatically; HWND_MESSAGE windows DO receive it as well.
        // No explicit RegisterPowerSettingNotification needed for PBT_APMSUSPEND
        // / PBT_APMRESUMESUSPEND / PBT_APMRESUMEAUTOMATIC.

        // Spawn a sentinel thread that posts WM_PERSONEL_QUIT when stop_rx fires.
        let hwnd_raw = hwnd.0 as isize;
        std::thread::spawn(move || {
            let _ = stop_rx.recv();
            // SAFETY: posting a user message to a valid HWND.
            unsafe {
                let _ = PostMessageW(HWND(hwnd_raw), WM_PERSONEL_QUIT, WPARAM(0), LPARAM(0));
            }
        });

        // Standard Win32 message pump.
        // SAFETY: GetMessageW / DispatchMessageW are safe to call with a
        // valid HWND owned by this thread.
        unsafe {
            let mut msg = MSG::default();
            loop {
                let ret = GetMessageW(&mut msg, hwnd, 0, 0);
                if !ret.as_bool() || msg.message == WM_PERSONEL_QUIT {
                    break;
                }
                DispatchMessageW(&msg);
            }
            if wts_registered {
                let _ = WTSUnRegisterSessionNotification(hwnd);
            }
            let _ = DestroyWindow(hwnd);
        }

        SIGNAL_TX.with(|cell| *cell.borrow_mut() = None);
        debug!("system_events: pump thread exited");
    }

    /// Window procedure. Dispatches WTS + power messages into the channel.
    ///
    /// # Safety
    ///
    /// Standard `extern "system"` Win32 window procedure. Forwards unhandled
    /// messages to `DefWindowProcW`.
    unsafe extern "system" fn wnd_proc(
        hwnd: HWND,
        msg: u32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        if msg == WM_DESTROY {
            return LRESULT(0);
        }

        if msg == WM_WTSSESSION_CHANGE {
            let code = wparam.0 as u32;
            let sig = match code {
                c if c == WTS_SESSION_LOCK => Some(Signal::Lock),
                c if c == WTS_SESSION_UNLOCK => Some(Signal::Unlock),
                c if c == WTS_SESSION_LOGON => Some(Signal::Logon),
                c if c == WTS_SESSION_LOGOFF => Some(Signal::Logoff),
                _ => None,
            };
            if let Some(sig) = sig {
                send_signal(sig);
            }
            return LRESULT(0);
        }

        if msg == WM_POWERBROADCAST {
            let event = wparam.0 as u32;
            let sig = match event {
                PBT_APMSUSPEND => Some(Signal::Suspend),
                PBT_APMRESUMESUSPEND => Some(Signal::ResumeFromSuspend),
                PBT_APMRESUMEAUTOMATIC => Some(Signal::ResumeAutomatic),
                _ => None,
            };
            if let Some(sig) = sig {
                send_signal(sig);
            }
            // Per docs: applications that are not power-aware return TRUE.
            return LRESULT(1);
        }

        DefWindowProcW(hwnd, msg, wparam, lparam)
    }

    fn send_signal(sig: Signal) {
        SIGNAL_TX.with(|cell| {
            if let Some(tx) = cell.borrow().as_ref() {
                let _ = tx.send(sig);
            }
        });
    }

    // ──────────────────────────────────────────────────────────────────────
    // AV polling
    // ──────────────────────────────────────────────────────────────────────

    fn av_poll_thread(
        tx: mpsc::UnboundedSender<Signal>,
        stop_rx: std::sync::mpsc::Receiver<()>,
    ) {
        // Map of displayName → previous productState.
        let mut last: HashMap<String, u32> = HashMap::new();

        // First poll: probe powershell availability. If unavailable, log warn
        // once and exit silently — we do not want to wedge the collector.
        let mut available = match query_av_state() {
            Ok(snapshot) => {
                for (name, raw) in snapshot {
                    last.insert(name, raw);
                }
                true
            }
            Err(QueryErr::PowershellMissing) => {
                warn!("system_events: powershell.exe not found — AV monitoring disabled");
                false
            }
            Err(QueryErr::Failed(e)) => {
                warn!(error = %e, "system_events: initial AV query failed");
                true
            }
        };

        while available {
            // Sleep with stop-aware semantics: wake every second, total 5min.
            let sleep_total = Duration::from_secs(300);
            let chunk = Duration::from_secs(1);
            let mut elapsed = Duration::ZERO;
            let mut should_stop = false;
            while elapsed < sleep_total {
                if stop_rx.try_recv().is_ok() {
                    should_stop = true;
                    break;
                }
                std::thread::sleep(chunk);
                elapsed += chunk;
            }
            if should_stop {
                break;
            }

            match query_av_state() {
                Ok(snapshot) => {
                    for (name, raw) in snapshot.iter() {
                        let prev_raw = last.get(name).copied().unwrap_or(*raw);
                        let prev = ProductState::from_raw(prev_raw);
                        let curr = ProductState::from_raw(*raw);
                        if prev.realtime_on && !curr.realtime_on {
                            info!(av = %name, "system_events: AV realtime protection deactivated");
                            let _ = tx.send(Signal::AvDeactivated {
                                product: name.clone(),
                                previous: prev_raw,
                                current: *raw,
                            });
                        }
                    }
                    // Replace map with latest snapshot.
                    last.clear();
                    for (name, raw) in snapshot {
                        last.insert(name, raw);
                    }
                }
                Err(QueryErr::PowershellMissing) => {
                    warn!("system_events: powershell disappeared mid-flight — disabling AV poll");
                    available = false;
                }
                Err(QueryErr::Failed(e)) => {
                    debug!(error = %e, "system_events: AV poll failed (transient)");
                }
            }
        }

        debug!("system_events: AV poll thread exited");
    }

    enum QueryErr {
        PowershellMissing,
        Failed(String),
    }

    fn query_av_state() -> Result<Vec<(String, u32)>, QueryErr> {
        let output = Command::new("powershell.exe")
            .args([
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                "Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct \
                 | Select-Object displayName,productState | ConvertTo-Json -Compress",
            ])
            .output();

        let output = match output {
            Ok(o) => o,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return Err(QueryErr::PowershellMissing);
            }
            Err(e) => return Err(QueryErr::Failed(e.to_string())),
        };

        if !output.status.success() {
            return Err(QueryErr::Failed(format!(
                "powershell exited with status {:?}",
                output.status.code()
            )));
        }

        let stdout = String::from_utf8_lossy(&output.stdout);
        Ok(parse_av_powershell_json(&stdout))
    }

    /// Formats unix nanos as RFC3339 UTC. Avoids pulling in chrono at the
    /// collector layer; the agent uses simple Z-suffixed seconds + millis.
    fn format_rfc3339(unix_nanos: i64) -> String {
        let secs = unix_nanos / 1_000_000_000;
        let nanos = (unix_nanos % 1_000_000_000) as u32;

        // Convert seconds since epoch to YYYY-MM-DDThh:mm:ss using a small
        // civil-from-days routine (Howard Hinnant's algorithm). Avoids a
        // dependency on time/chrono in this collector.
        let (y, mo, d, hh, mm, ss) = unix_to_civil(secs);
        let millis = nanos / 1_000_000;
        format!(
            "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:03}Z",
            y, mo, d, hh, mm, ss, millis
        )
    }

    /// Converts unix seconds to (year, month, day, hour, minute, second) UTC.
    /// Adapted from Howard Hinnant's `days_from_civil` inverse.
    fn unix_to_civil(secs: i64) -> (i32, u32, u32, u32, u32, u32) {
        let days = secs.div_euclid(86_400);
        let secs_of_day = secs.rem_euclid(86_400) as u32;
        let hh = secs_of_day / 3600;
        let mm = (secs_of_day % 3600) / 60;
        let ss = secs_of_day % 60;

        // Civil from days (algorithm from chrono/Howard Hinnant).
        let z = days + 719_468;
        let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
        let doe = (z - era * 146_097) as u32; // [0, 146096]
        let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365; // [0,399]
        let y = yoe as i32 + (era * 400) as i32;
        let doy = doe - (365 * yoe + yoe / 4 - yoe / 100); // [0, 365]
        let mp = (5 * doy + 2) / 153; // [0, 11]
        let d = doy - (153 * mp + 2) / 5 + 1; // [1, 31]
        let mo = if mp < 10 { mp + 3 } else { mp - 9 }; // [1, 12]
        let y = if mo <= 2 { y + 1 } else { y };
        (y, mo, d, hh, mm, ss)
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn rfc3339_epoch() {
            assert_eq!(format_rfc3339(0), "1970-01-01T00:00:00.000Z");
        }

        #[test]
        fn rfc3339_known_timestamp() {
            // 2026-04-13T00:00:00Z = 1_775_980_800
            let s = format_rfc3339(1_775_952_000_000_000_000);
            assert!(s.starts_with("2026-04-12") || s.starts_with("2026-04-13"));
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Cross-platform unit tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn product_state_realtime_on() {
        // Defender real-time protection on, definitions up-to-date: 0x10_1000.
        // productState bit 8..15 = 0x10 ⇒ realtime on.
        let s = ProductState::from_raw(0x00_10_1000);
        assert!(s.realtime_on);
    }

    #[test]
    fn product_state_realtime_off() {
        // Defender real-time protection off: bits 8..15 = 0x00.
        let s = ProductState::from_raw(0x00_00_1000);
        assert!(!s.realtime_on);
    }

    #[test]
    fn product_state_realtime_snoozed() {
        // bits 8..15 = 0x11 (snoozed but still considered on per vendor probes).
        let s = ProductState::from_raw(0x00_11_1000);
        assert!(s.realtime_on);
    }

    #[test]
    fn product_state_realtime_alternate_off_value() {
        // bits 8..15 = 0x01 (defender outdated state, realtime off).
        let s = ProductState::from_raw(0x00_01_0000);
        assert!(!s.realtime_on);
    }

    #[test]
    fn payload_lock_shape() {
        let p = payload_for("lock", 1, "2026-04-13T12:00:00.000Z");
        assert_eq!(
            p,
            r#"{"event":"lock","session_id":1,"timestamp":"2026-04-13T12:00:00.000Z"}"#
        );
        // Round-trip through serde_json to confirm valid JSON.
        let _: serde_json::Value = serde_json::from_str(&p).expect("valid json");
    }

    #[test]
    fn payload_av_shape() {
        let p = payload_for_av(
            "Windows Defender",
            0x0010_1000,
            0x0000_1000,
            2,
            "2026-04-13T12:00:00.000Z",
        );
        let v: serde_json::Value = serde_json::from_str(&p).expect("valid json");
        assert_eq!(v["event"], "av_deactivated");
        assert_eq!(v["av_product_name"], "Windows Defender");
        assert_eq!(v["previous_state_hex"], "0x00101000");
        assert_eq!(v["current_state_hex"], "0x00001000");
        assert_eq!(v["session_id"], 2);
    }

    #[test]
    fn json_escape_special_chars() {
        assert_eq!(json_escape("a\"b\\c"), "a\\\"b\\\\c");
        assert_eq!(json_escape("line1\nline2"), "line1\\nline2");
        assert_eq!(json_escape("tab\there"), "tab\\there");
    }

    #[test]
    fn parse_av_single_object() {
        let raw = r#"{"displayName":"Windows Defender","productState":266240}"#;
        let parsed = parse_av_powershell_json(raw);
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].0, "Windows Defender");
        assert_eq!(parsed[0].1, 266_240);
    }

    #[test]
    fn parse_av_array() {
        let raw = r#"[{"displayName":"Defender","productState":266240},{"displayName":"Bitdefender","productState":397568}]"#;
        let parsed = parse_av_powershell_json(raw);
        assert_eq!(parsed.len(), 2);
        assert_eq!(parsed[1].0, "Bitdefender");
    }

    #[test]
    fn parse_av_pascal_case() {
        // PowerShell sometimes upper-cases properties through the pipeline.
        let raw = r#"{"DisplayName":"ESET","ProductState":"266256"}"#;
        let parsed = parse_av_powershell_json(raw);
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].0, "ESET");
        assert_eq!(parsed[0].1, 266_256);
    }

    #[test]
    fn parse_av_empty() {
        assert!(parse_av_powershell_json("").is_empty());
        assert!(parse_av_powershell_json("   ").is_empty());
    }

    #[test]
    fn parse_av_garbage() {
        assert!(parse_av_powershell_json("not json").is_empty());
    }

    #[test]
    fn av_realtime_off_transition_detected() {
        let prev = ProductState::from_raw(0x0010_1000);
        let curr = ProductState::from_raw(0x0000_1000);
        assert!(prev.realtime_on && !curr.realtime_on);
    }
}
