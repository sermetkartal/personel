//! gRPC bidi stream client with mTLS, reconnect, exponential backoff, and
//! heartbeat.
//!
//! # Architecture
//!
//! One long-lived `AgentService.Stream` bidi RPC is maintained per agent
//! instance.  The client side sends [`AgentMessage`] frames; the server side
//! sends [`ServerMessage`] frames.
//!
//! ```text
//!  ┌─────────────────┐   mpsc(128)   ┌───────────────────────┐
//!  │  event_rx        │──────────────▶│  batch assembler      │
//!  │  (EventQueue     │               │  (up to 100 events or │
//!  │   dequeue)       │               │   5 s flush window)   │
//!  └─────────────────┘               └───────────┬───────────┘
//!                                                │  AgentMessage::EventBatch
//!  heartbeat ticker (30 s) ────────────────────▶│
//!                                                ▼
//!                                        gRPC bidi stream
//!                                                │
//!                              ServerMessage ◀───┘
//!                              (PolicyPush, BatchAck, Ping, …)
//! ```
//!
//! On any transport error the loop enters exponential back-off (1 s → 30 s
//! cap, 30 % jitter) and reconnects indefinitely until `stop_rx` fires.

use std::sync::Arc;
use std::time::Duration;

use futures::StreamExt;
use prost_types::Timestamp;
use rand::Rng;
use tokio::sync::oneshot;
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Endpoint, Identity};
use tracing::{debug, error, info, warn};

use personel_core::error::{AgentError, Result};
use personel_proto::v1::{
    agent_message, server_message, AgentMessage, AgentVersion, EndpointId,
    HardwareFingerprint, Heartbeat, Hello, TenantId,
};
use personel_proto::AgentServiceClient;

// ── Batch parameters ──────────────────────────────────────────────────────────

/// Maximum events to include in a single `EventBatch` before flushing.
const BATCH_MAX_EVENTS: usize = 100;
/// Maximum time to accumulate events before an upload flush.
const BATCH_FLUSH_INTERVAL: Duration = Duration::from_secs(5);
/// Heartbeat interval.
const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(30);

// ── BackoffConfig ─────────────────────────────────────────────────────────────

/// Reconnect backoff configuration.
#[derive(Debug, Clone)]
pub struct BackoffConfig {
    /// Base delay for the first retry.
    pub base: Duration,
    /// Maximum backoff cap.
    pub max: Duration,
    /// Jitter factor (0.0–1.0). Each step adds up to `jitter * current_delay`.
    pub jitter: f64,
    /// Backoff multiplier per step.
    pub multiplier: f64,
}

impl Default for BackoffConfig {
    fn default() -> Self {
        Self {
            base: Duration::from_secs(1),
            max: Duration::from_secs(30),
            jitter: 0.3,
            multiplier: 2.0,
        }
    }
}

impl BackoffConfig {
    /// Computes the next delay given the current retry attempt (0-indexed).
    #[must_use]
    pub fn next_delay(&self, attempt: u32) -> Duration {
        let base_secs = self.base.as_secs_f64();
        let exp = base_secs * self.multiplier.powi(attempt as i32);
        let capped = exp.min(self.max.as_secs_f64());
        let jitter_secs = rand::thread_rng().gen_range(0.0..capped * self.jitter);
        Duration::from_secs_f64(capped + jitter_secs)
    }
}

// ── ClientConfig ──────────────────────────────────────────────────────────────

/// Parameters for the gRPC stream client.
pub struct ClientConfig {
    /// Gateway address (e.g., `https://gw.personel.example:443`).
    pub gateway_url: String,
    /// PEM-encoded client certificate chain (leaf first).
    pub client_cert_pem: Vec<u8>,
    /// PEM-encoded client private key.
    pub client_key_pem: Vec<u8>,
    /// Optional PEM-encoded tenant CA certificate.
    ///
    /// When present, added as a trusted root so the gateway's server cert can
    /// be validated.  When absent, the system/Mozilla root store is used.
    pub tenant_ca_pem: Option<Vec<u8>>,
    /// Tenant UUID bytes (16 bytes big-endian). Used in the Hello frame.
    pub tenant_id: [u8; 16],
    /// Endpoint UUID bytes (16 bytes big-endian). Used in the Hello frame.
    pub endpoint_id: [u8; 16],
    /// Hardware fingerprint digest. Used in the Hello frame.
    pub hw_fingerprint: Vec<u8>,
    /// Operating system version string reported in Hello.
    pub os_version: String,
    /// Agent semver (e.g., "0.1.0").
    pub agent_version: String,
    /// Reconnect backoff settings.
    pub backoff: BackoffConfig,
}

