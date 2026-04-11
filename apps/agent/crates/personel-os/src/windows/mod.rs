//! Windows platform module.
//!
//! Each sub-module wraps a specific Win32 API surface into a safe abstraction.
//! Unsafe code is localised to the smallest possible scope within each file.

pub mod anti_tamper;
pub mod capture;
pub mod dpapi;
pub mod etw;
pub mod input;
pub mod service;
