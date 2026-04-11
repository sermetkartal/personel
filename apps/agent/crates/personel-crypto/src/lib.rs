//! `personel-crypto` — key hierarchy agent side.
//!
//! Implements the crypto operations described in `docs/architecture/key-hierarchy.md`:
//!
//! - AES-256-GCM envelope encrypt/decrypt ([`envelope`])
//! - PE-DEK storage via DPAPI + TPM-bound sealing ([`keystore`])
//! - X25519-sealed enrollment key delivery ([`enroll`])
//!
//! # Safety
//!
//! This crate does not contain any `unsafe` code. All Win32 calls are
//! delegated to `personel-os`.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod envelope;
pub mod enroll;
pub mod keystore;

/// A zeroizing 32-byte AES-256 key.
///
/// Using [`zeroize::Zeroizing`] ensures the key bytes are wiped on drop,
/// satisfying the requirement from `key-hierarchy.md` §Code-Level Rules:
/// *"PE-DEK MUST NOT be logged, serialized, or swapped to disk unprotected."*
pub type Aes256Key = zeroize::Zeroizing<[u8; 32]>;

/// A zeroizing 12-byte AES-GCM nonce.
pub type AesGcmNonce = zeroize::Zeroizing<[u8; 12]>;
