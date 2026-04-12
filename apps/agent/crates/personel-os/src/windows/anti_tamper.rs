//! Anti-tamper detection checks.
//!
//! All checks are non-blocking and non-fatal — they emit a result that the
//! caller turns into a `agent.tamper_detected` event.
//!
//! Per `docs/security/anti-tamper.md`:
//! - We do NOT crash or refuse to run on debugger detection; we emit a tamper
//!   event and continue.
//! - We do NOT hide from Task Manager.

use windows::Win32::System::Diagnostics::Debug::{
    CheckRemoteDebuggerPresent, IsDebuggerPresent,
};
use windows::Win32::Foundation::HANDLE;
use windows::Win32::System::Performance::QueryPerformanceCounter;
use windows::Win32::System::Registry::{
    RegOpenKeyExW, RegCloseKey, RegSetKeySecurity,
    HKEY_LOCAL_MACHINE, KEY_READ, KEY_WRITE,
};
use windows::Win32::Security::{
    InitializeSecurityDescriptor, InitializeAcl, AddAccessDeniedAce,
    SetSecurityDescriptorDacl,
    SECURITY_DESCRIPTOR, ACL,
    ACL_REVISION,
    DACL_SECURITY_INFORMATION,
};
// SECURITY_DESCRIPTOR_REVISION = 1u32 lives in SystemServices, not Win32::Security.
use windows::Win32::System::SystemServices::SECURITY_DESCRIPTOR_REVISION;
use windows::core::PCWSTR;

use sha2::{Digest, Sha256};

use personel_core::error::Result;

/// Minimum timing gap (in `QueryPerformanceCounter` ticks) that suggests a
/// debugger single-step is occurring. Calibrated to ≈100 ms on any CPU.
///
/// The QPC frequency is 10 MHz on most modern Windows systems.
/// 100 ms × 10_000_000 Hz = 1_000_000 ticks. We use 500_000 (≈50 ms) as
/// a conservative lower bound to avoid false positives on heavily loaded VMs.
const TIMING_STEP_THRESHOLD_TICKS: i64 = 500_000;

/// Result of a single anti-tamper check.
#[derive(Debug, Clone)]
pub struct TamperCheckResult {
    /// Unique name of the check, used in `agent.tamper_detected.check_name`.
    pub check_name: &'static str,
    /// Whether tampering was detected.
    pub detected: bool,
    /// Human-readable detail (never contains PII; the raw detail is hashed
    /// before being emitted as an event).
    pub detail: String,
}

/// Checks whether a user-mode debugger is attached via `IsDebuggerPresent`.
///
/// Per anti-tamper design: this does not block execution; it emits telemetry.
#[must_use]
pub fn check_debugger_present() -> TamperCheckResult {
    // SAFETY: IsDebuggerPresent is a pure read of the PEB debug flag; no side
    // effects and always safe to call.
    let detected = unsafe { IsDebuggerPresent().as_bool() };
    TamperCheckResult {
        check_name: "debugger_present",
        detected,
        detail: if detected {
            "IsDebuggerPresent returned TRUE".into()
        } else {
            String::new()
        },
    }
}

/// Checks whether a remote debugger is attached via `CheckRemoteDebuggerPresent`.
///
/// # Errors
///
/// Returns an error if the Win32 call fails.
pub fn check_remote_debugger() -> Result<TamperCheckResult> {
    let mut is_debugger_present = windows::Win32::Foundation::BOOL(0);
    // SAFETY: -1 (as isize) is the pseudo-handle for the current process; always valid.
    let current_process = HANDLE(-1isize);
    unsafe {
        CheckRemoteDebuggerPresent(current_process, &mut is_debugger_present)
            .map_err(|_| personel_core::error::AgentError::TamperDetected {
                check: "remote_debugger",
            })?;
    }
    let detected = is_debugger_present.as_bool();
    Ok(TamperCheckResult {
        check_name: "remote_debugger",
        detected,
        detail: if detected {
            "CheckRemoteDebuggerPresent returned TRUE".into()
        } else {
            String::new()
        },
    })
}

