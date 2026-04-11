//! Thin safe wrapper around ETW user-mode sessions.
//!
//! Provides a minimal surface for starting an ETW real-time session,
//! registering providers, and receiving events via a callback. The full ETW
//! implementation for each collector is in the respective collector module.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Implement `EtwSession::start()` via `StartTrace` / `EnableTraceEx2`.
//! - Implement `EtwSession::process_events()` via `ProcessTrace` on a
//!   dedicated OS thread (ETW callback must not be async).
//! - Wrap `EVENT_RECORD` decoding for `Microsoft-Windows-Kernel-Process`
//!   and `Microsoft-Windows-Kernel-FileIO` providers.

use personel_core::error::{AgentError, Result};

/// Handle to an active ETW real-time session.
///
/// Drop to stop the session (`CloseTrace` + `StopTrace`).
pub struct EtwSession {
    // TODO: store TRACEHANDLE here.
    _private: (),
}

impl EtwSession {
    /// Starts a new ETW real-time consumer session.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::CollectorStart`] if `StartTrace` fails.
    pub fn start(_session_name: &str) -> Result<Self> {
        // TODO: call StartTrace + EnableTraceEx2 for the desired providers.
        Err(AgentError::CollectorStart {
            name: "etw",
            reason: "ETW session start not yet implemented".into(),
        })
    }
}

impl Drop for EtwSession {
    fn drop(&mut self) {
        // TODO: CloseTrace + StopTrace.
    }
}
