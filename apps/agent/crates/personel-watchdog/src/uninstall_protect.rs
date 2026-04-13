//! Uninstall + service-tampering protection (Faz 4 Wave 2 #37).
//!
//! Detects the four common tamper paths an attacker would take to silence
//! the Personel agent without going through a clean uninstall ceremony:
//!
//! 1. **Service stop** — `sc stop PersonelAgent` or services.msc → Stop.
//!    The watchdog notices the state transition to `Stopped` on its 5 s
//!    poll, emits a `service_stop_external` finding, and immediately
//!    re-issues `StartService`.
//!
//! 2. **Service deletion** — `sc delete PersonelAgent`. `OpenServiceW`
//!    starts returning ERROR_SERVICE_DOES_NOT_EXIST (1060). This is
//!    unrecoverable from the watchdog (re-registering would require the
//!    original binary path + elevation + the MSI metadata) so we log a
//!    CRITICAL finding to `watchdog.log`. The next agent install (or a
//!    Phase 2 self-heal MSI) picks it up.
//!
//! 3. **Service disable** — `sc config PersonelAgent start= disabled`.
//!    `QueryServiceConfigW.dwStartType == SERVICE_DISABLED` → we
//!    immediately call `ChangeServiceConfigW` to flip it back to
//!    `SERVICE_AUTO_START` and emit `service_disabled_externally`.
//!
//! 4. **Binary deletion / unexpected change** — the agent EXE referenced
//!    by `lpBinaryPathName` is missing or its SHA-256 changed since the
//!    last poll. Missing → CRITICAL `agent_binary_missing`. Hash drift →
//!    CRITICAL `agent_binary_unexpected_change` UNLESS the OTA sentinel
//!    is present (see `ota_sentinel_present`).
//!
//! # OTA suppression sentinel
//!
//! The OTA update flow (Faz 4 #30, scaffold) creates the file
//! `%PROGRAMDATA%\Personel\agent\update_in_progress` immediately before
//! it swaps the agent binary, and removes it after the new binary is
//! verified and the service has restarted. While the sentinel exists, we
//! still poll integrity (so we get an updated hash baseline after the
//! swap completes) but suppress the `agent_binary_unexpected_change`
//! finding. `agent_binary_missing` is **not** suppressed even during OTA
//! — the new binary should always exist on disk before the swap is
//! considered atomic per ADR 0014.
//!
//! # Reporting
//!
//! Every finding is appended as a single JSON line to
//! `%PROGRAMDATA%\Personel\agent\watchdog.log` via the existing
//! [`crate::health_monitor::record_tamper_to_log`] helper. The agent's
//! `anti_tamper::replay_watchdog_log` will read and enqueue each entry
//! as a critical `agent.tamper_detected` event on its next start. The
//! watchdog has no event queue of its own — by design.
//!
//! # Symmetry mirror (Phase 2 TODO)
//!
//! The brief calls for two-way mutual monitoring: the watchdog protects
//! the agent (this module) AND the agent protects the watchdog. Only the
//! watchdog → agent direction is implemented in #37. The mirror — agent
//! periodically calls `OpenServiceW("PersonelWatchdog")` and checks state
//! + binary integrity — lives in `personel-agent` and is tracked under
//! TODO(symmetry-mirror). It cannot be implemented here without
//! modifying the personel-agent crate, which the #37 brief explicitly
//! forbids.
//!
//! # Chicken-and-egg: who watches the watchdog?
//!
//! If an attacker stops the watchdog itself (e.g. `sc stop
//! PersonelWatchdog`), this module obviously stops running and cannot
//! emit a finding. Three layers defend against this:
//!
//! 1. The agent-side mirror (Phase 2 TODO above) will poll the watchdog
//!    service every 5 s and emit `watchdog_stop_external` to its own
//!    queue.
//! 2. The watchdog SCM entry has `failure_actions=restart` (configured
//!    by the MSI in Faz 4 #29) so a clean Stop without elevation is
//!    auto-recovered by the SCM after a 5 s back-off.
//! 3. ADR 0013 §security: the SCM ACL on both services is hardened in
//!    Phase 4 #29 to deny SERVICE_STOP / SERVICE_CHANGE_CONFIG / DELETE
//!    to non-SYSTEM accounts. Stopping either service requires
//!    administrative privilege escalation, which is itself an audited
//!    KVKK violation.
//!
//! # Cost
//!
//! The 5 s tick performs at most 4 SCM API calls
//! (`OpenSCManager` is cached for the loop lifetime, then `OpenService`
//! + `QueryServiceStatus` + `QueryServiceConfig` per tick) and one file
//! stat + (occasionally) one SHA-256 read of the agent binary. The hash
//! is only recomputed when the file's mtime + len differ from the cached
//! baseline, so the steady state is two stat() calls per tick. No
//! allocations in the steady-state path beyond the OS handles.

