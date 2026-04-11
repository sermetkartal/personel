//! Keychain wrapper for storing the PE-DEK after macOS enrollment.
//!
//! Provides the macOS equivalent of the Windows DPAPI `protect`/`unprotect`
//! pair in `personel-os`. On macOS, the Keychain is the system credential
//! store; hardware-backed items use the Secure Enclave on Apple Silicon.
//!
//! # Key stored
//!
//! The **PE-DEK** (Per-Endpoint Data Encryption Key) generated during the
//! enrollment ceremony (ADR 0009/0013). After enrollment, the PE-DEK is
//! sealed in the Keychain under the service name `"com.personel.agent"` and
//! the account name `"pe-dek"`. Subsequent boots retrieve it without user
//! interaction (no UI flag set on the Keychain item).
//!
//! # `security-framework` crate
//!
//! The `security-framework` crate (version 2) wraps `Security.framework`
//! `SecItemAdd`, `SecItemCopyMatching`, and `SecItemDelete`. Items created
//! with `kSecAttrAccessible = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
//! are:
//! - Accessible after first unlock, without requiring the device to be
//!   unlocked again.
//! - Not migrated to other devices via iCloud Keychain (device-bound).
//! - Equivalent in posture to DPAPI `CRYPTPROTECT_LOCAL_MACHINE`.
//!
//! # Phase 2.1 status
//!
//! The `security-framework` crate is a declared dependency on macOS. The
//! `store` and `load` functions are implemented as real Keychain calls on
//! macOS (the crate compiles without a macOS SDK for type checking because
//! `security-framework` provides its own binding declarations). On non-macOS
//! platforms both functions return `Err(AgentError::Unsupported)`.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// Service name used for all Keychain items created by personel-agent.
pub const KEYCHAIN_SERVICE: &str = "com.personel.agent";

/// Keychain item account name for the PE-DEK.
pub const PEDEK_ACCOUNT: &str = "pe-dek";

/// Stores `secret` in the macOS Keychain under `service` / `account`.
///
/// If an item already exists under the same `service`/`account` pair it is
/// **replaced** (delete + add). This matches DPAPI `protect` semantics.
///
/// # Arguments
///
/// - `service`: reverse-DNS service name, e.g. `"com.personel.agent"`.
/// - `account`: item account label, e.g. `"pe-dek"`.
/// - `secret`: raw bytes to store. Zeroized on drop.
///
/// # Errors
///
/// - [`AgentError::Unsupported`] on non-macOS platforms.
/// - [`AgentError::Internal`] if the Keychain operation fails (Phase 2.2+
///   will map `security_framework::Error` to a typed variant).
///
/// # Example
///
/// ```rust,no_run
/// use personel_os_macos::keystore::{store, KEYCHAIN_SERVICE, PEDEK_ACCOUNT};
///
/// let key_material = b"32-byte-key-placeholder-here!!!";
/// store(KEYCHAIN_SERVICE, PEDEK_ACCOUNT, key_material).expect("keychain store failed");
/// ```
pub fn store(service: &str, account: &str, secret: &[u8]) -> Result<()> {
    #[cfg(target_os = "macos")]
    {
        use security_framework::passwords::{delete_generic_password, set_generic_password};

        // Attempt to delete any existing item first (ignore NotFound error).
        // `security_framework::passwords::delete_generic_password` returns
        // Err if the item does not exist; we treat that as a non-error.
        let _ = delete_generic_password(service, account);

        set_generic_password(service, account, secret).map_err(|e| {
            AgentError::Internal(format!("Keychain store failed: {e}"))
        })
    }

    #[cfg(not(target_os = "macos"))]
    {
        let _ = (service, account, secret);
        crate::stub::keystore::store(service, account, secret)
    }
}

/// Loads a secret from the macOS Keychain under `service` / `account`.
///
/// The returned bytes are wrapped in [`Zeroizing`] to ensure they are
/// scrubbed from memory when dropped, matching DPAPI `unprotect` semantics.
///
/// # Errors
///
/// - [`AgentError::Unsupported`] on non-macOS platforms.
/// - [`AgentError::Internal`] if the item is not found or the Keychain
///   operation fails (Phase 2.2+ will use a dedicated `NotFound` variant).
///
/// # Example
///
/// ```rust,no_run
/// use personel_os_macos::keystore::{load, KEYCHAIN_SERVICE, PEDEK_ACCOUNT};
///
/// let key = load(KEYCHAIN_SERVICE, PEDEK_ACCOUNT).expect("key not enrolled");
/// assert_eq!(key.len(), 32);
/// ```
pub fn load(service: &str, account: &str) -> Result<Zeroizing<Vec<u8>>> {
    #[cfg(target_os = "macos")]
    {
        use security_framework::passwords::get_generic_password;

        get_generic_password(service, account)
            .map(|bytes| Zeroizing::new(bytes))
            .map_err(|e| AgentError::Internal(format!("Keychain load failed: {e}")))
    }

    #[cfg(not(target_os = "macos"))]
    {
        let _ = (service, account);
        crate::stub::keystore::load(service, account)
    }
}

/// Deletes a secret from the macOS Keychain under `service` / `account`.
///
/// Used during un-enrollment or wipe. A missing item is treated as success
/// (idempotent).
///
/// # Errors
///
/// - [`AgentError::Unsupported`] on non-macOS platforms.
/// - [`AgentError::Internal`] if the Keychain operation fails for a reason
///   other than item-not-found.
pub fn delete(service: &str, account: &str) -> Result<()> {
    #[cfg(target_os = "macos")]
    {
        use security_framework::passwords::delete_generic_password;

        // Treat item-not-found as success (idempotent delete).
        match delete_generic_password(service, account) {
            Ok(()) => Ok(()),
            Err(e) if e.code() == -25300 /* errSecItemNotFound */ => Ok(()),
            Err(e) => Err(AgentError::Internal(format!("Keychain delete failed: {e}"))),
        }
    }

    #[cfg(not(target_os = "macos"))]
    {
        let _ = (service, account);
        crate::stub::keystore::delete(service, account)
    }
}
