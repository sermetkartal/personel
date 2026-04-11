//! Idle/active detection collector.
//!
//! Polls `GetLastInputInfo` every 10 seconds and emits
//! [`session.idle_start`] / [`session.idle_end`] events when the idle
//! threshold (from policy) is crossed.
//!
//! This is the only collector that is **fully implemented** in Phase 1
//! as a sanity-test for the collector infrastructure. All other collectors
//! are stubs with TODO markers.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use prost::Message;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::{AgentError, Result};
use personel_core::event::{EventEnvelope, EventKind, PiiClass};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;
use personel_proto::v1::{
    event::Payload, EndpointId as ProtoEndpointId, Event, EventMeta, SessionIdleEnd,
    SessionIdleStart, TenantId as ProtoTenantId,
};

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// State shared between the collector and its running task
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Default)]
struct IdleState {
    healthy: AtomicBool,
    events_since_last: AtomicU64,
    drops_since_last: AtomicU64,
}

/// Idle / active detection collector.
///
/// Emits `session.idle_start` after the user has been idle for
/// `policy.idle_threshold_secs` and `session.idle_end` when they return.
#[derive(Default)]
pub struct IdleCollector {
    state: Arc<IdleState>,
}

impl IdleCollector {
    /// Creates a new [`IdleCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for IdleCollector {
    fn name(&self) -> &'static str {
        "idle"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["session.idle_start", "session.idle_end"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel();
        let state = Arc::clone(&self.state);
        self.state.healthy.store(true, Ordering::Relaxed);

        let task = tokio::spawn(run_idle_loop(ctx, Arc::clone(&state), stop_rx));

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {
        // Policy change is picked up on next loop iteration via ctx.policy().
    }

    fn health(&self) -> HealthSnapshot {
        let s = &self.state;
        HealthSnapshot {
            healthy: s.healthy.load(Ordering::Relaxed),
            events_since_last: s.events_since_last.swap(0, Ordering::Relaxed),
            drops_since_last: s.drops_since_last.swap(0, Ordering::Relaxed),
            status: String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Run loop
// ──────────────────────────────────────────────────────────────────────────────

async fn run_idle_loop(
    ctx: CollectorCtx,
    state: Arc<IdleState>,
    mut stop_rx: oneshot::Receiver<()>,
) {
    const POLL_INTERVAL: Duration = Duration::from_secs(10);

    let mut was_idle = false;
    let mut idle_start_nanos: i64 = 0;
    let mut seq: u64 = 0;

    let mut ticker = tokio::time::interval(POLL_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        tokio::select! {
            _ = ticker.tick() => {
                match personel_platform::input::last_input_idle_ms() {
                    Ok(idle_ms) => {
                        state.healthy.store(true, Ordering::Relaxed);

                        let policy = ctx.policy();
                        let threshold_ms = u64::from(policy.idle_threshold_secs) * 1_000;
                        let now = ctx.clock.now_unix_nanos();

                        if !was_idle && idle_ms >= threshold_ms {
                            was_idle = true;
                            idle_start_nanos = now;
                            seq += 1;
                            debug!(idle_ms, threshold_ms, "idle start detected");
                            if let Err(e) = emit_idle_start(&ctx, now, seq, policy.idle_threshold_secs) {
                                error!("idle: failed to enqueue idle_start: {e}");
                                state.drops_since_last.fetch_add(1, Ordering::Relaxed);
                            } else {
                                state.events_since_last.fetch_add(1, Ordering::Relaxed);
                            }
                        } else if was_idle && idle_ms < threshold_ms {
                            was_idle = false;
                            let duration_ms = u64::try_from(now.saturating_sub(idle_start_nanos))
                                .unwrap_or(0)
                                / 1_000_000;
                            seq += 1;
                            debug!(duration_ms, "idle end detected");
                            if let Err(e) = emit_idle_end(&ctx, now, seq, duration_ms) {
                                error!("idle: failed to enqueue idle_end: {e}");
                                state.drops_since_last.fetch_add(1, Ordering::Relaxed);
                            } else {
                                state.events_since_last.fetch_add(1, Ordering::Relaxed);
                            }
                        }
                    }
                    Err(e) => {
                        warn!("idle: GetLastInputInfo error: {e}");
                        state.healthy.store(false, Ordering::Relaxed);
                    }
                }
            }
            _ = &mut stop_rx => {
                info!("idle collector: stop requested");
                break;
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Event emission helpers
// ──────────────────────────────────────────────────────────────────────────────

fn make_meta(ctx: &CollectorCtx, now_nanos: i64, seq: u64, kind: EventKind) -> EventMeta {
    // Phase 2.0/3 reserved enrichment fields (category, category_confidence,
    // sensitive_flagged, hris_department, hris_manager_user_id, ocr_language)
    // are populated server-side by the Phase 2 enricher. Agents leave them
    // at their prost-default values via `..Default::default()` spread.
    EventMeta {
        event_id: Some(personel_proto::v1::EventId {
            value: EventId::new_v7().to_bytes().to_vec(),
        }),
        event_type: kind.as_str().to_owned(),
        schema_version: 1,
        tenant_id: Some(ProtoTenantId { value: ctx.tenant_id.to_bytes().to_vec() }),
        endpoint_id: Some(ProtoEndpointId { value: ctx.endpoint_id.to_bytes().to_vec() }),
        user_sid: None,
        occurred_at: Some(prost_types::Timestamp {
            seconds: now_nanos / 1_000_000_000,
            nanos: (now_nanos % 1_000_000_000) as i32,
        }),
        received_at: None,
        agent_version: None,
        seq,
        pii: kind.pii_class() as i32,
        retention: 0,
        ..Default::default()
    }
}

fn enqueue_event_proto(ctx: &CollectorCtx, kind: EventKind, now_nanos: i64, event: Event) -> Result<()> {
    let mut payload_buf = Vec::new();
    event.encode(&mut payload_buf)?;

    let envelope = EventEnvelope::new(
        kind,
        ctx.tenant_id,
        ctx.endpoint_id,
        now_nanos,
        ctx.clock.now_unix_nanos(),
        bytes::Bytes::from(payload_buf),
    );

    ctx.queue.enqueue(
        &envelope.event_id.to_bytes(),
        kind.as_str(),
        envelope.priority,
        envelope.occurred_at_nanos,
        envelope.enqueued_at_nanos,
        &envelope.payload_pb,
    )?;
    Ok(())
}

fn emit_idle_start(ctx: &CollectorCtx, now_nanos: i64, seq: u64, threshold_secs: u32) -> Result<()> {
    let event = Event {
        meta: Some(make_meta(ctx, now_nanos, seq, EventKind::SessionIdleStart)),
        payload: Some(Payload::SessionIdleStart(SessionIdleStart {
            idle_threshold_sec: threshold_secs,
        })),
    };
    enqueue_event_proto(ctx, EventKind::SessionIdleStart, now_nanos, event)
}

fn emit_idle_end(ctx: &CollectorCtx, now_nanos: i64, seq: u64, duration_ms: u64) -> Result<()> {
    let event = Event {
        meta: Some(make_meta(ctx, now_nanos, seq, EventKind::SessionIdleEnd)),
        payload: Some(Payload::SessionIdleEnd(SessionIdleEnd {
            idle_duration_ms: duration_ms,
        })),
    };
    enqueue_event_proto(ctx, EventKind::SessionIdleEnd, now_nanos, event)
}
