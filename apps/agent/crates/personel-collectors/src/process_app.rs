//! Process / app usage collector.
//!
//! Polls running processes via `sysinfo` every 5 seconds and emits
//! `process.start` / `process.stop` events when the process set changes.
//! Also tracks the foreground application and emits `process.foreground_change`
//! when the active process switches.
//!
//! # ETW note
//!
//! A proper ETW `Microsoft-Windows-Kernel-Process` session would give exact
//! start/stop timestamps with lower overhead. That requires `StartTraceW` +
//! `EnableTraceEx2` + a dedicated OS thread running `ProcessTrace` — planned
//! for the Phase 2 ETW hardening sprint. The sysinfo poll approach used here
//! is correct, cross-platform-compilable, and sufficient for MVP.
//!
//! # Platform support
//!
//! `sysinfo` is cross-platform; on non-Windows the foreground-window check
//! is skipped gracefully.

use std::collections::HashSet;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use sysinfo::{Pid, ProcessRefreshKind, RefreshKind, System};
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// Process and application usage collector.
///
/// Emits `process.start`, `process.stop`, and `process.foreground_change`
/// events by polling the OS process list every 5 seconds.
#[derive(Default)]
pub struct ProcessAppCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl ProcessAppCollector {
    /// Creates a new [`ProcessAppCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ProcessAppCollector {
    fn name(&self) -> &'static str {
        "process_app"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["process.start", "process.stop", "process.foreground_change"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::task::spawn_blocking(move || {
            run_loop(ctx, healthy, events, drops, stop_rx);
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
// Run loop (blocking thread)
// ──────────────────────────────────────────────────────────────────────────────

fn run_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    mut stop_rx: oneshot::Receiver<()>,
) {
    const POLL_INTERVAL: Duration = Duration::from_secs(5);

    info!("process_app collector: started");
    healthy.store(true, Ordering::Relaxed);

    let mut sys = System::new_with_specifics(
        RefreshKind::new().with_processes(ProcessRefreshKind::everything()),
    );
    sys.refresh_processes();

    // Seed the known PID set so we don't emit start events for all existing
    // processes on the first tick.
    let mut known_pids: HashSet<u32> =
        sys.processes().keys().map(|p| p.as_u32()).collect();
    let mut last_fg_pid: u32 = 0;

    loop {
        if stop_rx.try_recv().is_ok() {
            break;
        }
        std::thread::sleep(POLL_INTERVAL);
        if stop_rx.try_recv().is_ok() {
            break;
        }

        sys.refresh_processes();
        let now = ctx.clock.now_unix_nanos();
        let current_pids: HashSet<u32> =
            sys.processes().keys().map(|p| p.as_u32()).collect();

        // New processes.
        for &pid in &current_pids {
            if !known_pids.contains(&pid) {
                if let Some(proc_) = sys.process(Pid::from_u32(pid)) {
                    let exe = proc_
                        .exe()
                        .and_then(|p| p.to_str())
                        .unwrap_or("")
                        .to_owned();
                    let name = proc_.name().to_string();
                    let ppid = proc_.parent().map(|p| p.as_u32()).unwrap_or(0);

                    debug!(pid, name = %name, "process.start");
                    let payload = format!(
                        r#"{{"pid":{},"ppid":{},"name":{:?},"exe":{:?}}}"#,
                        pid, ppid, name, exe
                    );
                    emit_event(&ctx, EventKind::ProcessStart, &payload, now, &events, &drops);
                }
            }
        }

        // Stopped processes.
        for &pid in &known_pids {
            if !current_pids.contains(&pid) {
                debug!(pid, "process.stop");
                let payload = format!(r#"{{"pid":{}}}"#, pid);
                emit_event(&ctx, EventKind::ProcessStop, &payload, now, &events, &drops);
            }
        }

        known_pids = current_pids;

        // Foreground process change.
        let fg_pid = foreground_pid();
        if fg_pid != 0 && fg_pid != last_fg_pid {
            let fg_name = sys
                .process(Pid::from_u32(fg_pid))
                .map(|p| p.name().to_string())
                .unwrap_or_default();

            debug!(pid = fg_pid, name = %fg_name, "process.foreground_change");
            let payload = format!(
                r#"{{"pid":{},"name":{:?},"prev_pid":{}}}"#,
                fg_pid, fg_name, last_fg_pid
            );
            emit_event(
                &ctx,
                EventKind::ProcessForegroundChange,
                &payload,
                now,
                &events,
                &drops,
            );
            last_fg_pid = fg_pid;
        }
    }

    info!("process_app collector: stopped");
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Returns the PID of the current foreground window, or 0 on error/unsupported.
fn foreground_pid() -> u32 {
    #[cfg(target_os = "windows")]
    {
        personel_os::input::foreground_window_info()
            .map(|i| i.pid)
            .unwrap_or(0)
    }
    #[cfg(not(target_os = "windows"))]
    {
        0
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Queue helper
// ──────────────────────────────────────────────────────────────────────────────

fn emit_event(
    ctx: &CollectorCtx,
    kind: EventKind,
    payload: &str,
    now: i64,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(&id, kind.as_str(), Priority::Normal, now, now, payload.as_bytes()) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            error!(error = %e, "process_app: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}
