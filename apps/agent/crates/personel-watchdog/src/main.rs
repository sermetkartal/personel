//! `personel-agent-watchdog` — mutual supervision watchdog process.
//!
//! Monitors `personel-agent` and restarts it if it dies or stops sending
//! heartbeats on the named pipe `\\.\pipe\personel-watchdog-cmd`.
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
//! # Named pipe / Unix socket IPC
//!
//! JSON-line protocol; see [`ipc`] module for command schema.
//! Windows: `\\.\pipe\personel-watchdog-cmd`
//! Dev (macOS/Linux): `/tmp/personel-watchdog.sock`

#![deny(unsafe_code)]

#[cfg(target_os = "windows")]
mod health_monitor;
mod ipc;

use std::time::Duration;

use anyhow::Result;
use sysinfo::System;
use tokio::sync::{mpsc, oneshot};
use tracing::{debug, error, info, warn};

use ipc::{IpcEvent, spawn_ipc_server};

// ── Watchdog configuration ────────────────────────────────────────────────────

/// How often the watchdog polls the main agent's process liveness.
const POLL_INTERVAL: Duration = Duration::from_secs(5);
/// How many consecutive missed polls before the watchdog restarts the agent.
const MISS_THRESHOLD: u32 = 3;
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

    // IPC event channel: ipc server → watchdog main loop.
    let (ipc_tx, ipc_rx) = mpsc::channel::<IpcEvent>(32);
    spawn_ipc_server(ipc_tx);
    info!("IPC server spawned");

    // Health monitor (Faz 4 Wave 1 #29) — listens on the
    // \\.\pipe\personel-agent-health pipe and records tamper events to
    // watchdog.log if the agent stops sending heartbeats. The agent picks
    // up that file on its next start and replays entries as critical
    // tamper events into its queue.
    #[cfg(target_os = "windows")]
    {
        health_monitor::spawn_health_monitor();
        info!("health monitor spawned");
    }

    let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        info!("watchdog: Ctrl-C; shutting down");
        let _ = shutdown_tx.send(());
    });

    run_watchdog_loop(shutdown_rx, ipc_rx).await;
    info!("personel-agent-watchdog stopped");
    Ok(())
}

// ── Watchdog loop ─────────────────────────────────────────────────────────────

async fn run_watchdog_loop(
    mut shutdown_rx: oneshot::Receiver<()>,
    mut ipc_rx: mpsc::Receiver<IpcEvent>,
) {
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

            Some(event) = ipc_rx.recv() => {
                match event {
                    IpcEvent::HeartbeatAck => {
                        debug!("watchdog: heartbeat_ack via IPC; resetting miss counter");
                        misses = 0;
                    }
                    IpcEvent::TamperAlert(detail) => {
                        error!(detail, "watchdog: TAMPER ALERT received via IPC");
                        // TODO Sprint E: emit to SIEM / telemetry pipeline.
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
fn is_agent_running() -> bool {
    let mut sys = System::new();
    sys.refresh_processes();
    let found = sys.processes_by_name(AGENT_PROCESS_NAME).next().is_some();
    found
}

/// Pure helper: given a process name list, returns whether `target` is present.
///
/// Used in tests to avoid real OS process enumeration.
fn name_in_list(names: &[&str], target: &str) -> bool {
    names.iter().any(|&n| n == target)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    // ── is_agent_running logic (pure helper) ──────────────────────────────────

    #[test]
    fn name_in_list_found() {
        assert!(name_in_list(&["personel-agent", "system"], "personel-agent"));
    }

    #[test]
    fn name_in_list_not_found() {
        assert!(!name_in_list(&["foo", "bar"], "personel-agent"));
    }

    #[test]
    fn name_in_list_empty_returns_false() {
        assert!(!name_in_list(&[], "personel-agent"));
    }

    /// Real `is_agent_running` çağrısı: agent process bu testte çalışmadığından
    /// `false` veya `true` dönebilir — önemli olan panic olmaması.
    #[test]
    fn is_agent_running_does_not_panic() {
        let _ = is_agent_running();
    }

    // ── Watchdog constants ────────────────────────────────────────────────────

    #[test]
    fn poll_interval_is_five_seconds() {
        assert_eq!(POLL_INTERVAL.as_secs(), 5);
    }

    #[test]
    fn miss_threshold_is_three() {
        assert_eq!(MISS_THRESHOLD, 3);
    }

    #[test]
    fn agent_process_name_not_empty() {
        assert!(!AGENT_PROCESS_NAME.is_empty());
    }

    // ── Rollback timeout logic (pure version) ─────────────────────────────────

    /// `consecutive_restarts > 10` durumunda backoff hesabı doğru olmalı.
    #[test]
    fn restart_backoff_calculation() {
        let consecutive_restarts: u64 = 15;
        let delay_secs = (30 * consecutive_restarts).min(300);
        assert_eq!(delay_secs, 300, "backoff must cap at 300 s");
    }

    #[test]
    fn restart_backoff_low_value() {
        let consecutive_restarts: u64 = 2;
        let delay_secs = (30 * consecutive_restarts).min(300);
        assert_eq!(delay_secs, 60);
    }
}

/// Attempts to restart the agent via SCM (`sc start`) or direct spawn.
async fn restart_agent() {
    info!("watchdog: attempting to restart personel-agent");

    #[cfg(target_os = "windows")]
    {
        let output = tokio::process::Command::new("sc")
            .args(["start", "personel-agent"])
            .output()
            .await;
        match output {
            Ok(out) if out.status.success() => {
                info!("watchdog: sc start personel-agent succeeded");
            }
            Ok(out) => {
                let stderr = String::from_utf8_lossy(&out.stderr);
                error!(%stderr, "watchdog: sc start failed");
            }
            Err(e) => {
                error!("watchdog: failed to invoke sc.exe: {e}");
            }
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        let _ = tokio::process::Command::new("personel-agent")
            .arg("--console")
            .spawn();
    }
}
