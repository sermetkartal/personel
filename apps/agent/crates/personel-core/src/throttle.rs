//! Self-throttle state for the Personel agent.
//!
//! This module lives in `personel-core` (not `personel-agent`) so that every
//! collector crate can read the current throttle state via its `CollectorCtx`
//! without creating a circular dependency on the agent binary crate. The
//! monitor task that *feeds* this state lives in `personel-agent::throttle`
//! because it performs Win32 `unsafe` calls (forbidden in core).
//!
//! # Concept
//!
//! The agent has two hard resource caps from CLAUDE.md §0:
//!
//! * **<2% CPU** (steady state, averaged)
//! * **<150 MB RSS** (working-set)
//!
//! A 5-second sampling loop measures the agent process's own CPU% + RSS and
//! pushes one [`Sample`] into a 6-slot rolling window (30 s observation). The
//! rolling average is evaluated against a three-state ladder with hysteresis
//! so a single noisy sample cannot flip the agent from Normal to Critical
//! and back inside one observation window.
//!
//! | State    | Enter condition (avg over window)             | Collector effect                               |
//! |----------|-----------------------------------------------|------------------------------------------------|
//! | Normal   | `cpu_avg < NORMAL_CPU`  AND `rss_avg < NORMAL_RSS` | no throttling                                  |
//! | Warn     | `cpu_avg ≥ WARN_CPU`    OR  `rss_avg ≥ WARN_RSS`   | `Priority::Low` collectors: tick rate ÷2       |
//! | Critical | `cpu_avg ≥ CRIT_CPU`    OR  `rss_avg ≥ CRIT_RSS`   | `Priority::Low` collectors: pause 60 s, drop   |
//!
//! # Hysteresis
//!
//! A state transition to a *higher* severity needs **two consecutive
//! windows** above the enter threshold. A transition to a *lower* severity
//! needs **three consecutive windows** below the exit threshold (which sits
//! 10-15% below the enter threshold). This prevents rapid flapping when the
//! agent hovers near a boundary.
//!
//! # Lock-free reads
//!
//! Collectors call [`ThrottleState::current_state`] on every tick — that path
//! must be free of contention. The current state is held in an
//! [`AtomicU8`], so reads are a single atomic load with no allocation and no
//! RwLock traversal. The rolling sample window is only touched by the
//! single-writer monitor task and the rare `current_metrics` caller, so its
//! `RwLock` never contends with the hot path.

use std::sync::atomic::{AtomicU8, AtomicU64, Ordering};
use std::sync::RwLock;
use std::collections::VecDeque;

// ──────────────────────────────────────────────────────────────────────────────
// Tunables
// ──────────────────────────────────────────────────────────────────────────────

/// Window size in samples (6 × 5 s = 30 s observation window).
pub const WINDOW_SAMPLES: usize = 6;

/// Sampling period in seconds (monitor task tick rate).
pub const SAMPLE_INTERVAL_SECS: u64 = 5;

// Enter thresholds — cross these (averaged) to move UP the severity ladder.
/// CPU% above which the agent considers itself in Warn state.
pub const WARN_CPU_PCT: f64 = 1.5;
/// RSS (MB) above which the agent considers itself in Warn state.
pub const WARN_RSS_MB: u64 = 120;
/// CPU% above which the agent considers itself in Critical state.
pub const CRIT_CPU_PCT: f64 = 2.0;
/// RSS (MB) above which the agent considers itself in Critical state.
pub const CRIT_RSS_MB: u64 = 150;

// Exit thresholds — must be *both* below these averages to move DOWN the
// severity ladder. Gap between enter/exit is the hysteresis band.
/// Exit Warn back to Normal only when avg CPU drops below this.
pub const WARN_EXIT_CPU_PCT: f64 = 1.2;
/// Exit Warn back to Normal only when avg RSS drops below this (MB).
pub const WARN_EXIT_RSS_MB: u64 = 105;
/// Exit Critical back to Warn only when avg CPU drops below this.
pub const CRIT_EXIT_CPU_PCT: f64 = 1.7;
/// Exit Critical back to Warn only when avg RSS drops below this (MB).
pub const CRIT_EXIT_RSS_MB: u64 = 135;

