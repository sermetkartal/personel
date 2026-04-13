//! `personel-updater` — signed self-update subsystem.
//!
//! Phase 1 (Sprint A) shipped `manifest` + `apply` as stubs that only
//! verified a single-binary SHA-256 and notified the watchdog over IPC.
//!
//! Faz 4 #30 (Phase 2 OTA apply) adds the two modules below:
//!
//! - [`package`] — parses + signature-verifies a `.tar.gz` update package
//!   containing multiple binaries (agent, watchdog, enroll) plus a signed
//!   manifest. Unsigned or tampered packages are rejected with no fallback.
//! - [`swap`] — performs the atomic binary swap inside
//!   `<install_dir>\.staging` with ordered cutover (enroll → watchdog →
//!   agent LAST), plus a rollback path that restores the previous set.
//!
//! Crate-level lints still forbid unsafe; `swap.rs` overrides with
//! `#![allow(unsafe_code)]` so it can call `MoveFileExW` with
//! `MOVEFILE_DELAY_UNTIL_REBOOT` for the running-agent fallback.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod apply;
pub mod manifest;
pub mod package;
pub mod swap;
pub mod version_checker;

pub use package::{verify_update_package, UpdateError, UpdateMetadata};
pub use swap::{apply_update, rollback_update};
pub use version_checker::{
    compare_semver, run_version_checker, UpdateManifest, VersionCheckerConfig,
};
