//! Update staging, atomic swap, and rollback.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Stage downloaded artifact to `<agent_dir>/update/pending/<version>/`.
//! - Notify watchdog via IPC (`UpdateReady { artifact_path, signature }`).
//! - Watchdog stops main, moves staging dir over current, restarts main.
//! - On rollback: watchdog detects missing health ping within N minutes and
//!   swaps back `update/rollback/<previous_version>/`.

use std::path::PathBuf;

use tracing::{info, warn};

use personel_core::error::{AgentError, Result};

/// Status of the update staging process.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum UpdateStageStatus {
    /// Artifact is staged and ready for the watchdog to apply.
    Ready {
        /// Path to the staged artifact binary.
        artifact_path: PathBuf,
    },
    /// Rollback copy is available.
    RollbackAvailable {
        /// Path to the previous-version binary kept for rollback.
        rollback_path: PathBuf,
    },
}

/// Stages a downloaded artifact for update.
///
/// # Errors
///
/// Returns an error if the staging directory cannot be created or the file
/// cannot be written.
pub fn stage_artifact(
    update_dir: &std::path::Path,
    version: &str,
    artifact_bytes: &[u8],
) -> Result<UpdateStageStatus> {
    let pending_dir = update_dir.join("pending").join(version);

    // TODO: implement staging:
    // std::fs::create_dir_all(&pending_dir)?;
    // let artifact_path = pending_dir.join("personel-agent.exe");
    // std::fs::write(&artifact_path, artifact_bytes)?;
    // info!(version, ?artifact_path, "update artifact staged");
    // Ok(UpdateStageStatus::Ready { artifact_path })

    let _ = (pending_dir, artifact_bytes);
    warn!(version, "update staging not yet implemented");
    Err(AgentError::Internal("update staging not yet implemented".into()))
}
