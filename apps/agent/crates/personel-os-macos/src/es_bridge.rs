//! Endpoint Security Framework bridge — IPC channel to the Swift ES helper.
//!
//! # Architecture (ADR 0015 §"ES daemon written in Swift")
//!
//! Rust cannot directly link `EndpointSecurity.framework` at this time;
//! existing unofficial FFI bindings lag the ES API revision cycle. The chosen
//! approach is a **Swift shim** process (`Personel ES Helper.app`) that:
//!
//! 1. Links `EndpointSecurity.framework` natively.
//! 2. Subscribes to the event set defined in ADR 0015 (NOTIFY_EXEC, EXIT,
//!    FORK, OPEN, CLOSE, RENAME, UNLINK, WRITE, CREATE, MOUNT, UNMOUNT,
//!    IOKIT_OPEN, LOGIN, LOGOUT, SCREENSHARING_ATTACH).
//! 3. Serialises each event to a Cap'n Proto or Protobuf message frame.
//! 4. Writes frames to a UNIX domain socket at a well-known path.
//!
//! This module (Rust side) connects to that UDS socket and deserialises events
//! into [`EsEvent`] values consumed by `personel-collectors`.
//!
//! # Socket path
//!
//! `/var/run/personel-es.sock` (created by the launchd socket activation entry
//! in the ES helper's `Info.plist`). The path is configurable via policy.
//!
//! # Phase 2.1 status
//!
//! All types are declared; connection always returns `Err(Unsupported)`.
//! The Swift source placeholder lives in `swift/README.md`.
//!
//! # Phase 2.4 plan
//!
//! - Build the Swift ES helper (see `swift/README.md` for design notes).
//! - Replace the stub `EsBridgeClient::connect` with a real `tokio::net::UnixStream`.
//! - Replace `next_event` with a framed protobuf reader using `tokio-util::codec`.

use personel_core::error::{AgentError, Result};

/// Default UDS socket path for the ES bridge.
pub const DEFAULT_SOCKET_PATH: &str = "/var/run/personel-es.sock";

/// An event delivered by the Endpoint Security Framework via the Swift helper.
///
/// Only NOTIFY events are subscribed (no AUTH events — see ADR 0015).
#[derive(Debug, Clone)]
pub struct EsEvent {
    /// The ES event type (matches `ES_EVENT_TYPE_NOTIFY_*` constants).
    pub event_type: EsEventType,
    /// Milliseconds since Unix epoch when the kernel delivered the event.
    pub timestamp_ms: u64,
    /// The process that triggered the event.
    pub process: EsProcess,
    /// Optional path of the file or resource involved.
    pub path: Option<String>,
}

/// The Endpoint Security event type.
///
/// Only the subset subscribed in ADR 0015 is represented here. Additional
/// variants can be added without breaking the `#[non_exhaustive]` match arm.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum EsEventType {
    /// `ES_EVENT_TYPE_NOTIFY_EXEC` — process execution.
    Exec,
    /// `ES_EVENT_TYPE_NOTIFY_EXIT` — process exit.
    Exit,
    /// `ES_EVENT_TYPE_NOTIFY_FORK` — process fork.
    Fork,
    /// `ES_EVENT_TYPE_NOTIFY_OPEN` — file open.
    Open,
    /// `ES_EVENT_TYPE_NOTIFY_CLOSE` — file close.
    Close,
    /// `ES_EVENT_TYPE_NOTIFY_RENAME` — file rename.
    Rename,
    /// `ES_EVENT_TYPE_NOTIFY_UNLINK` — file deletion.
    Unlink,
    /// `ES_EVENT_TYPE_NOTIFY_WRITE` — file write.
    Write,
    /// `ES_EVENT_TYPE_NOTIFY_CREATE` — file or directory creation.
    Create,
    /// `ES_EVENT_TYPE_NOTIFY_MOUNT` — volume mount.
    Mount,
    /// `ES_EVENT_TYPE_NOTIFY_UNMOUNT` — volume unmount.
    Unmount,
    /// `ES_EVENT_TYPE_NOTIFY_IOKIT_OPEN` — IOKit device open (USB).
    IokitOpen,
    /// `ES_EVENT_TYPE_NOTIFY_LOGIN` — user login.
    Login,
    /// `ES_EVENT_TYPE_NOTIFY_LOGOUT` — user logout.
    Logout,
    /// `ES_EVENT_TYPE_NOTIFY_SCREENSHARING_ATTACH` — screen sharing attach.
    ScreenSharingAttach,
}

/// Minimal process descriptor attached to each ES event.
#[derive(Debug, Clone)]
pub struct EsProcess {
    /// BSD process identifier.
    pub pid: u32,
    /// Executable path (e.g. `/usr/bin/swift`).
    pub executable_path: String,
    /// Team identifier (from the code signature), if present.
    pub team_id: Option<String>,
}

/// Client connection to the ES bridge UNIX domain socket.
///
/// In Phase 2.4 this wraps a `tokio::net::UnixStream`. In Phase 2.1
/// all operations return `Err(AgentError::Unsupported)`.
pub struct EsBridgeClient {
    _private: (),
}

impl EsBridgeClient {
    /// Connect to the ES bridge UNIX domain socket.
    ///
    /// The Swift ES helper must be running before this call succeeds.
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::Ipc`] in Phase 2.4+ if the socket is not present or
    ///   the ES helper process has crashed.
    pub fn connect(_socket_path: &str) -> Result<Self> {
        #[cfg(target_os = "macos")]
        {
            // Phase 2.4: tokio::net::UnixStream::connect(socket_path).await
            Err(AgentError::Unsupported {
                os: "macos",
                component: "es_bridge::EsBridgeClient::connect",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            crate::stub::es_bridge::EsBridgeClient::connect(_socket_path)
        }
    }

    /// Reads the next `EsEvent` from the bridge channel (blocking).
    ///
    /// In Phase 2.4 this reads a length-prefixed protobuf frame from the
    /// `tokio::net::UnixStream` and deserialises it into an [`EsEvent`].
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::Ipc`] in Phase 2.4+ on frame read or decode error.
    pub fn next_event(&mut self) -> Result<EsEvent> {
        #[cfg(target_os = "macos")]
        {
            Err(AgentError::Unsupported {
                os: "macos",
                component: "es_bridge::EsBridgeClient::next_event",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            Err(AgentError::Unsupported {
                os: "non-macos",
                component: "es_bridge::EsBridgeClient::next_event",
            })
        }
    }
}
