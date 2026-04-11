//! `personel-queue` — offline SQLCipher event buffer.
//!
//! Provides durable local storage for agent events while the gRPC connection
//! is unavailable or backpressure is applied. The database is AES-256-CBC
//! page-encrypted by SQLCipher, with the key provided at `PRAGMA key` time.
//!
//! # Architecture Decision (inline ADR)
//!
//! **Decision:** `rusqlite` with `bundled-sqlcipher` feature.
//!
//! **Alternatives considered:**
//! - `sqlx` + plain SQLite: requires a separate AES-GCM VFS layer (more code,
//!   no off-the-shelf security audit).
//! - `sqlx` + SQLCipher: sqlx doesn't support SQLCipher's `PRAGMA key` via its
//!   connection hook in a stable way at the time of this decision.
//! - System-installed SQLCipher DLL: introduces a DLL dependency that can be
//!   hijacked; bundled static linking eliminates this attack surface.
//!
//! **Consequences:**
//! - Binary size increases ~2 MB (acceptable given 500 MB disk budget).
//! - No runtime DLL dependency.
//! - Static SQLCipher avoids DLL hijacking (anti-tamper §9).
//! - `bundled-sqlcipher` links OpenSSL for AES on some platforms; the Cargo
//!   feature may require `OPENSSL_STATIC=1` on some CI setups — flag for
//!   devops-engineer.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod queue;
pub mod schema;

pub use queue::{EventQueue, QueueConfig, QueuedEvent, QueueStats};

// The conversion `From<rusqlite::Error> for AgentError` now lives in
// personel-core behind the `rusqlite` feature, which we enable in our
// Cargo.toml. Orphan rules prevented us from impl'ing it here.
