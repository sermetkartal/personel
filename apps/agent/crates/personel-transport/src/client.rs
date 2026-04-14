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

        let ca_len = cfg.tenant_ca_pem.as_ref().map(|c| c.len());
        if let Some(ca_pem) = cfg.tenant_ca_pem {
            tonic_tls = tonic_tls.ca_certificate(Certificate::from_pem(ca_pem));
        }

        let endpoint = Endpoint::from_shared(cfg.gateway_url.clone())
            .map_err(|e| AgentError::Grpc(format!("invalid gateway URL: {e}")))?
            .tls_config(tonic_tls)
            .map_err(|e| AgentError::Grpc(format!(
                "tls_config error: {e:?} (cert_len={}, key_len={}, ca_len={:?})",
                cfg.client_cert_pem.len(),
                cfg.client_key_pem.len(),
                ca_len,
            )))?
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
                        // Wrap each queue item into a minimal proto Event with meta populated
                        // from the SQLite row. The `payload_pb` column stores collector-emitted
                        // JSON bytes which we forward as the event's `raw_json` equivalent via
                        // the EventMeta.extra_json field (Phase 2.0 reservation). Gateway
                        // publishes whatever Event batch we send; enricher handles per-kind
                        // decoding from meta.event_type.
                        use personel_proto::v1::{Event, EventMeta, TenantId, EndpointId, AgentVersion, WindowsUserSid};
                        use prost_types::Timestamp;
                        let tenant_id_proto = TenantId { value: identity.tenant_id.to_vec() };
                        let endpoint_id_proto = EndpointId { value: identity.endpoint_id.to_vec() };
                        let (maj, min, pat) = parse_semver(&identity.agent_version);
                        let agent_ver = AgentVersion { major: maj, minor: min, patch: pat, build: String::new() };
                        // Faz 2 #12: stamp every EventMeta.user_sid with the currently-cached
                        // interactive user SID. The cache is populated every 60s by the
                        // Windows-gated refresh task in `personel-collectors::user_sid`
                        // (spawned from `CollectorRegistry::start_all`). When the agent
                        // boots before any user is logged on, or on non-Windows dev targets,
                        // the slot is `None` and we emit the `LOCAL_SYSTEM_SID` fallback so
                        // the downstream proto field is never empty.
                        let cached_user_sid = personel_core::user_context::current_sid_or_system();
                        let user_sid_proto = WindowsUserSid { value: cached_user_sid };
                        let proto_events: Vec<Event> = items
                            .iter()
                            .map(|raw| {
                                let payload_utf8 = String::from_utf8_lossy(&raw.payload_pb).to_string();
                                let meta = EventMeta {
                                    event_id: Some(personel_proto::v1::EventId { value: raw.event_id.clone() }),
                                    event_type: raw.event_type.clone(),
                                    schema_version: 1,
                                    tenant_id: Some(tenant_id_proto.clone()),
                                    endpoint_id: Some(endpoint_id_proto.clone()),
                                    user_sid: Some(user_sid_proto.clone()),
                                    occurred_at: Some(Timestamp {
                                        seconds: raw.occurred_at / 1_000_000_000,
                                        nanos: (raw.occurred_at % 1_000_000_000) as i32,
                                    }),
                                    received_at: None,
                                    agent_version: Some(agent_ver.clone()),
                                    seq: raw.id as u64,
                                    pii: 0,
                                    retention: 0,
                                    raw_payload_json: payload_utf8,
                                    category: String::new(),
                                    category_confidence: 0.0,
                                    sensitive_flagged: false,
                                    hris_department: String::new(),
                                    hris_manager_user_id: String::new(),
                                    ocr_language: String::new(),
                                };
                                Event { meta: Some(meta), payload: None }
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
                        handle_server_message(server_msg, &queue);
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

fn handle_server_message(
    msg: personel_proto::v1::ServerMessage,
    queue: &Arc<personel_queue::queue::EventQueue>,
) {
    use server_message::Payload;
    match msg.payload {
        Some(Payload::Welcome(w)) => {
            info!(server_version = %w.server_version, ack_up_to = w.ack_up_to_seq, "transport: Welcome received");
        }
        Some(Payload::BatchAck(ack)) => {
            // Commit the in-flight rows for this batch. The queue uses the
            // batch_id assigned by dequeue_batch as the correlation key;
            // ack_batch() deletes the rows from event_queue. A zero delete
            // count is not inherently fatal (the batch may have been nacked
            // earlier by a transport error, or the ack is stale after a
            // reconnect), but we log it at warn level so operators can
            // notice if it happens repeatedly.
            match queue.ack_batch(ack.batch_id) {
                Ok(n) => {
                    debug!(
                        batch_id = ack.batch_id,
                        accepted = ack.accepted_count,
                        rejected = ack.rejected_count,
                        committed_rows = n,
                        "transport: BatchAck"
                    );
                    if n == 0 {
                        warn!(
                            batch_id = ack.batch_id,
                            "transport: BatchAck committed 0 rows — stale or already-acked batch"
                        );
                    }
                }
                Err(e) => {
                    error!(
                        batch_id = ack.batch_id,
                        error = %e,
                        "transport: ack_batch failed — in-flight rows remain"
                    );
                }
            }
        }
        Some(Payload::PolicyPush(p)) => {
            info!(version = %p.policy_version, "transport: PolicyPush — TODO Phase 2: apply");
        }
        Some(Payload::UpdateNotify(u)) => {
            // Faz 4 #30 — wire real OTA apply. The download/network path is
            // still TODO; we consume a package that the control plane (or a
            // future downloader task) deposits at a well-known path under
            // %PROGRAMDATA%. If the file is not present this is a no-op —
            // the server is free to issue UpdateNotify as a pre-announce
            // hint. The UpdateAck response path is not plumbed yet: the
            // outbound mpsc lives inside `stream_once` and is not reachable
            // from this synchronous dispatch point. Logging success/failure
            // is the Phase 1 compromise until the response channel is
            // exposed (a small refactor: lift `msg_tx` into an Arc and hand
            // a clone into `handle_server_message` so it can spawn the
            // response send). For now the happy path still exits the
            // process on success so the watchdog + SCM can relaunch from
            // the new binary.
            let target = u.target_version.clone();
            let canary = u.canary;
            info!(?target, canary, "transport: UpdateNotify received — spawning apply task");
            tokio::spawn(async move {
                let pkg_path = incoming_update_package_path();
                if !pkg_path.exists() {
                    warn!(
                        path = ?pkg_path,
                        "UpdateNotify: no package at expected incoming path; \
                         nothing to apply (network download path is TODO)"
                    );
                    return;
                }
                // The verification key is baked in via env var at build
                // time or falls back to a sentinel that always rejects —
                // the point is that an unsigned package can never reach
                // `apply_update`.
                let signing_key_pem = std::env::var("PERSONEL_UPDATE_SIGNING_KEY_HEX")
                    .unwrap_or_default();
                if signing_key_pem.is_empty() {
                    error!("UpdateNotify: no signing key configured; refusing to apply");
                    return;
                }
                match personel_updater::verify_update_package(&pkg_path, &signing_key_pem) {
                    Ok(metadata) => {
                        let install_dir = default_install_dir();
                        info!(
                            version = %metadata.version,
                            ?install_dir,
                            "UpdateNotify: package verified; applying"
                        );
                        match personel_updater::apply_update(metadata, &install_dir) {
                            Ok(()) => {
                                warn!("UpdateNotify: apply_update ok — exiting so watchdog can relaunch");
                                // Exit path: the watchdog (already upgraded)
                                // observes the gap and invokes sc start.
                                // TODO: emit UpdateAck{success:true} before exit.
                                std::process::exit(0);
                            }
                            Err(e) => {
                                error!(error = %e, "UpdateNotify: apply_update failed; attempting rollback");
                                if let Err(r) = personel_updater::rollback_update(&install_dir) {
                                    error!(error = %r, "UpdateNotify: rollback failed");
                                }
                                // TODO: emit UpdateAck{success:false, error:...}.
                            }
                        }
                    }
                    Err(e) => {
                        error!(error = %e, "UpdateNotify: package verification failed — refusing to apply");
                        // TODO: emit UpdateAck{success:false, error:...}.
                    }
                }
            });
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

// ── OTA update paths ──────────────────────────────────────────────────────────

/// Returns the path where a downloaded update package is expected to be
/// deposited. Phase 1 simplification: a fixed path under PROGRAMDATA on
/// Windows, or /tmp on dev builds. The actual network download task is
/// Phase 2 and will write to this same path.
fn incoming_update_package_path() -> std::path::PathBuf {
    #[cfg(target_os = "windows")]
    {
        let pd = std::env::var("PROGRAMDATA")
            .unwrap_or_else(|_| r"C:\ProgramData".to_string());
        std::path::PathBuf::from(pd)
            .join("Personel")
            .join("agent")
            .join("incoming")
            .join("update.tar.gz")
    }
    #[cfg(not(target_os = "windows"))]
    {
        std::path::PathBuf::from("/tmp/personel-agent-update.tar.gz")
    }
}

/// Returns the directory the agent is installed to. Picks up an override
/// from `PERSONEL_INSTALL_DIR` so tests and dev runs don't need to touch
/// `C:\Program Files`.
fn default_install_dir() -> std::path::PathBuf {
    if let Ok(override_) = std::env::var("PERSONEL_INSTALL_DIR") {
        return std::path::PathBuf::from(override_);
    }
    #[cfg(target_os = "windows")]
    {
        std::path::PathBuf::from(r"C:\Program Files (x86)\Personel\Agent")
    }
    #[cfg(not(target_os = "windows"))]
    {
        std::path::PathBuf::from("/opt/personel/agent")
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

    /// Builds a fresh disk-backed queue in a temp directory for unit tests.
    /// We cannot use the crate-private `open_in_memory` test helper from
    /// personel-queue because it is gated on that crate's `cfg(test)`, so
    /// we stand up a real SQLCipher file with an all-zero key instead.
    fn test_queue() -> Arc<personel_queue::queue::EventQueue> {
        use personel_queue::queue::{EventQueue, QueueConfig};
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("queue.db");
        // Leak the TempDir so the file stays alive for the lifetime of
        // the test. The OS cleans up %TEMP% eventually; this is a unit
        // test so it's fine.
        std::mem::forget(dir);
        let key = zeroize::Zeroizing::new(vec![0u8; 32]);
        let cfg = QueueConfig::new(path, key);
        Arc::new(EventQueue::open(cfg).expect("open queue"))
    }

    /// BatchAck handler calls queue.ack_batch with the received batch_id
    /// and removes the in-flight rows. Regression for the pre-fix TODO where
    /// in-flight rows were never committed.
    #[test]
    fn batch_ack_commits_in_flight_rows() {
        use personel_core::event::Priority;
        use personel_proto::v1::{BatchAck, ServerMessage};

        let queue = test_queue();

        // Enqueue three events, then dequeue them into batch_id 42.
        let now = 1_700_000_000_000_000_000i64;
        for i in 0..3u8 {
            queue
                .enqueue(
                    &[i; 16],
                    "test.event",
                    Priority::Normal,
                    now,
                    now,
                    b"payload",
                )
                .unwrap();
        }
        let batch_id: u64 = 42;
        let dequeued = queue.dequeue_batch(100, batch_id).unwrap();
        assert_eq!(dequeued.len(), 3, "three rows should be in-flight");
        let stats = queue.stats().unwrap();
        assert_eq!(stats.in_flight_count, 3, "stats should reflect in-flight");
        assert_eq!(stats.pending_count, 0);

        // Send a BatchAck through handle_server_message — this is the
        // production dispatch entry point that was previously a TODO.
        let msg = ServerMessage {
            payload: Some(server_message::Payload::BatchAck(BatchAck {
                batch_id,
                accepted_count: 3,
                rejected_count: 0,
            })),
        };
        handle_server_message(msg, &queue);

        // After the ack the in-flight rows must be gone.
        let stats = queue.stats().unwrap();
        assert_eq!(
            stats.in_flight_count, 0,
            "in-flight rows must be committed by BatchAck"
        );
        assert_eq!(stats.pending_count, 0);
    }

    /// A BatchAck for an unknown batch_id is a no-op (does not panic).
    /// Exercises the "stale ack" path, which can happen after a reconnect.
    #[test]
    fn batch_ack_unknown_batch_is_noop() {
        use personel_proto::v1::{BatchAck, ServerMessage};

        let queue = test_queue();
        let msg = ServerMessage {
            payload: Some(server_message::Payload::BatchAck(BatchAck {
                batch_id: 999,
                accepted_count: 0,
                rejected_count: 0,
            })),
        };
        handle_server_message(msg, &queue); // must not panic
        let stats = queue.stats().unwrap();
        assert_eq!(stats.pending_count, 0);
        assert_eq!(stats.in_flight_count, 0);
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
