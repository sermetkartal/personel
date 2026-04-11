//! USB device event collector.
//!
//! Uses `SetupDi` / WMI `Win32_DeviceChangeEvent` or device notification
//! via `RegisterDeviceNotification` to detect USB attach/remove events.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Register for `DBT_DEVICEARRIVAL` / `DBT_DEVICEREMOVECOMPLETE` via
//!   `RegisterDeviceNotification` on a message-only window.
//! - Query device VID/PID/serial via `SetupDiGetDeviceRegistryProperty`.
//! - Hash serial number (SHA-256) before emitting per privacy requirements.
//! - Evaluate USB policy (`policy.usb_rules`) and emit
//!   `usb.mass_storage_policy_block` if appropriate.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// USB device event collector (stub).
#[derive(Default)]
pub struct UsbCollector {
    healthy: Arc<AtomicBool>,
}

impl UsbCollector {
    /// Creates a new [`UsbCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for UsbCollector {
    fn name(&self) -> &'static str {
        "usb"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["usb.device_attached", "usb.device_removed", "usb.mass_storage_policy_block"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("usb collector started (stub — SetupDi not wired)");
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
            status: "stub — SetupDi not wired".into(),
        }
    }
}
