//! File system event collector.
//!
//! Uses ETW `Microsoft-Windows-Kernel-FileIO` provider to capture
//! file create/read/write/delete/rename/copy events.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Wire ETW `FileIO` provider events via `personel_os::windows::etw`.
//! - Filter events using `policy.collectors.file`.
//! - Resolve full path from file object pointer (requires kernel name lookup).
//! - Emit `file.created`, `file.read`, `file.written`, `file.deleted`,
//!   `file.renamed`.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// File system event collector (ETW-based stub).
#[derive(Default)]
pub struct FileSystemCollector {
    healthy: Arc<AtomicBool>,
}

impl FileSystemCollector {
    /// Creates a new [`FileSystemCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for FileSystemCollector {
    fn name(&self) -> &'static str {
        "file_system"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["file.created", "file.read", "file.written", "file.deleted", "file.renamed", "file.copied"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("file_system collector started (stub — ETW FileIO not wired)");
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
            status: "stub — ETW FileIO not wired".into(),
        }
    }
}
