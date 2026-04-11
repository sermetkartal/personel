//! Network flow summary collector.
//!
//! Uses WFP (Windows Filtering Platform) user-mode callout or ETW
//! `Microsoft-Windows-TCPIP` provider to capture network flows and
//! DNS/TLS-SNI events.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Implement WFP user-mode event subscription via `FwpmNetEventSubscribe`.
//! - Aggregate flows per (pid, dest_ip, dest_port, protocol) over 60 s windows.
//! - Emit `network.flow_summary`, `network.dns_query`, `network.tls_sni`.
//! - NO deep packet inspection — header metadata only per MVP scope.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Network flow summary collector (WFP user-mode stub).
#[derive(Default)]
pub struct NetworkCollector {
    healthy: Arc<AtomicBool>,
}

impl NetworkCollector {
    /// Creates a new [`NetworkCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for NetworkCollector {
    fn name(&self) -> &'static str {
        "network"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["network.flow_summary", "network.dns_query", "network.tls_sni"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("network collector started (stub — WFP not wired)");
            healthy.store(true, Ordering::Relaxed);
            let _ = stop_rx.await;
        });
        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: 0,
            drops_since_last: 0,
            status: "stub — WFP not wired".into(),
        }
    }
}
