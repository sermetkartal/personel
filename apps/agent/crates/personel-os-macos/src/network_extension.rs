//! Network Extension bridge — `NEFilterDataProvider` traffic observation.
//!
//! # macOS implementation plan (Phase 2.4)
//!
//! ADR 0015 §"Network Extension" specifies `NEFilterDataProvider` for traffic
//! inspection and `NEDNSProxyProvider` for DNS observation. No packet
//! modification is performed — observation only.
//!
//! The Network Extension runs as a **System Extension** process (separate from
//! the main Rust agent). It communicates with the Rust agent via an IPC
//! channel (UNIX domain socket, same transport as the ES bridge). The Rust
//! side of this module:
//!
//! 1. Spawns or connects to the Network Extension process via
//!    `NEFilterManager.shared().loadFromPreferences`.
//! 2. Reads flow summaries over the UDS channel delivered by the extension:
//!    `(process_path, remote_addr, remote_port, bytes_sent, bytes_received, duration_ms)`.
//! 3. Emits these as `NetworkFlowEvent` values consumed by personel-collectors.
//!
//! ## System Extension lifecycle (Phase 2.4)
//!
//! ```text
//! SystemExtensions.OSSystemExtensionRequest.activationRequest(
//!     forExtensionWithIdentifier: "com.personel.network-extension",
//!     queue: .main
//! )
//! ```
//!
//! Requires the `com.apple.developer.networking.networkextension` entitlement
//! (manually approved by Apple — file in Phase 2 week 1 per ADR 0015).
//!
//! # TCC / entitlement requirements
//!
//! - `com.apple.developer.networking.networkextension` entitlement (Apple-approved).
//! - `com.apple.developer.system-extension.install` entitlement.
//! - No TCC user prompt required for the extension itself; the app bundle
//!   installing the extension triggers a one-time system prompt.
//!
//! # Phase 2.1 status
//!
//! All types are declared; all operations return `Err(AgentError::Unsupported)`.

use personel_core::error::{AgentError, Result};

/// A summarised network flow observation.
///
/// Produced by the `NEFilterDataProvider` System Extension and forwarded to
/// the Rust agent over the UDS bridge channel.
#[derive(Debug, Clone)]
pub struct NetworkFlowEvent {
    /// Absolute path of the process that initiated the connection.
    pub process_path: String,
    /// Remote IP address as a string (IPv4 or IPv6).
    pub remote_addr: String,
    /// Remote port number.
    pub remote_port: u16,
    /// Bytes sent by the local process.
    pub bytes_sent: u64,
    /// Bytes received by the local process.
    pub bytes_received: u64,
    /// Flow duration in milliseconds.
    pub duration_ms: u64,
    /// Optional DNS name resolved for `remote_addr`.
    pub hostname: Option<String>,
}

/// Connection to the Network Extension IPC channel.
///
/// In Phase 2.4 this wraps the UDS reader side of the channel established
/// between the `NEFilterDataProvider` System Extension and the Rust agent.
pub struct NetworkExtensionClient {
    _private: (),
}

impl NetworkExtensionClient {
    /// Connect to the Network Extension IPC socket.
    ///
    /// The socket path is managed by the Network Extension System Extension
    /// process and is announced via `launchd` socket activation.
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::Ipc`] in Phase 2.4+ if the socket path is unavailable
    ///   or the extension process has not started.
    pub fn connect(_socket_path: &str) -> Result<Self> {
        #[cfg(target_os = "macos")]
        {
            // Phase 2.4: connect to UDS socket, start async reader loop.
            Err(AgentError::Unsupported {
                os: "macos",
                component: "network_extension::NetworkExtensionClient::connect",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            crate::stub::network_extension::NetworkExtensionClient::connect(_socket_path)
        }
    }

    /// Reads the next network flow event from the extension IPC channel.
    ///
    /// Blocks until an event is available or the connection is closed.
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::Ipc`] in Phase 2.4+ on channel read error.
    pub fn next_event(&mut self) -> Result<NetworkFlowEvent> {
        #[cfg(target_os = "macos")]
        {
            Err(AgentError::Unsupported {
                os: "macos",
                component: "network_extension::NetworkExtensionClient::next_event",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            Err(AgentError::Unsupported {
                os: "non-macos",
                component: "network_extension::NetworkExtensionClient::next_event",
            })
        }
    }
}
