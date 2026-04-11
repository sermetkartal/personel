//! Screenshot / screen clip capture collector.
//!
//! Captures desktop frames using DXGI Desktop Duplication API via
//! `personel_os::windows::capture`. Frames are encoded as WebP and uploaded
//! to MinIO. Metadata is emitted as `screenshot.captured` events.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Wire `personel_os::windows::capture::DxgiCapture` for interval captures.
//! - Enforce `policy.screenshot.interval_seconds` from the scheduler tick.
//! - Apply blur to exe names in `policy.screenshot.blur_exe_names`.
//! - Implement screen clip (video) capture (≤ 30 s, DXGI → H.264/VP8).
//! - Upload sealed WebP blob to MinIO and emit `screenshot.captured`.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Screenshot capture collector (DXGI-based stub).
#[derive(Default)]
pub struct ScreenCollector {
    healthy: Arc<AtomicBool>,
}

impl ScreenCollector {
    /// Creates a new [`ScreenCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ScreenCollector {
    fn name(&self) -> &'static str {
        "screen"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["screenshot.captured", "screenclip.captured"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("screen collector started (stub — DXGI not wired)");
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
            status: "stub — DXGI not wired".into(),
        }
    }
}
