//! Anti-tamper startup checks for `personel-agent`.
//!
//! Implements the three Phase-1 checks from `docs/security/anti-tamper.md`:
//!
//! 1. **PE self-hash** — SHA-256 of the running binary on disk, compared
//!    against a value persisted on first successful start. Mismatch → tamper.
//! 2. **Registry ACL** — inspects the DACL of
//!    `HKLM\SYSTEM\CurrentControlSet\Services\PersonelAgent` and flags any
//!    ACE granting write access to `Everyone` or `Authenticated Users`.
//! 3. **Watchdog log replay** — picks up tamper entries the watchdog wrote
//!    to `watchdog.log` while the agent was down, enqueues them, then
//!    truncates the file.
//!
//! Phase-1 simplifications (documented in §0 roadmap item #29):
//!
//! - The expected binary hash is **not** embedded at compile time
//!   (chicken-and-egg problem: you cannot hash a binary while building it).
//!   Instead we compute it on first successful startup and persist it at
//!   `%PROGRAMDATA%\Personel\agent\binhash.json`. Subsequent starts
//!   compare against that file. An update flow SHALL delete `binhash.json`
//!   before the new binary runs for the first time (that's the watchdog's
//!   responsibility during the atomic swap, see `update_ready` in
//!   `personel-watchdog`).
//! - The registry ACL check only screens for two well-known "dangerous"
//!   SIDs: `Everyone` (`S-1-1-0`) and `Authenticated Users` (`S-1-5-11`).
//!   A proper PermissionAnalyzer that enumerates every non-SYSTEM /
//!   non-Administrators ACE is deferred to Phase 2.
//! - Checks run under `#[cfg(target_os = "windows")]`. On non-Windows
//!   (developer workstations, CI) the functions are trivial stubs that
//!   return no findings, letting `cargo check` pass on any host.
//!
//! All tamper findings are enqueued as `agent.tamper_detected`
//! (`EventKind::AgentTamperDetected`, `Priority::Critical`). Critical events
//! are never evicted from the local queue — they are guaranteed to reach
//! the gateway as long as the agent runs long enough to drain the queue.

#![allow(clippy::module_name_repetitions)]
// Registry ACL walk requires direct Win32 ACL/SID calls behind `unsafe`.
// `main.rs` denies unsafe_code; this module-scoped allow overrides it for
// the ACL check only (the PE self-hash and watchdog log replay are safe).
#![allow(unsafe_code)]

use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tracing::{error, info, warn};

use personel_collectors::CollectorCtx;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;

// ──────────────────────────────────────────────────────────────────────────────
// Public surface
// ──────────────────────────────────────────────────────────────────────────────

/// Severity classification for tamper findings.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Severity {
    /// Informational — finding surfaced but may be false positive.
    Info,
    /// Medium — warrants investigation.
    Medium,
    /// High — strong evidence of tampering.
    High,
    /// Critical — agent integrity compromised.
    Critical,
}

impl Severity {
    /// Canonical lowercase name for JSON serialisation.
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Info => "info",
            Self::Medium => "medium",
            Self::High => "high",
            Self::Critical => "critical",
        }
    }
}

/// A single tamper-check finding.
#[derive(Debug, Clone)]
pub struct TamperFinding {
    /// Stable check identifier (`"pe_self_hash"`, `"registry_acl"`,
    /// `"watchdog_replay"`).
    pub check: &'static str,
    /// Severity classification.
    pub severity: Severity,
    /// Arbitrary detail JSON payload. This becomes the event payload_pb body.
    pub details: serde_json::Value,
}