// ── TransportClient ───────────────────────────────────────────────────────────

/// Identity metadata cached between reconnect attempts. Used to rebuild the
/// Hello frame on every new `stream_once` invocation.
#[derive(Clone)]
pub struct AgentIdentity {
    /// Tenant UUID as raw 16 bytes (big-endian).
    pub tenant_id: [u8; 16],
    /// Endpoint UUID as raw 16 bytes (big-endian).
    pub endpoint_id: [u8; 16],
    /// Hardware fingerprint digest (typically SHA-256 of MachineGuid + extras).
    pub hw_fingerprint: Vec<u8>,
    /// Operating system identifier string reported in Hello.
    pub os_version: String,
    /// Agent semver string (e.g., "0.1.0").
    pub agent_version: String,
}

/// A connected gRPC transport client.
///
/// Constructed via [`TransportClient::connect`]; consumed by
/// [`TransportClient::run_bidi`].
pub struct TransportClient {
    channel: Channel,
    identity: AgentIdentity,
}

impl TransportClient {
    /// Builds a tonic [`Channel`] with mTLS using the provided config.
    ///
    /// The channel is lazily connected; the actual TCP + TLS handshake
    /// happens when the first RPC is made.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Grpc`] if the endpoint URL is malformed or TLS
    /// config is rejected by tonic.
    pub fn connect(cfg: ClientConfig) -> Result<Self> {
        // Build tonic mTLS config from PEM bytes.
        // tonic 0.11 only accepts PEM; callers must convert DER → PEM before
        // passing to this function (see `personel_agent::service`).
        let identity = Identity::from_pem(&cfg.client_cert_pem, &cfg.client_key_pem);

        let mut tonic_tls = ClientTlsConfig::new().identity(identity);

        if let Some(ca_pem) = cfg.tenant_ca_pem {
            tonic_tls = tonic_tls.ca_certificate(Certificate::from_pem(ca_pem));
        }

        let endpoint = Endpoint::from_shared(cfg.gateway_url.clone())
            .map_err(|e| AgentError::Grpc(format!("invalid gateway URL: {e}")))?
            .tls_config(tonic_tls)
            .map_err(|e| AgentError::Grpc(format!("tls_config error: {e}")))?
            // Keepalive — ensures the OS TCP stack doesn't drop idle connections.
            .keep_alive_while_idle(true)
            .http2_keep_alive_interval(Duration::from_secs(20))
            .keep_alive_timeout(Duration::from_secs(10))
            // Per-RPC deadline is not set here; the bidi stream is indefinite.
            .connect_timeout(Duration::from_secs(15));

        let channel = endpoint.connect_lazy();
        let identity = AgentIdentity {
            tenant_id: cfg.tenant_id,
            endpoint_id: cfg.endpoint_id,
            hw_fingerprint: cfg.hw_fingerprint,
            os_version: cfg.os_version,
            agent_version: cfg.agent_version,
        };
        Ok(Self { channel, identity })
    }

    /// Runs the bidi gRPC stream loop, reconnecting on errors.
    ///
    /// Blocks until `stop_rx` fires.  On each transport error the client
    /// waits for the configured backoff before reconnecting.
    ///
    /// # Errors
    ///
    /// Returns only non-transient setup errors.  Transport failures are
    /// handled internally with reconnect.
    pub async fn run_bidi(
        self,
        queue: Arc<personel_queue::queue::EventQueue>,
        backoff: BackoffConfig,
        mut stop_rx: oneshot::Receiver<()>,
    ) -> Result<()> {
        let mut attempt: u32 = 0;

        loop {
            tokio::select! {
                _ = &mut stop_rx => {
                    info!("transport: stop requested — exiting bidi loop");
                    return Ok(());
                }
                result = stream_once(self.channel.clone(), Arc::clone(&queue), self.identity.clone()) => {
                    match result {
                        Ok(()) => {
                            info!("transport: stream ended cleanly");
                            attempt = 0;
                        }
                        Err(e) => {
                            let delay = backoff.next_delay(attempt);
                            warn!(
                                attempt,
                                delay_ms = delay.as_millis(),
                                error = %e,
                                "transport error; reconnecting after backoff"
                            );
                            attempt = attempt.saturating_add(1);
                            tokio::time::sleep(delay).await;
                        }
                    }
                }
            }
        }
    }
}

// ── stream_once ───────────────────────────────────────────────────────────────

