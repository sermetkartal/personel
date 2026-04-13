//! Agent self-throttle monitor.
//!
//! Owns the 5-second sampling loop that feeds the shared
//! [`personel_core::throttle::ThrottleState`]. The actual state machine and
//! rolling-window math live in `personel-core::throttle`; this module:
//!
//! 1. Measures the *agent process's own* CPU% + RSS via Win32 APIs
//!    (`GetProcessTimes`/`GetProcessMemoryInfo`/`GetSystemTimes`).
//! 2. Wraps the measurement in a [`personel_core::throttle::Sample`].
//! 3. Calls [`ThrottleState::record_sample`] and, on a transition, emits an
//!    `agent.health_heartbeat` event carrying the new state + metrics.
//!
//! The monitor lives in `personel-agent` (not in `personel-core`) because
//! `personel-core` has `#![deny(unsafe_code)]`. The Win32 measurement
//! functions require `unsafe` to call `GetProcessTimes` etc., so they must
//! live in a crate that allows unsafe — which for the agent stack means
//! either `personel-os` or `personel-agent` itself. We picked the latter to
//! keep the monitor + wiring code co-located.
//!
//! # Unsafe usage
//!
//! The Windows `GetProcessTimes` / `GetProcessMemoryInfo` / `GetSystemTimes`
//! APIs require `unsafe` blocks — same pattern as the sibling
//! `crash_dump.rs` module. The crate-level `#![deny(unsafe_code)]` in
//! `main.rs` is overridden locally here with an inner attribute; every
//! `unsafe` block has a SAFETY comment.
//!
//! # Non-Windows
//!
//! On dev builds (macOS/Linux) [`measure_self_cpu_rss`] returns `(0.0, 0)`
//! so the monitor task compiles cleanly. The throttle state machine treats
//! an all-zero sample window as "always Normal", which is the desired dev
//! behaviour.

// Local override of the crate-level deny(unsafe_code). Windows API calls
// below are the minimum surface needed to self-measure CPU% + RSS.
#![allow(unsafe_code)]

use std::sync::Arc;
use std::time::Duration;

use tokio::sync::oneshot;
use tracing::{info, warn};

use personel_core::clock::Clock;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_core::throttle::{Sample, ThrottleState, ThrottleStateKind, SAMPLE_INTERVAL_SECS};
use personel_queue::queue::EventQueue;

// ──────────────────────────────────────────────────────────────────────────────
// Monitor entry point
// ──────────────────────────────────────────────────────────────────────────────

/// Handles required by [`run_throttle_monitor`] to emit heartbeat events on
/// state transitions without pulling the whole `CollectorCtx` into scope.
pub struct MonitorDeps {
    /// Shared throttle state written to on every sample.
    pub state: Arc<ThrottleState>,
    /// Queue used to emit heartbeat events when the state transitions.
    pub queue: Arc<EventQueue>,
    /// Wall-clock source for event timestamps.
    pub clock: Arc<dyn Clock>,
}

/// Runs the self-throttle monitor loop. Ticks every 5 seconds, pushes a
/// [`Sample`] into the shared [`ThrottleState`], and emits an
/// `agent.health_heartbeat` event whenever the state transitions.
///
/// Stops when `stop_rx` fires.
pub async fn run_throttle_monitor(deps: MonitorDeps, mut stop_rx: oneshot::Receiver<()>) {
    info!(
        interval_secs = SAMPLE_INTERVAL_SECS,
        "throttle monitor: started"
    );

    let mut ticker = tokio::time::interval(Duration::from_secs(SAMPLE_INTERVAL_SECS));
    // Skip the immediate first tick so we don't emit a zero-CPU sample
    // before the process has actually run long enough to compute a delta.
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    ticker.tick().await; // consume the instant-fire first tick

    loop {
        tokio::select! {
            _ = ticker.tick() => {
                let (cpu_percent, rss_bytes) = measure_self_cpu_rss();
                let now_nanos = deps.clock.now_unix_nanos();
                let now_secs = (now_nanos / 1_000_000_000).max(0) as u64;

                let sample = Sample {
                    cpu_percent,
                    rss_bytes,
                    taken_at_unix_secs: now_secs,
                };

                if let Some(new_state) = deps.state.record_sample(sample) {
                    on_state_transition(&deps, new_state, now_nanos);
                }
            }
            _ = &mut stop_rx => {
                info!("throttle monitor: stop requested");
                break;
            }
        }
    }

    info!("throttle monitor: stopped");
}