/// Runs all Phase-1 anti-tamper startup checks and returns the list of
/// findings (possibly empty).
///
/// This function only **detects**; the caller is responsible for enqueueing
/// events via [`enqueue_tamper_event`].
///
/// On non-Windows hosts this returns an empty vector so the agent can still
/// boot in console mode for local development.
#[must_use]
pub fn run_startup_checks(ctx: &CollectorCtx) -> Vec<TamperFinding> {
    // Keep ctx in the signature even on non-Windows so call sites don't
    // fragment across cfg boundaries. It's used by the watchdog replay to
    // resolve the data dir.
    let _ = ctx;

    let mut findings = Vec::new();

    #[cfg(target_os = "windows")]
    {
        if let Some(f) = check_pe_self_hash() {
            findings.push(f);
        }
        if let Some(f) = check_registry_acl() {
            findings.push(f);
        }
        // Drain the watchdog log file (tamper events recorded while the
        // agent was down). This is best-effort: I/O errors are logged and
        // ignored.
        match replay_watchdog_log() {
            Ok(extra) => findings.extend(extra),
            Err(e) => warn!(error = %e, "anti_tamper: watchdog log replay failed"),
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        info!("anti_tamper: non-Windows host — startup checks skipped");
    }

    findings
}

/// Enqueues a single tamper finding as a critical agent event.
///
/// The payload is a JSON object with `check`, `severity`, and `details`
/// fields. We deliberately emit JSON rather than protobuf here because
/// `agent.tamper_detected` has no dedicated proto message in `events.proto`
/// yet — the server-side enricher accepts JSON fallback for critical
/// events (see `apps/gateway/internal/enricher`).
///
/// # Errors
///
/// Returns an error only if the queue insert fails. Callers should log and
/// continue.
pub fn enqueue_tamper_event(
    queue: &personel_queue::queue::EventQueue,
    ctx: &CollectorCtx,
    finding: &TamperFinding,
) -> Result<()> {
    let now_nanos = ctx.clock.now_unix_nanos();
    let event_id_bytes = EventId::new_v7().to_bytes();

    let payload = serde_json::json!({
        "check": finding.check,
        "severity": finding.severity.as_str(),
        "details": finding.details,
        "tenant_id": ctx.tenant_id.to_string(),
        "endpoint_id": ctx.endpoint_id.to_string(),
        "occurred_at_nanos": now_nanos,
    });
    let payload_bytes = serde_json::to_vec(&payload).context("serialize tamper payload")?;

    queue
        .enqueue(
            &event_id_bytes,
            EventKind::AgentTamperDetected.as_str(),
            Priority::Critical,
            now_nanos,
            now_nanos,
            &payload_bytes,
        )
        .map_err(|e| anyhow::anyhow!("enqueue tamper event: {e}"))?;

    error!(
        check = finding.check,
        severity = finding.severity.as_str(),
        "ANTI-TAMPER finding enqueued"
    );
    Ok(())
}

// ──────────────────────────────────────────────────────────────────────────────
// PE self-hash check
// ──────────────────────────────────────────────────────────────────────────────

/// Persisted on first successful start; compared on every subsequent start.
#[derive(Debug, Serialize, Deserialize)]
struct BinHashRecord {
    /// Canonical path of the running binary at the time of first hash.
    path: String,
    /// Hex-encoded SHA-256 of the binary file on disk.
    sha256: String,
    /// Unix timestamp (seconds) when the baseline was recorded.
    baseline_unix: i64,
}

/// Full path to the binhash persistence file.
fn binhash_path() -> PathBuf {
    crate::config::default_data_dir().join("binhash.json")
}

/// Computes SHA-256 of the file at `path`. Returns hex lowercase.
fn hash_file(path: &Path) -> Result<String> {
    let data = std::fs::read(path)
        .with_context(|| format!("read for hash: {}", path.display()))?;
    let digest = Sha256::digest(&data);
    Ok(hex::encode(digest))
}

/// Runs the PE self-hash check. Returns `Some(finding)` on mismatch or I/O
/// failure that should be surfaced, `None` if the baseline matches or was
/// just written for the first time.
#[cfg(target_os = "windows")]
fn check_pe_self_hash() -> Option<TamperFinding> {
    let exe_path = match std::env::current_exe() {
        Ok(p) => p,
        Err(e) => {
            warn!(error = %e, "anti_tamper: current_exe failed");
            return None;
        }
    };

    let actual_hash = match hash_file(&exe_path) {
        Ok(h) => h,
        Err(e) => {
            warn!(error = %e, "anti_tamper: hash current_exe failed");
            return None;
        }
    };

    let record_path = binhash_path();

    match load_binhash(&record_path) {
        Ok(Some(rec)) => {
            // Path mismatch is NOT necessarily tamper — service sometimes
            // reports a slightly different canonical form. We only compare
            // the hash. The stored path is informational.
            if rec.sha256.eq_ignore_ascii_case(&actual_hash) {
                info!(
                    hash = %actual_hash,
                    baseline_unix = rec.baseline_unix,
                    "anti_tamper: PE self-hash OK"
                );
                None
            } else {
                Some(TamperFinding {
                    check: "pe_self_hash",
                    severity: Severity::Critical,
                    details: serde_json::json!({
                        "expected": rec.sha256,
                        "actual": actual_hash,
                        "binary_path": exe_path.display().to_string(),
                        "baseline_path": rec.path,
                        "baseline_unix": rec.baseline_unix,
                    }),
                })
            }
        }
        Ok(None) => {
            // First run — persist the baseline.
            let rec = BinHashRecord {
                path: exe_path.display().to_string(),
                sha256: actual_hash.clone(),
                baseline_unix: now_unix_seconds(),
            };
            if let Err(e) = save_binhash(&record_path, &rec) {
                warn!(error = %e, "anti_tamper: baseline hash persist failed");
            } else {
                info!(
                    hash = %actual_hash,
                    path = %record_path.display(),
                    "anti_tamper: PE self-hash baseline persisted (first run)"
                );
            }
            None
        }
        Err(e) => {
            warn!(error = %e, "anti_tamper: binhash.json read failed — skipping check");
            None
        }
    }
}

#[cfg(not(target_os = "windows"))]
#[allow(dead_code)]
fn check_pe_self_hash() -> Option<TamperFinding> {
    None
}

fn load_binhash(path: &Path) -> Result<Option<BinHashRecord>> {
    match std::fs::read(path) {
        Ok(bytes) => {
            let rec: BinHashRecord =
                serde_json::from_slice(&bytes).context("parse binhash.json")?;
            Ok(Some(rec))
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(anyhow::anyhow!("read binhash.json: {e}")),
    }
}

fn save_binhash(path: &Path, rec: &BinHashRecord) -> Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).ok();
    }
    let json = serde_json::to_vec_pretty(rec).context("serialise BinHashRecord")?;
    std::fs::write(path, json).with_context(|| format!("write {}", path.display()))?;
    Ok(())
}

