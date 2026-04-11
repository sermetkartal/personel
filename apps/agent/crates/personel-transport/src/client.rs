//! gRPC bidi stream client with reconnect and exponential backoff + jitter.
//!
//! Maintains a single long-lived `AgentService.Stream` bidi call. On any
//! transport error, the client enters an exponential-backoff-with-jitter
//! reconnect loop. Heartbeats are sent every 30 seconds. Event batches are
//! drained from the local queue on each upload tick.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Wire `EventQueue::dequeue_batch` → `EventBatch` proto → send on stream.
//! - Handle `BatchAck` server messages → call `EventQueue::ack_batch`.
//! - Handle `PolicyPush` → call `PolicyEngine::apply`.
//! - Handle `UpdateNotify` → notify watchdog via IPC.
//! - Handle `RotateCert` → generate new CSR, submit via `CsrSubmit`.
//! - Handle `PinUpdate` → verify signature, update pinset.

use std::sync::Arc;
use std::time::Duration;

use rand::Rng;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;

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
            base: Duration::from_secs(2),
            max: Duration::from_secs(120),
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

/// Parameters for the gRPC stream client.
pub struct ClientConfig {
    /// Gateway address (e.g., `https://gw.personel.example:443`).
    pub gateway_url: String,
    /// mTLS + pinning config.
    pub tls: crate::tls::TlsConfig,
    /// Reconnect backoff settings.
    pub backoff: BackoffConfig,
    /// How often to send heartbeats.
    pub heartbeat_interval: Duration,
    /// How often to flush the event queue.
    pub upload_interval: Duration,
}

/// Runs the gRPC stream client loop.
///
/// Blocks until `stop_rx` fires. On transport errors, reconnects with
/// exponential backoff + jitter.
///
/// # Errors
///
/// Returns only if the rustls config cannot be built. Transport errors are
/// handled internally with reconnect.
pub async fn run_stream(
    config: ClientConfig,
    queue: Arc<personel_queue::queue::EventQueue>,
    mut stop_rx: oneshot::Receiver<()>,
) -> Result<()> {
    let tls_config = crate::tls::build_client_config(config.tls)?;
    let _tls_config = Arc::new(tls_config);
    let mut attempt: u32 = 0;

    loop {
        tokio::select! {
            _ = &mut stop_rx => {
                info!("transport client: stop requested");
                return Ok(());
            }
            result = connect_and_stream(
                &config.gateway_url,
                &queue,
                config.heartbeat_interval,
                config.upload_interval,
            ) => {
                match result {
                    Ok(()) => {
                        // Clean disconnect (e.g., server-side shutdown).
                        info!("transport client: stream ended cleanly");
                        attempt = 0;
                    }
                    Err(e) => {
                        let delay = config.backoff.next_delay(attempt);
                        warn!(
                            attempt,
                            delay_secs = delay.as_secs_f64(),
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

/// Establishes a single stream connection and runs the send/receive loops.
///
/// Returns `Ok(())` on clean close, `Err(_)` on any transport failure.
async fn connect_and_stream(
    gateway_url: &str,
    queue: &Arc<personel_queue::queue::EventQueue>,
    heartbeat_interval: Duration,
    upload_interval: Duration,
) -> Result<()> {
    info!(%gateway_url, "transport: connecting to gateway");

    // TODO: create tonic channel with the pre-built rustls config.
    // let channel = tonic::transport::Channel::from_shared(gateway_url.to_owned())?
    //     .tls_config(tonic_tls_config)?
    //     .connect()
    //     .await?;
    // let mut client = AgentServiceClient::new(channel);
    // let (tx, rx) = tokio::sync::mpsc::channel::<AgentMessage>(128);
    // let stream = client.stream(tokio_stream::wrappers::ReceiverStream::new(rx)).await?;

    // Stub: sleep and return OK so the reconnect loop exercises the path.
    debug!("transport: stub loop (channel not wired)");

    let mut heartbeat_ticker = tokio::time::interval(heartbeat_interval);
    let mut upload_ticker = tokio::time::interval(upload_interval);

    // Limit stub run to avoid spinning forever in tests.
    let deadline = tokio::time::sleep(Duration::from_secs(30));
    tokio::pin!(deadline);

    loop {
        tokio::select! {
            _ = &mut deadline => {
                debug!("transport: stub deadline reached");
                return Ok(());
            }
            _ = heartbeat_ticker.tick() => {
                debug!("transport: heartbeat tick (stub)");
                // TODO: send Heartbeat { sent_at, queue_depth, ... }
            }
            _ = upload_ticker.tick() => {
                debug!("transport: upload tick (stub)");
                // TODO: dequeue_batch, encode EventBatch, send, await BatchAck.
                let _ = queue.stats(); // exercise the queue path
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn backoff_monotone_increasing() {
        let cfg = BackoffConfig::default();
        let d0 = cfg.next_delay(0).as_secs_f64();
        let d3 = cfg.next_delay(3).as_secs_f64();
        let d10 = cfg.next_delay(10).as_secs_f64();
        assert!(d0 < d3);
        assert!(d10 <= cfg.max.as_secs_f64() * 1.31); // max + 30% jitter
    }
}
