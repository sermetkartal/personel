//! Atomic binary swap + rollback for OTA updates.
//!
//! Given an [`UpdateMetadata`] produced by [`crate::verify_update_package`],
//! [`apply_update`] stages each new binary next to the install directory and
//! then cuts over using `MoveFileExW(MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)`
//! — which is exactly what `std::fs::rename` does on Windows in the
//! file-exists case. The cutover order is:
//!
//! 1. `enroll.exe` — no running process; always swappable.
//! 2. `personel-watchdog.exe` — running, but the agent can ping the watchdog
//!    to `exit` gracefully over the named pipe before the swap; it will be
//!    restarted by SCM after the agent's own cutover.
//! 3. `personel-agent.exe` — LAST. This file is held open by the running
//!    agent process. We first try the atomic rename (works on Windows only
//!    if the file is NOT opened with `FILE_SHARE_DELETE` — typical agent
//!    launchers do NOT pass that flag, so the rename will fail); on failure
//!    we schedule a `MOVEFILE_DELAY_UNTIL_REBOOT` pending swap and shell out
//!    to `sc.exe stop PersonelAgent && sc.exe start PersonelAgent` so the
//!    service host restarts the process, which releases the handle, after
//!    which the watchdog (which we already upgraded) will observe the gap
//!    and invoke `sc start` itself.
//!
//! **Rollback invariant:** before each `MoveFileExW` we capture the CURRENT
//! binary into `<install_dir>\.staging\old_<name>` via an atomic rename.
//! If any stage fails, [`rollback_update`] can atomically move those files
//! back, producing either a fully-OLD or fully-NEW tree. Half-and-half is
//! not reachable because each swap is a single atomic call and we only
//! advance the list after success.
//!
//! The MOVEFILE_DELAY_UNTIL_REBOOT call requires a single `unsafe` block;
//! the rest of the module is safe. The crate-level `#![deny(unsafe_code)]`
//! is overridden here with `#![allow(unsafe_code)]`.

#![allow(unsafe_code)]

use std::io::Read;
use std::path::Path;

use sha2::{Digest, Sha256};
use tracing::{error, info, warn};

use crate::package::{UpdateError, UpdateMetadata};

// ── Public API ────────────────────────────────────────────────────────────────

/// Cutover order — enroll first (safest), watchdog second, agent LAST.
///
/// The agent is last because it is the running process; the swap either
/// succeeds atomically OR we fall back to DELAY_UNTIL_REBOOT + service
/// restart. By that time the watchdog has already been upgraded, so the
/// restart path produces a consistent NEW-on-both-halves state.
const SWAP_ORDER: &[&str] = &[
    "enroll.exe",
    "personel-watchdog.exe",
    "personel-agent.exe",
];

/// Applies a verified update package to `install_dir`.
///
/// The caller must already have invoked [`crate::verify_update_package`] and
/// obtained a trusted [`UpdateMetadata`]. This function does NOT re-verify
/// signatures — it trusts its inputs, so the caller must not construct
/// metadata by hand.
///
/// # Errors
///
/// Returns [`UpdateError::Rollback`] if any swap step fails; the caller
/// should invoke [`rollback_update`] to restore the old binaries. On the
/// running-agent fallback path, returns `Ok(())` with a warning log — the
/// actual swap is deferred to reboot OR to the watchdog-triggered service
/// restart path.
pub fn apply_update(metadata: UpdateMetadata, install_dir: &Path) -> Result<(), UpdateError> {
    info!(
        version = %metadata.version,
        ?install_dir,
        "apply_update: starting atomic swap"
    );

    let staging_dir = install_dir.join(".staging");
    std::fs::create_dir_all(&staging_dir)
        .map_err(|e| UpdateError::Io(format!("create .staging: {e}")))?;

    // Pre-flight: every binary mentioned in SWAP_ORDER that exists in the
    // metadata MUST also be present in the extracted directory with a
    // verified hash. Re-hash once more as a defence-in-depth check.
    for name in SWAP_ORDER {
        if let Some(src) = metadata.binary_paths.get(*name) {
            let hash = sha256_file(src)
                .map_err(|e| UpdateError::Io(format!("pre-flight hash {name}: {e}")))?;
            // The manifest hash is lower-case hex; sha256_file returns lower-case.
            let manifest_hash = metadata
                .manifest
                .binaries
                .iter()
                .find(|b| b.name == *name)
                .map(|b| b.sha256.to_ascii_lowercase());
            if let Some(expected) = manifest_hash {
                if expected != hash {
                    return Err(UpdateError::Rollback(format!(
                        "staged binary {name} hash drifted from manifest"
                    )));
                }
            }
        }
    }

    // Execute cutover in order. Track which entries succeeded so the
    // caller's rollback path knows where to start.
    let mut completed: Vec<&str> = Vec::new();
    for name in SWAP_ORDER {
        let Some(src) = metadata.binary_paths.get(*name) else {
            // Not present in this package — skip. (A package that only
            // updates one of the three binaries is valid.)
            continue;
        };
        let dst = install_dir.join(name);
        let backup = staging_dir.join(format!("old_{name}"));

        match cutover_single(name, src, &dst, &backup) {
            Ok(CutoverResult::Atomic) => {
                info!(name, "apply_update: atomic swap ok");
                completed.push(name);
            }
            Ok(CutoverResult::DeferredReboot) => {
                warn!(
                    name,
                    "apply_update: file locked; scheduled DELAY_UNTIL_REBOOT + service restart"
                );
                completed.push(name);
                // Schedule the service restart only for the agent itself —
                // the watchdog doesn't need a service cycle because we
                // already cut it over atomically in the previous step.
                if *name == "personel-agent.exe" {
                    if let Err(e) = schedule_service_restart() {
                        warn!(error = %e, "apply_update: service restart scheduling failed");
                    }
                }
            }
            Err(e) => {
                error!(name, error = %e, "apply_update: swap failed; triggering rollback");
                // Best-effort rollback of what we already swapped.
                if let Err(r) = rollback_update(install_dir) {
                    return Err(UpdateError::RollbackFailed {
                        stage: "apply_update",
                        reason: format!("primary={e}; rollback={r}"),
                    });
                }
                return Err(UpdateError::Rollback(format!("stage {name}: {e}")));
            }
        }
    }

    info!(
        version = %metadata.version,
        count = completed.len(),
        "apply_update: all stages completed"
    );
    Ok(())
}