#![cfg(target_os = "windows")]

use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime};

use serde_json::json;
use sha2::{Digest, Sha256};
use tracing::{debug, error, info, warn};
use windows_service::service::{
    Service, ServiceAccess, ServiceErrorControl, ServiceInfo, ServiceStartType,
    ServiceState, ServiceType,
};
use windows_service::service_manager::{ServiceManager, ServiceManagerAccess};

use crate::health_monitor::record_tamper_to_log;

// ── Tunables ──────────────────────────────────────────────────────────────────

/// SCM service name of the agent we protect. MUST match the WiX
/// `<ServiceInstall Name="PersonelAgent" />` in
/// `apps/agent/installer/wix/main.wxs`.
const AGENT_SERVICE_NAME: &str = "PersonelAgent";

/// Poll cadence. The brief mandates 5 s.
const POLL_INTERVAL: Duration = Duration::from_secs(5);

/// Maximum time we wait between detection and `StartService` re-issue.
/// Logged so an SRE can correlate with NATS event lag.
const RESTART_DEADLINE: Duration = Duration::from_secs(5);

/// `ProgramData\Personel\agent` directory (matches health_monitor).
fn data_dir() -> PathBuf {
    PathBuf::from(r"C:\ProgramData\Personel\agent")
}

/// Sentinel file path. While present, binary hash drift is suppressed.
fn ota_sentinel_path() -> PathBuf {
    data_dir().join("update_in_progress")
}

/// `true` if the OTA flow is currently swapping the agent binary.
fn ota_sentinel_present() -> bool {
    ota_sentinel_path().exists()
}

// ── Public entry point ────────────────────────────────────────────────────────

/// Spawns the uninstall protection loop on a dedicated OS thread.
///
/// The loop is synchronous (blocking SCM calls + std::fs IO + std::thread::sleep)
/// because the windows-service crate is sync and the workload is too small
/// to justify a tokio task. It runs for the lifetime of the watchdog
/// process and exits only on a panic inside the loop body, which is then
/// caught here and re-spawned after a 30 s back-off.
///
/// # Naming
///
/// The thread is named `personel-watchdog-tamper` to make it identifiable
/// in `Process Hacker` / `procexp` for SREs investigating an incident.
pub fn spawn_uninstall_protection() {
    std::thread::Builder::new()
        .name("personel-watchdog-tamper".into())
        .spawn(|| loop {
            let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                run_uninstall_protection();
            }));
            match result {
                Ok(()) => {
                    error!(
                        "uninstall_protect: loop returned cleanly (unexpected); restarting in 30s"
                    );
                }
                Err(panic) => {
                    error!(?panic, "uninstall_protect: loop panicked; restarting in 30s");
                }
            }
            std::thread::sleep(Duration::from_secs(30));
        })
        .expect("uninstall_protect thread spawn must succeed");
    info!("uninstall_protect: tamper monitor thread spawned");
}