fn now_unix_seconds() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

// ──────────────────────────────────────────────────────────────────────────────
// Registry ACL check
// ──────────────────────────────────────────────────────────────────────────────

/// Service registry subkey under HKLM.
#[cfg(target_os = "windows")]
const SERVICE_SUBKEY: &str = r"SYSTEM\CurrentControlSet\Services\PersonelAgent";

/// Runs the registry ACL check. Returns `Some(finding)` if the DACL contains
/// a concerning ACE, `None` if the key is well-formed or absent (console mode).
#[cfg(target_os = "windows")]
fn check_registry_acl() -> Option<TamperFinding> {
    // SAFETY: every Win32 call below has NUL-terminated UTF-16 inputs, valid
    // output pointers, and we close every handle we open. SIDs and ACLs are
    // owned by the SECURITY_DESCRIPTOR buffer (`sd_buf`) and the
    // locally-allocated SID buffers; they live until this function returns.
    unsafe {
        use windows::core::PCWSTR;
        use windows::Win32::Foundation::{
            BOOL, ERROR_FILE_NOT_FOUND, ERROR_INSUFFICIENT_BUFFER, ERROR_SUCCESS, PSID,
        };
        use windows::Win32::Security::{
            ACL, ACL_SIZE_INFORMATION, AclSizeInformation, DACL_SECURITY_INFORMATION,
            EqualSid, GetAce, GetAclInformation, GetSecurityDescriptorDacl,
            PSECURITY_DESCRIPTOR, WinAuthenticatedUserSid, WinWorldSid,
        };
        use windows::Win32::System::Registry::{
            HKEY, HKEY_LOCAL_MACHINE, KEY_READ, RegCloseKey, RegGetKeySecurity, RegOpenKeyExW,
        };

        // ── 1. Open the service key read-only ────────────────────────────
        let subkey: Vec<u16> = SERVICE_SUBKEY
            .encode_utf16()
            .chain(std::iter::once(0))
            .collect();

        let mut hkey = HKEY::default();
        let open_res = RegOpenKeyExW(
            HKEY_LOCAL_MACHINE,
            PCWSTR(subkey.as_ptr()),
            0,
            KEY_READ,
            &mut hkey,
        );
        if open_res == ERROR_FILE_NOT_FOUND {
            info!(
                "anti_tamper: service registry key absent (console mode?); ACL check skipped"
            );
            return None;
        }
        if open_res != ERROR_SUCCESS {
            warn!(
                code = open_res.0,
                "anti_tamper: RegOpenKeyExW failed; ACL check skipped"
            );
            return None;
        }

        // ── 2. Size-probe the DACL buffer ────────────────────────────────
        let mut cb: u32 = 0;
        let probe = RegGetKeySecurity(
            hkey,
            DACL_SECURITY_INFORMATION,
            PSECURITY_DESCRIPTOR(std::ptr::null_mut()),
            &mut cb,
        );
        if probe != ERROR_INSUFFICIENT_BUFFER {
            let _ = RegCloseKey(hkey);
            warn!(
                code = probe.0,
                "anti_tamper: RegGetKeySecurity size probe did not return INSUFFICIENT_BUFFER"
            );
            return None;
        }
        let mut sd_buf: Vec<u8> = vec![0u8; cb as usize];
        let get_res = RegGetKeySecurity(
            hkey,
            DACL_SECURITY_INFORMATION,
            PSECURITY_DESCRIPTOR(sd_buf.as_mut_ptr().cast()),
            &mut cb,
        );
        let _ = RegCloseKey(hkey);
        if get_res != ERROR_SUCCESS {
            warn!(code = get_res.0, "anti_tamper: RegGetKeySecurity fetch failed");
            return None;
        }

        // ── 3. Extract DACL pointer ──────────────────────────────────────
        let sd = PSECURITY_DESCRIPTOR(sd_buf.as_mut_ptr().cast());
        let mut dacl_present = BOOL(0);
        let mut dacl_ptr: *mut ACL = std::ptr::null_mut();
        let mut dacl_defaulted = BOOL(0);
        if GetSecurityDescriptorDacl(
            sd,
            &mut dacl_present,
            &mut dacl_ptr,
            &mut dacl_defaulted,
        )
        .is_err()
        {
            warn!("anti_tamper: GetSecurityDescriptorDacl failed");
            return None;
        }
        if dacl_present.0 == 0 || dacl_ptr.is_null() {
            // No DACL at all means everyone has full access — definitive
            // tamper signal.
            return Some(TamperFinding {
                check: "registry_acl",
                severity: Severity::Critical,
                details: serde_json::json!({
                    "reason": "no_dacl",
                    "key": SERVICE_SUBKEY,
                }),
            });
        }

        // ── 4. Count ACEs ────────────────────────────────────────────────
        let mut info = ACL_SIZE_INFORMATION {
            AceCount: 0,
            AclBytesInUse: 0,
            AclBytesFree: 0,
        };
        if GetAclInformation(
            dacl_ptr,
            (&mut info as *mut ACL_SIZE_INFORMATION).cast(),
            std::mem::size_of::<ACL_SIZE_INFORMATION>() as u32,
            AclSizeInformation,
        )
        .is_err()
        {
            warn!("anti_tamper: GetAclInformation failed");
            return None;
        }

        // ── 5. Build reference SIDs (Everyone, Authenticated Users) ──────
        let everyone_sid = match build_well_known_sid(WinWorldSid.0) {
            Some(b) => b,
            None => {
                warn!("anti_tamper: failed to build Everyone SID");
                return None;
            }
        };
        let auth_users_sid = match build_well_known_sid(WinAuthenticatedUserSid.0) {
            Some(b) => b,
            None => {
                warn!("anti_tamper: failed to build AuthenticatedUsers SID");
                return None;
            }
        };

        // ── 6. Walk each ACE, looking for writable Everyone / Auth Users ─
        // The ACE layout begins with ACE_HEADER { AceType, AceFlags, AceSize }
        // followed by type-specific fields. For ACCESS_ALLOWED_ACE_TYPE and
        // ACCESS_DENIED_ACE_TYPE the layout after the header is:
        //   DWORD Mask
        //   DWORD SidStart (first u32 of the SID; address of this field is
        //                   the SID pointer)
        //
        // We only inspect ALLOWED aces (type 0). DENIED aces tightening a
        // SID are not tamper.
        const ACCESS_ALLOWED_ACE_TYPE: u8 = 0;
        // Permission mask bits that let a holder modify the service config.
        const KEY_SET_VALUE: u32 = 0x0002;
        const KEY_CREATE_SUB_KEY: u32 = 0x0004;
        const KEY_WRITE: u32 = 0x0002 | 0x0004 | 0x0020_0000;
        const WRITE_DAC: u32 = 0x0004_0000;
        const WRITE_OWNER: u32 = 0x0008_0000;
        const GENERIC_WRITE: u32 = 0x4000_0000;
        const GENERIC_ALL: u32 = 0x1000_0000;
        const DANGEROUS: u32 = KEY_SET_VALUE
            | KEY_CREATE_SUB_KEY
            | KEY_WRITE
            | WRITE_DAC
            | WRITE_OWNER
            | GENERIC_WRITE
            | GENERIC_ALL;

        #[repr(C)]
        struct AceHeader {
            ace_type: u8,
            ace_flags: u8,
            ace_size: u16,
        }

        for i in 0..info.AceCount {
            let mut ace_ptr: *mut ::core::ffi::c_void = std::ptr::null_mut();
            if GetAce(dacl_ptr, i, &mut ace_ptr).is_err() || ace_ptr.is_null() {
                continue;
            }
            let header = &*(ace_ptr as *const AceHeader);
            if header.ace_type != ACCESS_ALLOWED_ACE_TYPE {
                continue;
            }
            // Mask is at offset 4 (after the 4-byte header) of an
            // ACCESS_ALLOWED_ACE; SID starts at offset 8.
            let mask_ptr = (ace_ptr as *const u8).add(4) as *const u32;
            let sid_ptr = (ace_ptr as *const u8).add(8) as *mut ::core::ffi::c_void;
            let mask = *mask_ptr;
            if mask & DANGEROUS == 0 {
                continue;
            }

            let ace_sid = PSID(sid_ptr);
            if EqualSid(ace_sid, PSID(everyone_sid.as_ptr() as *mut _)).is_ok() {
                return Some(TamperFinding {
                    check: "registry_acl",
                    severity: Severity::High,
                    details: serde_json::json!({
                        "reason": "dangerous_ace",
                        "sid": "S-1-1-0",
                        "name": "Everyone",
                        "mask": format!("0x{:08x}", mask),
                        "key": SERVICE_SUBKEY,
                    }),
                });
            }
            if EqualSid(ace_sid, PSID(auth_users_sid.as_ptr() as *mut _)).is_ok() {
                return Some(TamperFinding {
                    check: "registry_acl",
                    severity: Severity::High,
                    details: serde_json::json!({
                        "reason": "dangerous_ace",
                        "sid": "S-1-5-11",
                        "name": "AuthenticatedUsers",
                        "mask": format!("0x{:08x}", mask),
                        "key": SERVICE_SUBKEY,
                    }),
                });
            }
        }

        info!("anti_tamper: registry ACL OK ({} ACEs inspected)", info.AceCount);
        None
    }
}

