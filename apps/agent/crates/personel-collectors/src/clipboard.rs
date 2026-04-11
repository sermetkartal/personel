//! Clipboard monitor collector.
//!
//! Listens for clipboard changes via `AddClipboardFormatListener` and emits
//! `clipboard.metadata` (always) and `clipboard.content_encrypted` (if enabled).
//!
//! # TODO (Phase 1 implementation)
//!
//! - Create a hidden message-only window (`HWND_MESSAGE`) and register it
//!   with `AddClipboardFormatListener`.
//! - On `WM_CLIPBOARDUPDATE`: enumerate formats, determine kind
//!   (text / image / files / other), emit `clipboard.metadata`.
//! - If `policy.clipboard.content_enabled`: read CF_UNICODETEXT, encrypt with
//!   a per-clipboard-event DEK (or reuse PE-DEK for content events), upload to
//!   MinIO, emit `clipboard.content_encrypted`.
//! - This requires a dedicated OS thread (message loop); bridge to async via
//!   `tokio::sync::mpsc`.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Clipboard monitor collector (stub).
#[derive(Default)]
pub struct ClipboardCollector {
    healthy: Arc<AtomicBool>,
}

impl ClipboardCollector {
    /// Creates a new [`ClipboardCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ClipboardCollector {
    fn name(&self) -> &'static str {
        "clipboard"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["clipboard.metadata", "clipboard.content_encrypted"]
    }

    async fn start(&self, _ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let task = tokio::spawn(async move {
            info!("clipboard collector started (stub — WM_CLIPBOARDUPDATE not wired)");
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
            status: "stub — WM_CLIPBOARDUPDATE not wired".into(),
        }
    }
}