/// Number of consecutive over-threshold windows required to ESCALATE.
pub const ESCALATE_CONSECUTIVE: u8 = 2;

/// Number of consecutive below-exit-threshold windows required to DE-ESCALATE.
pub const DEESCALATE_CONSECUTIVE: u8 = 3;

/// How long `Priority::Low` collectors should pause entirely when the
/// agent enters `Critical`. Collectors poll [`ThrottleState::low_pause_until_unix_secs`]
/// to decide whether they should skip the current tick.
pub const LOW_PAUSE_DURATION_SECS: u64 = 60;

// ──────────────────────────────────────────────────────────────────────────────
// State kind
// ──────────────────────────────────────────────────────────────────────────────

/// Three-level severity ladder.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
#[repr(u8)]
pub enum ThrottleStateKind {
    /// Within targets. No throttling.
    #[default]
    Normal = 0,
    /// Soft pushback: Low-priority collectors should halve their tick rate.
    Warn = 1,
    /// Hard pushback: Low-priority collectors should pause 60 s and drop.
    Critical = 2,
}

impl ThrottleStateKind {
    /// Returns the kebab-case name used in heartbeat event JSON payloads.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Normal => "normal",
            Self::Warn => "warn",
            Self::Critical => "critical",
        }
    }

    /// Converts the atomic tag byte back into a [`ThrottleStateKind`].
    #[must_use]
    pub const fn from_u8(v: u8) -> Self {
        match v {
            1 => Self::Warn,
            2 => Self::Critical,
            _ => Self::Normal,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Metrics snapshot
// ──────────────────────────────────────────────────────────────────────────────

/// Point-in-time measurement read from the OS.
#[derive(Debug, Clone, Copy, Default)]
pub struct Sample {
    /// Agent process CPU utilisation as a percent of total wall CPU time
    /// since the previous sample.
    pub cpu_percent: f64,
    /// Resident working-set size in bytes at sample time.
    pub rss_bytes: u64,
    /// Wall-clock Unix seconds when the sample was taken.
    pub taken_at_unix_secs: u64,
}

/// Averaged view of the current rolling window. Consumers use this in the
/// heartbeat event payload and `tracing` transition logs.
#[derive(Debug, Clone, Copy, Default)]
pub struct ThrottleMetrics {
    /// Rolling mean CPU utilisation percentage.
    pub cpu_percent: f64,
    /// Rolling mean RSS in MB (truncated).
    pub rss_mb: u64,
    /// Number of samples currently in the rolling window (0..=WINDOW_SAMPLES).
    pub sample_count: usize,
    /// Currently active throttle state.
    pub state: ThrottleStateKind,
}

// ──────────────────────────────────────────────────────────────────────────────
// ThrottleState
// ──────────────────────────────────────────────────────────────────────────────

/// Shared self-throttle state. Intended to be wrapped in `Arc`.
///
/// Collectors call [`current_state`](Self::current_state) and
/// [`low_pause_until_unix_secs`](Self::low_pause_until_unix_secs) on their
/// hot path. The monitor task owns writes through
/// [`record_sample`](Self::record_sample).
#[derive(Debug)]
pub struct ThrottleState {
    /// Currently active state as a [`ThrottleStateKind`] byte.
    state: AtomicU8,
    /// If non-zero, Low-priority collectors must pause until this Unix-second.
    /// Only set on entry to `Critical`, cleared by de-escalation below Warn.
    low_pause_until: AtomicU64,
    /// Rolling sample window. Single writer (monitor), rare readers
    /// (`current_metrics`). Never touched from the collector hot path.
    window: RwLock<VecDeque<Sample>>,
    /// Consecutive over-threshold sample windows (for escalation hysteresis).
    escalate_counter: AtomicU8,
    /// Consecutive below-exit-threshold sample windows (for de-escalation).
    deescalate_counter: AtomicU8,
}

impl ThrottleState {
    /// Creates a fresh `ThrottleState` in the `Normal` state with an empty
    /// rolling window.
    #[must_use]
    pub fn new() -> Self {
        Self {
            state: AtomicU8::new(ThrottleStateKind::Normal as u8),
            low_pause_until: AtomicU64::new(0),
            window: RwLock::new(VecDeque::with_capacity(WINDOW_SAMPLES)),
            escalate_counter: AtomicU8::new(0),
            deescalate_counter: AtomicU8::new(0),
        }
    }

    /// Reads the current throttle state with a single atomic load.
    ///
    /// This is the lock-free hot path invoked by every collector tick.
    #[must_use]
    pub fn current_state(&self) -> ThrottleStateKind {
        ThrottleStateKind::from_u8(self.state.load(Ordering::Relaxed))
    }

    /// Returns the Unix-second until which `Priority::Low` collectors should
    /// remain paused. A return value of `0` means "no pause in effect".
    #[must_use]
    pub fn low_pause_until_unix_secs(&self) -> u64 {
        self.low_pause_until.load(Ordering::Relaxed)
    }

    /// Convenience: returns `true` if a `Priority::Low` collector should
    /// skip work for the current tick given `now` (Unix seconds). Combines
    /// the hard pause window (`Critical`) with the half-rate hint (`Warn`);
    /// `Warn` returns `false` — collectors with `Warn` semantics should
    /// implement their own 50% skip based on a simple alternation counter.
    #[must_use]
    pub fn should_low_priority_skip(&self, now_unix_secs: u64) -> bool {
        match self.current_state() {
            ThrottleStateKind::Critical => {
                let until = self.low_pause_until.load(Ordering::Relaxed);
                until != 0 && now_unix_secs < until
            }
            _ => false,
        }
    }

    /// Returns the rolling-window averages + current state.
    ///
    /// Touches `RwLock::read` — callers should invoke this at most once per
    /// few seconds (heartbeat emission), never on the collector hot path.
    #[must_use]
    pub fn current_metrics(&self) -> ThrottleMetrics {
        let guard = match self.window.read() {
            Ok(g) => g,
            Err(p) => p.into_inner(), // poisoned: read anyway; monitor is single-writer
        };
        let count = guard.len();
        if count == 0 {
            return ThrottleMetrics {
                cpu_percent: 0.0,
                rss_mb: 0,
                sample_count: 0,
                state: self.current_state(),
            };
        }
        let mut cpu_sum = 0.0_f64;
        let mut rss_sum = 0_u128;
        for s in guard.iter() {
            cpu_sum += s.cpu_percent;
            rss_sum += u128::from(s.rss_bytes);
        }
        let cpu_avg = cpu_sum / count as f64;
        // MB = bytes / 1_048_576; use u128 to dodge overflow on huge RSS.
        let rss_avg_mb = (rss_sum / count as u128 / (1024 * 1024)) as u64;
        ThrottleMetrics {
            cpu_percent: cpu_avg,
            rss_mb: rss_avg_mb,
            sample_count: count,
            state: self.current_state(),
        }
    }

    /// Pushes a new [`Sample`] into the rolling window and re-evaluates the
    /// state ladder. Returns `Some(new_state)` if the state transitioned
    /// (for logging + heartbeat emission), `None` otherwise.
    ///
    /// This is the single writer — only the throttle monitor task calls it.
    pub fn record_sample(&self, sample: Sample) -> Option<ThrottleStateKind> {
        let (cpu_avg, rss_mb) = {
            let mut guard = match self.window.write() {
                Ok(g) => g,
                Err(p) => p.into_inner(),
            };
            if guard.len() == WINDOW_SAMPLES {
                guard.pop_front();
            }
            guard.push_back(sample);

            // Compute averages while we hold the lock so we never re-borrow.
            let count = guard.len();
            let mut cpu_sum = 0.0_f64;
            let mut rss_sum = 0_u128;
            for s in guard.iter() {
                cpu_sum += s.cpu_percent;
                rss_sum += u128::from(s.rss_bytes);
            }
            let cpu_avg = cpu_sum / count as f64;
            let rss_avg_mb = (rss_sum / count as u128 / (1024 * 1024)) as u64;
            (cpu_avg, rss_avg_mb)
        };

        // Don't make decisions until the window is fully populated. A
        // half-empty window under-reports the true 30 s average.
        let window_full = {
            let guard = self.window.read().ok();
            guard.map(|g| g.len() >= WINDOW_SAMPLES).unwrap_or(false)
        };
        if !window_full {
            return None;
        }

        self.evaluate(cpu_avg, rss_mb, sample.taken_at_unix_secs)
    }

    /// Core state-machine evaluation. Separate from [`record_sample`] so
    /// the unit tests can drive the ladder synthetically without touching
    /// the window storage.
    #[cfg(test)]
    pub(crate) fn evaluate(
        &self,
        cpu_avg: f64,
        rss_mb: u64,
        now_unix_secs: u64,
    ) -> Option<ThrottleStateKind> {
        self.evaluate_inner(cpu_avg, rss_mb, now_unix_secs)
    }

    #[cfg(not(test))]
    fn evaluate(
        &self,
        cpu_avg: f64,
        rss_mb: u64,
        now_unix_secs: u64,
    ) -> Option<ThrottleStateKind> {
        self.evaluate_inner(cpu_avg, rss_mb, now_unix_secs)
    }

    fn evaluate_inner(
        &self,
        cpu_avg: f64,
        rss_mb: u64,
        now_unix_secs: u64,
    ) -> Option<ThrottleStateKind> {
        let current = self.current_state();

        let target = classify(cpu_avg, rss_mb, current);

        if target == current {
            // Still at the same nominal level — reset counters that
            // were tracking a move we're no longer making.
            match current {
                ThrottleStateKind::Normal => {
                    self.escalate_counter.store(0, Ordering::Relaxed);
                    self.deescalate_counter.store(0, Ordering::Relaxed);
                }
                ThrottleStateKind::Warn => {
                    // If we're still at Warn, we're neither climbing to
                    // Critical nor falling back to Normal — zero both.
                    self.escalate_counter.store(0, Ordering::Relaxed);
                    self.deescalate_counter.store(0, Ordering::Relaxed);
                }
                ThrottleStateKind::Critical => {
                    self.escalate_counter.store(0, Ordering::Relaxed);
                    self.deescalate_counter.store(0, Ordering::Relaxed);
                }
            }
            return None;
        }

        // target != current. Apply hysteresis based on direction.
        if (target as u8) > (current as u8) {
            // Escalating. Require ESCALATE_CONSECUTIVE confirmations.
            self.deescalate_counter.store(0, Ordering::Relaxed);
            let prev = self.escalate_counter.fetch_add(1, Ordering::Relaxed);
            if prev + 1 >= ESCALATE_CONSECUTIVE {
                self.escalate_counter.store(0, Ordering::Relaxed);
                self.commit_state(target, now_unix_secs);
                return Some(target);
            }
            None
        } else {
            // De-escalating. Require DEESCALATE_CONSECUTIVE confirmations.
            self.escalate_counter.store(0, Ordering::Relaxed);
            let prev = self.deescalate_counter.fetch_add(1, Ordering::Relaxed);
            if prev + 1 >= DEESCALATE_CONSECUTIVE {
                self.deescalate_counter.store(0, Ordering::Relaxed);
                self.commit_state(target, now_unix_secs);
                return Some(target);
            }
            None
        }
    }

    /// Writes the atomic state byte and adjusts the `low_pause_until`
    /// window as a side effect of entering/leaving `Critical`.
    fn commit_state(&self, new_state: ThrottleStateKind, now_unix_secs: u64) {
        self.state.store(new_state as u8, Ordering::Relaxed);
        match new_state {
            ThrottleStateKind::Critical => {
                self.low_pause_until.store(
                    now_unix_secs.saturating_add(LOW_PAUSE_DURATION_SECS),
                    Ordering::Relaxed,
                );
            }
            ThrottleStateKind::Normal => {
                // Fully clear the pause — we're healthy again.
                self.low_pause_until.store(0, Ordering::Relaxed);
            }
            ThrottleStateKind::Warn => {
                // Leave low_pause_until in place — if we fell from
                // Critical to Warn, any remaining pause timer is still
                // respected by collectors (`should_low_priority_skip`
                // only checks in Critical anyway). Explicitly clear.
                self.low_pause_until.store(0, Ordering::Relaxed);
            }
        }
    }
}

impl Default for ThrottleState {
    fn default() -> Self {
        Self::new()
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Decision logic
// ──────────────────────────────────────────────────────────────────────────────

/// Pure-function classification: given the current averages and the
/// *current* state (for exit-hysteresis), return the *target* state. Does
/// not touch any shared state.
fn classify(cpu_avg: f64, rss_mb: u64, current: ThrottleStateKind) -> ThrottleStateKind {
    // Escalation checks always use the higher "enter" thresholds.
    let should_be_critical = cpu_avg >= CRIT_CPU_PCT || rss_mb >= CRIT_RSS_MB;
    let should_be_warn = cpu_avg >= WARN_CPU_PCT || rss_mb >= WARN_RSS_MB;

    // De-escalation checks use the lower "exit" thresholds.
    let can_leave_critical = cpu_avg < CRIT_EXIT_CPU_PCT && rss_mb < CRIT_EXIT_RSS_MB;
    let can_leave_warn = cpu_avg < WARN_EXIT_CPU_PCT && rss_mb < WARN_EXIT_RSS_MB;

    match current {
        ThrottleStateKind::Normal => {
            if should_be_critical {
                ThrottleStateKind::Critical
            } else if should_be_warn {
                ThrottleStateKind::Warn
            } else {
                ThrottleStateKind::Normal
            }
        }
        ThrottleStateKind::Warn => {
            if should_be_critical {
                ThrottleStateKind::Critical
            } else if can_leave_warn {
                ThrottleStateKind::Normal
            } else {
                ThrottleStateKind::Warn
            }
        }
        ThrottleStateKind::Critical => {
            if can_leave_critical {
                // We drop to Warn first, not straight to Normal — a second
                // full window below the WARN_EXIT thresholds is then
                // required for the next step down.
                ThrottleStateKind::Warn
            } else {
                ThrottleStateKind::Critical
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn mb(v: u64) -> u64 {
        v * 1024 * 1024
    }

    fn sample(cpu: f64, rss_bytes: u64, ts: u64) -> Sample {
        Sample {
            cpu_percent: cpu,
            rss_bytes,
            taken_at_unix_secs: ts,
        }
    }

    #[test]
    fn starts_normal_with_empty_window() {
        let st = ThrottleState::new();
        assert_eq!(st.current_state(), ThrottleStateKind::Normal);
        let m = st.current_metrics();
        assert_eq!(m.sample_count, 0);
        assert_eq!(m.state, ThrottleStateKind::Normal);
    }

    #[test]
    fn incomplete_window_never_transitions() {
        // Fewer than WINDOW_SAMPLES samples must not flip the state even
        // if every sample is catastrophically over-budget.
        let st = ThrottleState::new();
        for i in 0..(WINDOW_SAMPLES - 1) {
            let out = st.record_sample(sample(99.0, mb(500), 1000 + i as u64));
            assert!(out.is_none(), "no transition before window is full");
            assert_eq!(st.current_state(), ThrottleStateKind::Normal);
        }
    }

    #[test]
    fn escalates_normal_to_warn_after_two_full_windows() {
        let st = ThrottleState::new();
        // First full window — classify says Warn, escalate counter = 1.
        for i in 0..WINDOW_SAMPLES {
            let _ = st.record_sample(sample(1.6, mb(100), 1000 + i as u64));
        }
        // Still Normal because we need ESCALATE_CONSECUTIVE = 2.
        assert_eq!(st.current_state(), ThrottleStateKind::Normal);

        // Second confirmation sample (already window-full; every new record
        // re-evaluates). After one more over-threshold sample we should flip.
        let out = st.record_sample(sample(1.6, mb(100), 2000));
        assert_eq!(out, Some(ThrottleStateKind::Warn));
        assert_eq!(st.current_state(), ThrottleStateKind::Warn);
    }

    #[test]
    fn escalates_normal_to_critical_via_warn() {
        // A sustained critical load should pass through Warn (one evaluation
        // step) then reach Critical on the next confirmation.
        let st = ThrottleState::new();
        for i in 0..WINDOW_SAMPLES {
            let _ = st.record_sample(sample(2.5, mb(100), 1000 + i as u64));
        }
        // First full window — target is Critical, counter = 1. Still Normal.
        assert_eq!(st.current_state(), ThrottleStateKind::Normal);

        // Second confirmation — flip straight from Normal to Critical (two
        // consecutive over-threshold windows pointing at the same target).
        let out = st.record_sample(sample(2.5, mb(100), 2000));
        assert_eq!(out, Some(ThrottleStateKind::Critical));
        assert_eq!(st.current_state(), ThrottleStateKind::Critical);
        assert!(st.low_pause_until_unix_secs() >= 2000 + LOW_PAUSE_DURATION_SECS);
    }

    #[test]
    fn critical_rss_alone_is_sufficient() {
        let st = ThrottleState::new();
        // RSS well above 150 MB with near-zero CPU must still trip Critical.
        for i in 0..WINDOW_SAMPLES {
            let _ = st.record_sample(sample(0.1, mb(200), 1000 + i as u64));
        }
        let out = st.record_sample(sample(0.1, mb(200), 2000));
        assert_eq!(out, Some(ThrottleStateKind::Critical));
    }

    #[test]
    fn deescalation_requires_three_consecutive_low_windows() {
        // Use `evaluate` directly to side-step window averaging: we want
        // to test the hysteresis counter, not the rolling math. The
        // average-based integration is covered by `classify_matrix` plus
        // the other escalation tests.
        let st = ThrottleState::new();

        // Prime to Critical via two full critical evaluations.
        assert_eq!(st.evaluate(3.0, 200, 1000), None);
        assert_eq!(st.evaluate(3.0, 200, 1005), Some(ThrottleStateKind::Critical));
        assert_eq!(st.current_state(), ThrottleStateKind::Critical);

        // First quiet evaluation — counter=1.
        assert_eq!(st.evaluate(0.5, 50, 2000), None);
        assert_eq!(st.current_state(), ThrottleStateKind::Critical);

        // Second quiet evaluation — counter=2.
        assert_eq!(st.evaluate(0.5, 50, 2005), None);
        assert_eq!(st.current_state(), ThrottleStateKind::Critical);

        // Third quiet evaluation — counter=3, de-escalate to Warn.
        assert_eq!(
            st.evaluate(0.5, 50, 2010),
            Some(ThrottleStateKind::Warn)
        );
        assert_eq!(st.current_state(), ThrottleStateKind::Warn);

        // Three more quiet evaluations to go Warn → Normal.
        assert_eq!(st.evaluate(0.5, 50, 2015), None);
        assert_eq!(st.evaluate(0.5, 50, 2020), None);
        assert_eq!(
            st.evaluate(0.5, 50, 2025),
            Some(ThrottleStateKind::Normal)
        );
        assert_eq!(st.current_state(), ThrottleStateKind::Normal);
    }

    #[test]
    fn hysteresis_prevents_flapping() {
        // Sample right in the hysteresis band (above exit, below enter)
        // must NOT flip from Warn back to Normal repeatedly.
        let st = ThrottleState::new();
        // First drive into Warn.
        for i in 0..(WINDOW_SAMPLES + 1) {
            let _ = st.record_sample(sample(1.8, mb(100), 1000 + i as u64));
        }
        assert_eq!(st.current_state(), ThrottleStateKind::Warn);

        // 1.3% sits between WARN_EXIT_CPU_PCT (1.2) and WARN_CPU_PCT (1.5)
        // — above the exit threshold, so we stay in Warn.
        for i in 0..10 {
            let out = st.record_sample(sample(1.3, mb(100), 2000 + i));
            assert!(out.is_none(), "sample {i} must not cause transition");
            assert_eq!(st.current_state(), ThrottleStateKind::Warn);
        }
    }

    #[test]
    fn critical_sets_low_pause_window() {
        let st = ThrottleState::new();
        for i in 0..WINDOW_SAMPLES {
            let _ = st.record_sample(sample(3.0, mb(200), 5000 + i as u64));
        }
        let _ = st.record_sample(sample(3.0, mb(200), 5100));
        assert_eq!(st.current_state(), ThrottleStateKind::Critical);
        let until = st.low_pause_until_unix_secs();
        assert_eq!(until, 5100 + LOW_PAUSE_DURATION_SECS);

        // Collector helper: before `until` → skip.
        assert!(st.should_low_priority_skip(5100));
        assert!(st.should_low_priority_skip(5100 + LOW_PAUSE_DURATION_SECS - 1));
        // After `until` → no skip even if we're still Critical (pause
        // expires; the monitor is expected to either refresh or allow the
        // next Low tick).
        assert!(!st.should_low_priority_skip(5100 + LOW_PAUSE_DURATION_SECS));
    }

    #[test]
    fn warn_does_not_force_low_skip() {
        let st = ThrottleState::new();
        for i in 0..(WINDOW_SAMPLES + 1) {
            let _ = st.record_sample(sample(1.8, mb(100), 1000 + i as u64));
        }
        assert_eq!(st.current_state(), ThrottleStateKind::Warn);
        assert!(!st.should_low_priority_skip(2000));
    }

    #[test]
    fn classify_matrix() {
        // Classify is pure; the table below is the authoritative
        // decision matrix for the state machine.
        use ThrottleStateKind::{Critical, Normal, Warn};

        // From Normal
        assert_eq!(classify(0.5, 50, Normal), Normal);
        assert_eq!(classify(1.5, 50, Normal), Warn);
        assert_eq!(classify(0.5, 120, Normal), Warn);
        assert_eq!(classify(2.0, 50, Normal), Critical);
        assert_eq!(classify(0.5, 150, Normal), Critical);

        // From Warn — exit requires both below WARN_EXIT.
        assert_eq!(classify(1.0, 100, Warn), Normal); // both below exit
        assert_eq!(classify(1.3, 100, Warn), Warn); // cpu between exit and enter
        assert_eq!(classify(1.0, 110, Warn), Warn); // rss between exit and enter
        assert_eq!(classify(2.0, 100, Warn), Critical);

        // From Critical — exit requires both below CRIT_EXIT, and only
        // goes as far as Warn (second step down requires another window).
        assert_eq!(classify(3.0, 200, Critical), Critical);
        assert_eq!(classify(1.6, 130, Critical), Warn);
        assert_eq!(classify(1.8, 130, Critical), Critical); // cpu still ≥ exit
        assert_eq!(classify(1.6, 136, Critical), Critical); // rss still ≥ exit
    }

    #[test]
    fn zero_measurement_stays_normal() {
        // Non-Windows dev builds return (0.0, 0) — must remain Normal.
        let st = ThrottleState::new();
        for i in 0..(WINDOW_SAMPLES + 5) {
            let out = st.record_sample(sample(0.0, 0, 1000 + i as u64));
            assert!(out.is_none());
            assert_eq!(st.current_state(), ThrottleStateKind::Normal);
        }
    }

    #[test]
    fn metrics_average_across_window() {
        let st = ThrottleState::new();
        for i in 0..WINDOW_SAMPLES {
            let _ = st.record_sample(sample(1.0, mb(100), 1000 + i as u64));
        }
        let m = st.current_metrics();
        assert_eq!(m.sample_count, WINDOW_SAMPLES);
        assert!((m.cpu_percent - 1.0).abs() < 1e-9);
        assert_eq!(m.rss_mb, 100);
        assert_eq!(m.state, ThrottleStateKind::Normal);
    }
}