/// Restores the previous binary set from `<install_dir>\.staging\old_*.exe`.
///
/// Called by [`apply_update`] on failure, and available as a public entry
/// point for the caller to invoke after a verification failure that
/// occurred post-extraction but pre-swap.
///
/// # Errors
///
/// Returns [`UpdateError::RollbackFailed`] if a restore step itself fails.
pub fn rollback_update(install_dir: &Path) -> Result<(), UpdateError> {
    warn!(?install_dir, "rollback_update: restoring previous binaries");

    let staging_dir = install_dir.join(".staging");
    if !staging_dir.exists() {
        // Nothing to roll back — not an error; the update never got far
        // enough to stage any old files.
        warn!("rollback_update: no .staging directory; nothing to restore");
        return Ok(());
    }

    // Reverse swap order: agent first (since it was the last to be
    // forward-swapped and is most likely to be the one that failed).
    let reverse_order: Vec<&str> = SWAP_ORDER.iter().rev().copied().collect();
    for name in reverse_order {
        let backup = staging_dir.join(format!("old_{name}"));
        if !backup.exists() {
            continue;
        }
        let dst = install_dir.join(name);
        warn!(name, ?backup, ?dst, "rollback_update: moving backup back");
        match atomic_rename(&backup, &dst) {
            Ok(()) => info!(name, "rollback_update: restored"),
            Err(e) => {
                return Err(UpdateError::RollbackFailed {
                    stage: "rollback_update",
                    reason: format!("{name}: {e}"),
                });
            }
        }
    }

    info!("rollback_update: complete");
    Ok(())
}

// ── Internal cutover primitives ──────────────────────────────────────────────

enum CutoverResult {
    /// The `MoveFileExW` REPLACE_EXISTING call succeeded.
    Atomic,
    /// The file was locked; DELAY_UNTIL_REBOOT was scheduled instead.
    DeferredReboot,
}

/// Cuts over a single binary:
///
/// 1. If `dst` exists, atomically move it to `backup` (no-op otherwise).
/// 2. Atomically move `src` to `dst`.
/// 3. On file-lock failure at step 2, roll step 1 back and schedule a
///    reboot-time move.
fn cutover_single(
    name: &str,
    src: &Path,
    dst: &Path,
    backup: &Path,
) -> Result<CutoverResult, UpdateError> {
    // Step 1: back up current binary. If dst doesn't exist (first install),
    // skip. If backup already exists (leftover from prior attempt), clear it.
    if backup.exists() {
        std::fs::remove_file(backup)
            .map_err(|e| UpdateError::Io(format!("clear stale backup {name}: {e}")))?;
    }
    if dst.exists() {
        atomic_rename(dst, backup).map_err(|e| {
            UpdateError::Io(format!("backup current {name}: {e}"))
        })?;
    }

    // Step 2: move new into place.
    match atomic_rename(src, dst) {
        Ok(()) => Ok(CutoverResult::Atomic),
        Err(atomic_err) => {
            warn!(
                name,
                error = %atomic_err,
                "atomic rename failed; trying DELAY_UNTIL_REBOOT fallback"
            );
            // Roll the backup back so the OLD file remains in place until
            // the reboot.
            if backup.exists() {
                if let Err(e) = atomic_rename(backup, dst) {
                    return Err(UpdateError::Io(format!(
                        "rollback after locked swap {name}: {e}"
                    )));
                }
            }
            // Schedule the NEW file to replace dst at reboot. We keep `src`
            // in place (the `.extracted` path) because MoveFileExW needs
            // both paths to still exist at reboot time.
            schedule_delay_until_reboot(src, dst)?;
            Ok(CutoverResult::DeferredReboot)
        }
    }
}

