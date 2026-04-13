//! Agent-liveness monitor — watchdog-side half of the named-pipe heartbeat.
//!
//! Hosts the server end of `\\.\pipe\personel-agent-health` and reads one
//! byte from the agent at least every [`HEARTBEAT_INTERVAL`]. If no byte is
//! seen for [`STALENESS_TIMEOUT`] we treat the agent as unresponsive:
//!
//! 1. Append a JSON line to
//!    `%PROGRAMDATA%\Personel\agent\watchdog.log` — the next time the
//!    agent starts, its `anti_tamper::replay_watchdog_log` will read and
//!    enqueue this entry as a critical `agent.tamper_detected` event.
//! 2. Best-effort restart the agent via `sc.exe start PersonelAgent`.
//!
//! The log-file handoff avoids the need for a second transport inside the
//! watchdog: the watchdog has no queue, no gateway credentials, no
//! offline buffer. All event emission remains the agent's job. The
//! watchdog's role is purely to *record* what it observed and let the
//! agent replay those observations on next start.
//!
//! # Pipe protocol
//!
//! A single byte (any value) per heartbeat. We do not parse the byte — its
//! arrival is the only signal we need. See `personel-agent::health_pipe`
//! for the agent-side sender.
//!
//! # Shutdown
//!
//! The `run_health_monitor` function is synchronous and runs for the
//! lifetime of the watchdog process. Break the inner loop by exiting the
//! watchdog (Ctrl-C / service stop); there is no cooperative shutdown
//! channel because the pipe I/O is already interruptible by process
//! termination.

#![cfg(target_os = "windows")]

use std::path::PathBuf;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use serde_json::json;
use tracing::{debug, error, info, warn};

/// Pipe path for the agent→watchdog heartbeat channel.
///
/// MUST match `personel_agent::health_pipe::HEALTH_PIPE_NAME`.
const HEALTH_PIPE_NAME: &str = r"\\.\pipe\personel-agent-health";

/// How often we expect a heartbeat byte from the agent.
const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(30);

/// No heartbeat for this long → tamper: agent unresponsive.
const STALENESS_TIMEOUT: Duration = Duration::from_secs(90);

/// How often the blocking read wakes up to re-check staleness.
const READ_TIMEOUT: Duration = Duration::from_secs(10);

/// ProgramData data dir (matches agent side).
fn data_dir() -> PathBuf {
    PathBuf::from(r"C:\ProgramData\Personel\agent")
}

/// Full path to watchdog.log.
fn watchdog_log_path() -> PathBuf {
    data_dir().join("watchdog.log")
}

/// Spawns a background tokio task running the health monitor.
///
/// The task owns the server-side pipe and runs until the runtime shuts down.
/// It emits an info log on startup and a warn log on every staleness-detected
/// cycle. Restart attempts are best-effort (`sc.exe start PersonelAgent`).
pub fn spawn_health_monitor() {
    tokio::spawn(async move {
        if let Err(e) = run_health_monitor().await {
            error!("health_monitor: fatal error: {e}");
        }
    });
}

/// Runs the health monitor loop. Returns only on unrecoverable error.
async fn run_health_monitor() -> anyhow::Result<()> {
    use tokio::io::AsyncReadExt;
    use tokio::net::windows::named_pipe::{PipeMode, ServerOptions};

    // Ensure data dir exists so append_watchdog_log can write.
    if let Err(e) = std::fs::create_dir_all(data_dir()) {
        warn!(error = %e, "health_monitor: could not create data dir");
    }

    info!(pipe = HEALTH_PIPE_NAME, "health_monitor: starting");

    // Outer loop: accept one connection at a time.
    loop {
        let server = match ServerOptions::new()
            .pipe_mode(PipeMode::Byte)
            .first_pipe_instance(true)
            .in_buffer_size(64)
            .out_buffer_size(64)
            .create(HEALTH_PIPE_NAME)
        {
            Ok(s) => s,
            Err(e) => {
                warn!(
                    error = %e,
                    "health_monitor: create pipe failed; retrying in {}s",
                    READ_TIMEOUT.as_secs()
                );
                tokio::time::sleep(READ_TIMEOUT).await;
                continue;
            }
        };

        // Wait for the agent to connect. No timeout — the agent may take an
        // arbitrary amount of time to start. The staleness counter only
        // starts counting after the first successful connect.
        if let Err(e) = server.connect().await {
            warn!(error = %e, "health_monitor: pipe connect failed");
            continue;
        }
        info!("health_monitor: agent connected to health pipe");

        let mut reader = server;
        let mut last_ping = Instant::now();

        // Inner loop: read pings until the pipe breaks or staleness triggers.
        loop {
            let mut buf = [0u8; 16];
            // Read with a short timeout so we can also poll staleness.
            let read_res =
                tokio::time::timeout(READ_TIMEOUT, reader.read(&mut buf)).await;

            match read_res {
                Ok(Ok(0)) => {
                    warn!("health_monitor: agent disconnected (EOF)");
                    record_tamper_to_log(
                        "pipe_connect_failed",
                        "medium",
                        json!({
                            "reason": "agent_disconnected_eof",
                            "last_ping_secs_ago": last_ping.elapsed().as_secs(),
                        }),
                    );
                    break;
                }
                Ok(Ok(n)) => {
                    debug!(bytes = n, "health_monitor: heartbeat received");
                    last_ping = Instant::now();
                }
                Ok(Err(e)) => {
                    warn!(error = %e, "health_monitor: pipe read error");
                    break;
                }
                Err(_elapsed) => {
                    // Read timeout — check staleness.
                    let since = last_ping.elapsed();
                    if since > STALENESS_TIMEOUT {
                        error!(
                            since_secs = since.as_secs(),
                            threshold = STALENESS_TIMEOUT.as_secs(),
                            "health_monitor: TAMPER — agent unresponsive"
                        );
                        record_tamper_to_log(
                            "agent_unresponsive",
                            "critical",
                            json!({
                                "since_last_ping_secs": since.as_secs(),
                                "threshold_secs": STALENESS_TIMEOUT.as_secs(),
                                "heartbeat_interval_secs": HEARTBEAT_INTERVAL.as_secs(),
                            }),
                        );
                        invoke_service_restart();
                        // Break so we accept a fresh pipe client after restart.
                        break;
                    }
                }
            }
        }

        // Brief delay before rebuilding the server to avoid tight loops
        // when pipe creation and teardown race the agent restart.
        tokio::time::sleep(Duration::from_secs(2)).await;
    }
}

