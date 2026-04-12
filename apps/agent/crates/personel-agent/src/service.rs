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
    clipboard::ClipboardCollector,
    file_system::FileSystemCollector,
    idle::IdleCollector,
    keystroke::{KeystrokeContentCollector, KeystrokeMetaCollector},
    network::NetworkCollector,
    print::PrintCollector,
    process_app::ProcessAppCollector,
    screen::ScreenCollector,
    usb::UsbCollector,
    window_title::WindowTitleCollector,
    CollectorCtx,
};
use personel_core::clock::SystemClock;
use personel_policy::engine::PolicyEngine;
use personel_queue::queue::{EventQueue, QueueConfig};

use crate::config::AgentConfig;

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

    // ── Collector context ─────────────────────────────────────────────────────
    let clock = Arc::new(SystemClock);
    let ctx = CollectorCtx {
        queue: Arc::clone(&queue),
        clock,
        pe_dek: None, // TODO: load from keystore after enrollment
        policy_rx,
        tenant_id,
        endpoint_id,
    };

    // ── Collector registry ────────────────────────────────────────────────────
    let mut registry = CollectorRegistry::new();
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

    registry.start_all(&ctx).await.context("start collectors")?;
    info!("all collectors started");

    // ── Transport ─────────────────────────────────────────────────────────────
    let (transport_stop_tx, transport_stop_rx) = oneshot::channel::<()>();
    let transport_queue = Arc::clone(&queue);

    // Only start the real transport if the agent is enrolled (has gateway config).
    if let Some(enroll) = &config.enrollment {
        use personel_transport::client::{BackoffConfig, ClientConfig, run_stream};

        // Load PEM-encoded client cert and key from disk.
        // The files are DPAPI-sealed on Windows; in dev mode they may be plain PEM.
        let load_result = (|| -> anyhow::Result<(Vec<u8>, Vec<u8>)> {
            let cert_pem = std::fs::read(&enroll.cert_path)
                .with_context(|| format!("read client cert: {}", enroll.cert_path.display()))?;
            let key_pem = std::fs::read(&enroll.key_path)
                .with_context(|| format!("read client key: {}", enroll.key_path.display()))?;
            Ok((cert_pem, key_pem))
        })();

        match load_result {
            Ok((cert_pem, key_pem)) => {
                let gateway_url = enroll.gateway_url.clone();
                let transport_cfg = ClientConfig {
                    gateway_url,
                    client_cert_pem: cert_pem,
                    client_key_pem: key_pem,
                    tenant_ca_pem: None, // TODO Phase 2: load tenant CA from config
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

    info!("personel-agent stopped");
    Ok(())
}
