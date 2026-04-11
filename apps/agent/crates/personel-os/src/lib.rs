//! `personel-os` — OS abstraction layer.
//!
//! This is the **only crate in the workspace allowed to use `unsafe` code**.
//! All Win32 FFI calls are wrapped here into safe, `#[must_use]`-annotated
//! APIs. Every other crate calls into this crate and declares
//! `#![deny(unsafe_code)]`.
//!
//! On non-Windows platforms the `stub` module provides no-op implementations
//! that return `Err(...)` so the workspace compiles for developer ergonomics,
//! but the agent will refuse to start.

// Intentionally NO #![deny(unsafe_code)] here — this is the one crate that
// needs it in the windows submodules.
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

#[cfg(target_os = "windows")]
pub mod windows;

#[cfg(not(target_os = "windows"))]
pub mod stub;

// Re-export the platform-selected surface so callers use the same path.
#[cfg(target_os = "windows")]
pub use windows::{anti_tamper, capture, dpapi, etw, input, service};

#[cfg(not(target_os = "windows"))]
pub use stub::{anti_tamper, capture, dpapi, etw, input, service};