/// Establishes one bidi stream connection and runs until it closes or errors.
async fn stream_once(
    channel: Channel,
    queue: Arc<personel_queue::queue::EventQueue>,
    identity: AgentIdentity,
) -> Result<()> {
    info!("transport: opening bidi stream");

    // The sender side of the bidi stream.  All AgentMessage frames go through
    // this mpsc; the ReceiverStream adaptor turns it into a tonic Streaming.
    let (msg_tx, msg_rx) = tokio::sync::mpsc::channel::<AgentMessage>(256);
    let outbound = ReceiverStream::new(msg_rx);

    // The gateway stream handler requires the very first frame to be Hello.
    // Push it onto the mpsc BEFORE invoking client.stream() so the initial
    // HTTP/2 request body carries something — some tonic/h2 versions hold
    // the DATA frame until the first yield from the ReceiverStream.
    let (major, minor, patch) = parse_semver(&identity.agent_version);
    let hello = AgentMessage {
        payload: Some(agent_message::Payload::Hello(Hello {
            agent_version: Some(AgentVersion {
                major,
                minor,
                patch,
                build: String::new(),
            }),
            endpoint_id: Some(EndpointId {
                value: identity.endpoint_id.to_vec(),
            }),
            tenant_id: Some(TenantId {
                value: identity.tenant_id.to_vec(),
            }),
            hw_fingerprint: Some(HardwareFingerprint {
                blob: identity.hw_fingerprint.clone(),
            }),
            resume_cookie: Vec::new(),
            last_acked_seq: 0,
            os_version: identity.os_version.clone(),
            agent_build: identity.agent_version.clone(),
            pe_dek_version: 0,
            tmk_version: 0,
        })),
    };
    if msg_tx.send(hello).await.is_err() {
        return Err(AgentError::Grpc("outbound channel closed before Hello".into()));
    }
    info!("transport: Hello queued");

    let mut client = AgentServiceClient::new(channel);
    let response = client
        .stream(outbound)
        .await
        .map_err(|s| AgentError::Grpc(format!("stream RPC failed: {s}")))?;

    let mut inbound = response.into_inner();

    info!("transport: bidi stream established");

    let mut heartbeat_ticker = tokio::time::interval(HEARTBEAT_INTERVAL);
    let mut batch_ticker = tokio::time::interval(BATCH_FLUSH_INTERVAL);
    // Discard the first tick so we don't flush before any events accumulate.
    heartbeat_ticker.tick().await;
    batch_ticker.tick().await;

    let mut batch_seq: u64 = 0;

    loop {
        tokio::select! {
            // ── Heartbeat ─────────────────────────────────────────────────────
            _ = heartbeat_ticker.tick() => {
                let stats = queue.stats().map_err(|e| AgentError::Queue(e.to_string()))?;
                let now = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap_or_default();
                let hb = AgentMessage {
                    payload: Some(agent_message::Payload::Heartbeat(Heartbeat {
                        sent_at: Some(Timestamp {
                            seconds: now.as_secs() as i64,
                            nanos: now.subsec_nanos() as i32,
                        }),
                        queue_depth: stats.pending_count,
                        blob_queue_depth: 0,
                        cpu_percent: 0.0,   // TODO: sysinfo integration
                        rss_bytes: 0,       // TODO: sysinfo integration
                        policy_version: String::new(),
                    })),
                };
                if msg_tx.send(hb).await.is_err() {
                    warn!("transport: outbound channel closed during heartbeat");
                    return Err(AgentError::Grpc("outbound channel closed".into()));
                }
                debug!("transport: heartbeat sent");
            }

            // ── Event batch upload ────────────────────────────────────────────
            _ = batch_ticker.tick() => {
                // Dequeue up to BATCH_MAX_EVENTS from the offline queue.
                // dequeue_batch is synchronous (SQLite); run it on a blocking thread.
                batch_seq = batch_seq.wrapping_add(1);
                let current_batch_id = batch_seq;
                let queue_ref = Arc::clone(&queue);
                let events_result = tokio::task::spawn_blocking(move || {
                    queue_ref.dequeue_batch(BATCH_MAX_EVENTS, current_batch_id)
                })
                .await
                .map_err(|e| AgentError::Internal(format!("spawn_blocking join: {e}")))?;

                match events_result {
                    Ok(items) if items.is_empty() => {
                        // Nothing in queue — no-op.
                        batch_seq = batch_seq.wrapping_sub(1); // don't advance seq for empty polls
                        debug!("transport: upload tick — queue empty");
                    }
                    Ok(items) => {
                        let proto_events: Vec<personel_proto::v1::Event> = items
                            .iter()
                            .filter_map(|raw| {
                                use prost::Message;
                                personel_proto::v1::Event::decode(raw.payload_pb.as_slice()).ok()
                            })
                            .collect();
                        let n = proto_events.len();
                        let batch = crate::envelope::build_batch(batch_seq, proto_events)
                            .map_err(|e| AgentError::Grpc(format!("batch build: {e}")))?;
                        let msg = AgentMessage {
                            payload: Some(agent_message::Payload::EventBatch(batch)),
                        };
                        if msg_tx.send(msg).await.is_err() {
                            warn!("transport: outbound channel closed during batch upload");
                            return Err(AgentError::Grpc("outbound channel closed".into()));
                        }
                        debug!(batch_id = batch_seq, events = n, "transport: batch sent");
                    }
                    Err(e) => {
                        warn!(error = %e, "transport: dequeue_batch error — will retry next tick");
                    }
                }
            }

            // ── Inbound server messages ────────────────────────────────────────
            maybe_msg = inbound.next() => {
                match maybe_msg {
                    None => {
                        info!("transport: server closed the stream");
                        return Ok(());
                    }
                    Some(Err(status)) => {
                        error!(code = ?status.code(), message = %status.message(), "transport: stream error from server");
                        return Err(AgentError::Grpc(status.to_string()));
                    }
                    Some(Ok(server_msg)) => {
                        handle_server_message(server_msg);
                    }
                }
            }
        }
    }
}

