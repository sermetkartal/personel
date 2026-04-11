//! Secure key storage for the Linux agent.
//!
//! Provides an abstraction over two platform keystores, selected at runtime
//! based on what is available in the current session:
//!
//! 1. **`libsecret` / Secret Service D-Bus API** (GNOME Keyring, GNOME Shell,
//!    KDE Wallet via the `org.freedesktop.secrets` interface) — used when the
//!    agent runs in a user session with a D-Bus session bus and an unlocked
//!    keyring.
//!
//! 2. **KWallet D-Bus API** (`org.kde.kwalletd5` / `org.kde.kwalletd6`) — used
//!    on KDE Plasma sessions when the Secret Service interface is absent.
//!
//! 3. **File-based fallback** — a `chmod 600` file owned by `personel-agent`,
//!    protected only by DAC permissions. Used when the agent runs as a
//!    headless systemd unit without a user session keyring. The file path is
//!    `$STATE_DIRECTORY/keystore.bin` (systemd `StateDirectory` directive).
//!    This is the expected path for the background system service.
//!
//! The PE-DEK (per-endpoint data encryption key, ADR 0009) is stored in
//! whichever backend is selected. The same key wrapping ceremony as on
//! Windows (public-key seal to the TMK, stored in Vault) applies; this module
//! only handles *local* storage of the sealed blob.
//!
//! # Phase 2.2 implementation plan
//!
//! Use the `secret-service` crate (pure-Rust, no C FFI) for the
//! `org.freedesktop.secrets` interface. Fall through to the file path if the
//! D-Bus session bus is absent (headless/system service scenario).

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// The key storage backend in active use.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KeystoreBackend {
    /// `org.freedesktop.secrets` via `libsecret` / GNOME Keyring.
    SecretService,
    /// `org.kde.kwalletd5` or `org.kde.kwalletd6`.
    KWallet,
    /// File at `$STATE_DIRECTORY/keystore.bin` (DAC-protected, no daemon).
    File,
}

/// Stores an opaque blob under `key_name` in the platform keystore.
///
/// On the Secret Service path, the item is stored in the `personel-agent`
/// collection with the attribute `key_name`. On the file path, the blob is
/// written to `$STATE_DIRECTORY/{key_name}.bin` with mode `0o600`.
///
/// # Arguments
///
/// * `key_name` — ASCII identifier, e.g. `"pe-dek"`.
/// * `blob` — Sealed key material. The caller must have already applied
///   envelope encryption (X25519 + AES-GCM per ADR 0009) before storing.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// Phase 2.2: returns [`AgentError::Io`] on file write errors or D-Bus
/// communication failure.
pub fn store(key_name: &str, blob: &[u8]) -> Result<()> {
    let _ = (key_name, blob);
    Err(AgentError::Unsupported {
        os: "linux",
        component: "keystore::store",
    })
}

/// Retrieves the blob stored under `key_name`.
///
/// Returns the raw sealed blob; the caller is responsible for decrypting it.
/// The returned value is wrapped in [`Zeroizing`] so it is scrubbed from
/// memory on drop.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// Phase 2.2: returns [`AgentError::Io`] if the item does not exist or the
/// keyring is locked.
pub fn load(key_name: &str) -> Result<Zeroizing<Vec<u8>>> {
    let _ = key_name;
    Err(AgentError::Unsupported {
        os: "linux",
        component: "keystore::load",
    })
}

/// Deletes the item stored under `key_name`.
///
/// Used during agent uninstallation or key rotation. No-op if the item does
/// not exist.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
pub fn delete(key_name: &str) -> Result<()> {
    let _ = key_name;
    Err(AgentError::Unsupported {
        os: "linux",
        component: "keystore::delete",
    })
}

/// Detects which keystore backend is available in the current environment.
///
/// Probes (in order): Secret Service D-Bus interface, KWallet D-Bus interface,
/// `$STATE_DIRECTORY` presence. Returns [`KeystoreBackend::File`] as the
/// default when no daemon is available.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
pub fn detect_backend() -> Result<KeystoreBackend> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "keystore::detect_backend",
    })
}
