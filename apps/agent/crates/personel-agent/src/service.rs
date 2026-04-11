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
    // TODO: wire personel_transport::client::run_stream here.
    let (transport_stop_tx, transport_stop_rx) = oneshot::channel::<()>();
    let _transport_task = tokio::spawn(async move {
        // TODO: wire personel_transport::client::run_stream here.
        // Stub: wait for stop signal.
        let _ = transport_stop_rx.await;
        info!("transport stopped");
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

    info!("personel-agent stopped");
    Ok(())
}
