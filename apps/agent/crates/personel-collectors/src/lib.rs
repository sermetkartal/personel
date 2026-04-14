//! `personel-collectors` — collector trait, registry, and scheduler.
//!
//! Each collector is a unit of work that monitors one aspect of endpoint
//! activity and enqueues [`EventEnvelope`]s into the local queue. Collectors
//! never call the transport directly.
//!
//! # Collector contract
//!
//! 1. A collector MUST NOT call the transport directly.
//! 2. A collector MUST respect the backpressure signal (`queue nearly full →
//!    drop lowest-priority samples for its class`).
//! 3. A collector MUST zeroize any intermediate buffers carrying plaintext
//!    sensitive content (keystrokes, clipboard, screenshots).
//! 4. No panic may escape from a running collector. All errors flow through
//!    `Result` and are logged via `tracing::error!`.

// Platform-specific collectors (Windows ETW, clipboard, USB) require direct
// Win32 calls which need `unsafe`. The non-Windows stub path stays safe by
// construction. Per-function allow() is noisy across ~30 call sites.
#![allow(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod bluetooth_devices;
pub mod browser_history;
pub mod clipboard;
pub mod clipboard_content_redacted;
pub mod cloud_storage;
pub mod device_status;
pub mod email_metadata;
pub mod file_system;
pub mod firefox_history;
pub mod geo_ip;
pub mod idle;
pub mod keystroke;
pub mod mtp_devices;
pub mod network;
pub mod office_activity;
pub mod print;
pub mod process_app;
pub mod screen;
pub mod system_events;
pub mod usb;
pub mod user_sid;
pub mod window_title;
pub mod window_url_extraction;

#[cfg(test)]
mod tests_logic;

use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::watch;
use tokio::task::JoinHandle;
use tracing::{debug, error, info, warn};

use personel_core::clock::Clock;
use personel_core::error::Result;
use personel_core::ids::{EndpointId, TenantId};
use personel_core::throttle::ThrottleState;
use personel_crypto::Aes256Key;
use personel_policy::engine::PolicyView;
use personel_queue::queue::EventQueue;

// ──────────────────────────────────────────────────────────────────────────────
// HealthSnapshot
// ──────────────────────────────────────────────────────────────────────────────

/// A point-in-time health report from a single collector.
#[derive(Debug, Clone, Default)]
pub struct HealthSnapshot {
    /// Whether the collector is currently running without errors.
    pub healthy: bool,
    /// Number of events enqueued since last report.
    pub events_since_last: u64,
    /// Number of events dropped due to backpressure since last report.
    pub drops_since_last: u64,
    /// Human-readable status message (empty if healthy).
    pub status: String,
}

// ──────────────────────────────────────────────────────────────────────────────
// CollectorCtx — everything a collector needs, injected at start time
// ──────────────────────────────────────────────────────────────────────────────

/// Context injected into every collector at startup.
///
/// Provides testable seams for all external dependencies.
///
/// # Throttle awareness
///
/// `throttle` is a lock-free handle every collector CAN (but is not yet
/// required to) consult at the start of each tick. Faz 4 Wave 1 #32 wires
/// the field into the context and the monitor task but retrofits ZERO
/// existing collectors — all 23 current collectors remain *throttle-unaware*.
/// The `EventQueue` priority-based eviction continues to handle overload at
/// the tail. Collectors that are expensive enough to justify tick-rate
/// halving (e.g. future ML-in-agent workloads, screen capture) will call
/// [`ThrottleState::current_state`](personel_core::throttle::ThrottleState::current_state)
/// and [`ThrottleState::should_low_priority_skip`](personel_core::throttle::ThrottleState::should_low_priority_skip)
/// directly as they are retrofitted in follow-up sprints.
#[derive(Clone)]
pub struct CollectorCtx {
    /// Shared access to the local event queue (the ONLY write path for collectors).
    pub queue: Arc<EventQueue>,
    /// Wall clock abstraction (use `FakeClock` in tests).
    pub clock: Arc<dyn Clock>,
    /// PE-DEK for keystroke content encryption. `None` if content is disabled.
    pub pe_dek: Option<Arc<Aes256Key>>,
    /// Subscribe to receive updated `PolicyView` when a new bundle is pushed.
    pub policy_rx: watch::Receiver<Arc<PolicyView>>,
    /// Tenant that owns this endpoint.
    pub tenant_id: TenantId,
    /// This endpoint's unique identifier.
    pub endpoint_id: EndpointId,
    /// Shared self-throttle state fed by the agent's 5-second resource monitor.
    /// Lock-free on the read path. See the `Throttle awareness` note above.
    pub throttle: Arc<ThrottleState>,
}