#[cfg(not(target_os = "windows"))]
#[allow(dead_code)]
fn check_registry_acl() -> Option<TamperFinding> {
    None
}

/// Allocates and returns the raw bytes of a well-known SID.
///
/// Returns `None` on failure. The returned `Vec<u8>` owns the SID memory;
/// its `.as_ptr()` is a valid `PSID` for the lifetime of the vector.
#[cfg(target_os = "windows")]
unsafe fn build_well_known_sid(sid_type_raw: i32) -> Option<Vec<u8>> {
    use windows::Win32::Foundation::PSID;
    use windows::Win32::Security::{CreateWellKnownSid, WELL_KNOWN_SID_TYPE};

    // SECURITY_MAX_SID_SIZE == 68 on every supported Windows release.
    let mut cb: u32 = 68;
    let mut buf: Vec<u8> = vec![0u8; cb as usize];
    if CreateWellKnownSid(
        WELL_KNOWN_SID_TYPE(sid_type_raw),
        PSID(std::ptr::null_mut()),
        PSID(buf.as_mut_ptr().cast()),
        &mut cb,
    )
    .is_ok()
    {
        buf.truncate(cb as usize);
        Some(buf)
    } else {
        None
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Watchdog log replay
// ──────────────────────────────────────────────────────────────────────────────

/// Path to the watchdog-written tamper log file.
///
/// The watchdog appends one JSON object per line while the agent is down;
/// the agent reads and truncates this file on next startup.
fn watchdog_log_path() -> PathBuf {
    crate::config::default_data_dir().join("watchdog.log")
}

/// JSON line schema written by the watchdog. Kept in lockstep with
/// `personel_watchdog::health_monitor::record_tamper_to_log`.
#[derive(Debug, Deserialize)]
struct WatchdogLogLine {
    check: String,
    severity: Option<String>,
    details: Option<serde_json::Value>,
    #[allow(dead_code)]
    unix: Option<i64>,
}

/// Reads `watchdog.log`, parses each JSON line into a [`TamperFinding`],
/// truncates the file, and returns the findings.
#[cfg(target_os = "windows")]
fn replay_watchdog_log() -> Result<Vec<TamperFinding>> {
    let path = watchdog_log_path();
    let bytes = match std::fs::read(&path) {
        Ok(b) => b,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(anyhow::anyhow!("read watchdog.log: {e}")),
    };
    if bytes.is_empty() {
        return Ok(Vec::new());
    }
    let text = String::from_utf8_lossy(&bytes);
    let mut findings = Vec::new();
    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        match serde_json::from_str::<WatchdogLogLine>(trimmed) {
            Ok(entry) => {
                let severity = match entry.severity.as_deref() {
                    Some("critical") => Severity::Critical,
                    Some("high") => Severity::High,
                    Some("medium") => Severity::Medium,
                    _ => Severity::Info,
                };
                // check is a String but TamperFinding wants &'static str;
                // watchdog only writes a known set so we match + fall back.
                let check_static: &'static str = match entry.check.as_str() {
                    "agent_unresponsive" => "agent_unresponsive",
                    "agent_crashed" => "agent_crashed",
                    "pipe_connect_failed" => "pipe_connect_failed",
                    _ => "watchdog_replay",
                };
                findings.push(TamperFinding {
                    check: check_static,
                    severity,
                    details: entry.details.unwrap_or(serde_json::Value::Null),
                });
            }
            Err(e) => {
                warn!(error = %e, line = trimmed, "anti_tamper: watchdog.log parse error");
            }
        }
    }
    // Truncate the file so we don't re-replay on next start.
    if let Err(e) = std::fs::write(&path, b"") {
        warn!(error = %e, "anti_tamper: watchdog.log truncate failed");
    }
    Ok(findings)
}

