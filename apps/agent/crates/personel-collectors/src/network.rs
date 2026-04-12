//! Network flow / DNS collector.
//!
//! # Implementation status: synthetic-stub
//!
//! **WFP decision**: WFP (Windows Filtering Platform) user-mode callout
//! registration via `FwpmEngineOpen0` + `FwpmCalloutAdd0` + `FwpmNetEventSubscribe0`
//! requires driver-signed filter objects and significant Win32 surface area.
//! This is deferred to Phase 2.
//!
//! // Phase 2: WFP real callout — replace synthetic emit below with:
//! //   1. FwpmEngineOpen0  → engine handle
//! //   2. FwpmSessionCreateEnumHandle0 + FwpmNetEventEnum2 loop  → flow records
//! //   3. ETW Microsoft-Windows-DNS-Client provider for DNS events
//! //   4. Aggregate (pid, dst_ip, dst_port, proto) per 60-second windows
//! //   5. Emit network.flow_summary, network.dns_query, network.tls_sni
//!
//! For Sprint C this collector emits one synthetic `network.flow_summary` and
//! one `network.dns_query` event on startup to exercise the queue path, then
//! parks until shutdown.
//!
//! # Platform support
//!
//! Compiles on macOS/Linux; no Windows-specific code in this file.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::info;

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Network flow summary collector.
///
/// **Sprint C status: synthetic-stub.** Phase 2 will implement WFP
/// user-mode event subscription and ETW DNS-Client provider decoding.
#[derive(Default)]
pub struct NetworkCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
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

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            healthy.store(true, Ordering::Relaxed);
            info!("network collector: started (synthetic-stub — Phase 2: WFP + ETW DNS)");

            // Emit synthetic boot events to exercise the queue path.
            emit_synthetic_boot(&ctx, &events, &drops);

            // Park until shutdown.
            let _ = stop_rx.await;
            info!("network collector: stopped");
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: "synthetic-stub: Phase 2 WFP+ETW DNS pending".into(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Synthetic boot events
// ──────────────────────────────────────────────────────────────────────────────

/// Emits synthetic boot events to exercise the queue path.
///
/// Both events are marked `"synthetic":true` so dashboards can filter them.
/// Phase 2 replaces these with real WFP / ETW DNS events.
fn emit_synthetic_boot(ctx: &CollectorCtx, events: &Arc<AtomicU64>, drops: &Arc<AtomicU64>) {
    let now = ctx.clock.now_unix_nanos();

    // Phase 2: WFP real callout replaces this synthetic network.flow_summary.
    enqueue(
        ctx,
        EventKind::NetworkFlowSummary,
        Priority::Low,
        r#"{"synthetic":true,"pid":0,"dst_ip":"0.0.0.0","dst_port":0,"proto":"tcp","bytes_out":0,"bytes_in":0,"window_sec":60}"#,
        now,
        events,
        drops,
    );

    // Phase 2: ETW Microsoft-Windows-DNS-Client replaces this synthetic dns_query.
    enqueue(
        ctx,
        EventKind::NetworkDnsQuery,
        Priority::Low,
        r#"{"synthetic":true,"pid":0,"query":"<boot-sentinel>","response_ip":"0.0.0.0"}"#,
        now,
        events,
        drops,
    );
}

fn enqueue(
    ctx: &CollectorCtx,
    kind: EventKind,
    priority: Priority,
    payload: &str,
    now: i64,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(&id, kind.as_str(), priority, now, now, payload.as_bytes()) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(_) => {
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}
