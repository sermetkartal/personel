//! Non-Linux stub implementations.
//!
//! These modules provide the same public API as their Linux counterparts so
//! the workspace compiles on macOS developer machines and Windows CI runners.
//! Every function returns [`AgentError::Unsupported`] with `os` set to the
//! current platform identifier.
//!
//! Phase 2.2 Linux implementations live in the sibling modules under `src/`
//! (e.g. `src/input.rs`, `src/capture.rs`). The stub modules exist purely to
//! satisfy `cargo check` on non-Linux hosts — they are never executed in
//! production.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// OS identifier embedded in `Unsupported` errors.
#[cfg(target_os = "macos")]
const OS: &str = "macos";
#[cfg(target_os = "windows")]
const OS: &str = "windows";
#[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
const OS: &str = "other";

// ── input ─────────────────────────────────────────────────────────────────────

/// Stub input module — non-Linux builds.
pub mod input {
    use super::*;

    /// Stub for [`crate::input::InputActivity`].
    #[derive(Debug, Clone)]
    pub struct InputActivity {
        /// Milliseconds since last input event (always 0 in stub).
        pub idle_ms: u64,
        /// Keystroke count (always 0 in stub).
        pub keystroke_count: u64,
        /// Mouse move count (always 0 in stub).
        pub mouse_move_count: u64,
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn query_activity() -> Result<InputActivity> {
        Err(AgentError::Unsupported { os: OS, component: "input::query_activity" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn start_collector(
        _sender: tokio::sync::mpsc::Sender<InputActivity>,
    ) -> Result<tokio::task::JoinHandle<()>> {
        Err(AgentError::Unsupported { os: OS, component: "input::start_collector" })
    }
}

// ── capture ───────────────────────────────────────────────────────────────────

/// Stub capture module — non-Linux builds.
pub mod capture {
    use super::*;

    /// Stub for [`crate::capture::CapturedFrame`].
    pub struct CapturedFrame {
        /// Pixel data (empty in stub).
        pub pixels: Vec<u8>,
        /// Frame width (0 in stub).
        pub width: u32,
        /// Frame height (0 in stub).
        pub height: u32,
        /// Monitor index (0 in stub).
        pub monitor_index: u32,
    }

    /// Display session type (stub mirrors Linux enum).
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum SessionType {
        /// X11 session.
        X11,
        /// Wayland session.
        Wayland,
        /// Unknown.
        Unknown,
    }

    /// Stub X11 capture adapter.
    pub struct X11Adapter {
        _priv: (),
    }

    impl X11Adapter {
        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn open(_monitor: u32) -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "capture::X11Adapter::open" })
        }

        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn capture_frame(&self) -> Result<CapturedFrame> {
            Err(AgentError::Unsupported { os: OS, component: "capture::X11Adapter::capture_frame" })
        }
    }

    /// Stub Wayland capture adapter.
    pub struct WaylandAdapter {
        _priv: (),
    }

    impl WaylandAdapter {
        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn open(_monitor: u32) -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "capture::WaylandAdapter::open" })
        }

        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn capture_frame(&self) -> Result<CapturedFrame> {
            Err(AgentError::Unsupported { os: OS, component: "capture::WaylandAdapter::capture_frame" })
        }
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn capture_frame(_monitor_index: u32) -> Result<CapturedFrame> {
        Err(AgentError::Unsupported { os: OS, component: "capture::capture_frame" })
    }
}

// ── file_events ───────────────────────────────────────────────────────────────

/// Stub fanotify file-event module — non-Linux builds.
pub mod file_events {
    use super::*;

    /// Stub for [`crate::file_events::FileEvent`].
    #[derive(Debug, Clone)]
    pub struct FileEvent {
        /// File path (empty in stub).
        pub path: String,
        /// PID (0 in stub).
        pub pid: u32,
        /// Event kind.
        pub kind: EventKind,
    }

