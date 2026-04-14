//! `personel-core` — shared types, error definitions, ID newtypes, and clock
//! abstractions used across the entire agent workspace.
//!
//! Every crate in the workspace depends on this crate. It has no OS-specific
//! dependencies and compiles on Linux/macOS for developer ergonomics.

#![deny(unsafe_code)]
// 2026-04-14 polish pass (#190): all error enum fields and event struct
// fields now documented. Formal lint stance restored to
// `deny(missing_docs)`; any new undocumented public item is a compile error.
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod clock;
pub mod error;
pub mod event;
pub mod ids;
pub mod throttle;
pub mod user_context;

pub use error::{AgentError, Result};