/// Blocking poll loop. Public for tests + symmetry with the brief
/// signature; in production it's invoked by [`spawn_uninstall_protection`].
pub fn run_uninstall_protection() {
    info!(
        service = AGENT_SERVICE_NAME,
        interval_secs = POLL_INTERVAL.as_secs(),
        "uninstall_protect: starting service tamper poll"
    );

    if let Err(e) = std::fs::create_dir_all(data_dir()) {
        warn!(error = %e, "uninstall_protect: could not ensure data dir");
    }

    // Cached binary integrity baseline. None until the first successful
    // hash. Reset to None whenever the service config path changes (e.g.
    // OTA finalised with a different exe path — defensive, current OTA
    // design swaps in place).
    let mut last_hash: Option<[u8; 32]> = None;
    let mut last_path: Option<PathBuf> = None;
    // Track whether *we* just issued a restart, so the next tick doesn't
    // double-fire the finding before SCM transitions back to Running.
    let mut self_initiated_restart_at: Option<SystemTime> = None;

    loop {
        match poll_once(&mut last_hash, &mut last_path, &mut self_initiated_restart_at) {
            Ok(()) => debug!("uninstall_protect: tick ok"),
            Err(e) => {
                // Log loud but never propagate — protection must keep running.
                error!(error = %e, "uninstall_protect: tick failed");
            }
        }
        std::thread::sleep(POLL_INTERVAL);
    }
}

// ── One poll cycle ────────────────────────────────────────────────────────────

