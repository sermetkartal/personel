//! AES-256-GCM authenticated encryption envelope.
//!
//! This is the concrete implementation of §"Keystroke Encryption at the
//! Endpoint" from `docs/architecture/key-hierarchy.md`:
//!
//! ```text
//! ciphertext = AES-256-GCM(key=PE-DEK, nonce=random96, aad=endpoint_id||seq, plaintext=buffer)
//! ```
//!
//! The [`encrypt`] function returns a [`CipherEnvelope`] that contains the
//! nonce, ciphertext + GCM tag, and the AAD bytes so the caller can store
//! them in the `keystroke.content_encrypted` proto event.
//!
//! The [`decrypt`] function is provided for DLP service parity tests and
//! key-rotation jobs — it is NOT used on the agent in normal operation.

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Key, Nonce};
use rand::RngCore;
use zeroize::Zeroizing;

use personel_core::error::{AgentError, Result};

use crate::Aes256Key;

// ──────────────────────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────────────────────

/// Result of a single AES-256-GCM encryption operation.
///
/// The ciphertext field includes the 16-byte GCM authentication tag appended
/// by `aes-gcm` (i.e., `len(ciphertext) == len(plaintext) + 16`).
#[derive(Debug, Clone)]
pub struct CipherEnvelope {
    /// Random 96-bit (12-byte) nonce. Must never be reused with the same key.
    pub nonce: [u8; 12],
    /// Ciphertext with GCM tag appended (len = plaintext_len + 16).
    pub ciphertext: Vec<u8>,
    /// Additional authenticated data (not encrypted, but authenticated).
    pub aad: Vec<u8>,
}

// ──────────────────────────────────────────────────────────────────────────────
// AAD construction
// ──────────────────────────────────────────────────────────────────────────────

/// Builds the canonical AAD for keystroke encryption.
///
/// AAD = `endpoint_id_bytes (16) || seq (8, big-endian)`
///
/// This matches the `aad` field in `keystroke.content_encrypted` proto event
/// and binds the ciphertext to a specific endpoint and sequence number,
/// preventing replay of a ciphertext under a different identity.
#[must_use]
pub fn build_keystroke_aad(endpoint_id: &[u8; 16], seq: u64) -> Vec<u8> {
    let mut aad = Vec::with_capacity(24);
    aad.extend_from_slice(endpoint_id);
    aad.extend_from_slice(&seq.to_be_bytes());
    aad
}

// ──────────────────────────────────────────────────────────────────────────────
// Encrypt
// ──────────────────────────────────────────────────────────────────────────────

/// Encrypts `plaintext` under `key` with AES-256-GCM.
///
/// A fresh random 96-bit nonce is generated for every call using the OS CSPRNG.
/// The caller must ensure the key is never reused across more than 2^32
/// messages (GCM nonce collision safety limit).
///
/// After this function returns, the caller MUST zeroize the plaintext buffer
/// per `key-hierarchy.md` §Code-Level Rules.
///
/// # Errors
///
/// Returns [`AgentError::AeadError`] if the `aes-gcm` crate reports an
/// encryption error (should not happen in practice for valid key lengths).
pub fn encrypt(key: &Aes256Key, aad: Vec<u8>, plaintext: &[u8]) -> Result<CipherEnvelope> {
    let mut nonce_bytes = [0u8; 12];
    rand::thread_rng().fill_bytes(&mut nonce_bytes);

    let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(key.as_ref()));
    let nonce = Nonce::from(nonce_bytes);

    let ciphertext = cipher
        .encrypt(&nonce, aes_gcm::aead::Payload { msg: plaintext, aad: &aad })
        .map_err(|_| AgentError::AeadError)?;

    Ok(CipherEnvelope { nonce: nonce_bytes, ciphertext, aad })
}

// ──────────────────────────────────────────────────────────────────────────────
// Decrypt
// ──────────────────────────────────────────────────────────────────────────────

/// Decrypts a [`CipherEnvelope`] produced by [`encrypt`].
///
/// Returns a [`Zeroizing`] buffer so the caller can safely wipe the plaintext
/// after use.
///
/// # Errors
///
/// Returns [`AgentError::AeadError`] if the GCM tag does not verify
/// (tampered ciphertext, wrong key, or replayed nonce).
pub fn decrypt(key: &Aes256Key, envelope: &CipherEnvelope) -> Result<Zeroizing<Vec<u8>>> {
    let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(key.as_ref()));
    let nonce = Nonce::from(envelope.nonce);

    let plaintext = cipher
        .decrypt(
            &nonce,
            aes_gcm::aead::Payload { msg: &envelope.ciphertext, aad: &envelope.aad },
        )
        .map_err(|_| AgentError::AeadError)?;

    Ok(Zeroizing::new(plaintext))
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn test_key() -> Aes256Key {
        Zeroizing::new([0x42u8; 32])
    }

    #[test]
    fn encrypt_decrypt_roundtrip() {
        let key = test_key();
        let aad = build_keystroke_aad(&[0xAB; 16], 42);
        let plaintext = b"hello, keystroke world";

        let envelope = encrypt(&key, aad, plaintext).unwrap();
        // Ciphertext must be longer than plaintext (16-byte GCM tag appended).
        assert_eq!(envelope.ciphertext.len(), plaintext.len() + 16);

        let recovered = decrypt(&key, &envelope).unwrap();
        assert_eq!(recovered.as_slice(), plaintext);
    }

    #[test]
    fn decrypt_fails_on_tampered_ciphertext() {
        let key = test_key();
        let aad = build_keystroke_aad(&[0x01; 16], 1);
        let mut envelope = encrypt(&key, aad, b"sensitive").unwrap();
        // Flip a byte in the ciphertext.
        if let Some(b) = envelope.ciphertext.first_mut() {
            *b ^= 0xFF;
        }
        assert!(decrypt(&key, &envelope).is_err());
    }

    #[test]
    fn decrypt_fails_on_wrong_key() {
        let key = test_key();
        let wrong_key = Zeroizing::new([0xFFu8; 32]);
        let aad = build_keystroke_aad(&[0x02; 16], 2);
        let envelope = encrypt(&key, aad, b"sensitive").unwrap();
        assert!(decrypt(&wrong_key, &envelope).is_err());
    }

    #[test]
    fn different_nonces_each_call() {
        let key = test_key();
        let aad = build_keystroke_aad(&[0x03; 16], 3);
        let e1 = encrypt(&key, aad.clone(), b"msg").unwrap();
        let e2 = encrypt(&key, aad, b"msg").unwrap();
        // Probabilistically distinct (2^96 nonce space).
        assert_ne!(e1.nonce, e2.nonce);
    }
}
