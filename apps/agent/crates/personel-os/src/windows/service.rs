//! Windows Service Control Manager (SCM) lifecycle trampoline.
//!
//! Uses the `windows-service` crate to register with the SCM, handle service
//! control events (Stop, Shutdown, Interrogate), and report status transitions.
//!
//! # Service control events handled
//!
//! | SCM event     | Action                                         |
//! |---------------|------------------------------------------------|
//! | `Stop`        | Sends shutdown signal; transitions to Stopped  |
//! | `Shutdown`    | Same as Stop (system shutdown path)            |
//! | `Interrogate` | Re-emits current status — required by SCM     |
//!
//! # Install / Uninstall
//!
//! `install_service()` and `uninstall_service()` are thin wrappers for the
//! Windows SCM `CreateService` / `DeleteService` APIs. They are called by the
//! MSI custom action during setup and uninstall.

use std::ffi::OsString;
use std::sync::OnceLock;
use std::time::Duration;

use tokio::sync::oneshot;
use tracing::{error, info, warn};
use windows_service::{
    define_windows_service,
    service::{
        ServiceAccess, ServiceControl, ServiceControlAccept, ServiceErrorControl,
        ServiceExitCode, ServiceInfo, ServiceStartType, ServiceState, ServiceStatus,
        ServiceType,
    },
    service_control_handler::{self, ServiceControlHandlerResult},
    service_dispatcher,
    service_manager::{ServiceManager, ServiceManagerAccess},
};

use personel_core::error::{AgentError, Result};

// ── Service constants ─────────────────────────────────────────────────────────

/// SCM service name (must match the name used in `service_dispatcher::start`).
const SERVICE_NAME: &str = "personel-agent";
/// Human-readable display name shown in services.msc.
const SERVICE_DISPLAY_NAME: &str = "Personel Agent";
/// Service description shown in the SCM property sheet.
const SERVICE_DESCRIPTION: &str =
    "Personel UAM endpoint agent — activity monitoring and compliance.";

// ── Global shutdown channel ───────────────────────────────────────────────────
//
// The `define_windows_service!` macro generates a bare `extern "system"` FFI
// entry point that takes no arguments.  We need to get the shutdown sender into
// the service_main_wrapper without global mutable state.  The OnceLock lets us
// stash it exactly once before calling `service_dispatcher::start`.

static SHUTDOWN_TX: OnceLock<tokio::sync::Mutex<Option<oneshot::Sender<()>>>> =
    OnceLock::new();

// ── FFI trampoline ────────────────────────────────────────────────────────────

define_windows_service!(ffi_service_main, service_main_wrapper);

/// Called by the SCM on a new thread.  Sets up the control handler, reports
/// `Running`, and blocks until a stop signal is received.
fn service_main_wrapper(_arguments: Vec<OsString>) {
    if let Err(e) = run_service_main() {
        error!(error = %e, "service_main_wrapper: fatal error");
    }
}

fn run_service_main() -> std::result::Result<(), windows_service::Error> {
    // ── Register the service control handler ──────────────────────────────────
    //
    // The closure must be `Send + 'static`.  We share a clone of a channel so
    // that the Stop/Shutdown event can wake the tokio task below.
    let (stop_tx, stop_rx) = std::sync::mpsc::channel::<()>();
    let stop_tx_clone = stop_tx.clone();

    let status_handle =
        service_control_handler::register(SERVICE_NAME, move |event| match event {
            ServiceControl::Stop | ServiceControl::Shutdown => {
                info!("SCM → Stop/Shutdown received");
                let _ = stop_tx_clone.send(());
                ServiceControlHandlerResult::NoError
            }
            ServiceControl::Interrogate => ServiceControlHandlerResult::NoError,
            _ => ServiceControlHandlerResult::NotImplemented,
        })?;

    // ── StartPending ──────────────────────────────────────────────────────────
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::StartPending,
        controls_accepted: ServiceControlAccept::empty(),
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::from_secs(10),
        process_id: None,
    })?;

    // ── Running ───────────────────────────────────────────────────────────────
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::Running,
        controls_accepted: ServiceControlAccept::STOP | ServiceControlAccept::SHUTDOWN,
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::ZERO,
        process_id: None,
    })?;

    info!("service running; waiting for SCM stop event");

    // Signal the tokio runtime that the service is up (through the OnceLock).
    if let Some(lock) = SHUTDOWN_TX.get() {
        // We intentionally do NOT send here — the tokio tx lives until Stop.
        let _ = lock; // just assert it exists
    }

    // Block until Stop/Shutdown.
    let _ = stop_rx.recv();
    info!("stop event received; transitioning to StopPending");

    // Also forward the shutdown into the tokio world via the OnceLock sender.
    if let Some(lock) = SHUTDOWN_TX.get() {
        // Try a non-blocking lock; if it fails the runtime is already down.
        if let Ok(mut guard) = lock.try_lock() {
            if let Some(tx) = guard.take() {
                let _ = tx.send(());
            }
        }
    }

    // ── StopPending ───────────────────────────────────────────────────────────
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::StopPending,
        controls_accepted: ServiceControlAccept::empty(),
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::from_secs(30),
        process_id: None,
    })?;

    // ── Stopped ───────────────────────────────────────────────────────────────
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::Stopped,
        controls_accepted: ServiceControlAccept::empty(),
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::ZERO,
        process_id: None,
    })?;

    info!("service stopped cleanly");
    Ok(())
}

