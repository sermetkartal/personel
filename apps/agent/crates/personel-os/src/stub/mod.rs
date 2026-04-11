//! Non-Windows stub implementations.
//!
//! These modules provide the same public API as their `windows::` counterparts
//! so the workspace compiles on macOS/Linux for developer ergonomics. Every
//! function returns an error; the agent cannot run on non-Windows platforms
//! in Phase 1.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

// ── input ─────────────────────────────────────────────────────────────────────

/// Stub input module for non-Windows builds.
pub mod input {
    use super::*;

    /// Information about the foreground window (stub).
    #[derive(Debug, Clone)]
    pub struct ForegroundWindowInfo {
        /// Window title.
        pub title: String,
        /// Process ID.
        pub pid: u32,
        /// HWND value.
        pub hwnd: usize,
    }

    /// Returns the number of milliseconds since the last user input event.
    ///
    /// # Errors
    ///
    /// Always returns an error on non-Windows platforms.
    pub fn last_input_idle_ms() -> Result<u64> {
        Err(AgentError::CollectorStart {
            name: "idle",
            reason: "GetLastInputInfo is Windows-only".into(),
        })
    }

    /// Returns foreground window information.
    ///
    /// # Errors
    ///
    /// Always returns an error on non-Windows platforms.
    pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
        Err(AgentError::CollectorStart {
            name: "window_title",
            reason: "GetForegroundWindow is Windows-only".into(),
        })
    }
}

// ── dpapi ─────────────────────────────────────────────────────────────────────

/// Stub DPAPI module for non-Windows builds.
pub mod dpapi {
    use super::*;

    /// Seals `plaintext` — always errors on non-Windows.
    ///
    /// # Errors
    ///
    /// Always returns an error.
    pub fn protect(_plaintext: &[u8]) -> Result<Vec<u8>> {
        Err(AgentError::Dpapi("DPAPI is Windows-only".into()))
    }

    /// Unseals a blob — always errors on non-Windows.
    ///
    /// # Errors
    ///
    /// Always returns an error.
    pub fn unprotect(_sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
        Err(AgentError::Dpapi("DPAPI is Windows-only".into()))
    }
}

// ── anti_tamper ───────────────────────────────────────────────────────────────

/// Stub anti-tamper module for non-Windows builds.
pub mod anti_tamper {
    /// A tamper check result (stub).
    #[derive(Debug, Clone)]
    pub struct TamperCheckResult {
        /// Check name.
        pub check_name: &'static str,
        /// Whether tampering was detected.
        pub detected: bool,
        /// Detail string.
        pub detail: String,
    }

    /// Returns an empty list (no checks on non-Windows).
    #[must_use]
    pub fn run_all_checks() -> Vec<TamperCheckResult> {
        vec![]
    }
}

// ── etw ───────────────────────────────────────────────────────────────────────

/// Stub ETW module for non-Windows builds.
pub mod etw {
    use super::*;

    /// ETW session stub.
    pub struct EtwSession;

    impl EtwSession {
        /// Always errors on non-Windows.
        ///
        /// # Errors
        ///
        /// Always returns an error.
        pub fn start(_name: &str) -> Result<Self> {
            Err(AgentError::CollectorStart {
                name: "etw",
                reason: "ETW is Windows-only".into(),
            })
        }
    }
}

// ── capture ───────────────────────────────────────────────────────────────────

/// Stub capture module for non-Windows builds.
pub mod capture {
    use super::*;

    /// A captured frame (stub).
    pub struct CapturedFrame {
        /// Pixels.
        pub pixels: Vec<u8>,
        /// Width.
        pub width: u32,
        /// Height.
        pub height: u32,
        /// Monitor index.
        pub monitor_index: u32,
    }

    /// DXGI capture stub.
    pub struct DxgiCapture;

    impl DxgiCapture {
        /// Always errors on non-Windows.
        ///
        /// # Errors
        ///
        /// Always returns an error.
        pub fn open(_monitor: u32) -> Result<Self> {
            Err(AgentError::CollectorStart {
                name: "screen",
                reason: "DXGI is Windows-only".into(),
            })
        }

        /// Always errors.
        ///
        /// # Errors
        ///
        /// Always returns an error.
        pub fn capture_frame(&self) -> Result<CapturedFrame> {
            Err(AgentError::CollectorStart {
                name: "screen",
                reason: "DXGI is Windows-only".into(),
            })
        }
    }
}

// ── service ───────────────────────────────────────────────────────────────────

/// Stub service module for non-Windows builds.
pub mod service {
    use super::*;
    use tokio::sync::oneshot;

    /// Always errors on non-Windows.
    ///
    /// # Errors
    ///
    /// Always returns an error.
    pub fn run_as_service(_shutdown_tx: oneshot::Sender<()>) -> Result<()> {
        Err(AgentError::Internal("Windows service is not available on this platform".into()))
    }

    /// Returns false on non-Windows.
    #[must_use]
    pub fn is_service_context() -> bool {
        false
    }
}