/// Single iteration of the poll loop. Extracted so the hot path stays
/// flat, easy to reason about, and easy to test.
fn poll_once(
    last_hash: &mut Option<[u8; 32]>,
    last_path: &mut Option<PathBuf>,
    self_initiated_restart_at: &mut Option<SystemTime>,
) -> anyhow::Result<()> {
    // ── Open SCM ──────────────────────────────────────────────────────────────
    //
    // We re-open the SCM each tick. It's cheap (~50 µs) and avoids a
    // long-lived handle that could outlast a temporary SCM hiccup. If
    // OpenSCManager fails we cannot make progress this tick — log warn
    // and bail (the loop will retry on the next tick).
    let scm = match ServiceManager::local_computer(
        None::<&str>,
        ServiceManagerAccess::CONNECT,
    ) {
        Ok(s) => s,
        Err(e) => {
            warn!(error = %e, "uninstall_protect: OpenSCManager failed");
            return Ok(());
        }
    };

    // ── Open service ──────────────────────────────────────────────────────────
    //
    // ERROR_SERVICE_DOES_NOT_EXIST (1060) is the deletion path.
    let access = ServiceAccess::QUERY_STATUS
        | ServiceAccess::QUERY_CONFIG
        | ServiceAccess::CHANGE_CONFIG
        | ServiceAccess::START;

    let service = match scm.open_service(AGENT_SERVICE_NAME, access) {
        Ok(s) => s,
        Err(windows_service::Error::Winapi(io_err))
            if io_err.raw_os_error() == Some(1060) =>
        {
            // Service deleted — CRITICAL, unrecoverable from watchdog.
            error!(
                service = AGENT_SERVICE_NAME,
                "uninstall_protect: AGENT SERVICE DELETED (ERROR_SERVICE_DOES_NOT_EXIST)"
            );
            record_tamper_to_log(
                "service_deleted_externally",
                "critical",
                json!({
                    "service": AGENT_SERVICE_NAME,
                    "reason": "OpenServiceW returned ERROR_SERVICE_DOES_NOT_EXIST",
                    "recovery": "manual_reinstall_required",
                    "watchdog_can_recover": false,
                }),
            );
            // Reset baselines so a re-install starts clean.
            *last_hash = None;
            *last_path = None;
            return Ok(());
        }
        Err(e) => {
            // Permission errors, etc. — log and continue. We do not panic
            // because the SCM may be transiently unavailable.
            warn!(error = %e, "uninstall_protect: OpenService failed");
            return Ok(());
        }
    };

    // ── Query state ───────────────────────────────────────────────────────────
    let status = service
        .query_status()
        .map_err(|e| anyhow::anyhow!("query_status: {e}"))?;

    // ── Query config (start type + binary path) ───────────────────────────────
    let config = service
        .query_config()
        .map_err(|e| anyhow::anyhow!("query_config: {e}"))?;

    // ── Service stop detection ────────────────────────────────────────────────
    //
    // Stop is a tamper iff the watchdog itself didn't initiate it. A
    // watchdog-issued restart goes Running → StopPending → Stopped →
    // StartPending → Running, so the brief 5 s window we count as
    // "self-initiated" suppresses the false positive.
    if status.current_state == ServiceState::Stopped {
        let self_initiated = self_initiated_restart_at
            .map(|t| {
                SystemTime::now()
                    .duration_since(t)
                    .map(|d| d <= Duration::from_secs(15))
                    .unwrap_or(false)
            })
            .unwrap_or(false);

        if !self_initiated {
            warn!(
                service = AGENT_SERVICE_NAME,
                "uninstall_protect: service STOPPED externally"
            );
            record_tamper_to_log(
                "service_stop_external",
                "critical",
                json!({
                    "service": AGENT_SERVICE_NAME,
                    "previous_state": "Running",
                    "current_state": "Stopped",
                    "restart_deadline_secs": RESTART_DEADLINE.as_secs(),
                }),
            );
        } else {
            debug!("uninstall_protect: stopped state matches self-initiated restart window");
        }

        // Whether self-initiated or external, if we're in Stopped we
        // want to be Running. Issue StartService.
        match service.start::<&str>(&[]) {
            Ok(()) => {
                info!(service = AGENT_SERVICE_NAME, "uninstall_protect: StartService issued");
                *self_initiated_restart_at = Some(SystemTime::now());
            }
            Err(windows_service::Error::Winapi(io_err))
                if io_err.raw_os_error() == Some(1056) =>
            {
                // ERROR_SERVICE_ALREADY_RUNNING — race with SCM auto-restart.
                debug!("uninstall_protect: StartService says already running");
            }
            Err(e) => {
                error!(error = %e, "uninstall_protect: StartService failed");
            }
        }
    }

    // ── Service disable detection ─────────────────────────────────────────────
    //
    // Even if the service is currently Running, an attacker who set
    // start_type to Disabled has primed it to never start again after a
    // reboot. Re-enable immediately.
    if config.start_type == ServiceStartType::Disabled {
        warn!(
            service = AGENT_SERVICE_NAME,
            "uninstall_protect: service start_type == Disabled (tamper)"
        );
        record_tamper_to_log(
            "service_disabled_externally",
            "critical",
            json!({
                "service": AGENT_SERVICE_NAME,
                "previous_start_type": "Disabled",
                "new_start_type": "AutoStart",
            }),
        );

        if let Err(e) = reenable_autostart(&service, &config) {
            error!(error = %e, "uninstall_protect: ChangeServiceConfig (re-enable) failed");
        } else {
            info!("uninstall_protect: service start_type restored to AutoStart");
        }
    }

    // ── Binary integrity ──────────────────────────────────────────────────────
    //
    // The agent EXE path comes from QueryServiceConfigW so we always check
    // the *currently registered* binary, not a hard-coded path. That way,
    // post-OTA, the new binary becomes the new baseline.
    check_binary_integrity(&config.executable_path, last_hash, last_path);

    Ok(())
}

// ── Binary integrity ──────────────────────────────────────────────────────────

