//! Non-Windows stub implementations.
//!
//! These modules provide the same public API as their `windows::` counterparts
//! so the workspace compiles on macOS/Linux for developer ergonomics. Every
//! function returns `AgentError::Unsupported { os, component }`. Phase 2 will
//! replace these stubs with real macOS (Endpoint Security Framework,
//! ScreenCaptureKit, Network Extension) and Linux (fanotify, eBPF,
//! X11/Wayland) implementations — see ADRs 0015 and 0016.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// The current OS identifier used in `Unsupported` errors.
#[cfg(target_os = "macos")]
const OS: &str = "macos";
#[cfg(target_os = "linux")]
const OS: &str = "linux";
#[cfg(not(any(target_os = "macos", target_os = "linux", target_os = "windows")))]
const OS: &str = "other";

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
        /// HWND value (placeholder on non-Windows).
        pub hwnd: usize,
    }

    /// Returns the number of milliseconds since the last user input event.
    ///
    /// # Errors
    ///
    /// Always returns `AgentError::Unsupported` on non-Windows platforms.
    pub fn last_input_idle_ms() -> Result<u64> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "input::last_input_idle_ms",
        })
    }

    /// Returns foreground window information.
    ///
    /// # Errors
    ///
    /// Always returns `AgentError::Unsupported` on non-Windows platforms.
    pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "input::foreground_window_info",
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
    /// Always returns `AgentError::Unsupported`.
    pub fn protect(_plaintext: &[u8]) -> Result<Vec<u8>> {
        Err(AgentError::Unsupported { os: OS, component: "dpapi::protect" })
    }

    /// Unseals a blob — always errors on non-Windows.
    ///
    /// # Errors
    ///
    /// Always returns `AgentError::Unsupported`.
    pub fn unprotect(_sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
        Err(AgentError::Unsupported { os: OS, component: "dpapi::unprotect" })
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
        /// Always returns `AgentError::Unsupported`.
        pub fn start(_name: &str) -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "etw::EtwSession::start" })
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
        /// Always returns `AgentError::Unsupported`.
        pub fn open(_monitor: u32) -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "capture::DxgiCapture::open" })
        }

        /// Always errors.
        ///
        /// # Errors
        ///
        /// Always returns `AgentError::Unsupported`.
        pub fn capture_frame(&self) -> Result<CapturedFrame> {
            Err(AgentError::Unsupported { os: OS, component: "capture::DxgiCapture::capture_frame" })
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
    /// Always returns `AgentError::Unsupported`.
    pub fn run_as_service(_shutdown_tx: oneshot::Sender<()>) -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "service::run_as_service" })
    }

    /// Returns false on non-Windows.
    #[must_use]
    pub fn is_service_context() -> bool {
        false
    }
}