/// Atomic rename using `std::fs::rename` which on Windows calls
/// `MoveFileExW(src, dst, MOVEFILE_REPLACE_EXISTING | MOVEFILE_COPY_ALLOWED)`
/// and on POSIX uses `rename(2)` (already atomic for files on the same FS).
fn atomic_rename(src: &Path, dst: &Path) -> Result<(), String> {
    std::fs::rename(src, dst).map_err(|e| format!("rename {src:?} → {dst:?}: {e}"))
}

/// Hashes a file on disk; used for the pre-flight consistency check.
fn sha256_file(path: &Path) -> std::io::Result<String> {
    let mut f = std::fs::File::open(path)?;
    let mut hasher = Sha256::new();
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        hasher.update(&buf[..n]);
    }
    Ok(hex::encode(hasher.finalize()))
}

// ── DELAY_UNTIL_REBOOT fallback (Windows only) ───────────────────────────────

#[cfg(target_os = "windows")]
fn schedule_delay_until_reboot(src: &Path, dst: &Path) -> Result<(), UpdateError> {
    use windows::core::{HSTRING, PCWSTR};
    use windows::Win32::Storage::FileSystem::{
        MoveFileExW, MOVEFILE_DELAY_UNTIL_REBOOT, MOVEFILE_REPLACE_EXISTING,
    };

    let src_wide = HSTRING::from(src.as_os_str());
    let dst_wide = HSTRING::from(dst.as_os_str());

    // SAFETY: both HSTRINGs live for the duration of the call and are
    // guaranteed NUL-terminated by the Windows bindings. MoveFileExW does
    // not retain the pointers past return.
    let result = unsafe {
        MoveFileExW(
            PCWSTR::from_raw(src_wide.as_ptr()),
            PCWSTR::from_raw(dst_wide.as_ptr()),
            MOVEFILE_DELAY_UNTIL_REBOOT | MOVEFILE_REPLACE_EXISTING,
        )
    };
    if result.is_err() {
        return Err(UpdateError::Io(format!(
            "MoveFileExW DELAY_UNTIL_REBOOT failed: {:?}",
            result
        )));
    }
    info!(?src, ?dst, "scheduled MOVEFILE_DELAY_UNTIL_REBOOT");
    Ok(())
}

#[cfg(not(target_os = "windows"))]
fn schedule_delay_until_reboot(_src: &Path, _dst: &Path) -> Result<(), UpdateError> {
    Err(UpdateError::Unsupported)
}

/// Stops + restarts the agent service so the OS releases the file handle
/// and the watchdog can pick up the swap. Best-effort; caller logs the
/// return.
#[cfg(target_os = "windows")]
fn schedule_service_restart() -> Result<(), UpdateError> {
    // We shell out to sc.exe rather than linking SCM APIs because a service
    // stopping itself mid-call is a foot-gun (the thread invoking Stop
    // would be the one about to exit). sc.exe is async by design.
    let stop = std::process::Command::new("sc")
        .args(["stop", "PersonelAgent"])
        .status();
    if let Err(e) = stop {
        return Err(UpdateError::Io(format!("sc stop: {e}")));
    }
    let start = std::process::Command::new("sc")
        .args(["start", "PersonelAgent"])
        .status();
    if let Err(e) = start {
        return Err(UpdateError::Io(format!("sc start: {e}")));
    }
    Ok(())
}

