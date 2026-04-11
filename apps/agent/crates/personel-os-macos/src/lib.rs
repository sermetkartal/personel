//! `personel-os-macos` — macOS OS-abstraction layer.
//!
//! This crate is the macOS counterpart to `personel-os` (Windows). It exposes
//! the same collector API surface but backed by Apple system frameworks:
//!
//! | Module              | macOS framework                        | Phase    |
//! |---------------------|----------------------------------------|----------|
//! | [`input`]           | IOHIDManager / NSWorkspace             | 2.1 stub |
//! | [`capture`]         | ScreenCaptureKit                       | 2.1 stub |
//! | [`file_events`]     | FSEvents + Endpoint Security Framework | 2.1 stub |
//! | [`network_extension`] | Network Extension (NEFilter)         | 2.1 stub |
//! | [`tcc`]             | Security.framework AuthorizationCopyRights | 2.1 partial |
//! | [`es_bridge`]       | UDS bridge to Swift ES helper process  | 2.1 stub |
//! | [`service`]         | launchd plist generation               | 2.1 partial |
//! | [`keystore`]        | Keychain (Security.framework SecItem)  | 2.1 partial |
//!
//! # Cross-platform compilation
//!
//! On non-macOS hosts every public function returns
//! [`AgentError::Unsupported`][personel_core::error::AgentError::Unsupported]
//! so the workspace compiles cleanly on Linux and Windows. Only the type
//! shapes (structs / enums) are always present; behaviour is gated on
//! `#[cfg(target_os = "macos")]`.
//!
//! # Safety policy
//!
//! This is the **only Phase 2 crate** allowed to use `unsafe` code. All
//! unsafe blocks must carry a `// SAFETY:` comment explaining the invariant
//! that makes the operation sound.
//!
//! # Phase roadmap
//!
//! - **2.1 (this crate)**: type-correct stubs; `cargo check` clean on all OSes.
//! - **2.2**: Wire into `personel-agent` via an OS facade; real `tcc`, `service`,
//!   and `keystore` implementations validated on macOS CI.
//! - **2.3**: Real `input` (IOHIDManager), `capture` (ScreenCaptureKit), and
//!   `file_events` (FSEvents path).
//! - **2.4**: Swift ES helper process; `es_bridge` → real UDS framing; real
//!   `network_extension` (NEFilterDataProvider).

// personel-os-macos may use unsafe for Objective-C / CoreFoundation FFI.
// Every unsafe block requires a SAFETY comment.
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

// ── Module declarations ────────────────────────────────────────────────────

pub mod capture;
pub mod es_bridge;
pub mod file_events;
pub mod input;
pub mod keystore;
pub mod network_extension;
pub mod service;
pub mod tcc;

/// Non-macOS stub implementations of the full API surface.
///
/// Each function in this module mirrors a macOS-backed function but returns
/// `Err(AgentError::Unsupported { ... })`. Compiled in on Linux and Windows;
/// excluded on macOS (the real module tree handles that platform).
#[cfg(not(target_os = "macos"))]
pub mod stub;

// ── Re-exports ─────────────────────────────────────────────────────────────
// On macOS the concrete module implementations are the public surface.
// On other OSes the stub module fills the same shape.

// NOTE: The individual modules already branch internally with
// `#[cfg(target_os = "macos")]` / `#[cfg(not(target_os = "macos"))]`
// so callers always import from the same module path regardless of OS.
// This approach is chosen over a single top-level re-export because each
// module may carry types (structs, enums) that must be unconditionally
// available for the compiler to resolve cross-crate uses in Phase 2.2+.