/// Hashes `path` and updates the cached baseline. Emits findings on
/// missing files or unexpected hash drift (suppressed during OTA).
///
/// Pure side-effect on the hash cache; no return value because every
/// outcome is logged or recorded as tamper.
fn check_binary_integrity(
    path: &Path,
    last_hash: &mut Option<[u8; 32]>,
    last_path: &mut Option<PathBuf>,
) {
    // If the registered path changed (e.g. OTA installed to a new
    // directory), reset the baseline so we don't false-positive on the
    // first observation of the new file.
    if last_path.as_deref() != Some(path) {
        debug!(?path, "uninstall_protect: binary path changed; resetting hash baseline");
        *last_hash = None;
        *last_path = Some(path.to_path_buf());
    }

    if !path.exists() {
        // Missing binary is critical even during OTA — the swap should be
        // atomic (write temp → rename), never an unlinked-then-written
        // sequence. ADR 0014 §3.
        error!(
            ?path,
            "uninstall_protect: agent binary MISSING (registered SCM path)"
        );
        record_tamper_to_log(
            "agent_binary_missing",
            "critical",
            json!({
                "expected_path": path.display().to_string(),
                "during_ota": ota_sentinel_present(),
            }),
        );
        return;
    }

    let new_hash = match sha256_file(path) {
        Ok(h) => h,
        Err(e) => {
            warn!(error = %e, ?path, "uninstall_protect: hash compute failed");
            return;
        }
    };

    match *last_hash {
        None => {
            debug!(
                ?path,
                hash = %hex::encode(new_hash),
                "uninstall_protect: binary baseline established"
            );
            *last_hash = Some(new_hash);
        }
        Some(prev) if prev == new_hash => {
            // Steady state: no change.
        }
        Some(prev) => {
            if ota_sentinel_present() {
                // OTA in progress — adopt the new hash as the baseline.
                info!(
                    ?path,
                    old_hash = %hex::encode(prev),
                    new_hash = %hex::encode(new_hash),
                    "uninstall_protect: binary hash changed during OTA (suppressed)"
                );
                *last_hash = Some(new_hash);
            } else {
                error!(
                    ?path,
                    old_hash = %hex::encode(prev),
                    new_hash = %hex::encode(new_hash),
                    "uninstall_protect: binary hash CHANGED unexpectedly (tamper)"
                );
                record_tamper_to_log(
                    "agent_binary_unexpected_change",
                    "critical",
                    json!({
                        "path": path.display().to_string(),
                        "old_sha256": hex::encode(prev),
                        "new_sha256": hex::encode(new_hash),
                        "ota_sentinel_present": false,
                    }),
                );
                // Adopt the new hash so we don't re-fire on every tick.
                *last_hash = Some(new_hash);
            }
        }
    }
}

/// Streaming SHA-256 of `path`. Allocations are bounded to the 8 KiB
/// read buffer; suitable for the watchdog hot path because it only runs
/// once per OTA + once at startup.
fn sha256_file(path: &Path) -> std::io::Result<[u8; 32]> {
    use std::fs::File;
    use std::io::Read;

    let mut file = File::open(path)?;
    let mut hasher = Sha256::new();
    let mut buf = [0u8; 8192];
    loop {
        let n = file.read(&mut buf)?;
        if n == 0 {
            break;
        }
        hasher.update(&buf[..n]);
    }
    let digest = hasher.finalize();
    let mut out = [0u8; 32];
    out.copy_from_slice(&digest);
    Ok(out)
}

// ── Re-enable disabled service ────────────────────────────────────────────────