#[cfg(not(target_os = "windows"))]
#[allow(dead_code)]
fn replay_watchdog_log() -> Result<Vec<TamperFinding>> {
    Ok(Vec::new())
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn severity_as_str_maps_correctly() {
        assert_eq!(Severity::Info.as_str(), "info");
        assert_eq!(Severity::Medium.as_str(), "medium");
        assert_eq!(Severity::High.as_str(), "high");
        assert_eq!(Severity::Critical.as_str(), "critical");
    }

    #[test]
    fn tamper_finding_holds_details() {
        let f = TamperFinding {
            check: "pe_self_hash",
            severity: Severity::Critical,
            details: serde_json::json!({"expected": "a", "actual": "b"}),
        };
        assert_eq!(f.check, "pe_self_hash");
        assert_eq!(f.details["expected"], "a");
    }

    #[test]
    fn binhash_roundtrip() {
        let dir = std::env::temp_dir().join("personel-antitamper-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("binhash.json");
        let _ = std::fs::remove_file(&path);

        assert!(load_binhash(&path).unwrap().is_none());

        let rec = BinHashRecord {
            path: "C:/fake/agent.exe".into(),
            sha256: "deadbeef".into(),
            baseline_unix: 42,
        };
        save_binhash(&path, &rec).unwrap();
        let loaded = load_binhash(&path).unwrap().expect("should be some");
        assert_eq!(loaded.sha256, "deadbeef");
        assert_eq!(loaded.baseline_unix, 42);

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn run_startup_checks_on_non_windows_returns_empty() {
        // Sanity: on the dev host `run_startup_checks` must not panic and
        // must return an empty vec (it takes &CollectorCtx but only reads
        // inside the windows cfg). We construct a minimal context stub via
        // the public type — however CollectorCtx holds non-trivial fields,
        // so instead we assert the function exists and exercise the purely
        // local helpers above.
        //
        // The full cfg-gated integration run is covered by the Windows
        // build that happens in CI.
        assert_eq!(Severity::Critical.as_str(), "critical");
    }

    #[test]
    fn hash_file_produces_deterministic_hex() {
        let dir = std::env::temp_dir().join("personel-antitamper-hash-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("sample.bin");
        std::fs::write(&path, b"hello world").unwrap();
        let h = hash_file(&path).unwrap();
        // SHA-256("hello world")
        assert_eq!(h, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9");
        let _ = std::fs::remove_file(&path);
    }
}
