//! `personel-platform` — compile-time platform selector facade for the
//! Personel agent's OS-specific backends.
//!
//! Consumers (personel-collectors, personel-agent) depend on this crate and
//! call the re-exported modules without `#[cfg(target_os)]` branches. At
//! compile time, this facade dispatches to the right backend:
//!
//! - `target_os = "windows"` → `personel-os` (real Win32 implementation)
//! - `target_os = "macos"`   → `personel-os-macos` (Phase 2.1 scaffold)
//! - `target_os = "linux"`   → `personel-os-linux` (Phase 2.1 scaffold)
//! - everything else         → local `stub` module returning `Unsupported`
//!
//! Only the API items that are present on ALL three backends are re-exported
//! here. Platform-specific modules (Windows ETW, macOS TCC, Linux fanotify,
//! etc.) must be accessed directly via `#[cfg]` in downstream crates — the
//! facade does not try to unify fundamentally non-overlapping surfaces.

#![deny(unsafe_code)]
#![warn(missing_docs)]

use personel_core::error::{AgentError, Result};

// ── input: foreground_window_info + last_input_idle_ms ──────────────────────
//
// These two functions are present on all backends with identical signatures,
// so we re-export them through a thin `input` module.

/// Input-related OS queries (foreground window, idle detection).
pub mod input {
    use super::*;

    /// Information about the currently foreground window. Fields are
    /// best-effort: on non-Windows backends the `hwnd` is a placeholder.
    pub use backend_input::ForegroundWindowInfo;

    /// Milliseconds since the last user input event on the primary session.
    ///
    /// # Errors
    ///
    /// Returns `AgentError::Unsupported` when called on a backend that does
    /// not yet have a real implementation (macOS and Linux during Phase 2.1
    /// scaffolding).
    pub fn last_input_idle_ms() -> Result<u64> {
        backend_input::last_input_idle_ms()
    }

    /// Returns the foreground window's title, owning PID, and opaque handle.
    ///
    /// # Errors
    ///
    /// Returns `AgentError::Unsupported` on backends without a real
    /// implementation.
    pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
        backend_input::foreground_window_info()
    }

    // Platform dispatch -------------------------------------------------------

    #[cfg(target_os = "windows")]
    mod backend_input {
        pub use personel_os::input::{ForegroundWindowInfo, foreground_window_info, last_input_idle_ms};
    }

    #[cfg(target_os = "macos")]
    mod backend_input {
        pub use personel_os_macos::input::{ForegroundWindowInfo, foreground_window_info, last_input_idle_ms};
    }

    #[cfg(target_os = "linux")]
    mod backend_input {
        pub use personel_os_linux::input::{ForegroundWindowInfo, foreground_window_info, last_input_idle_ms};
    }

    #[cfg(not(any(target_os = "windows", target_os = "macos", target_os = "linux")))]
    mod backend_input {
        use super::*;

        #[derive(Debug, Clone)]
        pub struct ForegroundWindowInfo {
            pub title: String,
            pub pid: u32,
            pub hwnd: usize,
        }

        pub fn last_input_idle_ms() -> Result<u64> {
            Err(AgentError::Unsupported { os: "other", component: "input::last_input_idle_ms" })
        }

        pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
            Err(AgentError::Unsupported { os: "other", component: "input::foreground_window_info" })
        }
    }
}

// ── service: is_service_context ─────────────────────────────────────────────

/// Service lifecycle helpers (Windows SCM, macOS launchd, Linux systemd).
pub mod service {
    use super::*;

    /// Returns `true` if the current process was launched as a system service
    /// (Windows SCM, macOS launchd, Linux systemd with `Type=notify`).
    /// Used by `personel-agent::main` to decide between service mode and
    /// console mode.
    #[must_use]
    pub fn is_service_context() -> bool {
        backend_service::is_service_context()
    }

    /// Runs the current process under the platform's service supervisor.
    /// On Windows this enters the SCM message loop; on macOS it hands off to
    /// launchd; on Linux it sends the systemd `READY=1` notification.
    ///
    /// # Errors
    ///
    /// Returns `AgentError::Unsupported` on backends without a real
    /// implementation during Phase 2.1.
    pub fn run_as_service() -> Result<()> {
        backend_service::run_as_service()
    }

    #[cfg(target_os = "windows")]
    mod backend_service {
        use super::*;
        pub fn is_service_context() -> bool {
            personel_os::service::is_service_context()
        }
        pub fn run_as_service() -> Result<()> {
            // The real Windows service trampoline takes a oneshot sender; for
            // the facade we surface a not-yet-wired error until personel-agent
            // refactors its shutdown orchestration to go through this layer.
            Err(AgentError::Unsupported {
                os: "windows",
                component: "service::run_as_service (facade wire pending)",
            })
        }
    }

    #[cfg(target_os = "macos")]
    mod backend_service {
        use super::*;
        pub fn is_service_context() -> bool {
            personel_os_macos::service::is_launchd_context()
        }
        pub fn run_as_service() -> Result<()> {
            Err(AgentError::Unsupported {
                os: "macos",
                component: "service::run_as_service",
            })
        }
    }

    #[cfg(target_os = "linux")]
    mod backend_service {
        use super::*;
        pub fn is_service_context() -> bool {
            // personel-os-linux exposes a systemd module; for Phase 2.1 we
            // simply check $NOTIFY_SOCKET which is the stable systemd signal.
            std::env::var_os("NOTIFY_SOCKET").is_some()
        }
        pub fn run_as_service() -> Result<()> {
            Err(AgentError::Unsupported {
                os: "linux",
                component: "service::run_as_service",
            })
        }
    }

    #[cfg(not(any(target_os = "windows", target_os = "macos", target_os = "linux")))]
    mod backend_service {
        use super::*;
        pub fn is_service_context() -> bool {
            false
        }
        pub fn run_as_service() -> Result<()> {
            Err(AgentError::Unsupported { os: "other", component: "service::run_as_service" })
        }
    }
}

// ── platform constants ──────────────────────────────────────────────────────

/// Short identifier for the current target OS, suitable for logging and
/// error messages.
#[cfg(target_os = "windows")]
pub const TARGET_OS: &str = "windows";
/// Short identifier for the current target OS.
#[cfg(target_os = "macos")]
pub const TARGET_OS: &str = "macos";
/// Short identifier for the current target OS.
#[cfg(target_os = "linux")]
pub const TARGET_OS: &str = "linux";
/// Short identifier for the current target OS.
#[cfg(not(any(target_os = "windows", target_os = "macos", target_os = "linux")))]
pub const TARGET_OS: &str = "other";
