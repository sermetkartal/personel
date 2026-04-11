//! X25519-sealed enrollment key delivery.
//!
//! During enrollment the DLP Service generates a fresh PE-DEK and delivers
//! it to the agent via an ephemeral X25519 ECDH + HKDF-SHA256 + AES-256-GCM
//! channel. This module implements the **agent side** of that handshake:
//!
//! 1. The agent generates an ephemeral X25519 key pair at enrollment time.
//! 2. The agent sends the public key to the enrollment endpoint (in the CSR
//!    subject alt name or a JSON field — server-side concern).
//! 3. The DLP Service responds with `SealedKey { ephemeral_pub, ciphertext }`.
//! 4. The agent calls [`unseal_dek`] to recover the plaintext PE-DEK.
//!
//! The shared secret is:
//! ```text
//! shared = X25519(agent_private, server_ephemeral_pub)
//! key    = HKDF-SHA256(ikm=shared, salt=b"personel-enrollment-v1", info=endpoint_id)
//! dek    = AES-256-GCM-Decrypt(key, nonce, ciphertext, aad=endpoint_id)
//! ```

use hkdf::Hkdf;
use sha2::Sha256;
use x25519_dalek::{EphemeralSecret, PublicKey, StaticSecret};
use zeroize::Zeroizing;

use personel_core::error::{AgentError, Result};

use crate::envelope::{decrypt, CipherEnvelope};
use crate::Aes256Key;

/// Salt used in HKDF during enrollment key derivation.
const HKDF_SALT: &[u8] = b"personel-enrollment-v1";
/// HKDF info distinguishes the derived key from any other use of the same IKM.
const HKDF_INFO: &[u8] = b"pe-dek-v1";

/// A sealed PE-DEK as delivered by the DLP Service.
///
/// `ciphertext` is an AES-256-GCM ciphertext (plaintext PE-DEK + 16-byte tag).
/// `nonce` is the GCM nonce. `aad` is the endpoint_id bytes (16 bytes).
#[derive(Debug, Clone)]
pub struct SealedDek {
    /// Server's ephemeral X25519 public key (32 bytes).
    pub server_ephemeral_pub: [u8; 32],
    /// GCM nonce (12 bytes).
    pub nonce: [u8; 12],
    /// AES-256-GCM ciphertext of the 32-byte PE-DEK (i.e., 48 bytes total).
    pub ciphertext: Vec<u8>,
    /// AAD = endpoint_id bytes.
    pub aad: Vec<u8>,
}

/// Generates a fresh agent enrollment key pair.
///
/// Returns `(secret, public_key_bytes)`. The secret must be stored securely
/// (DPAPI-protected) until `unseal_dek` is called during enrollment.
#[must_use]
pub fn generate_enrollment_keypair() -> (StaticSecret, [u8; 32]) {
    let secret = StaticSecret::random_from_rng(rand::thread_rng());
    let public = PublicKey::from(&secret);
    (secret, *public.as_bytes())
}

/// Unseals the PE-DEK using the agent's enrollment private key.
///
/// # Errors
///
/// - [`AgentError::AeadError`] if the GCM tag does not verify.
/// - [`AgentError::HkdfLength`] if HKDF expansion fails (should not happen).
pub fn unseal_dek(agent_secret: &StaticSecret, sealed: &SealedDek) -> Result<Aes256Key> {
    // X25519 DH.
    let server_pub = PublicKey::from(sealed.server_ephemeral_pub);
    let shared_secret = Zeroizing::new(agent_secret.diffie_hellman(&server_pub));

    // HKDF-SHA256 to derive the wrapping key.
    let hk = Hkdf::<Sha256>::new(Some(HKDF_SALT), shared_secret.as_bytes());
    let mut wrapping_key = Aes256Key::default();
    hk.expand(HKDF_INFO, wrapping_key.as_mut())
        .map_err(|_| AgentError::HkdfLength)?;

    // AES-256-GCM decrypt.
    let envelope = CipherEnvelope {
        nonce: sealed.nonce,
        ciphertext: sealed.ciphertext.clone(),
        aad: sealed.aad.clone(),
    };
    let dek_bytes = decrypt(&wrapping_key, &envelope)?;

    // Expect exactly 32 bytes.
    let dek_arr: [u8; 32] = dek_bytes
        .as_slice()
        .try_into()
        .map_err(|_| AgentError::InvalidId { reason: "PE-DEK must be 32 bytes" })?;

    Ok(Zeroizing::new(dek_arr))
}

/// Seals a DEK for delivery to an agent (used in test / DLP service parity).
///
/// This is the **server side** of the handshake provided here for testing.
/// It is NOT called on the agent in production.
#[cfg(test)]
pub(crate) fn seal_dek_for_test(
    agent_pub: &[u8; 32],
    dek: &Aes256Key,
    endpoint_id: &[u8; 16],
) -> SealedDek {
    use crate::envelope::encrypt;

    let server_ephemeral = EphemeralSecret::random_from_rng(rand::thread_rng());
    let server_pub_bytes = *PublicKey::from(&server_ephemeral).as_bytes();

    let agent_pub_key = PublicKey::from(*agent_pub);
    let shared_secret = Zeroizing::new(server_ephemeral.diffie_hellman(&agent_pub_key));

    let hk = Hkdf::<Sha256>::new(Some(HKDF_SALT), shared_secret.as_bytes());
    let mut wrapping_key = Aes256Key::default();
    hk.expand(HKDF_INFO, wrapping_key.as_mut()).unwrap();

    let aad = endpoint_id.to_vec();
    let envelope = encrypt(&wrapping_key, aad.clone(), dek.as_ref()).unwrap();

    SealedDek {
        server_ephemeral_pub: server_pub_bytes,
        nonce: envelope.nonce,
        ciphertext: envelope.ciphertext,
        aad,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use zeroize::Zeroizing;

    #[test]
    fn enrollment_seal_unseal_roundtrip() {
        let endpoint_id = [0xABu8; 16];
        let original_dek = Zeroizing::new([0x99u8; 32]);

        let (agent_secret, agent_pub) = generate_enrollment_keypair();
        let sealed = seal_dek_for_test(&agent_pub, &original_dek, &endpoint_id);
        let recovered = unseal_dek(&agent_secret, &sealed).unwrap();

        assert_eq!(recovered.as_ref(), original_dek.as_ref());
    }

    #[test]
    fn wrong_agent_key_fails() {
        let endpoint_id = [0x11u8; 16];
        let dek = Zeroizing::new([0xAAu8; 32]);

        let (_correct_secret, agent_pub) = generate_enrollment_keypair();
        let (wrong_secret, _) = generate_enrollment_keypair();

        let sealed = seal_dek_for_test(&agent_pub, &dek, &endpoint_id);
        let result = unseal_dek(&wrong_secret, &sealed);
        assert!(result.is_err());
    }
}
