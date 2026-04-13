//! Windows service lifecycle management.
//!
//! Delegates to `personel_os::service` for the Win32 SCM trampoline and
//! provides the async agent run loop called from both service and standalone
//! (console) modes.

use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use tokio::sync::oneshot;
use tracing::{info, warn};

use personel_collectors::CollectorRegistry;
use personel_collectors::{
    bluetooth_devices::BluetoothDevicesCollector,
    browser_history::BrowserHistoryCollector,
    clipboard::ClipboardCollector,
    clipboard_content_redacted::ClipboardContentRedactedCollector,
    cloud_storage::CloudStorageCollector,
    device_status::DeviceStatusCollector,
    email_metadata::EmailMetadataCollector,
    file_system::FileSystemCollector,
    firefox_history::FirefoxHistoryCollector,
    geo_ip::GeoIpCollector,
    idle::IdleCollector,
    keystroke::{KeystrokeContentCollector, KeystrokeMetaCollector},
    mtp_devices::MtpDevicesCollector,
    network::NetworkCollector,
    office_activity::OfficeActivityCollector,
    print::PrintCollector,
    process_app::ProcessAppCollector,
    screen::ScreenCollector,
    system_events::SystemEventsCollector,
    usb::UsbCollector,
    window_title::WindowTitleCollector,
    window_url_extraction::WindowUrlExtractionCollector,
    CollectorCtx,
};
use personel_core::clock::SystemClock;
use personel_core::throttle::ThrottleState;
use personel_policy::engine::PolicyEngine;
use personel_queue::queue::{EventQueue, QueueConfig};

use crate::config::AgentConfig;
use crate::throttle::{run_throttle_monitor, MonitorDeps};