/// Parses a dotted semver string ("0.1.0") into (major, minor, patch). Missing
/// or non-numeric components default to 0.
fn parse_semver(s: &str) -> (u32, u32, u32) {
    let mut parts = s.split('.').map(|p| p.parse::<u32>().unwrap_or(0));
    (
        parts.next().unwrap_or(0),
        parts.next().unwrap_or(0),
        parts.next().unwrap_or(0),
    )
}

// ── Server message dispatch ───────────────────────────────────────────────────

fn handle_server_message(msg: personel_proto::v1::ServerMessage) {
    use server_message::Payload;
    match msg.payload {
        Some(Payload::Welcome(w)) => {
            info!(server_version = %w.server_version, ack_up_to = w.ack_up_to_seq, "transport: Welcome received");
        }
        Some(Payload::BatchAck(ack)) => {
            debug!(
                batch_id = ack.batch_id,
                accepted = ack.accepted_count,
                rejected = ack.rejected_count,
                "transport: BatchAck"
            );
            // TODO Phase 2: call queue.ack_batch(ack.batch_id)
        }
        Some(Payload::PolicyPush(p)) => {
            info!(version = %p.policy_version, "transport: PolicyPush — TODO Phase 2: apply");
        }
        Some(Payload::UpdateNotify(u)) => {
            info!(target = ?u.target_version, canary = u.canary, "transport: UpdateNotify — TODO Phase 2: notify watchdog");
        }
        Some(Payload::LiveViewStart(_)) => {
            info!("transport: LiveViewStart — TODO Phase 2: start screen capture");
        }
        Some(Payload::LiveViewStop(_)) => {
            info!("transport: LiveViewStop — TODO Phase 2: stop screen capture");
        }
        Some(Payload::RotateCert(r)) => {
            warn!(reason = %r.reason, "transport: RotateCert — TODO Phase 2: generate CSR");
        }
        Some(Payload::PinUpdate(_)) => {
            warn!("transport: PinUpdate — TODO Phase 2: verify signature and update pinset");
        }
        Some(Payload::Ping(_)) => {
            debug!("transport: Ping received (no-op; heartbeat covers liveness)");
        }
        Some(Payload::CsrResponse(r)) => {
            info!(cert_len = r.cert_der.len(), "transport: CsrResponse — TODO Phase 2: store cert");
        }
        None => {
            warn!("transport: ServerMessage with no payload — ignoring");
        }
    }
}

// ── Convenience runner ────────────────────────────────────────────────────────