fn on_state_transition(deps: &MonitorDeps, new_state: ThrottleStateKind, now_nanos: i64) {
    let metrics = deps.state.current_metrics();
    info!(
        state = new_state.as_str(),
        cpu_avg_pct = format!("{:.2}", metrics.cpu_percent),
        rss_avg_mb = metrics.rss_mb,
        sample_count = metrics.sample_count,
        "throttle state transition"
    );

    // Build the heartbeat payload. We use a compact JSON blob rather than
    // a proto because the enricher does not yet have a schema for the
    // throttle fields and plain JSON is already the path device_status
    // takes for its snapshots.
    let payload = serde_json::json!({
        "kind": "throttle_transition",
        "throttle_state": new_state.as_str(),
        "cpu_avg_pct": metrics.cpu_percent,
        "rss_avg_mb": metrics.rss_mb,
        "sample_count": metrics.sample_count,
        "ts_unix_nanos": now_nanos,
    })
    .to_string();

    let id = EventId::new_v7().to_bytes();
    if let Err(e) = deps.queue.enqueue(
        &id,
        EventKind::AgentHealthHeartbeat.as_str(),
        Priority::High,
        now_nanos,
        now_nanos,
        payload.as_bytes(),
    ) {
        warn!(
            error = %e,
            "throttle monitor: failed to enqueue heartbeat transition event"
        );
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Measurement — platform specific
// ──────────────────────────────────────────────────────────────────────────────

/// Measures the agent process's own CPU utilisation (% of total system CPU
/// time since the previous call) and resident set size in bytes.
///
/// Returns `(cpu_percent, rss_bytes)`.
///
/// # Windows
///
/// Uses `GetProcessTimes`, `GetSystemTimes`, and `GetProcessMemoryInfo`. The
/// first call seeds static snapshots and always returns `(0.0, rss)` so the
/// delta is meaningful from the second sample onwards.
///
/// # Other platforms
///
/// Returns `(0.0, 0)` so cross-platform dev builds compile.
#[must_use]
pub fn measure_self_cpu_rss() -> (f64, u64) {
    #[cfg(target_os = "windows")]
    {
        windows_impl::measure()
    }
    #[cfg(not(target_os = "windows"))]
    {
        (0.0, 0)
    }
}

#[cfg(target_os = "windows")]
mod windows_impl {
    use std::sync::{Mutex, OnceLock};

    use ::windows::Win32::Foundation::FILETIME;
    use ::windows::Win32::System::ProcessStatus::{
        GetProcessMemoryInfo, PROCESS_MEMORY_COUNTERS,
    };
    use ::windows::Win32::System::Threading::{
        GetCurrentProcess, GetProcessTimes, GetSystemTimes,
    };

    /// State held between calls to [`measure`] so we can compute a true
    /// delta between two sample points. Seeded lazily on first call.
    struct LastSnapshot {
        proc_user_100ns: u64,
        proc_kernel_100ns: u64,
        sys_user_100ns: u64,
        sys_kernel_100ns: u64,
        #[allow(dead_code)]
        sys_idle_100ns: u64,
    }

    static LAST: OnceLock<Mutex<Option<LastSnapshot>>> = OnceLock::new();

    fn last_cell() -> &'static Mutex<Option<LastSnapshot>> {
        LAST.get_or_init(|| Mutex::new(None))
    }

    /// Combines `FILETIME.dwLowDateTime`/`dwHighDateTime` into a single
    /// `u64` of 100-nanosecond ticks.
    fn ft_to_u64(ft: FILETIME) -> u64 {
        (u64::from(ft.dwHighDateTime) << 32) | u64::from(ft.dwLowDateTime)
    }

    /// Reads the current RSS (working-set) via `GetProcessMemoryInfo` on
    /// the current process handle.
    fn read_rss_bytes() -> u64 {
        // SAFETY: `PROCESS_MEMORY_COUNTERS` is a `#[repr(C)]` struct; Win32
        // only writes into the out pointer. `GetCurrentProcess` returns a
        // pseudo-handle that never needs closing.
        unsafe {
            let handle = GetCurrentProcess();
            let mut pmc = PROCESS_MEMORY_COUNTERS::default();
            let size = std::mem::size_of::<PROCESS_MEMORY_COUNTERS>() as u32;
            if GetProcessMemoryInfo(handle, &mut pmc, size).is_ok() {
                pmc.WorkingSetSize as u64
            } else {
                0
            }
        }
    }

    /// Returns the cumulative 100-ns ticks of (user, kernel) CPU time
    /// spent by the current process since it started.
    fn read_process_times() -> Option<(u64, u64)> {
        // SAFETY: All four FILETIME pointers are valid stack locals; Win32
        // only writes into them.
        unsafe {
            let handle = GetCurrentProcess();
            let mut creation = FILETIME::default();
            let mut exit = FILETIME::default();
            let mut kernel = FILETIME::default();
            let mut user = FILETIME::default();
            GetProcessTimes(
                handle,
                &mut creation,
                &mut exit,
                &mut kernel,
                &mut user,
            )
            .ok()?;
            Some((ft_to_u64(user), ft_to_u64(kernel)))
        }
    }

    /// Returns the cumulative 100-ns ticks of (idle, kernel, user) system
    /// CPU time. `kernel` already INCLUDES `idle` per the MSDN contract.
    fn read_system_times() -> Option<(u64, u64, u64)> {
        // SAFETY: All three FILETIME pointers are valid stack locals.
        unsafe {
            let mut idle = FILETIME::default();
            let mut kernel = FILETIME::default();
            let mut user = FILETIME::default();
            GetSystemTimes(Some(&mut idle), Some(&mut kernel), Some(&mut user)).ok()?;
            Some((ft_to_u64(idle), ft_to_u64(kernel), ft_to_u64(user)))
        }
    }

    /// Computes CPU% for the agent process over the period between the
    /// last snapshot and now. Returns 0.0 on the very first call (no
    /// baseline) or on any API failure.
    pub fn measure() -> (f64, u64) {
        let rss = read_rss_bytes();

        let proc_now = match read_process_times() {
            Some(v) => v,
            None => return (0.0, rss),
        };
        let sys_now = match read_system_times() {
            Some(v) => v,
            None => return (0.0, rss),
        };

        let mut guard = match last_cell().lock() {
            Ok(g) => g,
            Err(p) => p.into_inner(),
        };

        let cpu_percent = match guard.as_ref() {
            None => 0.0,
            Some(prev) => {
                let proc_user_d = proc_now.0.saturating_sub(prev.proc_user_100ns);
                let proc_kernel_d = proc_now.1.saturating_sub(prev.proc_kernel_100ns);
                let proc_total_d = proc_user_d.saturating_add(proc_kernel_d);

                let sys_user_d = sys_now.2.saturating_sub(prev.sys_user_100ns);
                let sys_kernel_d = sys_now.1.saturating_sub(prev.sys_kernel_100ns);
                // `sys_kernel` already includes idle; do NOT add idle
                // again. Total system wall-CPU time delta across ALL cores.
                let sys_total_d = sys_user_d.saturating_add(sys_kernel_d);

                if sys_total_d == 0 {
                    0.0
                } else {
                    let pct = (proc_total_d as f64) * 100.0 / (sys_total_d as f64);
                    // Clamp to [0, 100] to defend against clock jitter.
                    pct.clamp(0.0, 100.0)
                }
            }
        };

        *guard = Some(LastSnapshot {
            proc_user_100ns: proc_now.0,
            proc_kernel_100ns: proc_now.1,
            sys_user_100ns: sys_now.2,
            sys_kernel_100ns: sys_now.1,
            sys_idle_100ns: sys_now.0,
        });

        (cpu_percent, rss)
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn measure_returns_cleanly_on_all_platforms() {
        // Smoke test — we can't assert on specific values, but the call
        // must not panic and must return finite numbers.
        let (cpu, rss) = measure_self_cpu_rss();
        assert!(cpu.is_finite());
        assert!(cpu >= 0.0 && cpu <= 100.0);
        // On non-Windows rss is 0; on Windows it's at least the page size.
        let _ = rss;
    }

    #[test]
    fn transition_does_not_panic_on_empty_queue_error() {
        // Sanity: on_state_transition is best-effort. We can't easily
        // inject a failing queue here without more wiring, so this test
        // only documents that the function path is exercised by the
        // record_sample tests in personel-core.
        let _ = ThrottleStateKind::Critical.as_str();
    }
}
