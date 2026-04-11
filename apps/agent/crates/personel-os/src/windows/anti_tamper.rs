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

use personel_core::error::Result;

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
    // SAFETY: -1 is the pseudo-handle for the current process; always valid.
    let current_process = HANDLE(-1isize as *mut _);
    unsafe {
        CheckRemoteDebuggerPresent(current_process, &mut is_debugger_present)
            .ok()
            .map_err(|e| personel_core::error::AgentError::TamperDetected {
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

/// Runs all non-blocking anti-tamper checks and returns their results.
///
/// This function must complete quickly (<1 ms); it is called on the 30-second
/// health tick.
pub fn run_all_checks() -> Vec<TamperCheckResult> {
    let mut results = vec![check_debugger_present()];
    if let Ok(r) = check_remote_debugger() {
        results.push(r);
    }
    // TODO: add timing-based anti-step check (QPC delta over NOP sled).
    // TODO: add registry ACL check.
    // TODO: add PE self-hash check.
    results
}
