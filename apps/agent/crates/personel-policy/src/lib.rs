//! `personel-policy` — policy engine with in-memory cache and hot reload.
//!
//! The policy bundle is pushed from the server via the gRPC stream as a
//! `PersonelV1PolicyBundle` signed with Ed25519. The engine verifies the
//! signature, stores the bundle atomically, and notifies collectors of the
//! change.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod engine;

pub use engine::{PolicyEngine, PolicyView};