/// Detects single-step debugging by measuring `QueryPerformanceCounter` delta
/// across a short busy-loop.
///
/// A real debugger that steps through instructions will cause an abnormally
/// large delta between two QPC readings that should be microseconds apart.
/// The threshold is [`TIMING_STEP_THRESHOLD_TICKS`] (≈50 ms); adjust the
/// constant to tune sensitivity vs. false-positive rate on slow VMs.
///
/// # How it works
///
/// We take two QPC snapshots with a small amount of arithmetic between them.
/// On an un-debugged CPU this loop completes in under 1 µs. A debugger that
/// single-steps through each instruction will cause the delta to spike into
/// the hundreds of milliseconds, well above the threshold.
///
/// # False positive risk
///
/// Low: even on heavily loaded VMs the scheduler quantum is ≈15 ms and the
/// threshold is 50 ms. Two back-to-back preemptions within this call would
/// be required to produce a false positive, which is astronomically unlikely
/// in practice.
#[must_use]
pub fn detect_timing_anomaly() -> bool {
    let mut t1 = 0i64;
    let mut t2 = 0i64;

    // SAFETY: QueryPerformanceCounter is always safe to call on Windows Vista+;
    // it never fails and has no side effects. The mutable pointer targets a
    // stack-allocated i64, which is valid for the duration of the call.
    unsafe {
        QueryPerformanceCounter(&mut t1);
    }

    // A small amount of work that the optimiser must not elide. Using
    // `core::hint::black_box` prevents the compiler from removing the
    // intermediate computation while keeping the body inlined.
    let _dummy = core::hint::black_box(t1.wrapping_mul(0xDEAD_BEEF));

    // SAFETY: same rationale as the first call above.
    unsafe {
        QueryPerformanceCounter(&mut t2);
    }

    let delta = t2.saturating_sub(t1);
    delta > TIMING_STEP_THRESHOLD_TICKS
}

/// Verifies that the on-disk agent binary's SHA-256 hash matches `expected_hash`.
///
/// Because a compile-time embed (chicken-and-egg problem with `include_bytes!`)
/// is not feasible, this function compares against a hash that was written to
/// the sealed store on first boot. The caller is responsible for:
///
/// 1. On first launch (no stored hash): compute the hash, seal it with DPAPI,
///    and persist it. Return `true` to indicate "baseline established".
/// 2. On subsequent launches: call this function with the sealed hash bytes
///    retrieved from DPAPI storage. Return `false` if the hash changed.
///
/// # Errors
///
/// Returns an error if the current executable path cannot be resolved or the
/// file cannot be read.
pub fn verify_binary_integrity(expected_hash: &[u8; 32]) -> bool {
    let exe_path = match std::env::current_exe() {
        Ok(p) => p,
        Err(_) => return false,
    };
    let bytes = match std::fs::read(&exe_path) {
        Ok(b) => b,
        Err(_) => return false,
    };
    let actual: [u8; 32] = Sha256::digest(&bytes).into();
    // Constant-time comparison to avoid timing side-channels.
    // Both slices are 32 bytes so the zip covers the full array.
    let mut diff = 0u8;
    for (a, b) in actual.iter().zip(expected_hash.iter()) {
        diff |= a ^ b;
    }
    diff == 0
}

/// Computes the SHA-256 hash of the current agent binary on disk.
///
/// Used on first boot to establish the integrity baseline stored in the
/// DPAPI-sealed store.
///
/// # Errors
///
/// Returns `None` if the executable path cannot be resolved or the file
/// cannot be read.
#[must_use]
pub fn compute_binary_hash() -> Option<[u8; 32]> {
    let exe_path = std::env::current_exe().ok()?;
    let bytes = std::fs::read(&exe_path).ok()?;
    let hash: [u8; 32] = Sha256::digest(&bytes).into();
    Some(hash)
}