// ── Public API ────────────────────────────────────────────────────────────────

/// Runs the current process as a Windows service.
///
/// Registers `ffi_service_main` with the SCM and blocks until the SCM sends a
/// `Stop` or `Shutdown` control.  When that happens the provided `shutdown_tx`
/// is consumed to signal the tokio runtime.
///
/// # Errors
///
/// Returns [`AgentError::Internal`] if the SCM dispatcher cannot be started
/// (e.g. the process was not launched by the SCM).
pub fn run_as_service(shutdown_tx: oneshot::Sender<()>) -> Result<()> {
    // Stash the sender in the OnceLock so service_main_wrapper can reach it.
    SHUTDOWN_TX
        .set(tokio::sync::Mutex::new(Some(shutdown_tx)))
        .map_err(|_| AgentError::Internal("SHUTDOWN_TX already initialised".into()))?;

    service_dispatcher::start(SERVICE_NAME, ffi_service_main)
        .map_err(|e| AgentError::Internal(format!("SCM dispatcher error: {e}")))?;

    Ok(())
}

/// Returns `true` if the current process was launched by the Windows SCM.
///
/// Heuristic: if `GetStdHandle(STD_ERROR_HANDLE)` is NULL the process has no
/// console, which is characteristic of a service launch.  The `--console` flag
/// overrides this in dev mode.
#[must_use]
pub fn is_service_context() -> bool {
    use windows::Win32::System::Console::{GetStdHandle, STD_ERROR_HANDLE};
    // SAFETY: GetStdHandle is a simple query with no invariants to uphold.
    let handle = unsafe { GetStdHandle(STD_ERROR_HANDLE) };
    match handle {
        Ok(h) => h.is_invalid(),
        Err(_) => false,
    }
}

// ── Install / Uninstall helpers ───────────────────────────────────────────────

/// Installs the `personel-agent` Windows service via the SCM.
///
/// The service is configured to start automatically (`SERVICE_AUTO_START`) and
/// run in its own process under the LocalSystem account.  The binary path is
/// taken from the current executable.
///
/// Called by the MSI `InstallService` custom action.
///
/// # Errors
///
/// Returns [`AgentError::Internal`] if the SCM cannot be opened or the service
/// already exists and cannot be updated.
pub fn install_service() -> Result<()> {
    let manager = ServiceManager::local_computer(
        None::<&str>,
        ServiceManagerAccess::CONNECT | ServiceManagerAccess::CREATE_SERVICE,
    )
    .map_err(|e| AgentError::Internal(format!("OpenSCManager failed: {e}")))?;

    let binary_path = std::env::current_exe()
        .map_err(|e| AgentError::Internal(format!("current_exe: {e}")))?;

    let info = ServiceInfo {
        name: OsString::from(SERVICE_NAME),
        display_name: OsString::from(SERVICE_DISPLAY_NAME),
        service_type: ServiceType::OWN_PROCESS,
        start_type: ServiceStartType::AutoStart,
        error_control: ServiceErrorControl::Normal,
        executable_path: binary_path,
        launch_arguments: vec![],
        dependencies: vec![],
        account_name: None, // LocalSystem
        account_password: None,
    };

    match manager.create_service(&info, ServiceAccess::CHANGE_CONFIG) {
        Ok(svc) => {
            svc.set_description(SERVICE_DESCRIPTION)
                .map_err(|e| AgentError::Internal(format!("set_description: {e}")))?;
            info!("service '{}' installed", SERVICE_NAME);
            Ok(())
        }
        Err(windows_service::Error::Winapi(e))
            if e.raw_os_error() == Some(1073 /* ERROR_SERVICE_EXISTS */) =>
        {
            warn!("service '{}' already exists; skipping install", SERVICE_NAME);
            Ok(())
        }
        Err(e) => Err(AgentError::Internal(format!("CreateService failed: {e}"))),
    }
}

/// Uninstalls the `personel-agent` Windows service.
///
/// If the service is currently running it is stopped first.  Called by the MSI
/// `RemoveService` custom action.
///
/// # Errors
///
/// Returns [`AgentError::Internal`] if the SCM or service handle cannot be
/// opened, or if the deletion itself fails.
pub fn uninstall_service() -> Result<()> {
    let manager =
        ServiceManager::local_computer(None::<&str>, ServiceManagerAccess::CONNECT)
            .map_err(|e| AgentError::Internal(format!("OpenSCManager failed: {e}")))?;

    let svc = manager
        .open_service(
            SERVICE_NAME,
            ServiceAccess::STOP | ServiceAccess::DELETE | ServiceAccess::QUERY_STATUS,
        )
        .map_err(|e| AgentError::Internal(format!("OpenService failed: {e}")))?;

    // Best-effort stop before delete.
    match svc.stop() {
        Ok(_) => info!("service stopped before uninstall"),
        Err(e) => warn!(error = %e, "could not stop service before uninstall (may already be stopped)"),
    }

    svc.delete()
        .map_err(|e| AgentError::Internal(format!("DeleteService failed: {e}")))?;

    info!("service '{}' uninstalled", SERVICE_NAME);
    Ok(())
}
