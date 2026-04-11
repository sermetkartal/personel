//! Key storage abstraction: DPAPI + TPM-bound sealing.
//!
//! On Windows, sensitive key material (PE-DEK, agent private key) is wrapped
//! with DPAPI (machine scope) and optionally bound to a TPM protector.
//!
//! On non-Windows builds, the `KeyStore` returns errors on all protect/
//! unprotect operations, which prevents agent startup (as intended — the
//! agent is Windows-only in Phase 1).
//!
//! The actual DPAPI Win32 calls are delegated to `personel_os::windows::dpapi`
//! which is the only crate allowed to use `unsafe`.

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

use crate::Aes256Key;

/// Abstracts over key sealing (DPAPI, TPM, or test no-op).
///
/// Implementors must ensure that:
/// - `seal` never writes the plaintext key to disk.
/// - `unseal` returns a `Zeroizing` buffer.
/// - Both operations are synchronous (called during agent init, not hot path).
pub trait KeyStore: Send + Sync {
    /// Seals `plaintext` key material and returns opaque ciphertext bytes
    /// suitable for persistent storage (e.g., registry or config file).
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Dpapi`] if the OS sealing call fails.
    fn seal(&self, plaintext: &[u8]) -> Result<Vec<u8>>;

    /// Unseals previously-sealed bytes and returns the plaintext.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Dpapi`] if unsealing fails (wrong machine,
    /// TPM mismatch, or corrupt blob).
    fn unseal(&self, sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>>;

    /// Seals a 32-byte AES key. Convenience wrapper around [`seal`].
    fn seal_key(&self, key: &Aes256Key) -> Result<Vec<u8>> {
        self.seal(key.as_ref())
    }

    /// Unseals a 32-byte AES key. Returns an error if the recovered bytes are
    /// not exactly 32 bytes.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Dpapi`] or [`AgentError::InvalidId`] on failure.
    fn unseal_key(&self, sealed: &[u8]) -> Result<Aes256Key> {
        let plaintext = self.unseal(sealed)?;
        let arr: [u8; 32] = plaintext.as_slice().try_into().map_err(|_| AgentError::InvalidId {
            reason: "unsealed key is not 32 bytes",
        })?;
        Ok(Zeroizing::new(arr))
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows DPAPI implementation
// ──────────────────────────────────────────────────────────────────────────────

/// DPAPI-backed [`KeyStore`] for Windows.
///
/// Sealing uses `CryptProtectData` with `CRYPTPROTECT_LOCAL_MACHINE` so the
/// sealed blob can only be unsealed on the same machine. When a TPM PCR policy
/// is available (future work), the DPAPI blob can be additionally bound to PCR
/// state via the Storage Root Key.
#[cfg(target_os = "windows")]
pub struct DpapiKeyStore;

#[cfg(target_os = "windows")]
impl KeyStore for DpapiKeyStore {
    fn seal(&self, plaintext: &[u8]) -> Result<Vec<u8>> {
        // Delegated to personel-os to keep unsafe code in one place.
        personel_os::windows::dpapi::protect(plaintext)
            .map_err(|e| AgentError::Dpapi(e.to_string()))
    }

    fn unseal(&self, sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
        personel_os::windows::dpapi::unprotect(sealed)
            .map_err(|e| AgentError::Dpapi(e.to_string()))
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Non-Windows stub (returns error at runtime; compile for dev ergonomics)
// ──────────────────────────────────────────────────────────────────────────────

/// Stub [`KeyStore`] for non-Windows builds.
///
/// Calling any method will return `AgentError::Dpapi` because DPAPI is a
/// Windows-only API. The agent binary will not start on non-Windows.
#[cfg(not(target_os = "windows"))]
pub struct DpapiKeyStore;

#[cfg(not(target_os = "windows"))]
impl KeyStore for DpapiKeyStore {
    fn seal(&self, _plaintext: &[u8]) -> Result<Vec<u8>> {
        Err(AgentError::Dpapi("DPAPI is not available on this platform".into()))
    }

    fn unseal(&self, _sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
        Err(AgentError::Dpapi("DPAPI is not available on this platform".into()))
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// In-memory no-op store (tests only)
// ──────────────────────────────────────────────────────────────────────────────

/// A [`KeyStore`] that stores keys in plaintext in memory.
///
/// **Only for use in tests.** Do not instantiate in production code.
#[cfg(test)]
pub struct PlaintextKeyStore;

#[cfg(test)]
impl KeyStore for PlaintextKeyStore {
    fn seal(&self, plaintext: &[u8]) -> Result<Vec<u8>> {
        Ok(plaintext.to_vec())
    }

    fn unseal(&self, sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
        Ok(Zeroizing::new(sealed.to_vec()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use zeroize::Zeroizing;

    #[test]
    fn plaintext_store_roundtrip() {
        let store = PlaintextKeyStore;
        let key: Aes256Key = Zeroizing::new([0x77u8; 32]);
        let sealed = store.seal_key(&key).unwrap();
        let recovered = store.unseal_key(&sealed).unwrap();
        assert_eq!(recovered.as_ref(), key.as_ref());
    }
}
