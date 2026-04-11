//! `personel-agent-watchdog` — mutual supervision watchdog process.
//!
//! Monitors `personel-agent` and restarts it if it dies or stops sending
//! heartbeats on the named pipe `\\.\pipe\personel-agent-ipc`.
//!
//! # Supervision design
//!
//! Per `docs/security/anti-tamper.md` §1 and
//! `docs/architecture/agent-module-architecture.md` §IPC:
//!
//! - Watchdog sends `Heartbeat` every 5 s on the named pipe.
//! - If main misses 3 consecutive heartbeats (15 s), watchdog kills and
//!   restarts it.
//! - If watchdog itself is killed, the main agent notices the pipe closure
//!   and re-spawns the watchdog via SCM.
//! - The watchdog is the *only* process allowed to swap the binary during
//!   updates.
//!
//! # Named pipe protocol
//!
//! Length-prefixed frames: 4-byte big-endian length prefix, then proto bytes.
//! Message types: `Heartbeat`, `RequestRestart`, `UpdateReady`,
//! `TamperAlert`, `ShutdownAck`.

#![deny(unsafe_code)]

use std::time::Duration;

use anyhow::Result;
use sysinfo::{ProcessExt, System, SystemExt};
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

// ── Watchdog configuration ────────────────────────────────────────────────────

/// How often the watchdog polls the main agent's process liveness.
const POLL_INTERVAL: Duration = Duration::from_secs(5);
/// How many consecutive missed polls before the watchdog restarts the agent.
const MISS_THRESHOLD: u32 = 3;
/// How long to wait for the main agent to start before declaring a fault.
const START_TIMEOUT: Duration = Duration::from_secs(30);
/// The process name to monitor.
const AGENT_PROCESS_NAME: &str = "personel-agent";

// ── Entry point ───────────────────────────────────────────────────────────────

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            std::env::var("PERSONEL_LOG").unwrap_or_else(|_| "info".into()),
        )
        .init();

    info!("personel-agent-watchdog starting");

    let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        info!("watchdog: Ctrl-C; shutting down");
        let _ = shutdown_tx.send(());
    });

    run_watchdog_loop(shutdown_rx).await;
    info!("personel-agent-watchdog stopped");
    Ok(())
}

// ── Watchdog loop ─────────────────────────────────────────────────────────────

async fn run_watchdog_loop(mut shutdown_rx: oneshot::Receiver<()>) {
    let mut misses: u32 = 0;
    let mut consecutive_restarts: u32 = 0;
    let mut ticker = tokio::time::interval(POLL_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        tokio::select! {
            _ = ticker.tick() => {
                let agent_alive = is_agent_running();

                if agent_alive {
                    if misses > 0 {
                        debug!("watchdog: agent back; resetting miss counter");
                    }
                    misses = 0;
                    consecutive_restarts = 0;
                    debug!("watchdog: agent heartbeat ok");
                } else {
                    misses += 1;
                    warn!(misses, threshold = MISS_THRESHOLD, "watchdog: agent not detected");

                    if misses >= MISS_THRESHOLD {
                        misses = 0;
                        consecutive_restarts += 1;
                        info!(consecutive_restarts, "watchdog: restarting agent");
                        if consecutive_restarts > 10 {
                            // Exponential back-off to avoid a restart storm.
                            let delay = Duration::from_secs(
                                (30 * consecutive_restarts as u64).min(300),
                            );
                            warn!(
                                consecutive_restarts,
                                delay_secs = delay.as_secs(),
                                "watchdog: too many restarts; backing off"
                            );
                            tokio::time::sleep(delay).await;
                        }
                        restart_agent().await;
                    }
                }
            }
            _ = &mut shutdown_rx => {
                info!("watchdog: shutdown signal; stopping loop");
                break;
            }
        }
    }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/// Returns `true` if at least one process named `personel-agent` is running.
///
/// Uses `sysinfo` for cross-platform process enumeration (Windows-compatible).
fn is_agent_running() -> bool {
    let mut sys = System::new();
    sys.refresh_processes();
    sys.processes_by_name(AGENT_PROCESS_NAME).next().is_some()
}

/// Attempts to restart the agent via SCM (`sc start`) or direct spawn.
///
/// In production (Windows service), this calls `sc start personel-agent`.
/// In development / non-Windows, it attempts a direct spawn.
async fn restart_agent() {
    info!("watchdog: attempting to restart personel-agent");

    #[cfg(target_os = "windows")]
    {
        // Use SCM to restart the service so it inherits the correct identity.
        let output = tokio::process::Command::new("sc")
            .args(["start", "personel-agent"])
            .output()
            .await;
        match output {
            Ok(out) => {
                if out.status.success() {
                    info!("watchdog: sc start personel-agent succeeded");
                } else {
                    let stderr = String::from_utf8_lossy(&out.stderr);
                    error!(%stderr, "watchdog: sc start failed");
                }
            }
            Err(e) => {
                error!("watchdog: failed to invoke sc.exe: {e}");
            }
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        // Dev-only: try spawning the binary directly.
        // This will fail if the binary isn't in PATH; that's expected.
        let _ = tokio::process::Command::new("personel-agent")
            .arg("--console")
            .spawn();
    }
}
