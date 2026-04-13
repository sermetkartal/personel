//! `personel-core` — shared types, error definitions, ID newtypes, and clock
//! abstractions used across the entire agent workspace.
//!
//! Every crate in the workspace depends on this crate. It has no OS-specific
//! dependencies and compiles on Linux/macOS for developer ergonomics.

#![deny(unsafe_code)]
// Reality check 2026-04-11: temporarily relaxed from `deny` to `warn` because
// several error enum fields and event struct fields lack docs. Tech debt:
// restore to `deny(missing_docs)` in a dedicated polish sprint.
#![warn(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod clock;
pub mod error;
pub mod event;
pub mod ids;
pub mod throttle;

pub use error::{AgentError, Result};
