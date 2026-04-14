//! Keystroke metadata + encrypted content collector.
//!
//! Two logical sub-collectors share this module:
//!
//! 1. **`KeystrokeMetaCollector`** — emits `keystroke.window_stats` (counts
//!    only, no content). Enabled when `policy.collectors.keystroke_meta`.
//!
//! 2. **`KeystrokeContentCollector`** — captures raw keystroke content,
//!    encrypts it in-place with the PE-DEK, and emits
//!    `keystroke.content_encrypted`. Enabled **only** when
//!    `policy.collectors.keystroke_content && policy.keystroke_content_enabled`
//!    — the double gate enforces ADR 0013.
//!
//! # ADR 0013 — keystroke content default-OFF
//!
//! Clear-text keystrokes **never** enter the event queue. Two conditions must
//! both be true for any content-mode bytes to be produced:
//!
//! 1. `policy.keystroke_content_enabled == true`  (DLP ceremony was completed)
//! 2. `ctx.pe_dek.is_some()`                      (PE-DEK was provisioned)
//!
//! When either condition is false, the collector runs in metadata-only mode
//! regardless of collector type. The `Zeroizing<Vec<u8>>` per-window plaintext
//! buffers are wiped on flush and on drop.
//!
//! # Crypto envelope (key-hierarchy.md)
//!
//! ```text
//! ciphertext = AES-256-GCM(key=PE-DEK, nonce=random96,
//!                           aad=endpoint_id(16)||seq(8 BE),
//!                           plaintext=keystroke_buffer)
//! ```
//!
//! The plaintext buffer is a `Zeroizing<Vec<u8>>` and is dropped immediately
//! after `encrypt()` returns — never written to the queue in plaintext.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full WH_KEYBOARD_LL hook loop.
//! Other platforms: collectors park until stopped (no-op). This allows
//! `cargo check` to pass on macOS/Linux.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Flush interval
// ──────────────────────────────────────────────────────────────────────────────

/// How often per-window keystroke counts / encrypted buffers are flushed.
#[cfg(target_os = "windows")]
const FLUSH_SECS: u64 = 60;
/// Maximum plaintext buffer per window before a forced flush (bytes).
#[cfg(target_os = "windows")]
const MAX_BUFFER_BYTES: usize = 4096;

// ──────────────────────────────────────────────────────────────────────────────
// KeystrokeMetaCollector
// ──────────────────────────────────────────────────────────────────────────────

/// Keystroke metadata (counts-only) collector.
#[derive(Default)]
pub struct KeystrokeMetaCollector {
    healthy: Arc<AtomicBool>,
    events:  Arc<AtomicU64>,
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
    fn name(&self) -> &'static str { "keystroke_meta" }

