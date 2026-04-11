//! Non-macOS stub implementations of the full `personel-os-macos` API surface.
//!
//! Every public function and type in this module mirrors its macOS-backed
//! counterpart but returns
//! [`AgentError::Unsupported`][personel_core::error::AgentError::Unsupported]
//! so the workspace compiles cleanly on Linux and Windows.
//!
//! These stubs are **compiled in on all non-macOS platforms**. They are
//! conditionally re-exported by each module using the pattern:
//!
//! ```rust,ignore
//! #[cfg(not(target_os = "macos"))]
//! {
//!     crate::stub::some_module::some_fn(...)
//! }
//! ```
//!
//! Phase 2.2 integrators: do not modify this file; modify the real macOS
//! implementations in the sibling modules instead.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// Identifies the current non-macOS OS for `Unsupported` error messages.
#[cfg(target_os = "linux")]
const OS: &str = "linux";
#[cfg(target_os = "windows")]
const OS: &str = "windows";
#[cfg(not(any(target_os = "macos", target_os = "linux", target_os = "windows")))]
const OS: &str = "other";

// ── input ──────────────────────────────────────────────────────────────────

/// Stub `input` module for non-macOS builds.
pub mod input {
    use super::*;
    use crate::input::ForegroundWindowInfo;

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn last_input_idle_ms() -> Result<u64> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "input::last_input_idle_ms",
        })
    }

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "input::foreground_window_info",
        })
    }
}

// ── capture ────────────────────────────────────────────────────────────────

/// Stub `capture` module for non-macOS builds.
pub mod capture {
    use super::*;

    /// Stub `ScCapture` for non-macOS builds.
    pub struct ScCapture {
        _private: (),
    }

    impl ScCapture {
        /// Returns `Err(Unsupported)` on non-macOS platforms.
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`].
        pub fn open(_monitor_index: u32) -> Result<Self> {
            Err(AgentError::Unsupported {
                os: OS,
                component: "capture::ScCapture::open",
            })
        }
    }
}

// ── file_events ────────────────────────────────────────────────────────────

/// Stub `file_events` module for non-macOS builds.
pub mod file_events {
    use super::*;

    /// Stub `FsEventsStream` for non-macOS builds.
    pub struct FsEventsStream {
        _private: (),
    }

    impl FsEventsStream {
        /// Returns `Err(Unsupported)` on non-macOS platforms.
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`].
        pub fn start<F>(_paths: &[&str], _latency_secs: f64, _callback: F) -> Result<Self>
        where
            F: Fn(Vec<crate::file_events::FileEvent>) + Send + 'static,
        {
            Err(AgentError::Unsupported {
                os: OS,
                component: "file_events::FsEventsStream::start",
            })
        }
    }
}

// ── network_extension ──────────────────────────────────────────────────────

/// Stub `network_extension` module for non-macOS builds.
pub mod network_extension {
    use super::*;

    /// Stub `NetworkExtensionClient` for non-macOS builds.
    pub struct NetworkExtensionClient {
        _private: (),
    }

    impl NetworkExtensionClient {
        /// Returns `Err(Unsupported)` on non-macOS platforms.
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`].
        pub fn connect(_socket_path: &str) -> Result<Self> {
            Err(AgentError::Unsupported {
                os: OS,
                component: "network_extension::NetworkExtensionClient::connect",
            })
        }
    }
}

// ── tcc ────────────────────────────────────────────────────────────────────

/// Stub `tcc` module for non-macOS builds.
pub mod tcc {
    use super::*;
    use crate::tcc::{TccPermission, TccStatus};

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn check_permission(_permission: TccPermission) -> Result<TccStatus> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "tcc::check_permission",
        })
    }
}

// ── es_bridge ──────────────────────────────────────────────────────────────

/// Stub `es_bridge` module for non-macOS builds.
pub mod es_bridge {
    use super::*;

    /// Stub `EsBridgeClient` for non-macOS builds.
    pub struct EsBridgeClient {
        _private: (),
    }

    impl EsBridgeClient {
        /// Returns `Err(Unsupported)` on non-macOS platforms.
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`].
        pub fn connect(_socket_path: &str) -> Result<Self> {
            Err(AgentError::Unsupported {
                os: OS,
                component: "es_bridge::EsBridgeClient::connect",
            })
        }
    }
}

// ── service ────────────────────────────────────────────────────────────────

/// Stub `service` module for non-macOS builds.
pub mod service {
    use super::*;
    use tokio::sync::oneshot;

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn run_as_launchd_service(_shutdown_tx: oneshot::Sender<()>) -> Result<()> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "service::run_as_launchd_service",
        })
    }
}

// ── keystore ───────────────────────────────────────────────────────────────

/// Stub `keystore` module for non-macOS builds.
pub mod keystore {
    use super::*;

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn store(_service: &str, _account: &str, _secret: &[u8]) -> Result<()> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "keystore::store",
        })
    }

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn load(_service: &str, _account: &str) -> Result<Zeroizing<Vec<u8>>> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "keystore::load",
        })
    }

    /// Returns `Err(Unsupported)` on non-macOS platforms.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn delete(_service: &str, _account: &str) -> Result<()> {
        Err(AgentError::Unsupported {
            os: OS,
            component: "keystore::delete",
        })
    }
}