impl CollectorCtx {
    /// Returns the current `PolicyView` without blocking.
    #[must_use]
    pub fn policy(&self) -> Arc<PolicyView> {
        self.policy_rx.borrow().clone()
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// CollectorHandle — returned by start(), used to manage the running collector
// ──────────────────────────────────────────────────────────────────────────────

/// Handle to a running collector task.
pub struct CollectorHandle {
    /// The name of the collector (for logging).
    pub name: &'static str,
    /// The background task. Abort it to stop the collector.
    pub task: JoinHandle<()>,
    /// Shutdown signal sender. Send `()` to request graceful stop.
    pub stop_tx: tokio::sync::oneshot::Sender<()>,
}

// ──────────────────────────────────────────────────────────────────────────────
// Collector trait
// ──────────────────────────────────────────────────────────────────────────────

/// The core trait every collector must implement.
///
/// # Implementation notes
///
/// - `start` spawns a tokio task and returns a [`CollectorHandle`]. It must
///   not block; all polling/blocking work goes into the task.
/// - `reload_policy` is called after `PolicyEngine::apply` completes. If the
///   collector has internal state derived from policy (e.g., screenshot
///   interval), it should update that state here. It must not block.
/// - `health` is called on the 30-second health tick. It must be cheap.
#[async_trait]
pub trait Collector: Send + Sync {
    /// A static human-readable name for this collector (e.g., `"idle"`).
    fn name(&self) -> &'static str;

    /// The event kinds this collector emits, for logging and priority mapping.
    fn event_types(&self) -> &'static [&'static str];

    /// Starts the collector background task and returns a handle.
    ///
    /// # Errors
    ///
    /// Returns [`personel_core::error::AgentError::CollectorStart`] if a
    /// required OS API is unavailable.
    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle>;

    /// Notifies the collector of a new policy bundle.
    ///
    /// The collector should read `ctx.policy()` on its next tick rather than
    /// caching the bundle directly. This method should not block.
    async fn reload_policy(&self, policy: Arc<PolicyView>);

    /// Returns a health snapshot for the last reporting period.
    fn health(&self) -> HealthSnapshot;
}

// ──────────────────────────────────────────────────────────────────────────────
// CollectorRegistry
// ──────────────────────────────────────────────────────────────────────────────

/// Registry that holds all registered collectors and their running handles.
pub struct CollectorRegistry {
    collectors: Vec<Box<dyn Collector>>,
    handles: Vec<CollectorHandle>,
}

impl CollectorRegistry {
    /// Creates an empty registry.
    #[must_use]
    pub fn new() -> Self {
        Self { collectors: vec![], handles: vec![] }
    }

    /// Registers a collector. Must be called before [`start_all`].
    pub fn register(&mut self, collector: Box<dyn Collector>) {
        info!(name = collector.name(), "collector registered");
        self.collectors.push(collector);
    }