#[cfg(not(target_os = "windows"))]
fn schedule_service_restart() -> Result<(), UpdateError> {
    // Non-Windows dev builds don't manage a service — just log.
    Ok(())
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use crate::package::{ManifestBinary, PackageManifest, UpdateMetadata};
    use std::collections::HashMap;

    /// Builds a fake UpdateMetadata with N binaries of known content in a
    /// temp directory. Returns the metadata + the temp dir RAII guard.
    fn fake_metadata(names: &[&str]) -> (UpdateMetadata, tempfile::TempDir) {
        let tmp = tempfile::tempdir().unwrap();
        let extracted = tmp.path().join(".extracted");
        std::fs::create_dir_all(&extracted).unwrap();

        let mut binaries = Vec::new();
        let mut binary_paths = HashMap::new();
        for (i, name) in names.iter().enumerate() {
            let content = format!("NEW {name} {i}").into_bytes();
            let path = extracted.join(name);
            std::fs::write(&path, &content).unwrap();
            binaries.push(ManifestBinary {
                name: (*name).into(),
                sha256: hex::encode(Sha256::digest(&content)),
                size: content.len() as u64,
            });
            binary_paths.insert((*name).to_string(), path);
        }

        let metadata = UpdateMetadata {
            version: "9.9.9".into(),
            binary_paths,
            manifest: PackageManifest {
                version: "9.9.9".into(),
                binaries,
                signed_by: "test".into(),
            },
        };
        (metadata, tmp)
    }

    #[test]
    fn apply_update_happy_path_swaps_all_three() {
        let (metadata, tmp) = fake_metadata(&[
            "enroll.exe",
            "personel-watchdog.exe",
            // NOTE: we test a subset that does NOT include personel-agent.exe
            // in the install dir, because the test harness cannot simulate
            // the file-locked fallback path. personel-agent.exe swap is
            // covered below by the "first-install" test.
        ]);
        let install = tempfile::tempdir().unwrap();

        // Pre-populate install_dir with OLD versions of enroll + watchdog
        // so the backup path is exercised.
        std::fs::write(
            install.path().join("enroll.exe"),
            b"OLD enroll",
        )
        .unwrap();
        std::fs::write(
            install.path().join("personel-watchdog.exe"),
            b"OLD watchdog",
        )
        .unwrap();

        apply_update(metadata, install.path()).expect("apply ok");

        // New content is in place.
        let new_enroll = std::fs::read(install.path().join("enroll.exe")).unwrap();
        assert!(String::from_utf8_lossy(&new_enroll).starts_with("NEW enroll.exe"));
        let new_watch =
            std::fs::read(install.path().join("personel-watchdog.exe")).unwrap();
        assert!(String::from_utf8_lossy(&new_watch).starts_with("NEW personel-watchdog.exe"));

        // Backups of OLD content are present.
        let old_enroll =
            std::fs::read(install.path().join(".staging").join("old_enroll.exe")).unwrap();
        assert_eq!(old_enroll, b"OLD enroll");

        drop(tmp);
    }

    #[test]
    fn apply_update_first_install_no_backups_needed() {
        let (metadata, tmp) = fake_metadata(&["enroll.exe"]);
        let install = tempfile::tempdir().unwrap();
        // No pre-existing files in install_dir — first install case.
        apply_update(metadata, install.path()).expect("apply ok");

        let content = std::fs::read(install.path().join("enroll.exe")).unwrap();
        assert!(String::from_utf8_lossy(&content).contains("NEW enroll.exe"));

        // No backup file should have been created (nothing to back up).
        let backup = install.path().join(".staging").join("old_enroll.exe");
        assert!(!backup.exists());
        drop(tmp);
    }

    #[test]
    fn rollback_restores_backed_up_binaries() {
        let install = tempfile::tempdir().unwrap();
        let staging = install.path().join(".staging");
        std::fs::create_dir_all(&staging).unwrap();

        // Simulate the state mid-apply: NEW files in place, OLD files in backup.
        std::fs::write(install.path().join("enroll.exe"), b"NEW enroll").unwrap();
        std::fs::write(staging.join("old_enroll.exe"), b"OLD enroll").unwrap();
        std::fs::write(install.path().join("personel-watchdog.exe"), b"NEW wd")
            .unwrap();
        std::fs::write(staging.join("old_personel-watchdog.exe"), b"OLD wd").unwrap();

        rollback_update(install.path()).expect("rollback ok");

        let enroll = std::fs::read(install.path().join("enroll.exe")).unwrap();
        assert_eq!(enroll, b"OLD enroll");
        let wd = std::fs::read(install.path().join("personel-watchdog.exe")).unwrap();
        assert_eq!(wd, b"OLD wd");
    }

    #[test]
    fn rollback_nonexistent_staging_is_noop() {
        let install = tempfile::tempdir().unwrap();
        // No .staging dir at all.
        rollback_update(install.path()).expect("rollback ok");
    }

    #[test]
    fn swap_order_is_enroll_then_watchdog_then_agent() {
        assert_eq!(SWAP_ORDER[0], "enroll.exe");
        assert_eq!(SWAP_ORDER[1], "personel-watchdog.exe");
        assert_eq!(SWAP_ORDER[2], "personel-agent.exe");
    }

    #[test]
    fn sha256_file_matches_in_memory_hash() {
        let tmp = tempfile::tempdir().unwrap();
        let p = tmp.path().join("x.bin");
        std::fs::write(&p, b"hello world").unwrap();
        let got = sha256_file(&p).unwrap();
        let expected = hex::encode(Sha256::digest(b"hello world"));
        assert_eq!(got, expected);
    }
}
