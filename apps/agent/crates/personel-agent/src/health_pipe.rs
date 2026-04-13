//! Health heartbeat pipe writer — agent-side half of the watchdog mutual
//! monitoring channel.
//!
//! The agent spawns [`run_health_pipe_writer`] at startup. It connects (as
//! client) to the named pipe `\\.\pipe\personel-agent-health` hosted by
//! `personel-watchdog` and writes a single ping byte every
//! [`HEARTBEAT_INTERVAL`]. If the watchdog observes no byte for
//! [`WATCHDOG_TIMEOUT`] it classifies the agent as unresponsive and records
//! a tamper entry in `watchdog.log`.
//!
//! # Pipe protocol
//!
//! Minimal: a 1-byte ping (`0x01`). No framing, no JSON. The watchdog only
//! needs the wire-up signal that "the agent is alive within the last 30 s";
//! no additional structure is required.
//!
//! Why not JSON heartbeats? We rejected JSON because:
//!
//! 1. The watchdog reuses the *existence* of reads as the signal; content
//!    is irrelevant.
//! 2. A 1-byte payload avoids any parsing pathway, eliminating a class of
//!    denial-of-service bugs against the watchdog.
//! 3. This pipe is distinct from the existing `personel-watchdog-cmd`
//!    command pipe (see `personel-watchdog::ipc`), which already carries
//!    newline-delimited JSON for update orchestration.
//!
//! # Reconnect behaviour
//!
//! If the pipe connect fails (watchdog not yet started), the writer sleeps
//! [`RECONNECT_BACKOFF`] and retries. There is no upper bound on retries —
//! the watchdog may be restarted at any time and we should resume
//! heartbeats automatically.

#![cfg(target_os = "windows")]

use std::time::Duration;

use anyhow::Result;
use tokio::sync::oneshot;
use tracing::{debug, info, warn};

/// Pipe path for the agent→watchdog heartbeat channel.
pub const HEALTH_PIPE_NAME: &str = r"\\.\pipe\personel-agent-health";

/// Heartbeat ping cadence — one byte every 30 s.
pub const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(30);

/// Watchdog staleness threshold — if no byte is seen for this long the
/// watchdog flags the agent as unresponsive.
#[allow(dead_code)]
pub const WATCHDOG_TIMEOUT: Duration = Duration::from_secs(90);

/// Backoff between reconnect attempts when the watchdog pipe is unavailable.
pub const RECONNECT_BACKOFF: Duration = Duration::from_secs(5);

/// Single heartbeat byte sent on the pipe.
const PING_BYTE: u8 = 0x01;

/// Runs the health-pipe writer loop until `stop_rx` fires.
///
/// Ownership model: spawned from `service::run_agent` with a oneshot
/// shutdown receiver. Returns once the shutdown signal arrives or the
/// pipe writer task errors unrecoverably.
///
/// # Errors
///
/// Returns an error only if the tokio runtime itself fails. All
/// pipe-level errors are logged and retried internally.
pub async fn run_health_pipe_writer(mut stop_rx: oneshot::Receiver<()>) -> Result<()> {
    use tokio::io::AsyncWriteExt;
    use tokio::net::windows::named_pipe::{ClientOptions, NamedPipeClient};

    info!(pipe = HEALTH_PIPE_NAME, "health_pipe: writer task started");
    let mut ticker = tokio::time::interval(HEARTBEAT_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    // Outer loop: (re-)connect. Inner loop: ping until the pipe breaks.
    loop {
        // Bail early on shutdown without attempting another connect.
        if let Ok(()) = stop_rx.try_recv() {
            info!("health_pipe: shutdown before connect");
            return Ok(());
        }

        let mut client: NamedPipeClient = match ClientOptions::new().open(HEALTH_PIPE_NAME) {
            Ok(c) => {
                info!(pipe = HEALTH_PIPE_NAME, "health_pipe: connected to watchdog");
                c
            }
            Err(e) => {
                // PIPE_BUSY / FILE_NOT_FOUND are expected when the watchdog
                // hasn't started yet or is cycling. Log at debug, wait, retry.
                debug!(
                    error = %e,
                    backoff_secs = RECONNECT_BACKOFF.as_secs(),
                    "health_pipe: connect failed; will retry"
                );
                tokio::select! {
                    _ = tokio::time::sleep(RECONNECT_BACKOFF) => continue,
                    _ = &mut stop_rx => {
                        info!("health_pipe: shutdown during reconnect wait");
                        return Ok(());
                    }
                }
            }
        };

        // Inner ping loop. Breaks out on any write error → reconnect.
        loop {
            tokio::select! {
                _ = ticker.tick() => {
                    if let Err(e) = client.write_all(&[PING_BYTE]).await {
                        warn!(error = %e, "health_pipe: write failed; reconnecting");
                        break;
                    }
                    if let Err(e) = client.flush().await {
                        warn!(error = %e, "health_pipe: flush failed; reconnecting");
                        break;
                    }
                    debug!("health_pipe: ping sent");
                }
                _ = &mut stop_rx => {
                    info!("health_pipe: shutdown signal; exiting writer");
                    return Ok(());
                }
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn heartbeat_interval_is_thirty_seconds() {
        assert_eq!(HEARTBEAT_INTERVAL.as_secs(), 30);
    }

    #[test]
    fn watchdog_timeout_is_ninety_seconds() {
        assert_eq!(WATCHDOG_TIMEOUT.as_secs(), 90);
    }

    #[test]
    fn ping_byte_is_nonzero() {
        assert_eq!(PING_BYTE, 0x01);
    }

    #[test]
    fn pipe_name_uses_windows_pipe_namespace() {
        assert!(HEALTH_PIPE_NAME.starts_with(r"\\.\pipe\"));
        assert!(HEALTH_PIPE_NAME.contains("personel-agent-health"));
    }
}