/// Appends a single JSON line to `watchdog.log`. The agent reads and
/// truncates this file on next startup, replaying entries as critical
/// tamper events.
///
/// # File-format contract
///
/// Each line MUST be a standalone JSON object with the fields:
///
/// ```json
/// {"check":"agent_unresponsive","severity":"critical","details":{...},"unix":1712990123}
/// ```
///
/// See `personel_agent::anti_tamper::WatchdogLogLine` for the deserialiser.
pub fn record_tamper_to_log(check: &str, severity: &str, details: serde_json::Value) {
    use std::fs::OpenOptions;
    use std::io::Write;

    let unix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);

    let line = json!({
        "check": check,
        "severity": severity,
        "details": details,
        "unix": unix,
    })
    .to_string();

    let path = watchdog_log_path();
    match OpenOptions::new().create(true).append(true).open(&path) {
        Ok(mut f) => {
            if let Err(e) = writeln!(f, "{}", line) {
                error!(error = %e, path = %path.display(), "health_monitor: log write failed");
            } else {
                info!(path = %path.display(), "health_monitor: tamper entry persisted");
            }
        }
        Err(e) => {
            error!(error = %e, path = %path.display(), "health_monitor: open log failed");
        }
    }
}

/// Best-effort service restart via `sc.exe start PersonelAgent`.
///
/// Failures are logged but not propagated — the main watchdog restart
/// loop in `main.rs` will still detect a missing process on its next tick
/// (every 5 s) and attempt its own restart path. This second restart
/// attempt from the health monitor exists because the process may still
/// technically be running (hung) while the heartbeat has stopped, so a
/// name-based liveness check in `main.rs` would not trigger.
pub fn invoke_service_restart() {
    info!("health_monitor: attempting to restart PersonelAgent via sc.exe");
    match std::process::Command::new("sc")
        .args(["start", "PersonelAgent"])
        .output()
    {
        Ok(out) if out.status.success() => {
            info!("health_monitor: sc start PersonelAgent succeeded");
        }
        Ok(out) => {
            let stderr = String::from_utf8_lossy(&out.stderr);
            let stdout = String::from_utf8_lossy(&out.stdout);
            warn!(%stderr, %stdout, "health_monitor: sc start non-zero exit");
        }
        Err(e) => {
            error!(error = %e, "health_monitor: sc.exe invocation failed");
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
    fn staleness_timeout_is_ninety_seconds() {
        assert_eq!(STALENESS_TIMEOUT.as_secs(), 90);
    }

    #[test]
    fn read_timeout_is_positive() {
        assert!(READ_TIMEOUT.as_secs() > 0);
    }

    #[test]
    fn watchdog_log_path_is_under_programdata() {
        let p = watchdog_log_path();
        let s = p.display().to_string();
        assert!(
            s.contains("Personel") && s.contains("watchdog.log"),
            "unexpected watchdog log path: {}",
            s
        );
    }

    #[test]
    fn pipe_name_matches_agent_side() {
        // If this ever diverges from personel_agent::health_pipe::HEALTH_PIPE_NAME
        // the watchdog and agent will silently stop hearing each other.
        assert_eq!(HEALTH_PIPE_NAME, r"\\.\pipe\personel-agent-health");
    }
}