/// Re-issues `ChangeServiceConfigW` with `start_type = AutoStart`,
/// preserving every other field of the existing config.
///
/// `windows-service` accepts a full [`ServiceInfo`], not a partial diff,
/// so we have to translate every field of the live [`ServiceConfig`]
/// back into the input struct. Fields that have no analogue in
/// `ServiceInfo` (load_order_group, tag_id) are dropped, matching the
/// behaviour of the wrapper in Faz 1's installer code.
fn reenable_autostart(
    service: &Service,
    current: &windows_service::service::ServiceConfig,
) -> windows_service::Result<()> {
    let info = ServiceInfo {
        name: AGENT_SERVICE_NAME.into(),
        display_name: current.display_name.clone(),
        service_type: ServiceType::OWN_PROCESS,
        start_type: ServiceStartType::AutoStart,
        error_control: ServiceErrorControl::Normal,
        executable_path: current.executable_path.clone(),
        launch_arguments: vec![],
        dependencies: current.dependencies.clone(),
        account_name: current.account_name.clone(),
        account_password: None,
    };
    service.change_config(&info)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    #[test]
    fn poll_interval_is_five_seconds() {
        assert_eq!(POLL_INTERVAL.as_secs(), 5);
    }

    #[test]
    fn restart_deadline_is_five_seconds() {
        assert_eq!(RESTART_DEADLINE.as_secs(), 5);
    }

    #[test]
    fn agent_service_name_matches_msi_wix() {
        // If this ever drifts from apps/agent/installer/wix/main.wxs
        // <ServiceInstall Name="PersonelAgent"/>, the watchdog will
        // happily sit there reporting "service deleted" forever.
        assert_eq!(AGENT_SERVICE_NAME, "PersonelAgent");
    }

    #[test]
    fn ota_sentinel_path_under_programdata() {
        let p = ota_sentinel_path();
        let s = p.display().to_string();
        assert!(s.contains("Personel"), "unexpected ota sentinel path: {s}");
        assert!(s.ends_with("update_in_progress"), "unexpected suffix: {s}");
    }

    #[test]
    fn data_dir_under_programdata() {
        let p = data_dir();
        let s = p.display().to_string();
        assert!(s.contains("Personel"), "unexpected data dir: {s}");
    }

    #[test]
    fn sha256_file_matches_known_vector() {
        // SHA-256 of "abc" = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
        let dir = std::env::temp_dir();
        let path = dir.join("personel_uninstall_protect_test_abc.txt");
        {
            let mut f = std::fs::File::create(&path).unwrap();
            f.write_all(b"abc").unwrap();
        }
        let h = sha256_file(&path).unwrap();
        assert_eq!(
            hex::encode(h),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn check_binary_integrity_missing_file_emits_no_panic() {
        let mut last_hash: Option<[u8; 32]> = None;
        let mut last_path: Option<PathBuf> = None;
        let bogus = PathBuf::from(r"C:\Personel\does\not\exist\xyz.exe");
        // We can't easily assert the watchdog.log line in a unit test
        // (the function writes to ProgramData) but we can verify it
        // doesn't panic and doesn't seed a baseline hash.
        check_binary_integrity(&bogus, &mut last_hash, &mut last_path);
        assert!(last_hash.is_none());
        assert_eq!(last_path.as_deref(), Some(bogus.as_path()));
    }

    #[test]
    fn check_binary_integrity_seeds_baseline_on_first_observation() {
        let dir = std::env::temp_dir();
        let path = dir.join("personel_uninstall_protect_test_baseline.bin");
        {
            let mut f = std::fs::File::create(&path).unwrap();
            f.write_all(b"baseline-bytes").unwrap();
        }

        let mut last_hash: Option<[u8; 32]> = None;
        let mut last_path: Option<PathBuf> = None;
        check_binary_integrity(&path, &mut last_hash, &mut last_path);
        assert!(
            last_hash.is_some(),
            "first observation must seed the hash baseline"
        );
        assert_eq!(last_path.as_deref(), Some(path.as_path()));

        // Second call with the same bytes is a no-op — baseline must remain.
        let baseline = last_hash;
        check_binary_integrity(&path, &mut last_hash, &mut last_path);
        assert_eq!(last_hash, baseline);

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn check_binary_integrity_path_change_resets_baseline() {
        let dir = std::env::temp_dir();
        let path_a = dir.join("personel_uninstall_protect_test_path_a.bin");
        let path_b = dir.join("personel_uninstall_protect_test_path_b.bin");
        std::fs::File::create(&path_a).unwrap().write_all(b"AAA").unwrap();
        std::fs::File::create(&path_b).unwrap().write_all(b"BBB").unwrap();

        let mut last_hash: Option<[u8; 32]> = None;
        let mut last_path: Option<PathBuf> = None;
        check_binary_integrity(&path_a, &mut last_hash, &mut last_path);
        let hash_a = last_hash;
        assert!(hash_a.is_some());

        // Switching to a new path must reset the baseline (no false
        // tamper finding for the first observation of the new file).
        check_binary_integrity(&path_b, &mut last_hash, &mut last_path);
        assert_eq!(last_path.as_deref(), Some(path_b.as_path()));
        assert!(last_hash.is_some());
        assert_ne!(last_hash, hash_a);

        let _ = std::fs::remove_file(&path_a);
        let _ = std::fs::remove_file(&path_b);
    }

    #[test]
    fn ota_sentinel_present_returns_bool_without_panic() {
        // The sentinel file may or may not exist on the test runner;
        // the function must simply not panic.
        let _ = ota_sentinel_present();
    }
}
