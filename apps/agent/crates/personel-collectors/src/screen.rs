//! Screenshot capture collector (DXGI Desktop Duplication).
//!
//! `ScreenCollector` fires on a policy-driven interval (default 5 min) and
//! captures the primary monitor via
//! [`personel_os::capture::DxgiCapture`] (Windows only). Each frame is
//! JPEG-encoded and written to the local queue as a `screenshot.captured`
//! event.
//!
//! # Sensitivity guard
//!
//! Before each capture the collector checks whether the current foreground
//! process executable name matches any entry in
//! `policy.screenshot.exclude_apps`. A match causes the frame to be skipped
//! entirely and logged at `DEBUG` level. This implements the KVKK m.6 / ADR
//! 0013 sensitivity gap-1 requirement.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full DXGI capture loop.
//! Other platforms: collector starts, logs once that DXGI is unavailable, and
//! parks until stopped. This allows `cargo check` to pass on macOS/Linux.
//!
//! # Error handling (Windows)
//!
//! | Error             | Action                                            |
//! |-------------------|---------------------------------------------------|
//! | `FrameTimeout`    | Skip tick; healthy stays `true`                   |
//! | `AccessLost`      | Call `reopen()`; retry on next tick               |
//! | `DeviceRemoved`   | Reconstruct `DxgiCapture`; wait 5 s before retry |
//! | Other fatal error | Log `error!`; healthy → `false`; keep running    |

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Screenshot capture collector (DXGI-based on Windows; no-op on other platforms).
#[derive(Default)]
pub struct ScreenCollector {
    healthy: Arc<AtomicBool>,
    events:  Arc<AtomicU64>,
    drops:   Arc<AtomicU64>,
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
        let healthy = Arc::clone(&self.healthy);
        let events  = Arc::clone(&self.events);
        let drops   = Arc::clone(&self.drops);

        // Capture loop must run on a blocking thread: DXGI requires a
        // dedicated OS thread and cannot be called from an async context.
        let task = tokio::task::spawn_blocking(move || {
            run_capture_loop(ctx, healthy, events, drops, stop_rx);
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {
        // Loop reads live policy via ctx.policy() on every tick.
    }

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy:           self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last:  self.drops.swap(0, Ordering::Relaxed),
            status:            if self.healthy.load(Ordering::Relaxed) {
                                   String::new()
               } else {
                   "DXGI capture unhealthy".into()
               },
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

fn run_capture_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    run_windows(ctx, healthy, events, drops, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        info!("screen collector: DXGI not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        // Suppress unused-variable warnings on non-Windows.
        let _ = (ctx, events, drops);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
fn run_windows(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    info!("screen collector: starting (DXGI)");
    healthy.store(true, Ordering::Relaxed);

    let quality = ctx.policy().screenshot.quality_percent as u8;

    let mut capture = match personel_os::capture::DxgiCapture::open(0, quality) {
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
                        return capture_loop(c, ctx, healthy, events, drops, stop_rx);
                    }
                    Err(e2) => error!(error = %e2, "screen: DXGI retry failed"),
                }
            }
        }
    };

    capture_loop(capture, ctx, healthy, events, drops, stop_rx);
}

#[cfg(target_os = "windows")]
fn capture_loop(
    mut capture: personel_os::capture::DxgiCapture,
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    mut stop_rx: tokio::sync::oneshot::Receiver<()>,
) {
    use personel_core::error::AgentError;

    loop {
        if stop_rx.try_recv().is_ok() {
            info!("screen collector: stop received");
            return;
        }

        // ── Interval sleep with per-second stop checks ────────────────────
        let interval_secs = ctx.policy().screenshot.interval_seconds.max(10) as u64;
        for _ in 0..interval_secs {
            std::thread::sleep(Duration::from_secs(1));
            if stop_rx.try_recv().is_ok() {
                info!("screen collector: stop received during interval sleep");
                return;
            }
        }

        // ── Sensitivity guard ─────────────────────────────────────────────
        let policy = ctx.policy();
        if !policy.screenshot.exclude_apps.is_empty() {
            if let Ok(fg) = personel_os::input::foreground_window_info() {
                let exe = exe_name_for_pid(fg.pid);
                let title = fg.title.to_lowercase();
                let skip = policy.screenshot.exclude_apps.iter().any(|app| {
                    let a = app.to_lowercase();
                    exe.to_lowercase().contains(&a) || title.contains(&a)
                });
                if skip {
                    debug!(exe = %exe, "screen: skip — foreground app in exclude_apps");
                    drops.fetch_add(1, Ordering::Relaxed);
                    continue;
                }
            }
        }
        let quality = policy.screenshot.quality_percent as u8;
        drop(policy);

        // ── Grab + encode ─────────────────────────────────────────────────
        match capture.grab_frame() {
            Ok(jpeg) => {
                enqueue_screenshot(&ctx, jpeg, &events);
                healthy.store(true, Ordering::Relaxed);
            }

            Err(AgentError::CollectorRuntime { ref reason, .. })
                if reason.contains("frame timeout") =>
            {
                debug!("screen: frame timeout — no desktop update");
            }

            Err(AgentError::CollectorRuntime { ref reason, .. })
                if reason.contains("access lost") =>
            {
                warn!("screen: access lost — reopening duplication output");
                if let Err(e) = capture.reopen() {
                    error!(error = %e, "screen: reopen failed");
                    healthy.store(false, Ordering::Relaxed);
                }
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
            }

            Err(e) => {
                error!(error = %e, "screen: unexpected capture error");
                healthy.store(false, Ordering::Relaxed);
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Queue helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Enqueues a JPEG screenshot into the local SQLCipher queue.
#[cfg(target_os = "windows")]
fn enqueue_screenshot(ctx: &CollectorCtx, jpeg: Vec<u8>, events: &Arc<AtomicU64>) {
    let now = ctx.clock.now_unix_nanos();
    let id  = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::ScreenshotCaptured.as_str(),
        Priority::Normal,
        now,
        now,
        &jpeg,
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
            debug!(bytes = jpeg.len(), "screen: screenshot enqueued");
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
    // sysinfo 0.30: refresh_processes() refreshes all processes; then look up by PID.
    sys.refresh_processes();
    sys.process(Pid::from_u32(pid))
        .and_then(|p| p.exe())
        .and_then(|p| p.file_name())
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default()
}
