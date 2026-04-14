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
use std::sync::{Mutex, OnceLock};
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

/// Type of the service body closure: receives a oneshot::Receiver that fires
/// when the SCM has delivered Stop or Shutdown. The closure is expected to
/// block until the receiver fires and all work has wound down.
type ServiceBody = Box<dyn FnOnce(oneshot::Receiver<()>) + Send + 'static>;

/// Holds the body closure between `run_as_service` and `service_main_wrapper`.
/// `service_dispatcher::start` spawns a brand-new thread for the FFI trampoline
/// which has no way to carry a captured closure, so we stash it here. The
/// `Mutex<Option<_>>` is taken exactly once inside `run_service_main`.
static SERVICE_BODY: OnceLock<Mutex<Option<ServiceBody>>> = OnceLock::new();

// ── Service constants ─────────────────────────────────────────────────────────

/// SCM service name (must match the name used in `service_dispatcher::start`).
const SERVICE_NAME: &str = "personel-agent";
/// Human-readable display name shown in services.msc.
const SERVICE_DISPLAY_NAME: &str = "Personel Agent";
/// Service description shown in the SCM property sheet.
const SERVICE_DESCRIPTION: &str =
    "Personel UAM endpoint agent — activity monitoring and compliance.";

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
    // ── Register the service control handler FIRST ────────────────────────────
    //
    // Windows SCM requires the control handler to be registered and the
    // service to report StartPending within ~30 s of the service process
    // being launched. If we do any heavy pre-work before this point we risk
    // error 1053 ("service did not respond to start request in a timely
    // fashion"). Everything here must stay cheap and allocation-light —
    // the real agent bring-up (tokio runtime build, queue open, TLS channel
    // construction, collector spawn) happens later from inside the body
    // closure, AFTER we have transitioned to Running.
    let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
    let shutdown_tx_slot = std::sync::Arc::new(Mutex::new(Some(shutdown_tx)));
    let shutdown_tx_for_handler = std::sync::Arc::clone(&shutdown_tx_slot);

    let status_handle =
        service_control_handler::register(SERVICE_NAME, move |event| match event {
            ServiceControl::Stop | ServiceControl::Shutdown => {
                info!("SCM → Stop/Shutdown received");
                if let Ok(mut guard) = shutdown_tx_for_handler.lock() {
                    if let Some(tx) = guard.take() {
                        // `oneshot::Sender::send` consumes self on both Ok
                        // and Err; the Err path just means the receiver has
                        // already been dropped, which is fine.
                        let _ = tx.send(());
                    }
                }
                ServiceControlHandlerResult::NoError
            }
            ServiceControl::Interrogate => ServiceControlHandlerResult::NoError,
            _ => ServiceControlHandlerResult::NotImplemented,
        })?;

    // ── StartPending ──────────────────────────────────────────────────────────
    // Report StartPending immediately so the SCM stops counting down from
    // 30 s. The wait_hint tells the SCM to wait up to 60 s for the body
    // closure to finish its own bring-up before Running is reported.
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::StartPending,
        controls_accepted: ServiceControlAccept::empty(),
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::from_secs(60),
        process_id: None,
    })?;

    // ── Running ───────────────────────────────────────────────────────────────
    // We transition to Running BEFORE the body closure runs. This is safe
    // because the handler is already registered and will deliver Stop into
    // the shutdown channel the body owns. Transitioning to Running now
    // unblocks `sc start` on the caller side and prevents the watchdog
    // from tripping the 1053 deadline even if agent bring-up takes a few
    // seconds (queue open, TLS, enrollment fingerprint verification, …).
    status_handle.set_service_status(ServiceStatus {
        service_type: ServiceType::OWN_PROCESS,
        current_state: ServiceState::Running,
        controls_accepted: ServiceControlAccept::STOP | ServiceControlAccept::SHUTDOWN,
        exit_code: ServiceExitCode::Win32(0),
        checkpoint: 0,
        wait_hint: Duration::ZERO,
        process_id: None,
    })?;

    info!("service running; invoking body closure");

    // ── Run the body closure (heavy agent bring-up + event loop) ──────────────
    // Take the body out of the static slot and invoke it with the
    // shutdown_rx. The body is expected to block until the receiver fires
    // and all worker tasks have drained. If no body was stashed this is a
    // programmer error — we log loudly and fall through to Stopped.
    let body = SERVICE_BODY
        .get()
        .and_then(|slot| slot.lock().ok().and_then(|mut g| g.take()));

    match body {
        Some(body) => {
            body(shutdown_rx);
            info!("service body closure returned; transitioning to StopPending");
        }
        None => {
            error!("SERVICE_BODY was not stashed before service_dispatcher::start");
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

/// Runs the current process as a Windows service with `body` as the service
/// body closure.
///
/// The body is invoked **after** the SCM control handler has been registered
/// and the service has transitioned to Running. This ordering is critical:
/// Windows requires the control handler to be registered within ~30 s of the
/// service process launching or it kills the process with error 1053
/// ("service did not respond to start request in a timely fashion"). Moving
/// heavy bring-up (tokio runtime build, queue open, TLS channel construction,
/// collector spawn, enrollment verification) into the body closure guarantees
/// it happens AFTER registration and therefore never races the deadline.
///
/// `body` receives a `oneshot::Receiver<()>` that fires when SCM delivers a
/// Stop or Shutdown control. The body should block until the receiver fires
/// and all work has drained, then return. A typical body:
///
/// ```ignore
/// run_as_service(Box::new(|shutdown_rx| {
///     let rt = build_runtime().unwrap();
///     rt.block_on(async move {
///         service::run_agent(config, shutdown_rx).await.ok();
///     });
/// }))
/// ```
///
/// # Errors
///
/// Returns [`AgentError::Internal`] if the SCM dispatcher cannot be started
/// (e.g. the process was not launched by the SCM), or if `run_as_service` is
/// called more than once in the same process lifetime.
pub fn run_as_service<F>(body: F) -> Result<()>
where
    F: FnOnce(oneshot::Receiver<()>) + Send + 'static,
{
    // Stash the body in the OnceLock so `service_main_wrapper` can pick it
    // up after it has finished the fast-path registration.
    let slot = SERVICE_BODY.get_or_init(|| Mutex::new(None));
    {
        let mut guard = slot
            .lock()
            .map_err(|_| AgentError::Internal("SERVICE_BODY mutex poisoned".into()))?;
        if guard.is_some() {
            return Err(AgentError::Internal(
                "run_as_service already called — body slot occupied".into(),
            ));
        }
        *guard = Some(Box::new(body));
    }

    // `service_dispatcher::start` blocks until the service has stopped. The
    // SCM spawns a fresh thread for `ffi_service_main`; everything inside
    // `run_service_main` runs on that thread.
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
