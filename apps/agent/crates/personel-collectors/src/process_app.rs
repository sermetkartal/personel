//! Process / app usage collector.
//!
//! Uses ETW (Event Tracing for Windows) user-mode sessions to capture
//! `Microsoft-Windows-Kernel-Process` provider events for process start/stop
//! and foreground process changes.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Wire `personel_os::windows::etw::EtwSession` to receive
//!   `MSNT_SystemTrace` / `Microsoft-Windows-Kernel-Process` events.
//! - Decode `ProcessStart` / `ProcessStop` from ETW MOF buffer.
//! - Resolve PE signer via Authenticode (`WinVerifyTrust`).
//! - Emit `process.start`, `process.stop`, `process.foreground_change`.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Process and application usage collector (ETW-based stub).
#[derive(Default)]
pub struct ProcessAppCollector {
    healthy: Arc<AtomicBool>,
}

impl ProcessAppCollector {
    /// Creates a new [`ProcessAppCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ProcessAppCollector {
    fn name(&self) -> &'static str {
        "process_app"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["process.start", "process.stop", "process.foreground_change"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("process_app collector started (stub — ETW not wired)");
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
            status: "stub — ETW not wired".into(),
        }
    }
}