    /// Stub event kind.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum EventKind {
        /// File opened.
        Open,
        /// File closed after write.
        CloseWrite,
        /// File modified.
        Modify,
        /// File moved.
        MoveSelf,
        /// Binary executed.
        OpenExec,
    }

    /// Stub capability status.
    #[derive(Debug, Clone, PartialEq, Eq)]
    pub enum CapabilityStatus {
        /// Capability present.
        Present,
        /// Capability absent.
        Absent,
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn check_fanotify_capability() -> Result<CapabilityStatus> {
        Err(AgentError::Unsupported { os: OS, component: "file_events::check_fanotify_capability" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn start_collector(
        _sender: tokio::sync::mpsc::Sender<FileEvent>,
    ) -> Result<tokio::task::JoinHandle<()>> {
        Err(AgentError::Unsupported { os: OS, component: "file_events::start_collector" })
    }
}

// ── ebpf ─────────────────────────────────────────────────────────────────────

/// Stub eBPF module — non-Linux builds.
pub mod ebpf {
    use super::*;

    /// Stub process eBPF submodule.
    pub mod process {
        use super::*;

        /// Stub process event.
        #[derive(Debug, Clone)]
        pub struct ProcessEvent {
            /// PID.
            pub pid: u32,
            /// Parent PID.
            pub ppid: u32,
            /// Process name.
            pub comm: String,
            /// Executable path.
            pub exe_path: Option<String>,
            /// Event kind.
            pub kind: ProcessEventKind,
        }

        /// Stub process event kind.
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum ProcessEventKind {
            /// Exec event.
            Exec,
            /// Exit event.
            Exit,
            /// Fork event.
            Fork,
        }

        /// Stub process collector.
        pub struct ProcessCollector {
            _priv: (),
        }

        impl ProcessCollector {
            /// Always returns [`AgentError::Unsupported`].
            ///
            /// # Errors
            ///
            /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
            pub fn load() -> Result<Self> {
                Err(AgentError::Unsupported { os: OS, component: "ebpf::process::load" })
            }

            /// Always returns [`AgentError::Unsupported`].
            ///
            /// # Errors
            ///
            /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
            pub fn start_polling(
                self,
                _sender: tokio::sync::mpsc::Sender<ProcessEvent>,
            ) -> Result<tokio::task::JoinHandle<()>> {
                Err(AgentError::Unsupported { os: OS, component: "ebpf::process::start_polling" })
            }
        }
    }

    /// Stub network eBPF submodule.
    pub mod network {
        use super::*;
        use std::net::IpAddr;

        /// Stub network event.
        #[derive(Debug, Clone)]
        pub struct NetworkEvent {
            /// Source address.
            pub src_addr: IpAddr,
            /// Source port.
            pub src_port: u16,
            /// Destination address.
            pub dst_addr: IpAddr,
            /// Destination port.
            pub dst_port: u16,
            /// PID.
            pub pid: u32,
            /// Process name.
            pub comm: String,
            /// Protocol.
            pub protocol: NetworkProtocol,
            /// Event kind.
            pub kind: NetworkEventKind,
        }

        /// Stub network protocol.
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum NetworkProtocol {
            /// TCP.
            Tcp,
            /// UDP.
            Udp,
        }

        /// Stub network event kind.
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum NetworkEventKind {
            /// TCP connect.
            TcpConnect,
            /// TCP close.
            TcpClose,
            /// UDP send aggregate.
            UdpSendAggregate,
        }

        /// Stub network collector.
        pub struct NetworkCollector {
            _priv: (),
        }

        impl NetworkCollector {
            /// Always returns [`AgentError::Unsupported`].
            ///
            /// # Errors
            ///
            /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
            pub fn load() -> Result<Self> {
                Err(AgentError::Unsupported { os: OS, component: "ebpf::network::load" })
            }

            /// Always returns [`AgentError::Unsupported`].
            ///
            /// # Errors
            ///
            /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
            pub fn start_polling(
                self,
                _sender: tokio::sync::mpsc::Sender<NetworkEvent>,
            ) -> Result<tokio::task::JoinHandle<()>> {
                Err(AgentError::Unsupported { os: OS, component: "ebpf::network::start_polling" })
            }
        }
    }
}

// ── window_title ─────────────────────────────────────────────────────────────

/// Stub window title module — non-Linux builds.
pub mod window_title {
    use super::*;

    /// Stub session type.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum SessionType {
        /// X11.
        X11,
        /// Wayland.
        Wayland,
        /// Unknown.
        Unknown,
    }

    /// Stub active window info.
    #[derive(Debug, Clone)]
    pub struct ActiveWindowInfo {
        /// Window title (empty in stub).
        pub title: String,
        /// PID (None in stub).
        pub pid: Option<u32>,
    }

    /// Always returns [`SessionType::Unknown`] on non-Linux.
    #[must_use]
    pub fn detect_session_type() -> SessionType {
        SessionType::Unknown
    }

    /// Stub X11 window adapter.
    pub struct X11Adapter {
        _priv: (),
    }

    impl X11Adapter {
        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn new() -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "window_title::X11Adapter::new" })
        }

        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn poll(&self) -> Result<ActiveWindowInfo> {
            Err(AgentError::Unsupported { os: OS, component: "window_title::X11Adapter::poll" })
        }
    }

    /// Stub Wayland window adapter.
    pub struct WaylandAdapter {
        _priv: (),
    }

    impl WaylandAdapter {
        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn new() -> Result<Self> {
            Err(AgentError::Unsupported { os: OS, component: "window_title::WaylandAdapter::new" })
        }

        /// Always returns [`AgentError::Unsupported`].
        ///
        /// # Errors
        ///
        /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
        pub fn active_window(&self) -> Result<ActiveWindowInfo> {
            Err(AgentError::Unsupported { os: OS, component: "window_title" })
        }
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn active_window_title() -> Result<ActiveWindowInfo> {
        Err(AgentError::Unsupported { os: OS, component: "window_title::active_window_title" })
    }
}

// ── systemd ───────────────────────────────────────────────────────────────────

/// Stub systemd module — non-Linux builds.
pub mod systemd {
    use super::*;

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn notify_ready() -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "systemd::notify_ready" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn notify_watchdog() -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "systemd::notify_watchdog" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn notify_status(_status: &str) -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "systemd::notify_status" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn notify_stopping() -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "systemd::notify_stopping" })
    }

    /// Returns `None` on non-Linux.
    #[must_use]
    pub fn watchdog_interval_us() -> Option<u64> {
        None
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn init_journal_logger() -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "systemd::init_journal_logger" })
    }
}

// ── keystore ─────────────────────────────────────────────────────────────────

/// Stub keystore module — non-Linux builds.
pub mod keystore {
    use super::*;

    /// Stub keystore backend.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum KeystoreBackend {
        /// Secret Service.
        SecretService,
        /// KWallet.
        KWallet,
        /// File-based.
        File,
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn store(_key_name: &str, _blob: &[u8]) -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "keystore::store" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn load(_key_name: &str) -> Result<Zeroizing<Vec<u8>>> {
        Err(AgentError::Unsupported { os: OS, component: "keystore::load" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn delete(_key_name: &str) -> Result<()> {
        Err(AgentError::Unsupported { os: OS, component: "keystore::delete" })
    }

    /// Always returns [`AgentError::Unsupported`].
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`] on non-Linux platforms.
    pub fn detect_backend() -> Result<KeystoreBackend> {
        Err(AgentError::Unsupported { os: OS, component: "keystore::detect_backend" })
    }
}
