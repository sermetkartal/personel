//! Workspace-wide error types.
//!
//! All library crates return `personel_core::Result<T>` or their own error
//! type implementing `From<AgentError>`. Application-level crates (main
//! binaries) use `anyhow` for context chains.
//!
//! Note: `rusqlite::Error` and `tonic::Status` are NOT referenced directly
//! here to avoid pulling those large crates into `personel-core`. Instead,
//! subsystem crates define their own thin `From` impls that convert to
//! `AgentError::Queue(String)` or `AgentError::Grpc(String)`.

use thiserror::Error;

/// Canonical result alias for library functions.
pub type Result<T, E = AgentError> = std::result::Result<T, E>;

/// Top-level agent error enum. Variants cover all subsystem failure modes.
///
/// When adding a variant, update the documentation comment and ensure the
/// severity mapping in `error_severity()` is correct.
#[derive(Debug, Error)]
#[non_exhaustive]
pub enum AgentError {
    // ── ID / config ──────────────────────────────────────────────────────
    /// A UUID or ULID could not be parsed from its byte representation.
    #[error("invalid id bytes: {reason}")]
    InvalidId { reason: &'static str },

    /// An agent configuration value is missing or out of range.
    #[error("configuration error: {0}")]
    Config(String),

    // ── Queue ─────────────────────────────────────────────────────────────
    /// A SQLite / queue operation failed.
    #[error("queue database error: {0}")]
    Queue(String),

    /// The local queue has reached its capacity limit.
    #[error("queue capacity exceeded (cap={cap_bytes} bytes)")]
    QueueFull { cap_bytes: u64 },

    // ── Transport ─────────────────────────────────────────────────────────
    /// A gRPC status was returned by the server.
    #[error("gRPC error: {0}")]
    Grpc(String),

    /// The TLS handshake or certificate validation failed.
    #[error("TLS error: {0}")]
    Tls(String),

    /// The server's certificate did not match the pinned SPKI hash.
    #[error("certificate pin mismatch")]
    PinMismatch,

    /// Network I/O failure.
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    // ── Crypto ─────────────────────────────────────────────────────────────
    /// AES-GCM encryption or decryption failed (authentication tag mismatch).
    #[error("AEAD error: authentication tag mismatch or invalid ciphertext")]
    AeadError,

    /// A key derivation step produced unexpected output length.
    #[error("HKDF output length error")]
    HkdfLength,

    /// DPAPI protection or unprotection call failed.
    #[error("DPAPI error: {0}")]
    Dpapi(String),

    // ── Platform ──────────────────────────────────────────────────────────
    /// A function is not supported on the current operating system. Used
    /// primarily by personel-os non-Windows stubs to signal "compile-time
    /// available, runtime not implemented" to callers during Phase 2
    /// macOS/Linux agent development.
    #[error("operation unsupported on this platform: {component} ({os})")]
    Unsupported {
        /// Short OS identifier, e.g. "macos", "linux", "windows".
        os: &'static str,
        /// Short component identifier, e.g. "etw", "dpapi", "dxgi_capture".
        component: &'static str,
    },

    // ── Collectors ────────────────────────────────────────────────────────
    /// A collector could not start because a required OS API was unavailable.
    #[error("collector '{name}' failed to start: {reason}")]
    CollectorStart { name: &'static str, reason: String },

    /// A collector emitted an error during its run loop.
    #[error("collector '{name}' runtime error: {reason}")]
    CollectorRuntime { name: &'static str, reason: String },

    // ── Policy ────────────────────────────────────────────────────────────
    /// A received policy bundle failed signature verification.
    #[error("policy signature verification failed")]
    PolicySignature,

    /// Policy deserialization failed.
    #[error("policy deserialization error: {0}")]
    PolicyDeserialize(String),

    // ── Serialization ─────────────────────────────────────────────────────
    /// Prost encoding error.
    #[error("protobuf encode error: {0}")]
    ProtobufEncode(#[from] prost::EncodeError),

    /// Prost decode error.
    #[error("protobuf decode error: {0}")]
    ProtobufDecode(#[from] prost::DecodeError),

    // ── Watchdog / IPC ────────────────────────────────────────────────────
    /// Named-pipe IPC error.
    #[error("IPC error: {0}")]
    Ipc(String),

    // ── Update ────────────────────────────────────────────────────────────
    /// Signature on the update manifest is invalid.
    #[error("update manifest signature invalid")]
    UpdateSignature,

    /// Hash of downloaded artifact does not match manifest.
    #[error("artifact hash mismatch")]
    ArtifactHash,

    // ── Anti-tamper ───────────────────────────────────────────────────────
    /// A tamper detection check triggered.
    #[error("tamper detected: {check}")]
    TamperDetected { check: &'static str },

    // ── Catch-all ─────────────────────────────────────────────────────────
    /// Unrecoverable internal invariant violated. Should never reach production.
    #[error("internal error: {0}")]
    Internal(String),
}

impl AgentError {
    /// Returns whether this error is transient and the caller should retry.
    #[must_use]
    pub fn is_transient(&self) -> bool {
        matches!(self, Self::Io(_) | Self::Grpc(_) | Self::Ipc(_))
    }

    /// Returns `true` if this error should be treated as a tamper event.
    #[must_use]
    pub fn is_tamper(&self) -> bool {
        matches!(self, Self::TamperDetected { .. } | Self::PinMismatch | Self::AeadError)
    }
}

// Optional conversion from rusqlite::Error. Only compiled when the
// `rusqlite` feature is enabled (e.g. by personel-queue). This lets
// queue code use `?` directly without running into orphan rules in
// personel-queue.
#[cfg(feature = "rusqlite")]
impl From<rusqlite::Error> for AgentError {
    fn from(e: rusqlite::Error) -> Self {
        Self::Queue(e.to_string())
    }
}
