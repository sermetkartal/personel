//! Window title / focus change collector.
//!
//! Polls `GetForegroundWindow` every 500 ms and emits
//! `window.title_changed` when the title or foreground process changes.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Replace polling with `SetWinEventHook(EVENT_SYSTEM_FOREGROUND)` for
//!   lower CPU overhead.
//! - Resolve executable name from PID via `personel_os::windows::input`.
//! - Emit `window.focus_lost` when the foreground window drops to null.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Window title / foreground process change collector.
#[derive(Default)]
pub struct WindowTitleCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
}

impl WindowTitleCollector {
    /// Creates a new [`WindowTitleCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for WindowTitleCollector {
    fn name(&self) -> &'static str {
        "window_title"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["window.title_changed", "window.focus_lost"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);

        let task = tokio::spawn(async move {
            // TODO: replace with WinEvent hook in Phase 1 hardening sprint.
            let mut last_title = String::new();
            let mut last_pid: u32 = 0;
            let mut last_change_nanos: i64 = ctx.clock.now_unix_nanos();
            let mut ticker = tokio::time::interval(Duration::from_millis(500));
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        match personel_platform::input::foreground_window_info() {
                            Ok(info) => {
                                healthy.store(true, Ordering::Relaxed);
                                if info.title != last_title || info.pid != last_pid {
                                    let now = ctx.clock.now_unix_nanos();
                                    let prev_duration_ms = u64::try_from(
                                        now.saturating_sub(last_change_nanos)
                                    ).unwrap_or(0) / 1_000_000;

                                    debug!(
                                        title = %info.title,
                                        pid = info.pid,
                                        prev_duration_ms,
                                        "foreground window changed"
                                    );

                                    // TODO: encode and enqueue window.title_changed event.
                                    // Pending: wire up proto encode + queue.enqueue here.
                                    events.fetch_add(1, Ordering::Relaxed);

                                    last_title = info.title;
                                    last_pid = info.pid;
                                    last_change_nanos = now;
                                }
                            }
                            Err(e) => {
                                warn!("window_title: foreground_window_info error: {e}");
                                healthy.store(false, Ordering::Relaxed);
                            }
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("window_title collector: stop requested");
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
            drops_since_last: 0,
            status: String::new(),
        }
    }
}
