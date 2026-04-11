//! Print job metadata collector.
//!
//! Subscribes to print spooler events to capture `print.job_submitted`
//! metadata (printer name, document name, page count, byte size, user).
//!
//! # TODO (Phase 1 implementation)
//!
//! - Use ETW `Microsoft-Windows-PrintService` provider or WMI
//!   `Win32_PrintJob` eventing to receive print job events.
//! - No document content is captured, only metadata.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Print job metadata collector (stub).
#[derive(Default)]
pub struct PrintCollector {
    healthy: Arc<AtomicBool>,
}

impl PrintCollector {
    /// Creates a new [`PrintCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for PrintCollector {
    fn name(&self) -> &'static str {
        "print"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["print.job_submitted"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("print collector started (stub — spooler not wired)");
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
            status: "stub — spooler not wired".into(),
        }
    }
}
