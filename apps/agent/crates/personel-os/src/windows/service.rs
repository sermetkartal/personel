//! Windows service lifecycle trampoline.
//!
//! Wraps the `windows-service` crate to provide the SCM handshake and
//! service control handler registration.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Implement `run_as_service()` calling `windows_service::service_dispatcher::start`.
//! - Handle `ServiceControl::Stop` → send shutdown signal to tokio runtime.
//! - Handle `ServiceControl::Interrogate` → return current status.
//! - Emit service status updates at each lifecycle transition.

use personel_core::error::{AgentError, Result};
use tokio::sync::oneshot;

/// Runs the process as a Windows service.
///
/// This function blocks until the service is stopped. On non-service invocations
/// (e.g., console debug run), call [`run_standalone`] instead.
///
/// # Errors
///
/// Returns an error if the SCM dispatcher cannot be started.
pub fn run_as_service(shutdown_tx: oneshot::Sender<()>) -> Result<()> {
    // TODO: implement with windows-service crate:
    //
    // windows_service::service_dispatcher::start("personel-agent", ffi_service_main)?;
    //
    // The ffi_service_main trampoline registers the service control handler
    // and then calls back into Rust with a stop channel.
    let _ = shutdown_tx;
    Err(AgentError::Internal("Windows service dispatcher not yet implemented".into()))
}

/// Returns `true` if the current process was launched by the Windows SCM.
///
/// This is a heuristic: if stderr is not a console device, assume service mode.
#[must_use]
pub fn is_service_context() -> bool {
    // A more robust check uses `GetStdHandle(STD_ERROR_HANDLE)` and checks
    // the handle type. For now we use the `--service` CLI argument flag.
    false // TODO: check CLI args or handle type
}