    /// Starts all registered collectors.
    ///
    /// Collector failures are **isolated**: if one collector fails to start,
    /// the error is logged at `error!` level and the remaining collectors
    /// continue to start. This prevents a single faulty collector (e.g., a
    /// missing OS API on an unsupported Windows build) from taking down the
    /// entire agent.
    ///
    /// Callers can inspect [`health_all`] immediately after `start_all` to
    /// identify which collectors started successfully.
    ///
    /// # Returns
    ///
    /// Always returns `Ok(())`. Individual start failures are observable
    /// through the `tracing` log stream (level `error`).
    pub async fn start_all(&mut self, ctx: &CollectorCtx) -> Result<()> {
        // Launch the background user-SID refresh task ONCE per registry
        // start. The task pushes results into `personel_core::user_context`
        // so the transport layer can stamp `EventMeta.user_sid` without
        // depending on `personel-collectors` (which would be a crate cycle).
        // On non-Windows targets this is a no-op; see user_sid.rs.
        user_sid::spawn_refresh_task();

        for collector in &self.collectors {
            let name = collector.name();
            info!(name, "starting collector");
            match collector.start(ctx.clone()).await {
                Ok(handle) => self.handles.push(handle),
                Err(e) => {
                    // Log loudly but do NOT propagate — other collectors must
                    // continue to run. The 30-second health tick will surface
                    // the gap as a missing health snapshot.
                    error!(
                        collector = name,
                        error = %e,
                        "collector failed to start — continuing with remaining collectors"
                    );
                }
            }
        }
        Ok(())
    }

    /// Stops all running collectors gracefully.
    pub async fn stop_all(&mut self) {
        let handles = std::mem::take(&mut self.handles);
        for handle in handles {
            debug!(name = handle.name, "stopping collector");
            // stop_tx.send returns error if the receiver dropped, which is fine.
            let _ = handle.stop_tx.send(());
            // Give the task a moment to clean up before aborting.
            tokio::time::timeout(Duration::from_secs(5), handle.task)
                .await
                .ok();
        }
    }

    /// Returns health snapshots for all registered collectors.
    #[must_use]
    pub fn health_all(&self) -> Vec<(&'static str, HealthSnapshot)> {
        self.collectors.iter().map(|c| (c.name(), c.health())).collect()
    }

    /// Broadcasts a policy reload to all collectors.
    pub async fn reload_policy_all(&self, policy: Arc<PolicyView>) {
        for collector in &self.collectors {
            collector.reload_policy(policy.clone()).await;
        }
    }
}

impl Default for CollectorRegistry {
    fn default() -> Self {
        Self::new()
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Scheduler
// ──────────────────────────────────────────────────────────────────────────────

/// Drives periodic ticks for time-based collectors.
///
/// The scheduler runs in its own tokio task and sends ticks at the configured
/// interval. Collectors that need polling (e.g., idle, screenshot interval)
/// listen on a broadcast channel instead of managing their own timers. This
/// lets the scheduler be the single source of time, making the system
/// testable with a [`FakeClock`].
///
/// Each collector tick carries the current wall-clock timestamp so collectors
/// don't need to call the clock themselves.
#[derive(Clone)]
pub struct Scheduler {
    tx: tokio::sync::broadcast::Sender<i64>,
}

impl Scheduler {
    /// Creates a new scheduler with a broadcast channel capacity of 16.
    #[must_use]
    pub fn new() -> Self {
        let (tx, _) = tokio::sync::broadcast::channel(16);
        Self { tx }
    }

    /// Returns a new subscriber to the tick broadcast.
    #[must_use]
    pub fn subscribe(&self) -> tokio::sync::broadcast::Receiver<i64> {
        self.tx.subscribe()
    }

    /// Starts the scheduler background task.
    ///
    /// Sends a tick (wall-clock nanos from `clock`) every `interval`.
    /// The task stops when `stop_rx` fires.
    pub fn run(
        self,
        clock: Arc<dyn Clock>,
        interval: Duration,
        mut stop_rx: tokio::sync::oneshot::Receiver<()>,
    ) -> JoinHandle<()> {
        tokio::spawn(async move {
            let mut ticker = tokio::time::interval(interval);
            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        let ts = clock.now_unix_nanos();
                        // Lagging receivers are silently dropped; that's fine —
                        // a collector that can't keep up with the tick rate will
                        // just skip a cycle.
                        if self.tx.send(ts).is_err() {
                            debug!("scheduler: no active receivers");
                        }
                    }
                    _ = &mut stop_rx => {
                        debug!("scheduler: stop requested");
                        break;
                    }
                }
            }
        })
    }
}

impl Default for Scheduler {
    fn default() -> Self {
        Self::new()
    }
}