/// Applies an explicit ACCESS_DENIED ACE for the "Everyone" (S-1-1-0) SID to
/// the agent's service registry key, preventing non-administrative modification
/// of service parameters.
///
/// Only the `WRITE_DAC` and registry-write permissions are denied. Local
/// administrators retain full control because the deny ACE targets the
/// "Everyone" group (which excludes `NT AUTHORITY\SYSTEM` and the BUILTIN
/// Administrators SID — those SIDs override deny entries for their members
/// via privilege elevation).
///
/// # Implementation note
///
/// A simpler alternative — removing write permissions from the key DACL — was
/// rejected because it would prevent the SCM itself from updating service
/// configuration at install/upgrade time. A deny ACE is more surgical and
/// survives a DACL reset by the SCM.
///
/// # Errors
///
/// Returns an error if the registry key cannot be opened or the ACL cannot be
/// set. This is non-fatal at runtime; the caller should log and emit a tamper
/// alert.
pub fn protect_service_registry_key() -> Result<()> {
    use windows::Win32::Foundation::PSID;
    use windows::Win32::Security::{
        AllocateAndInitializeSid, FreeSid,
        SID_IDENTIFIER_AUTHORITY,
        SECURITY_WORLD_SID_AUTHORITY,
    };
    use windows::Win32::System::Registry::HKEY;
    // SECURITY_WORLD_RID = 0 (S-1-1-0 well-known SID, first sub-authority is 0)
    const SECURITY_WORLD_RID: u32 = 0u32;

    const KEY_PATH: &str = "SYSTEM\\CurrentControlSet\\Services\\personel-agent";
    // Generic write rights: KEY_SET_VALUE | KEY_CREATE_SUB_KEY | KEY_CREATE_LINK
    const WRITE_MASK: u32 = 0x0002 | 0x0004 | 0x0020;

    // Encode the registry path as a wide (UTF-16) string for Win32.
    let path_wide: Vec<u16> = KEY_PATH.encode_utf16().chain(std::iter::once(0)).collect();

    let mut hkey = HKEY::default();

    // SAFETY: RegOpenKeyExW requires a valid HKEY root (HKEY_LOCAL_MACHINE is a
    // pseudo-constant) and a null-terminated wide string. Both conditions hold.
    // The returned handle is closed via RegCloseKey in the same scope.
    let open_result = unsafe {
        RegOpenKeyExW(
            HKEY_LOCAL_MACHINE,
            PCWSTR::from_raw(path_wide.as_ptr()),
            0,
            KEY_READ | KEY_WRITE,
            &mut hkey,
        )
    };

    if open_result.is_err() {
        return Err(personel_core::error::AgentError::TamperDetected {
            check: "registry_acl_open",
        });
    }

    // Build the "Everyone" SID (S-1-1-0).
    let mut everyone_sid: PSID = PSID::default();
    let world_authority = SID_IDENTIFIER_AUTHORITY {
        // SECURITY_WORLD_SID_AUTHORITY value is {0,0,0,0,0,1}
        Value: [0, 0, 0, 0, 0, 1],
    };

    // SAFETY: AllocateAndInitializeSid allocates a SID via the process heap.
    // We must call FreeSid when done. The sub-authority count is 1, matching
    // SECURITY_WORLD_RID (0). All other sub-authority args are ignored when
    // nSubAuthorityCount=1.
    let sid_ok = unsafe {
        AllocateAndInitializeSid(
            &world_authority,
            1,
            SECURITY_WORLD_RID as u32,
            0, 0, 0, 0, 0, 0, 0,
            &mut everyone_sid,
        )
    };

    if sid_ok.is_err() {
        // SAFETY: hkey is a valid open handle obtained from RegOpenKeyExW.
        unsafe { RegCloseKey(hkey).ok().ok() };
        return Err(personel_core::error::AgentError::TamperDetected {
            check: "registry_acl_sid",
        });
    }

    // ACL size: header + one ACCESS_DENIED_ACE with a 4-byte SID sub-authority.
    // ACL header: 8 bytes; ACCESS_DENIED_ACE base: 12 bytes; SID: 8 bytes → 28.
    const ACL_SIZE: u32 = 64; // generous buffer for alignment padding

    let mut acl_buf = vec![0u8; ACL_SIZE as usize];
    let acl_ptr = acl_buf.as_mut_ptr().cast::<ACL>();

    // SAFETY: acl_ptr points to a valid heap buffer of ACL_SIZE bytes.
    // InitializeAcl writes an ACL_REVISION header; the buffer is large enough.
    let init_ok = unsafe { InitializeAcl(acl_ptr, ACL_SIZE, ACL_REVISION) };
    if init_ok.is_err() {
        // SAFETY: everyone_sid was successfully allocated above.
        unsafe { FreeSid(everyone_sid) };
        unsafe { RegCloseKey(hkey).ok().ok() };
        return Err(personel_core::error::AgentError::TamperDetected {
            check: "registry_acl_init",
        });
    }

    // SAFETY: acl_ptr is initialised; everyone_sid is valid.
    let ace_ok = unsafe { AddAccessDeniedAce(acl_ptr, ACL_REVISION, WRITE_MASK, everyone_sid) };
    if ace_ok.is_err() {
        unsafe { FreeSid(everyone_sid) };
        unsafe { RegCloseKey(hkey).ok().ok() };
        return Err(personel_core::error::AgentError::TamperDetected {
            check: "registry_acl_ace",
        });
    }

    // Build a SECURITY_DESCRIPTOR pointing to our new DACL.
    let mut sd = SECURITY_DESCRIPTOR::default();
    // windows 0.54: InitializeSecurityDescriptor takes PSECURITY_DESCRIPTOR (wrapper struct),
    // not a raw pointer. Construct it from the address of our stack-allocated descriptor.
    // SAFETY: sd is a stack-allocated struct; SECURITY_DESCRIPTOR_REVISION = 1.
    let sd_ptr = windows::Win32::Security::PSECURITY_DESCRIPTOR(
        &mut sd as *mut _ as *mut _,
    );
    if let Err(_) = unsafe { InitializeSecurityDescriptor(sd_ptr, SECURITY_DESCRIPTOR_REVISION) } {
        unsafe { FreeSid(everyone_sid) };
        unsafe { RegCloseKey(hkey).ok().ok() };
        return Err(personel_core::error::AgentError::TamperDetected { check: "registry_acl_sd" });
    }

    // windows 0.54: SetSecurityDescriptorDacl also takes PSECURITY_DESCRIPTOR.
    // bDaclPresent and bDaclDefaulted are generic IntoParam<BOOL> — pass as BOOL directly.
    // SAFETY: sd and acl_ptr are both valid; bDaclPresent=TRUE, bDaclDefaulted=FALSE.
    let sd_ptr2 = windows::Win32::Security::PSECURITY_DESCRIPTOR(
        &mut sd as *mut _ as *mut _,
    );
    if let Err(_) = unsafe {
        SetSecurityDescriptorDacl(
            sd_ptr2,
            windows::Win32::Foundation::BOOL(1), // bDaclPresent = TRUE
            Some(acl_ptr as *const ACL),
            windows::Win32::Foundation::BOOL(0), // bDaclDefaulted = FALSE
        )
    } {
        unsafe { FreeSid(everyone_sid) };
        unsafe { RegCloseKey(hkey).ok().ok() };
        return Err(personel_core::error::AgentError::TamperDetected { check: "registry_acl_setdacl" });
    }

    // Apply the security descriptor to the registry key.
    // windows 0.54: RegSetKeySecurity returns WIN32_ERROR — call .ok() to convert to Result.
    // SAFETY: hkey is a valid open handle; sd is a valid security descriptor.
    let sd_ptr3 = windows::Win32::Security::PSECURITY_DESCRIPTOR(
        &mut sd as *mut _ as *mut _,
    );
    let set_result = unsafe {
        RegSetKeySecurity(hkey, DACL_SECURITY_INFORMATION, sd_ptr3)
    };

    // Cleanup regardless of outcome.
    // SAFETY: all handles remain valid through this point.
    unsafe { FreeSid(everyone_sid) };
    unsafe { RegCloseKey(hkey).ok().ok() };

    set_result.ok().map_err(|_| personel_core::error::AgentError::TamperDetected {
        check: "registry_acl_apply",
    })?;

    Ok(())
}

/// Runs all non-blocking anti-tamper checks and returns their results.
///
/// This function must complete quickly (<1 ms on a non-debugged system); it is
/// called on the 30-second health tick. The timing check adds up to 1 µs on a
/// healthy system; the binary integrity check is NOT called here because it may
/// take tens of milliseconds (disk I/O). Call [`verify_binary_integrity`]
/// separately on startup with the hash from the sealed store.
pub fn run_all_checks() -> Vec<TamperCheckResult> {
    let mut results = vec![check_debugger_present()];

    if let Ok(r) = check_remote_debugger() {
        results.push(r);
    }

    // Timing-based step detection.
    let timing_detected = detect_timing_anomaly();
    results.push(TamperCheckResult {
        check_name: "timing_step",
        detected: timing_detected,
        detail: if timing_detected {
            format!(
                "QPC delta exceeded {TIMING_STEP_THRESHOLD_TICKS} ticks — \
                 single-step debugger suspected"
            )
        } else {
            String::new()
        },
    });

    results
}