/// Runs the main agent loop.
///
/// Called from `main` (service mode or console mode). Blocks until
/// `shutdown_rx` fires.
///
/// # Errors
///
/// Returns an error if any critical subsystem fails to initialise.
pub async fn run_agent(config: AgentConfig, mut shutdown_rx: oneshot::Receiver<()>) -> Result<()> {
    info!(version = crate::config::AGENT_VERSION, "personel-agent starting");

    let data_dir = config.data_dir.clone().unwrap_or_else(crate::config::default_data_dir);
    std::fs::create_dir_all(&data_dir).context("create data dir")?;

    // ── Identity ──────────────────────────────────────────────────────────────
    let tenant_id = config.tenant_id().context("tenant_id not configured")?;
    let endpoint_id = config.endpoint_id().context("endpoint_id not configured")?;

    // ── Queue ─────────────────────────────────────────────────────────────────
    // TODO: load the DPAPI-protected SQLCipher key from disk.
    // Placeholder: use a fixed test key until keystore is wired.
    let queue_key = zeroize::Zeroizing::new(vec![0u8; 32]);
    let queue_config = QueueConfig::new(data_dir.join("queue.db"), queue_key);
    let queue = Arc::new(EventQueue::open(queue_config).context("open event queue")?);
    info!("queue opened");

    // ── Crash dump uploader (Faz 4 #31) ───────────────────────────────────────
    // Spawn a one-shot task to scan the dumps directory for any crash dump
    // files from a previous run, enqueue them as Critical tamper events, and
    // move them to `uploaded/`. Runs on the blocking pool so large file reads
    // + base64 don't stall the reactor. Non-fatal on any error.
    let (crash_stop_tx, crash_stop_rx) = oneshot::channel::<()>();
    let crash_queue = Arc::clone(&queue);
    let _crash_task = tokio::spawn(async move {
        crate::crash_dump::run_dump_uploader(crash_queue, crash_stop_rx).await;
    });

    // ── Policy engine ─────────────────────────────────────────────────────────
    // TODO: load the real Ed25519 policy-signing public key from config.
    // Placeholder key (all zeros) — will fail on real policy bundles.
    let signing_key_bytes = [0u8; 32];
    let (policy_engine, policy_rx) = PolicyEngine::new(&signing_key_bytes)
        .unwrap_or_else(|_| {
            // Fallback to unsigned mode if the baked key is not yet set.
            warn!("policy signing key not configured; running unsigned (dev mode)");
            PolicyEngine::new_unsigned()
        });
    info!("policy engine initialised (version={})", policy_engine.current().version);

    // ── Self-throttle state (Faz 4 Wave 1 #32) ────────────────────────────────
    // Shared between the throttle monitor (writer) and every collector
    // (lock-free readers). Lives in personel-core so collectors can
    // import it without depending on the agent binary crate.
    let throttle_state = Arc::new(ThrottleState::new());

    // ── Collector context ─────────────────────────────────────────────────────
    // `dyn Clock` upcast so both the ctx and the throttle monitor deps
    // can share the same Arc.
    let clock: Arc<dyn personel_core::clock::Clock> = Arc::new(SystemClock);
    let ctx = CollectorCtx {
        queue: Arc::clone(&queue),
        clock: Arc::clone(&clock),
        pe_dek: None, // TODO: load from keystore after enrollment
        policy_rx,
        tenant_id,
        endpoint_id,
        throttle: Arc::clone(&throttle_state),
    };

    // ── Throttle monitor task ─────────────────────────────────────────────────
    // The monitor ticks every 5 s, measures the agent process's own
    // CPU% + RSS, feeds the rolling window, and emits an
    // `agent.health_heartbeat` event on every state transition.
    let (throttle_stop_tx, throttle_stop_rx) = oneshot::channel::<()>();
    let throttle_deps = MonitorDeps {
        state: Arc::clone(&throttle_state),
        queue: Arc::clone(&queue),
        clock: Arc::clone(&clock),
    };
    let _throttle_task = tokio::spawn(async move {
        run_throttle_monitor(throttle_deps, throttle_stop_rx).await;
    });

    // ── Collector registry ────────────────────────────────────────────────────
    let mut registry = CollectorRegistry::new();
    // Wave 0 — original 12 collectors
    registry.register(Box::new(IdleCollector::new()));
    registry.register(Box::new(WindowTitleCollector::new()));
    registry.register(Box::new(ProcessAppCollector::new()));
    registry.register(Box::new(ScreenCollector::new()));
    registry.register(Box::new(FileSystemCollector::new()));
    registry.register(Box::new(NetworkCollector::new()));
    registry.register(Box::new(ClipboardCollector::new()));
    registry.register(Box::new(UsbCollector::new()));
    registry.register(Box::new(PrintCollector::new()));
    registry.register(Box::new(KeystrokeMetaCollector::new()));
    registry.register(Box::new(KeystrokeContentCollector::new()));
    // Faz 2 Wave 2 — browser + cloud + email
    registry.register(Box::new(BrowserHistoryCollector::new()));
    registry.register(Box::new(FirefoxHistoryCollector::new()));
    registry.register(Box::new(CloudStorageCollector::new()));
    registry.register(Box::new(EmailMetadataCollector::new()));
    // Faz 2 Wave 3 — office + system + BT + MTP + device + geo + URL + DLP scaffold
    registry.register(Box::new(OfficeActivityCollector::new()));
    registry.register(Box::new(SystemEventsCollector::new()));
    registry.register(Box::new(BluetoothDevicesCollector::new()));
    registry.register(Box::new(MtpDevicesCollector::new()));
    registry.register(Box::new(DeviceStatusCollector::new()));
    registry.register(Box::new(GeoIpCollector::new()));
    registry.register(Box::new(WindowUrlExtractionCollector::new()));
    registry.register(Box::new(ClipboardContentRedactedCollector::new()));

    // start_all is intentionally infallible: individual collector failures are
    // logged at error level and do not abort the agent. Inspect health_all()
    // on the 30-second tick to detect collectors that failed to start.
    registry.start_all(&ctx).await.context("start collectors")?;
    info!("all collectors started — individual failures logged above if any");

    // ── Anti-tamper startup checks (Faz 4 Wave 1 #29) ─────────────────────────
    // Three checks: PE self-hash, registry ACL, watchdog log replay. Findings
    // become critical agent.tamper_detected events. Failures NEVER abort agent
    // startup — tamper findings should still reach the gateway via the queue.
    {
        let findings = crate::anti_tamper::run_startup_checks(&ctx);
        if findings.is_empty() {
            info!("anti_tamper: startup checks clean");
        } else {
            warn!(count = findings.len(), "anti_tamper: startup findings emitted");
            for finding in &findings {
                if let Err(e) = crate::anti_tamper::enqueue_tamper_event(&queue, &ctx, finding) {
                    warn!(error = %e, check = finding.check, "anti_tamper: enqueue failed");
                }
            }
        }
    }

    // ── Health pipe writer (Faz 4 Wave 1 #29) ─────────────────────────────────
    // Spawns a 1-byte heartbeat sender on \\.\pipe\personel-agent-health every
    // 30 s. The watchdog reads these pings and classifies the agent as
    // unresponsive if no byte is seen for 90 s. Windows-only; on dev hosts
    // the writer is not spawned and the watchdog log replay path also no-ops.
    #[cfg(target_os = "windows")]
    let (health_pipe_stop_tx, health_pipe_stop_rx) = oneshot::channel::<()>();
    #[cfg(target_os = "windows")]
    {
        tokio::spawn(async move {
            if let Err(e) = crate::health_pipe::run_health_pipe_writer(health_pipe_stop_rx).await {
                warn!(error = %e, "health_pipe: writer task exited with error");
            }
        });
    }

    // ── Transport ─────────────────────────────────────────────────────────────
    let (transport_stop_tx, transport_stop_rx) = oneshot::channel::<()>();
    let transport_queue = Arc::clone(&queue);

    // Only start the real transport if the agent is enrolled (has gateway config).
    if let Some(enroll) = &config.enrollment {
        use personel_transport::client::{BackoffConfig, ClientConfig, run_stream};

        // Load the client cert (PEM bundle: leaf || chain), unseal the
        // DPAPI-protected PKCS#8 DER private key and re-wrap it as PEM, and
        // load the Vault PKI root CA used as the mTLS trust anchor.
        let load_result = (|| -> anyhow::Result<(Vec<u8>, Vec<u8>, Option<Vec<u8>>)> {
            let cert_pem = std::fs::read(&enroll.cert_path)
                .with_context(|| format!("read client cert: {}", enroll.cert_path.display()))?;

            let sealed_key = std::fs::read(&enroll.key_path)
                .with_context(|| format!("read sealed key: {}", enroll.key_path.display()))?;
            let key_der = unseal_private_key(&sealed_key)
                .context("unseal private key")?;
            let key_pem = pkcs8_der_to_pem(&key_der);

            let root_ca_pem = match enroll.root_ca_path.as_ref() {
                Some(p) => Some(
                    std::fs::read(p)
                        .with_context(|| format!("read root CA: {}", p.display()))?,
                ),
                None => {
                    warn!(
                        "transport: root_ca_path not configured — mTLS server verification \
                         will fail unless the gateway cert chains to a system trust anchor"
                    );
                    None
                }
            };

            Ok((cert_pem, key_pem, root_ca_pem))
        })();

        match load_result {
            Ok((cert_pem, key_pem, tenant_ca_pem)) => {
                let gateway_url = enroll.gateway_url.clone();
                // Parse UUIDs into 16-byte arrays for the Hello frame. If
                // parsing fails we fall back to zeros and log — the gateway
                // will reject the Hello but at least the stream loop runs.
                let tenant_bytes = uuid::Uuid::parse_str(&enroll.tenant_id)
                    .map(|u| *u.as_bytes())
                    .unwrap_or([0u8; 16]);
                let endpoint_bytes = uuid::Uuid::parse_str(&enroll.endpoint_id)
                    .map(|u| *u.as_bytes())
                    .unwrap_or([0u8; 16]);
                // hw_fingerprint lives on disk in config.toml only when we
                // cache it; for Phase 1 re-derive a stable digest from the
                // endpoint UUID so the gateway does not reject Hello on
                // missing fingerprint. The authoritative fingerprint lives
                // in the endpoints table keyed on enrollment time.
                let hw_digest = {
                    use sha2::{Digest, Sha256};
                    let mut h = Sha256::new();
                    h.update(enroll.endpoint_id.as_bytes());
                    h.finalize().to_vec()
                };
                let transport_cfg = ClientConfig {
                    gateway_url,
                    client_cert_pem: cert_pem,
                    client_key_pem: key_pem,
                    tenant_ca_pem,
                    tenant_id: tenant_bytes,
                    endpoint_id: endpoint_bytes,
                    hw_fingerprint: hw_digest,
                    os_version: std::env::consts::OS.to_string(),
                    agent_version: env!("CARGO_PKG_VERSION").to_string(),
                    backoff: BackoffConfig::default(),
                };
                let _transport_task = tokio::spawn(async move {
                    if let Err(e) =
                        run_stream(transport_cfg, transport_queue, transport_stop_rx).await
                    {
                        warn!(error = %e, "transport: run_stream exited with error");
                    }
                    info!("transport stopped");
                });
            }
            Err(e) => {
                warn!(error = %e, "transport: cert/key load failed — running in offline mode");
                let _transport_task = tokio::spawn(async move {
                    let _ = transport_stop_rx.await;
                    info!("transport stopped (offline mode)");
                });
            }
        }
    } else {
        warn!("agent not enrolled — transport not started (offline mode)");
        let _transport_task = tokio::spawn(async move {
            let _ = transport_stop_rx.await;
            info!("transport stopped (not enrolled)");
        });
    }

    // ── Post-update health report ─────────────────────────────────────────────
    // After a successful update swap the watchdog waits up to 60 s for a
    // `health_ok` message. We send it once all critical subsystems are up.
    tokio::spawn(async {
        if let Err(e) = report_health_ok().await {
            // Non-fatal: watchdog will roll back after timeout if it was waiting.
            tracing::warn!(error = %e, "health_ok report to watchdog failed");
        }
    });

    // ── Health tick ───────────────────────────────────────────────────────────
    let health_queue = Arc::clone(&queue);
    let _health_task = tokio::spawn(async move {
        let mut ticker = tokio::time::interval(Duration::from_secs(30));
        loop {
            ticker.tick().await;
            if let Ok(stats) = health_queue.stats() {
                info!(
                    pending = stats.pending_count,
                    in_flight = stats.in_flight_count,
                    pending_bytes = stats.pending_bytes,
                    "queue health"
                );
            }
        }
    });

    // ── Wait for shutdown ─────────────────────────────────────────────────────
    let _ = shutdown_rx.await;
    info!("shutdown signal received; stopping collectors");

    registry.stop_all().await;
    let _ = transport_stop_tx.send(());
    let _ = crash_stop_tx.send(());
    let _ = throttle_stop_tx.send(());
    #[cfg(target_os = "windows")]
    let _ = health_pipe_stop_tx.send(());

    info!("personel-agent stopped");
    Ok(())
}

