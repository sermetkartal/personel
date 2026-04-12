//! File system event collector.
//!
//! # Implementation status: synthetic-stub
//!
//! A production implementation would use ETW `Microsoft-Windows-Kernel-FileIO`
//! provider: `StartTraceW` → `EnableTraceEx2(GUID)` → `ProcessTrace` on a
//! dedicated OS thread → decode `EVENT_RECORD` MOF buffers to extract
//! `FileName`, `IrpPtr`, `FileObject`, and operation type fields.
//!
//! That requires `StartTraceW` / `ProcessTrace` WinAPI calls, which in turn
//! need the `Win32_System_Diagnostics_Etw` feature and real Windows kernel
//! provider access. The fully-generic ETW session wrapper in
//! `personel_os::etw` is a skeleton for that work.
//!
//! For Sprint C this collector emits a single synthetic `file.created` event
//! on startup (to prove the queue path compiles and works end-to-end) then
//! parks waiting for the shutdown signal. It is clearly labelled as a
//! synthetic stub so operators know no real file events are collected yet.
//!
//! # Phase 2 plan
//!
//! 1. Implement `EtwSession::start()` in `personel_os::windows::etw`.
//! 2. Register `Microsoft-Windows-Kernel-FileIO` provider GUID.
//! 3. Decode `FileIo_Create`, `FileIo_Write`, `FileIo_Delete`, `FileIo_Rename`
//!    event classes from the `EVENT_RECORD` MOF buffer.
//! 4. Replace the synthetic emit below with the real event loop.
//!
//! # Platform support
//!
//! Compiles on macOS/Linux (`cargo check` passes); parks on non-Windows.

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

/// File system event collector.
///
/// **Sprint C status: synthetic-stub.** Emits one boot-time `file.created`
/// synthetic event so the queue pipeline is exercised. Real ETW FileIO
/// implementation is Phase 2.
///
/// See module doc for Phase 2 plan.
#[derive(Default)]
pub struct FileSystemCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
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

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            healthy.store(true, Ordering::Relaxed);
            info!("file_system collector: started (synthetic-stub — Phase 2: ETW FileIO)");

            // Emit one synthetic boot event so the queue pipeline is exercised.
            emit_synthetic_boot(&ctx, &events, &drops);

            // Park until shutdown.
            let _ = stop_rx.await;
            info!("file_system collector: stopped");
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: "synthetic-stub: Phase 2 ETW FileIO pending".into(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Synthetic boot event
// ──────────────────────────────────────────────────────────────────────────────

/// Emits a single synthetic `file.created` event to exercise the queue path.
///
/// The payload clearly marks the event as synthetic so ops dashboards can
/// filter it out.  Phase 2 replaces this with real ETW events.
fn emit_synthetic_boot(ctx: &CollectorCtx, events: &Arc<AtomicU64>, drops: &Arc<AtomicU64>) {
    // Phase 2: WFP + ETW FileIO real events — replace this synthetic emit.
    let payload = r#"{"synthetic":true,"op":"created","path":"<boot-sentinel>","pid":0}"#;
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        EventKind::FileCreated.as_str(),
        Priority::Low,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(_) => {
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}