    fn event_types(&self) -> &'static [&'static str] {
        &["keystroke.window_stats"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events  = Arc::clone(&self.events);

        let task = tokio::task::spawn_blocking(move || {
            run_meta_loop(ctx, healthy, events, stop_rx);
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy:           self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last:  0,
            status:            String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// KeystrokeContentCollector
// ──────────────────────────────────────────────────────────────────────────────

/// Keystroke content collector.
///
/// **ADR 0013**: Only activates in content mode when
/// `policy.keystroke_content_enabled && ctx.pe_dek.is_some()`.
/// Falls back to metadata-only silently otherwise.
/// Clear-text keystrokes are **never** written to the queue.
#[derive(Default)]
pub struct KeystrokeContentCollector {
    healthy: Arc<AtomicBool>,
    events:  Arc<AtomicU64>,
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
    fn name(&self) -> &'static str { "keystroke_content" }

    fn event_types(&self) -> &'static [&'static str] {
        &["keystroke.content_encrypted"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events  = Arc::clone(&self.events);

        let task = tokio::task::spawn_blocking(move || {
            run_content_loop(ctx, healthy, events, stop_rx);
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy:           self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last:  0,
            status:            String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

fn run_meta_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    stop_rx: oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    windows::run_meta(ctx, healthy, events, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events);
        info!("keystroke_meta: keyboard hook not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

fn run_content_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    stop_rx: oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    windows::run_content(ctx, healthy, events, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events);
        info!("keystroke_content: keyboard hook not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows {
    use std::collections::HashMap;
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::{mpsc, Arc};
    use std::time::{Duration, Instant};

    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};
    use zeroize::Zeroizing;

    use personel_core::error::{AgentError, Result};
    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;
    use personel_crypto::envelope;

    use super::{FLUSH_SECS, MAX_BUFFER_BYTES};
    use crate::CollectorCtx;

    // ── Meta loop ─────────────────────────────────────────────────────────────

    pub fn run_meta(
        ctx: CollectorCtx,
        healthy: Arc<std::sync::atomic::AtomicBool>,
        events: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("keystroke_meta: starting");

        let (tx, rx) = mpsc::channel::<personel_os::input::KeyEvent>();
        let _hook = match personel_os::input::install_keyboard_hook(tx) {
            Ok(h) => {
                healthy.store(true, Ordering::Relaxed);
                info!("keystroke_meta: WH_KEYBOARD_LL hook installed");
                Some(h)
            }
            Err(e) => {
                error!(error = %e, "keystroke_meta: hook install failed");
                healthy.store(false, Ordering::Relaxed);
                None
            }
        };

        let mut counts: HashMap<String, u64> = HashMap::new();
        let mut total_keys_since_flush: u64 = 0;
        let mut last_flush = Instant::now();
        let flush_dur = Duration::from_secs(FLUSH_SECS);
        let mut next_progress_log = Instant::now() + Duration::from_secs(10);

        loop {
            if stop_rx.try_recv().is_ok() {
                break;
            }

            // Drain events.
            while let Ok(ev) = rx.try_recv() {
                // Key-down only (LLKHF_UP = bit 7 of flags).
                if ev.flags & 0x80 == 0 {
                    let window = personel_os::input::foreground_window_info()
                        .map(|f| f.title)
                        .unwrap_or_default();
                    *counts.entry(window).or_insert(0) += 1;
                    total_keys_since_flush += 1;
                }
            }

            // Periodic progress log — lets operators confirm the hook callback
            // is firing without waiting a full 60 s flush interval.
            if Instant::now() >= next_progress_log {
                info!(
                    keys_since_flush = total_keys_since_flush,
                    unique_windows = counts.len(),
                    seconds_until_flush = flush_dur.saturating_sub(last_flush.elapsed()).as_secs(),
                    "keystroke_meta: progress"
                );
                next_progress_log = Instant::now() + Duration::from_secs(10);
            }

            if last_flush.elapsed() >= flush_dur {
                info!(
                    unique_windows = counts.len(),
                    total_keys = total_keys_since_flush,
                    "keystroke_meta: flush interval elapsed — draining counts"
                );
                if counts.is_empty() {
                    info!("keystroke_meta: flush skipped — no keys captured in last interval");
                }
                for (window, count) in counts.drain() {
                    if count > 0 {
                        flush_meta(&ctx, &window, count, FLUSH_SECS, &events);
                    }
                }
                total_keys_since_flush = 0;
                last_flush = Instant::now();
            }

            std::thread::sleep(Duration::from_millis(50));
        }

        // Final flush.
        for (window, count) in counts.drain() {
            if count > 0 {
                flush_meta(&ctx, &window, count, FLUSH_SECS, &events);
            }
        }
        info!("keystroke_meta: stopped");
    }

    fn flush_meta(
        ctx: &CollectorCtx,
        window: &str,
        count: u64,
        interval_sec: u64,
        events: &Arc<AtomicU64>,
    ) {
        let payload = format!(
            r#"{{"window_title":{:?},"count":{},"interval_sec":{}}}"#,
            window, count, interval_sec
        );
        let now = ctx.clock.now_unix_nanos();
        let id  = EventId::new_v7().to_bytes();
        match ctx.queue.enqueue(
            &id,
            EventKind::KeystrokeWindowStats.as_str(),
            Priority::Normal,
            now,
            now,
            payload.as_bytes(),
        ) {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
                info!(window, count, interval_sec, "keystroke_meta: flushed window_stats event");
            }
            Err(e) => warn!(error = %e, "keystroke_meta: queue error"),
        }
    }

    // ── Content loop (ADR 0013) ───────────────────────────────────────────────

    pub fn run_content(
        ctx: CollectorCtx,
        healthy: Arc<std::sync::atomic::AtomicBool>,
        events: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("keystroke_content: starting");

        // ── ADR 0013 double gate ──────────────────────────────────────────
        // Both must be true for content mode. If either is missing: metadata only.
        let pe_dek      = ctx.pe_dek.clone();
        let content_on  = pe_dek.is_some() && ctx.policy().keystroke_content_enabled;

        if !content_on {
            if pe_dek.is_none() {
                warn!("keystroke_content: PE-DEK not provisioned — \
                       ADR 0013 default-OFF. Parking collector (meta collector \
                       owns the sole WH_KEYBOARD_LL hook slot).");
            } else {
                info!("keystroke_content: policy.keystroke_content_enabled=false — \
                       ADR 0013 default-OFF. Parking collector (meta collector \
                       owns the sole WH_KEYBOARD_LL hook slot).");
            }
            // CRITICAL: do NOT install a second global WH_KEYBOARD_LL hook here.
            // The personel_os::input::KEY_EVENT_TX global is a single slot —
            // a second install would clobber the meta collector's Sender and
            // starve its receive loop (the bug that made both collectors emit
            // zero events in the Faz 2 bring-up). Just park until stopped.
            healthy.store(true, Ordering::Relaxed);
            let _ = stop_rx.blocking_recv();
            drop(ctx);
            drop(pe_dek);
            drop(events);
            info!("keystroke_content: stopped (parked)");
            return;
        }

        let (tx, rx) = mpsc::channel::<personel_os::input::KeyEvent>();
        let _hook = match personel_os::input::install_keyboard_hook(tx) {
            Ok(h) => {
                healthy.store(true, Ordering::Relaxed);
                info!("keystroke_content: WH_KEYBOARD_LL hook installed (content_mode=true)");
                Some(h)
            }
            Err(e) => {
                error!(error = %e, "keystroke_content: hook install failed");
                healthy.store(false, Ordering::Relaxed);
                None
            }
        };

        let mut seq: u64 = 0;
        // Plaintext buffers: only populated in content mode; Zeroizing → wiped on drop.
        let mut ptxt_bufs: HashMap<String, Zeroizing<Vec<u8>>> = HashMap::new();
        let mut counts:    HashMap<String, u64> = HashMap::new();
        let mut last_flush = Instant::now();
        let flush_dur = Duration::from_secs(FLUSH_SECS);

        loop {
            if stop_rx.try_recv().is_ok() {
                break;
            }

            while let Ok(ev) = rx.try_recv() {
                // Key-down only.
                if ev.flags & 0x80 != 0 {
                    continue;
                }
                let window = personel_os::input::foreground_window_info()
                    .map(|f| f.title)
                    .unwrap_or_default();

                *counts.entry(window.clone()).or_insert(0) += 1;

                if content_on {
                    // ADR 0013: VK code bytes buffered in Zeroizing<Vec<u8>>.
                    // Never leave this buffer or the encrypt() path as clear text.
                    let buf = ptxt_bufs
                        .entry(window)
                        .or_insert_with(|| Zeroizing::new(Vec::new()));
                    buf.push(ev.vk_code as u8);

                    // Size-based flush trigger.
                    if buf.len() >= MAX_BUFFER_BYTES {
                        last_flush = Instant::now()
                            .checked_sub(flush_dur)
                            .unwrap_or_else(Instant::now);
                    }
                }
            }

            if last_flush.elapsed() >= flush_dur {
                if content_on {
                    let dek = pe_dek.as_ref().expect("pe_dek Some when content_on");
                    let ep  = ctx.endpoint_id.to_bytes();
                    for (window, buf) in ptxt_bufs.drain() {
                        if buf.is_empty() {
                            drop(buf); // zeroize
                            continue;
                        }
                        let aad = envelope::build_keystroke_aad(&ep, seq);
                        seq += 1;
                        match envelope::encrypt(dek, aad, buf.as_slice()) {
                            Ok(env) => {
                                // ADR 0013: plaintext buf is Zeroizing — wiped on drop.
                                drop(buf);
                                flush_content(&ctx, &window, &env, seq - 1, &events);
                            }
                            Err(e) => {
                                drop(buf); // always wipe on error
                                error!(error = %e, window = window,
                                       "keystroke_content: encryption failed — buf wiped");
                            }
                        }
                    }
                } else {
                    // Metadata-only fallback.
                    for (window, count) in counts.drain() {
                        if count > 0 {
                            flush_meta_fallback(&ctx, &window, count, FLUSH_SECS, &events);
                        }
                    }
                }
                last_flush = Instant::now();
            }

            std::thread::sleep(Duration::from_millis(50));
        }

        // Final flush on shutdown.
        if content_on {
            let dek = pe_dek.as_ref().expect("pe_dek Some when content_on");
            let ep  = ctx.endpoint_id.to_bytes();
            for (window, buf) in ptxt_bufs.drain() {
                if buf.is_empty() {
                    drop(buf);
                    continue;
                }
                let aad = envelope::build_keystroke_aad(&ep, seq);
                seq += 1;
                match envelope::encrypt(dek, aad, buf.as_slice()) {
                    Ok(env) => {
                        drop(buf);
                        flush_content(&ctx, &window, &env, seq - 1, &events);
                    }
                    Err(_) => { drop(buf); }
                }
            }
        } else {
            for (window, count) in counts.drain() {
                if count > 0 {
                    flush_meta_fallback(&ctx, &window, count, FLUSH_SECS, &events);
                }
            }
        }
        info!("keystroke_content: stopped");
    }

    fn flush_content(
        ctx: &CollectorCtx,
        window: &str,
        env: &envelope::CipherEnvelope,
        seq: u64,
        events: &Arc<AtomicU64>,
    ) {
        // Payload: nonce + ciphertext hex-encoded JSON (interim; proto is transport concern).
        // ADR 0013: no plaintext field in this event.
        let payload = format!(
            r#"{{"window_title":{:?},"seq":{},"nonce":"{}","ciphertext":"{}","aad":"{}"}}"#,
            window,
            seq,
            hex::encode(env.nonce),
            hex::encode(&env.ciphertext),
            hex::encode(&env.aad),
        );
        let now = ctx.clock.now_unix_nanos();
        let id  = EventId::new_v7().to_bytes();
        match ctx.queue.enqueue(
            &id,
            EventKind::KeystrokeContentEncrypted.as_str(),
            Priority::High,
            now,
            now,
            payload.as_bytes(),
        ) {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
                debug!(window, seq, "keystroke_content: encrypted blob enqueued");
            }
            Err(e) => warn!(error = %e, "keystroke_content: queue error"),
        }
    }

    fn flush_meta_fallback(
        ctx: &CollectorCtx,
        window: &str,
        count: u64,
        interval_sec: u64,
        events: &Arc<AtomicU64>,
    ) {
        let payload = format!(
            r#"{{"window_title":{:?},"count":{},"interval_sec":{}}}"#,
            window, count, interval_sec
        );
        let now = ctx.clock.now_unix_nanos();
        let id  = EventId::new_v7().to_bytes();
        match ctx.queue.enqueue(
            &id,
            EventKind::KeystrokeWindowStats.as_str(),
            Priority::Normal,
            now,
            now,
            payload.as_bytes(),
        ) {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
                debug!(window, count, "keystroke_content (meta fallback): flushed");
            }
            Err(e) => warn!(error = %e, "keystroke_content meta fallback: queue error"),
        }
    }
}