// ── Post-update health report ─────────────────────────────────────────────────

/// Sends `{"cmd":"health_ok","version":"<ver>"}` to the watchdog IPC pipe/socket.
///
/// Called once at startup so the watchdog can confirm a successful update swap
/// and remove the rollback copy. If the watchdog is not in an update-swap window
/// this message is silently ignored on the watchdog side.
///
/// Errors are non-fatal: the watchdog has a 60-second timeout and will roll back
/// if no health confirmation arrives.
async fn report_health_ok() -> anyhow::Result<()> {
    use tokio::io::AsyncWriteExt;

    let line = format!(
        "{}\n",
        serde_json::json!({
            "cmd": "health_ok",
            "version": crate::config::AGENT_VERSION,
        })
    );

    #[cfg(target_os = "windows")]
    {
        use tokio::net::windows::named_pipe::ClientOptions;
        const PIPE: &str = r"\\.\pipe\personel-watchdog-cmd";
        let mut client = ClientOptions::new()
            .open(PIPE)
            .map_err(|e| anyhow::anyhow!("health pipe open: {e}"))?;
        client.write_all(line.as_bytes()).await
            .map_err(|e| anyhow::anyhow!("health pipe write: {e}"))?;
        client.flush().await
            .map_err(|e| anyhow::anyhow!("health pipe flush: {e}"))?;
    }

    #[cfg(not(target_os = "windows"))]
    {
        use tokio::net::UnixStream;
        const SOCK: &str = "/tmp/personel-watchdog-health.sock";
        let mut stream = UnixStream::connect(SOCK).await
            .map_err(|e| anyhow::anyhow!("health socket connect ({SOCK}): {e}"))?;
        stream.write_all(line.as_bytes()).await
            .map_err(|e| anyhow::anyhow!("health socket write: {e}"))?;
        stream.flush().await
            .map_err(|e| anyhow::anyhow!("health socket flush: {e}"))?;
    }

    tracing::info!(version = crate::config::AGENT_VERSION, "health_ok sent to watchdog");
    Ok(())
}

