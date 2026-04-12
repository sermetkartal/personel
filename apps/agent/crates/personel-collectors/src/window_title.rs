//! Window title / focus change collector.
//!
//! Polls `foreground_window_info` every 500 ms and emits
//! `window.title_changed` when the foreground window title or PID changes.
//! Emits `window.focus_lost` when the foreground window disappears.
//!
//! # Platform support
//!
//! Uses `personel_platform::input::foreground_window_info` on all platforms.
//! On non-Windows the platform facade returns `AgentError::Unsupported`; the
//! collector then parks and reports healthy (non-blocking for dev builds).

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Window title / foreground process change collector.
#[derive(Default)]
pub struct WindowTitleCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
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
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            let mut last_title = String::new();
            let mut last_pid: u32 = 0;
            let mut last_change_nanos: i64 = ctx.clock.now_unix_nanos();
            let mut was_focused = false;

            let mut ticker = tokio::time::interval(Duration::from_millis(500));
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            info!("window_title collector: started");

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        match personel_platform::input::foreground_window_info() {
                            Ok(info) => {
                                healthy.store(true, Ordering::Relaxed);

                                // Focus regained after a focus_lost.
                                let now = ctx.clock.now_unix_nanos();

                                if info.title.is_empty() && info.pid == 0 {
                                    // No foreground window.
                                    if was_focused {
                                        was_focused = false;
                                        emit_event(
                                            &ctx,
                                            EventKind::WindowFocusLost,
                                            &format!(
                                                r#"{{"prev_title":{:?},"prev_pid":{}}}"#,
                                                last_title, last_pid
                                            ),
                                            &events,
                                            &drops,
                                        );
                                        debug!(prev_title = %last_title, "window focus lost");
                                    }
                                } else if info.title != last_title || info.pid != last_pid {
                                    let prev_duration_ms = u64::try_from(
                                        now.saturating_sub(last_change_nanos)
                                    ).unwrap_or(0) / 1_000_000;

                                    debug!(
                                        title = %info.title,
                                        pid = info.pid,
                                        prev_duration_ms,
                                        "foreground window changed"
                                    );

                                    let payload = format!(
                                        r#"{{"title":{:?},"pid":{},"prev_title":{:?},"prev_pid":{},"prev_duration_ms":{}}}"#,
                                        info.title,
                                        info.pid,
                                        last_title,
                                        last_pid,
                                        prev_duration_ms,
                                    );

                                    emit_event(&ctx, EventKind::WindowTitleChanged, &payload, &events, &drops);

                                    last_title = info.title;
                                    last_pid = info.pid;
                                    last_change_nanos = now;
                                    was_focused = true;
                                }
                            }
                            Err(personel_core::error::AgentError::Unsupported { .. }) => {
                                // Non-Windows dev build: park gracefully.
                                healthy.store(true, Ordering::Relaxed);
                                info!("window_title: platform unsupported — parking");
                                let _ = stop_rx.await;
                                break;
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
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

fn emit_event(
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
            error!(error = %e, "window_title: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}