/// Runs the gRPC stream client loop.
///
/// Builds the [`TransportClient`], then runs the bidi loop until `stop_rx`
/// fires.  On transport errors, reconnects with exponential backoff + jitter.
///
/// # Errors
///
/// Returns [`AgentError::Grpc`] if the initial channel cannot be built.
/// Transport errors during operation are handled internally with reconnect.
pub async fn run_stream(
    config: ClientConfig,
    queue: Arc<personel_queue::queue::EventQueue>,
    stop_rx: oneshot::Receiver<()>,
) -> Result<()> {
    let backoff = config.backoff.clone();
    let client = TransportClient::connect(config)?;
    client.run_bidi(queue, backoff, stop_rx).await
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn backoff_monotone_increasing() {
        let cfg = BackoffConfig::default();
        let d0 = cfg.next_delay(0).as_secs_f64();
        let d3 = cfg.next_delay(3).as_secs_f64();
        let d10 = cfg.next_delay(10).as_secs_f64();
        assert!(d0 < d3, "d0={d0} should be less than d3={d3}");
        // At max + 30 % jitter the cap should hold.
        assert!(
            d10 <= cfg.max.as_secs_f64() * 1.31,
            "d10={d10} exceeded max + jitter"
        );
    }

    #[test]
    fn backoff_respects_cap() {
        // jitter=0.001 (non-zero) to avoid gen_range(0..0) panic; still
        // verifies that the cap holds (5 s + tiny jitter < 5.05 s).
        let cfg = BackoffConfig {
            base: Duration::from_secs(1),
            max: Duration::from_secs(5),
            jitter: 0.001,
            multiplier: 10.0,
        };
        let d5 = cfg.next_delay(5).as_secs_f64();
        assert!(d5 <= 5.05, "d5={d5} exceeded cap + jitter margin");
    }

    /// attempt=0 → base delay (1 s) + minimal jitter: result must be in [1.0, 1.0 + jitter_max).
    #[test]
    fn backoff_attempt_zero_is_near_base() {
        let cfg = BackoffConfig {
            base: Duration::from_secs(1),
            max: Duration::from_secs(30),
            jitter: 0.001, // tiny but non-zero to avoid gen_range(0..0) panic
            multiplier: 2.0,
        };
        let d = cfg.next_delay(0).as_secs_f64();
        // base=1s, jitter≤0.001 → result in [1.0, 1.001)
        assert!(d >= 1.0 && d < 1.002, "attempt 0 should be ~1s, got {d}");
    }

    /// attempt=3 → 1 * 2^3 = 8 s base (+ tiny jitter).
    #[test]
    fn backoff_attempt_three_is_near_eight_seconds() {
        let cfg = BackoffConfig {
            base: Duration::from_secs(1),
            max: Duration::from_secs(30),
            jitter: 0.001,
            multiplier: 2.0,
        };
        let d = cfg.next_delay(3).as_secs_f64();
        // 8 s + up to 0.001 * 8 = 0.008 jitter
        assert!(d >= 8.0 && d < 8.1, "attempt 3 should be ~8 s, got {d}");
    }

    /// attempt=10 with default config → capped at 30 s (+ up to 30 % jitter).
    #[test]
    fn backoff_attempt_ten_capped_at_thirty() {
        let cfg = BackoffConfig::default(); // max=30s, jitter=0.3
        for _ in 0..20 {
            // Run multiple times to exercise random jitter
            let d = cfg.next_delay(10).as_secs_f64();
            assert!(
                d <= 30.0 * 1.31,
                "attempt 10 must be ≤30s * 1.31 jitter factor, got {d}"
            );
            assert!(d >= 30.0, "attempt 10 base (no jitter subtraction) must be ≥30s");
        }
    }

    /// BATCH_MAX_EVENTS ve BATCH_FLUSH_INTERVAL sabitleri spesifikasyona uygun.
    #[test]
    fn batch_constants_match_spec() {
        assert_eq!(BATCH_MAX_EVENTS, 100, "batch max must be 100 events");
        assert_eq!(
            BATCH_FLUSH_INTERVAL,
            Duration::from_secs(5),
            "flush interval must be 5 s"
        );
    }

    /// HEARTBEAT_INTERVAL sabiti 30 s olmalı.
    #[test]
    fn heartbeat_interval_is_thirty_seconds() {
        assert_eq!(HEARTBEAT_INTERVAL, Duration::from_secs(30));
    }

    /// BackoffConfig::default() değerlerini doğrula.
    #[test]
    fn backoff_default_values() {
        let cfg = BackoffConfig::default();
        assert_eq!(cfg.base, Duration::from_secs(1));
        assert_eq!(cfg.max, Duration::from_secs(30));
        assert!((cfg.jitter - 0.3).abs() < 1e-9);
        assert!((cfg.multiplier - 2.0).abs() < 1e-9);
    }

    /// Minimal jitter ile art arda iki çağrı aynı cap'ten gelir.
    #[test]
    fn backoff_large_attempt_stays_at_cap() {
        // multiplier=10, attempt=5 → 1 * 10^5 = 100_000 >> max=5 → capped at 5
        let cfg = BackoffConfig {
            base: Duration::from_secs(1),
            max: Duration::from_secs(5),
            jitter: 0.001,
            multiplier: 10.0,
        };
        let d = cfg.next_delay(5).as_secs_f64();
        // max=5, jitter≤0.001 → result in [5.0, 5.006)
        assert!(d >= 5.0 && d < 5.01, "capped backoff should be ~5 s, got {d}");
    }
}
