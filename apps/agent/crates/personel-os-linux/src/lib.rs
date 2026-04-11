//! `personel-os-linux` — Linux OS abstraction layer for the Personel agent.
//!
//! This crate provides the same collector interface as `personel-os` (Windows)
//! but implemented against Linux APIs. It is the Linux sibling introduced in
//! Phase 2.1 of the Personel roadmap (see ADR 0016).
//!
//! # Architecture
//!
//! The crate uses a **dual-path compile strategy**:
//!
//! * On `target_os = "linux"`: real Linux module tree (`input`, `capture`,
//!   `file_events`, `ebpf`, `window_title`, `systemd`, `keystore`).
//! * On any other OS: the `stub` module provides the same public surface with
//!   every function returning [`AgentError::Unsupported`]. This keeps the
//!   workspace compiling on macOS developer machines and Windows CI runners
//!   without any platform-specific branching at the call site.
//!
//! # Phase status
//!
//! **Phase 2.1 (current):** scaffold only. All Linux-path functions return
//! `Err(AgentError::Unsupported { … })` with doc comments describing the
//! intended real implementation. Phase 2.2 will fill in real implementations.
//!
//! # Safety
//!
//! Like `personel-os`, this crate is allowed to use `unsafe` where Linux FFI
//! (fanotify ioctls, libinput raw fd access) demands it. All other crates in
//! the workspace set `#![deny(unsafe_code)]`.
//!
//! [`AgentError::Unsupported`]: personel_core::error::AgentError::Unsupported

#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

// ── Linux-native module tree ──────────────────────────────────────────────────

#[cfg(target_os = "linux")]
pub mod capture;

#[cfg(target_os = "linux")]
pub mod ebpf;

#[cfg(target_os = "linux")]
pub mod file_events;

#[cfg(target_os = "linux")]
pub mod input;

#[cfg(target_os = "linux")]
pub mod keystore;

#[cfg(target_os = "linux")]
pub mod systemd;

#[cfg(target_os = "linux")]
pub mod window_title;

// ── Cross-platform stub tree (non-Linux) ─────────────────────────────────────

#[cfg(not(target_os = "linux"))]
pub mod stub;

// ── Re-export the platform-selected surface ───────────────────────────────────
//
// Callers can write `personel_os_linux::capture::…` regardless of the host OS.

#[cfg(target_os = "linux")]
pub use capture::SessionType;

#[cfg(not(target_os = "linux"))]
pub use stub::{capture, ebpf, file_events, input, keystore, systemd, window_title};
