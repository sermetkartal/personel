//! Update apply flow: SHA-256 verify, Authenticode stub, watchdog IPC notify.
//!
//! The caller (update poller) is responsible for downloading the artifact to
//! `staging_dir` before calling [`apply_update`]. This module:
//!
//! 1. Verifies the staged file's SHA-256 against the manifest.
//! 2. Stubs Authenticode verification (Windows `WinVerifyTrust` — Sprint E).
//! 3. Sends `{"cmd":"update_ready", ...}` to the watchdog over the named pipe
//!    (Windows) or Unix socket (dev/macOS/Linux).
//!
//! Steps 4-7 (binary swap, service restart, health confirmation, rollback) are
//! performed by the watchdog — see `personel-watchdog/src/ipc.rs`.

#![deny(unsafe_code)]

use std::path::Path;

use sha2::{Digest, Sha256};
use tracing::{info, warn};

use personel_core::error::{AgentError, Result};

use crate::manifest::UpdateManifest;

// ── Public API ────────────────────────────────────────────────────────────────

/// Result of the update apply operation (agent side).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ApplyResult {
    /// Binary is staged and hash-verified; watchdog has not yet been notified.
    Staged,
    /// Watchdog was notified successfully via IPC.
    WatchdogNotified,
    /// A non-fatal failure occurred; details in the message.
    Failed(String),
}

/// Applies a downloaded update artifact.
///
/// # Arguments
///
/// * `manifest` — verified [`UpdateManifest`] produced by
///   [`UpdateManifest::parse_and_verify`].
/// * `staged_path` — path of the already-downloaded binary inside the staging
///   directory.
///
/// # Steps
///
/// 1. Read and SHA-256-hash the staged file.
/// 2. Compare hash against `manifest.artifact_sha256`.
/// 3. Stub Authenticode check (TODO Sprint E: `WinVerifyTrust`).
/// 4. Send `update_ready` command to watchdog via named pipe / Unix socket.
///
/// # Errors
///
/// Returns [`AgentError::ArtifactHash`] on hash mismatch,
/// [`AgentError::Ipc`] on pipe/socket failure, or [`AgentError::Io`] on
/// file-read failure.
pub async fn apply_update(manifest: &UpdateManifest, staged_path: &Path) -> Result<ApplyResult> {
    // ── Step 1 & 2: verify SHA-256 ────────────────────────────────────────────
    info!(?staged_path, version = %manifest.version, "apply_update: reading staged binary");

    let artifact_bytes = tokio::fs::read(staged_path).await.map_err(|e| {
        AgentError::Io(std::io::Error::new(
            e.kind(),
            format!("staged binary read failed: {e}"),
        ))
    })?;

    verify_sha256(&artifact_bytes, &manifest.artifact_sha256)?;
    info!(version = %manifest.version, "apply_update: SHA-256 verified");

    // ── Step 3: Authenticode (stub) ───────────────────────────────────────────
    verify_authenticode_stub(staged_path);

    // ── Step 4: notify watchdog ───────────────────────────────────────────────
    let hash_hex = hex::encode(Sha256::digest(&artifact_bytes));
    let staged_path_str = staged_path.to_string_lossy().into_owned();

    match notify_watchdog(&staged_path_str, &hash_hex).await {
        Ok(()) => {
            info!(version = %manifest.version, "apply_update: watchdog notified");
            Ok(ApplyResult::WatchdogNotified)
        }
        Err(e) => {
            warn!(version = %manifest.version, error = %e, "apply_update: watchdog IPC failed; binary staged but not applied");
            // Return Staged so the caller knows the binary is safe to retry.
            Ok(ApplyResult::Staged)
        }
    }
}

// ── Internal helpers ──────────────────────────────────────────────────────────

/// Verifies that `data` hashes to `expected_hex` (case-insensitive).
fn verify_sha256(data: &[u8], expected_hex: &str) -> Result<()> {
    let actual = hex::encode(Sha256::digest(data));
    if actual.eq_ignore_ascii_case(expected_hex) {
        Ok(())
    } else {
        Err(AgentError::ArtifactHash)
    }
}

/// Stub for Windows Authenticode / `WinVerifyTrust`.
///
/// Full implementation deferred to Sprint E. Logs a warning on non-Windows
/// builds. On Windows this would call `WinVerifyTrust` via the `windows` crate.
fn verify_authenticode_stub(path: &Path) {
    // TODO Sprint E: implement WinVerifyTrust call.
    // On Windows:
    //   use windows::Win32::Security::WinVerifyTrust;
    //   ... build WINTRUST_DATA, call WinVerifyTrust, check HRESULT.
    let _ = path;
    warn!(
        "apply_update: Authenticode verification not yet implemented (Sprint E TODO); \
         proceeding on SHA-256 only"
    );
}

/// Sends the `update_ready` JSON command to the watchdog process.
///
/// Uses a named pipe on Windows and a Unix socket on other platforms.
async fn notify_watchdog(staged_path: &str, hash_hex: &str) -> Result<()> {
    let cmd = serde_json::json!({
        "cmd": "update_ready",
        "path": staged_path,
        "hash": hash_hex,
    });
    let line = format!("{}\n", cmd);

    send_ipc_line(line.as_bytes()).await
}

#[cfg(target_os = "windows")]
async fn send_ipc_line(data: &[u8]) -> Result<()> {
    use tokio::io::AsyncWriteExt;
    use tokio::net::windows::named_pipe::ClientOptions;

    const PIPE_PATH: &str = r"\\.\pipe\personel-watchdog-cmd";

    let mut client = ClientOptions::new()
        .open(PIPE_PATH)
        .map_err(|e| AgentError::Ipc(format!("watchdog pipe open failed: {e}")))?;

    client
        .write_all(data)
        .await
        .map_err(|e| AgentError::Ipc(format!("watchdog pipe write failed: {e}")))?;

    client
        .flush()
        .await
        .map_err(|e| AgentError::Ipc(format!("watchdog pipe flush failed: {e}")))?;

    Ok(())
}

#[cfg(not(target_os = "windows"))]
async fn send_ipc_line(data: &[u8]) -> Result<()> {
    use tokio::io::AsyncWriteExt;
    use tokio::net::UnixStream;

    const SOCKET_PATH: &str = "/tmp/personel-watchdog.sock";

    let mut stream = UnixStream::connect(SOCKET_PATH)
        .await
        .map_err(|e| AgentError::Ipc(format!("watchdog socket connect failed ({SOCKET_PATH}): {e}")))?;

    stream
        .write_all(data)
        .await
        .map_err(|e| AgentError::Ipc(format!("watchdog socket write failed: {e}")))?;

    stream
        .flush()
        .await
        .map_err(|e| AgentError::Ipc(format!("watchdog socket flush failed: {e}")))?;

    Ok(())
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn verify_sha256_matches() {
        let data = b"hello personel";
        let hex = hex::encode(Sha256::digest(data));
        assert!(verify_sha256(data, &hex).is_ok());
    }

    #[test]
    fn verify_sha256_mismatch() {
        let data = b"hello personel";
        let result = verify_sha256(data, "deadbeef");
        assert!(matches!(result, Err(AgentError::ArtifactHash)));
    }

    #[test]
    fn verify_sha256_case_insensitive() {
        let data = b"hello personel";
        let hex_upper = hex::encode(Sha256::digest(data)).to_uppercase();
        assert!(verify_sha256(data, &hex_upper).is_ok());
    }
}
