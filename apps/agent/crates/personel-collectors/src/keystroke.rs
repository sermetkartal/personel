//! Keystroke metadata + encrypted content collector.
//!
//! Two logical sub-collectors share this module:
//!
//! 1. **`KeystrokeMetaCollector`** — emits `keystroke.window_stats` (counts
//!    only, no content). Enabled when `policy.collectors.keystroke_meta`.
//!
//! 2. **`KeystrokeContentCollector`** — captures raw keystroke content,
//!    encrypts it in-place with the PE-DEK, and emits
//!    `keystroke.content_encrypted`. Enabled when
//!    `policy.collectors.keystroke_content` AND `policy.keystroke_content_enabled`.
//!
//! # Crypto envelope (concrete per key-hierarchy.md)
//!
//! ```text
//! ciphertext = AES-256-GCM(key=PE-DEK, nonce=random96,
//!                           aad=endpoint_id(16)||seq(8 BE),
//!                           plaintext=keystroke_buffer)
//! ```
//!
//! The plaintext buffer is zeroized immediately after encryption.
//!
//! # TODO (Phase 1)
//!
//! - Hook `SetWindowsHookEx(WH_KEYBOARD_LL)` via `personel-os` to receive
//!   keystrokes.
//! - Per-window buffer management with `policy.keystroke.max_buffer_bytes`.
//! - Upload ciphertext blob to MinIO (transport layer concern; collector just
//!   produces the envelope + blob path).
//! - Exclude password managers per `policy.keystroke.exclude_exe_names`.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};
use zeroize::Zeroizing;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Metadata collector
// ──────────────────────────────────────────────────────────────────────────────

/// Keystroke metadata (counts-only) collector.
#[derive(Default)]
pub struct KeystrokeMetaCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
}

impl KeystrokeMetaCollector {
    /// Creates a new [`KeystrokeMetaCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for KeystrokeMetaCollector {
    fn name(&self) -> &'static str {
        "keystroke_meta"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["keystroke.window_stats"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);

        let task = tokio::spawn(async move {
            // TODO: install WH_KEYBOARD_LL hook via personel_os::windows::input
            // and accumulate per-window keystroke counts in a HashMap<hwnd, Stats>.
            // Flush on policy.keystroke.flush_interval_seconds tick.
            info!("keystroke_meta collector started (stub)");
            let mut ticker = tokio::time::interval(Duration::from_secs(
                ctx.policy().screenshot.interval_seconds.max(60) as u64,
            ));
            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        debug!("keystroke_meta: flush tick (stub — no hook installed yet)");
                        healthy.store(true, Ordering::Relaxed);
                    }
                    _ = &mut stop_rx => {
                        info!("keystroke_meta collector: stop requested");
                        break;
                    }
                }
            }
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: 0,
            status: "stub".into(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Content collector (concrete crypto envelope)
// ──────────────────────────────────────────────────────────────────────────────

/// Keystroke content collector — captures and encrypts raw keystrokes.
///
/// The `pe_dek` field must be `Some` for this collector to produce
/// `keystroke.content_encrypted` events. If it is `None`, the collector
/// starts but produces no events.
#[derive(Default)]
pub struct KeystrokeContentCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
}

impl KeystrokeContentCollector {
    /// Creates a new [`KeystrokeContentCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for KeystrokeContentCollector {
    fn name(&self) -> &'static str {
        "keystroke_content"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["keystroke.content_encrypted"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let pe_dek = ctx.pe_dek.clone();

        let task = tokio::spawn(async move {
            if pe_dek.is_none() {
                warn!("keystroke_content: PE-DEK not provided; collector will produce no events");
            }

            // TODO: install WH_KEYBOARD_LL hook. Accumulate plaintext in a
            //       Zeroizing<Vec<u8>> per-window buffer. On flush:
            //
            //   let aad = personel_crypto::envelope::build_keystroke_aad(
            //       &ctx.endpoint_id.to_bytes(), seq,
            //   );
            //   let envelope = personel_crypto::envelope::encrypt(
            //       pe_dek.as_ref().unwrap(), aad, &plaintext_buf,
            //   )?;
            //   // plaintext_buf.zeroize() — already Zeroizing<Vec<u8>>, handled on drop.
            //   // Upload ciphertext to MinIO (transport responsibility).
            //   // Enqueue keystroke.content_encrypted metadata event.

            info!("keystroke_content collector started (stub)");
            let mut ticker = tokio::time::interval(Duration::from_secs(30));
            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        healthy.store(true, Ordering::Relaxed);
                        debug!("keystroke_content: flush tick (stub)");
                    }
                    _ = &mut stop_rx => {
                        info!("keystroke_content collector: stop requested");
                        break;
                    }
                }
            }
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: 0,
            status: "stub".into(),
        }
    }
}
