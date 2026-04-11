//! systemd service integration.
//!
//! Provides helpers for systemd `Type=notify` readiness signalling, watchdog
//! keep-alive, and journal-structured logging integration. The agent systemd
//! unit is configured with `Type=notify`; without a `sd_notify(READY=1)` call
//! the unit remains in `activating` state indefinitely and systemd will
//! eventually timeout and restart it.
//!
//! # systemd unit model (ADR 0016)
//!
//! ```text
//! personel-agent.service  — Type=notify, Restart=on-failure
//! personel-watchdog.service — Requires=personel-agent, Restart=always
//! personel-agent.socket   — UNIX domain socket for admin CLI
//! personel-session.service — systemd --user, Wayland/X11 session collectors
//! ```
//!
//! # Phase 2.2 implementation plan
//!
//! Replace stubs with calls to `sd_notify(3)` via the `libsystemd` crate or
//! by writing directly to `$NOTIFY_SOCKET` (a Unix datagram socket) in the
//! format systemd expects: newline-separated `KEY=VALUE` strings.
//!
//! ```text
//! READY=1\n                 — service is ready
//! WATCHDOG=1\n              — watchdog keep-alive ping
//! STATUS=collecting\n       — human-readable status shown in `systemctl status`
//! RELOADING=1\n             — service is reloading configuration
//! STOPPING=1\n              — orderly shutdown initiated
//! ```

use personel_core::error::{AgentError, Result};

/// Signals systemd that the service has finished initialisation and is ready
/// to handle requests. Corresponds to `sd_notify(0, "READY=1")`.
///
/// Must be called after all collectors are started and the transport connection
/// is established. Until this call the unit stays in `activating` state.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// Phase 2.2: returns [`AgentError::Io`] if `$NOTIFY_SOCKET` is unset (i.e.
/// the process was not started by systemd — not an error in that case, caller
/// should treat absence as no-op).
pub fn notify_ready() -> Result<()> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "systemd::notify_ready",
    })
}

/// Sends a watchdog keep-alive ping to systemd.
///
/// Call this at an interval of at most `WatchdogUSec / 2` (the value is
/// provided in `$WATCHDOG_USEC`). If the watchdog ping is missed systemd
/// restarts the unit.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
pub fn notify_watchdog() -> Result<()> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "systemd::notify_watchdog",
    })
}

/// Updates the human-readable status string shown in `systemctl status`.
///
/// # Arguments
///
/// * `status` — free-form ASCII string, e.g. `"collecting: 42 events/s"`.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
pub fn notify_status(_status: &str) -> Result<()> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "systemd::notify_status",
    })
}

/// Signals that the service is about to stop cleanly.
///
/// Allows systemd to distinguish a deliberate shutdown from a crash and avoid
/// triggering the `Restart=on-failure` policy.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
pub fn notify_stopping() -> Result<()> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "systemd::notify_stopping",
    })
}

/// Returns the watchdog interval in microseconds from `$WATCHDOG_USEC`, or
/// `None` if the variable is absent (not running under systemd with
/// `WatchdogSec` set).
///
/// The caller should use `interval / 2` as the keep-alive ping frequency.
#[must_use]
pub fn watchdog_interval_us() -> Option<u64> {
    std::env::var("WATCHDOG_USEC")
        .ok()
        .and_then(|v| v.parse::<u64>().ok())
}

/// Initialises the systemd journal logger for structured log output.
///
/// After this call all `tracing` events are forwarded to the systemd journal
/// with structured fields (`PRIORITY`, `CODE_FILE`, `CODE_LINE`, custom fields).
/// This replaces the default `tracing-subscriber` stderr formatter when running
/// under systemd.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// Phase 2.2: returns [`AgentError::Io`] if the journal socket is unavailable.
pub fn init_journal_logger() -> Result<()> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "systemd::init_journal_logger",
    })
}