// ── Private key unsealing + PKCS#8 DER → PEM ──────────────────────────────────

/// Unseals the DPAPI-protected private key blob written by `enroll.exe`.
///
/// On Windows the file is a `CryptProtectData` machine-scope blob containing
/// PKCS#8 DER bytes. On non-Windows dev builds the blob is the raw bytes —
/// we just pass them through and hope the caller knows what it's doing.
#[cfg(target_os = "windows")]
fn unseal_private_key(sealed: &[u8]) -> anyhow::Result<Vec<u8>> {
    let plaintext = personel_os::windows::dpapi::unprotect(sealed)
        .map_err(|e| anyhow::anyhow!("DPAPI unprotect: {e}"))?;
    Ok(plaintext.to_vec())
}

#[cfg(not(target_os = "windows"))]
fn unseal_private_key(sealed: &[u8]) -> anyhow::Result<Vec<u8>> {
    // Dev fallback: enroll.rs stores the DER bytes verbatim on non-Windows.
    Ok(sealed.to_vec())
}

/// Wraps PKCS#8 DER bytes in a PEM envelope (`-----BEGIN PRIVATE KEY-----`
/// markers, base64-encoded body wrapped at 64 columns) suitable for
/// `tonic::transport::Identity::from_pem`.
fn pkcs8_der_to_pem(der: &[u8]) -> Vec<u8> {
    use base64::engine::general_purpose::STANDARD;
    use base64::Engine as _;

    let b64 = STANDARD.encode(der);
    let mut out = String::with_capacity(b64.len() + 64);
    out.push_str("-----BEGIN PRIVATE KEY-----\n");
    for chunk in b64.as_bytes().chunks(64) {
        // SAFETY: STANDARD base64 alphabet is pure ASCII, every chunk is valid UTF-8.
        out.push_str(std::str::from_utf8(chunk).expect("base64 alphabet is ASCII"));
        out.push('\n');
    }
    out.push_str("-----END PRIVATE KEY-----\n");
    out.into_bytes()
}
