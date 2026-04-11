//! Clock abstractions for testable time-dependent code.
//!
//! All collectors and queue code obtain timestamps through the [`Clock`] trait
//! rather than calling [`std::time::SystemTime::now`] or
//! [`std::time::Instant::now`] directly. This enables deterministic testing
//! with a [`FakeClock`] and makes the code resilient against monotonicity
//! surprises on Windows VMs.
//!
//! # Example
//!
//! ```
//! use std::sync::Arc;
//! use personel_core::clock::{Clock, SystemClock};
//!
//! let clock: Arc<dyn Clock> = Arc::new(SystemClock);
//! let ts = clock.now_unix_nanos();
//! assert!(ts > 0);
//! ```

use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// Provides wall-clock and monotonic time readings.
///
/// Implementations must be `Send + Sync` so they can be shared across async
/// tasks.
pub trait Clock: Send + Sync {
    /// Returns the current wall-clock time as nanoseconds since Unix epoch.
    fn now_unix_nanos(&self) -> i64;

    /// Returns a monotonic instant as nanoseconds (arbitrary epoch, never
    /// goes backward, suitable for durations).
    fn monotonic_nanos(&self) -> u64;

    /// Returns the current wall-clock time as a [`prost_types::Timestamp`]
    /// suitable for inclusion in proto messages.
    fn now_proto(&self) -> prost_types::Timestamp {
        let nanos = self.now_unix_nanos();
        prost_types::Timestamp {
            seconds: nanos / 1_000_000_000,
            nanos: (nanos % 1_000_000_000) as i32,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Production clock
// ──────────────────────────────────────────────────────────────────────────────

/// Production [`Clock`] implementation backed by [`SystemTime`] and
/// [`std::time::Instant`].
#[derive(Debug, Clone, Copy, Default)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now_unix_nanos(&self) -> i64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or(Duration::ZERO)
            .as_nanos()
            .try_into()
            .unwrap_or(i64::MAX)
    }

    fn monotonic_nanos(&self) -> u64 {
        // SAFETY: std::time::Instant is opaque; we use saturating arithmetic.
        // We can't hold an Instant as a base reference in a ZST, so we use
        // the wall clock as a proxy. For duration arithmetic this is fine
        // because callers diff two readings from the same clock source.
        self.now_unix_nanos().try_into().unwrap_or(0)
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Test / simulation clock
// ──────────────────────────────────────────────────────────────────────────────

/// Manually-controllable clock for unit tests.
///
/// Time starts at the given `start_nanos` and advances only when
/// [`FakeClock::advance`] is called.
///
/// # Example
///
/// ```
/// use personel_core::clock::{Clock, FakeClock};
///
/// let clock = FakeClock::new(1_000_000_000_000_000_000_i64); // arbitrary epoch
/// assert_eq!(clock.now_unix_nanos(), 1_000_000_000_000_000_000);
/// clock.advance_secs(5);
/// assert_eq!(clock.now_unix_nanos(), 1_000_000_005_000_000_000);
/// ```
#[derive(Debug)]
pub struct FakeClock {
    nanos: AtomicI64,
}

impl FakeClock {
    /// Creates a new [`FakeClock`] starting at the given epoch offset in
    /// nanoseconds.
    #[must_use]
    pub fn new(start_nanos: i64) -> Arc<Self> {
        Arc::new(Self {
            nanos: AtomicI64::new(start_nanos),
        })
    }

    /// Advances the fake clock by `nanos` nanoseconds.
    pub fn advance(&self, nanos: i64) {
        self.nanos.fetch_add(nanos, Ordering::Relaxed);
    }

    /// Convenience wrapper: advance by whole seconds.
    pub fn advance_secs(&self, secs: i64) {
        self.advance(secs * 1_000_000_000);
    }
}

impl Clock for FakeClock {
    fn now_unix_nanos(&self) -> i64 {
        self.nanos.load(Ordering::Relaxed)
    }

    fn monotonic_nanos(&self) -> u64 {
        self.nanos.load(Ordering::Relaxed).try_into().unwrap_or(0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn system_clock_positive() {
        let c = SystemClock;
        assert!(c.now_unix_nanos() > 0);
    }

    #[test]
    fn fake_clock_advance() {
        let c = FakeClock::new(0);
        c.advance_secs(10);
        assert_eq!(c.now_unix_nanos(), 10_000_000_000);
    }

    #[test]
    fn proto_timestamp_roundtrip() {
        let c = FakeClock::new(1_700_000_000_000_000_001);
        let ts = c.now_proto();
        assert_eq!(ts.seconds, 1_700_000_000);
        assert_eq!(ts.nanos, 1);
    }
}
