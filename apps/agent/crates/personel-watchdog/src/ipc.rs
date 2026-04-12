//! Watchdog IPC server — named pipe (Windows) / Unix socket (dev).
//!
//! Listens for JSON-line commands from `personel-agent` and external tooling.
//!
//! # Command protocol
//!
//! Each command is a single UTF-8 JSON line terminated with `\n`.
//!
//! | `cmd` field       | Payload fields              | Action                                     |
//! |-------------------|-----------------------------|--------------------------------------------|
//! | `update_ready`    | `path`, `hash`              | Verify hash → stop → swap → start → health |
//! | `heartbeat_ack`   | _(none)_                    | Reset consecutive-miss counter             |
//! | `tamper_alert`    | `detail`                    | Log + emit event; future: escalate         |
//! | `health_ok`       | `version`                   | Confirm post-update agent health           |
//!
//! # Update swap sequence (steps 5-7 from spec)
//!
//! 1. Verify SHA-256 of `path` matches `hash`.
//! 2. `sc stop personel-agent` (Windows) / direct kill (dev).
//! 3. Rename `personel-agent.exe` → `personel-agent.exe.old`.
//! 4. Rename staged `path` → `personel-agent.exe`.
//! 5. `sc start personel-agent`.
//! 6. Wait up to 60 s for a `health_ok` command on the pipe.
//! 7. If no health within 60 s → rollback: stop, rename `.old` → current, start.

#![deny(unsafe_code)]

use std::path::PathBuf;
use std::time::Duration;

use serde::Deserialize;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::sync::mpsc;
use tracing::{error, info, warn};

// ── Constants ─────────────────────────────────────────────────────────────────

/// Maximum time to wait for a `health_ok` before rolling back.
const HEALTH_TIMEOUT: Duration = Duration::from_secs(60);

/// Named pipe / socket path identifiers.
#[cfg(target_os = "windows")]
const PIPE_NAME: &str = r"\\.\pipe\personel-watchdog-cmd";

#[cfg(not(target_os = "windows"))]
const SOCKET_PATH: &str = "/tmp/personel-watchdog.sock";

// ── IPC command protocol ──────────────────────────────────────────────────────

/// Incoming commands from the agent or update tooling.
#[derive(Debug, Deserialize)]
#[serde(tag = "cmd", rename_all = "snake_case")]
enum IpcCommand {
    /// Agent notifies watchdog that a staged update binary is ready.
    UpdateReady { path: String, hash: String },
    /// Agent acknowledges watchdog's heartbeat (bidirectional heartbeat).
    HeartbeatAck,
    /// Agent or OS security layer detected tampering.
    TamperAlert { detail: String },
    /// Agent reports healthy startup after an update swap.
    HealthOk { version: String },
}

// ── Public interface ──────────────────────────────────────────────────────────

/// Sender used by the IPC server to push events to the watchdog main loop.
pub type IpcEventTx = mpsc::Sender<IpcEvent>;

/// Events the IPC server delivers to the watchdog main loop.
#[derive(Debug)]
pub enum IpcEvent {
    /// Reset the consecutive-miss counter.
    HeartbeatAck,
    /// Tamper alert — main loop should log + emit.
    TamperAlert(String),
}

/// Spawns the IPC listener task and returns the event sender.
///
/// The spawned task runs until the server errors fatally or `tokio` shuts down.
/// It emits [`IpcEvent`]s on `event_tx` for the main watchdog loop to act on.
/// Update commands are handled internally (swap + restart + rollback).
pub fn spawn_ipc_server(event_tx: IpcEventTx) {
    tokio::spawn(async move {
        if let Err(e) = run_server(event_tx).await {
            error!("IPC server fatal error: {e}");
        }
    });
}

// ── Server implementation ─────────────────────────────────────────────────────

/// Top-level server loop. Accepts one client at a time.
async fn run_server(event_tx: IpcEventTx) -> anyhow::Result<()> {
    #[cfg(target_os = "windows")]
    run_named_pipe_server(event_tx).await?;

    #[cfg(not(target_os = "windows"))]
    run_unix_server(event_tx).await?;

    Ok(())
}

// ── Windows named pipe server ─────────────────────────────────────────────────

#[cfg(target_os = "windows")]
async fn run_named_pipe_server(event_tx: IpcEventTx) -> anyhow::Result<()> {
    use tokio::net::windows::named_pipe::{PipeMode, ServerOptions};

    info!("IPC: listening on named pipe {}", PIPE_NAME);

    loop {
        let server = ServerOptions::new()
            .pipe_mode(PipeMode::Byte)
            .in_buffer_size(4096)
            .out_buffer_size(4096)
            .create(PIPE_NAME)?;

        server.connect().await?;
        info!("IPC: client connected on named pipe");

        let reader = BufReader::new(server);
        handle_client(reader, event_tx.clone()).await;
    }
}

// ── Unix socket server (dev / macOS / Linux) ──────────────────────────────────

#[cfg(not(target_os = "windows"))]
async fn run_unix_server(event_tx: IpcEventTx) -> anyhow::Result<()> {
    use tokio::net::UnixListener;

    // Remove stale socket file from previous run.
    let _ = tokio::fs::remove_file(SOCKET_PATH).await;

    let listener = UnixListener::bind(SOCKET_PATH)?;
    info!("IPC: listening on Unix socket {}", SOCKET_PATH);

    loop {
        let (stream, _addr) = listener.accept().await?;
        info!("IPC: client connected on Unix socket");
        let tx = event_tx.clone();
        tokio::spawn(async move {
            let reader = BufReader::new(stream);
            handle_client(reader, tx).await;
        });
    }
}

// ── Client handler ────────────────────────────────────────────────────────────

/// Reads newline-delimited JSON commands from a connected client.
async fn handle_client<R>(mut reader: BufReader<R>, event_tx: IpcEventTx)
where
    R: tokio::io::AsyncRead + Unpin,
{
    let mut line = String::new();
    loop {
        line.clear();
        match reader.read_line(&mut line).await {
            Ok(0) => {
                info!("IPC: client disconnected");
                break;
            }
            Ok(_) => {
                dispatch_command(line.trim(), &event_tx).await;
            }
            Err(e) => {
                warn!("IPC: read error: {e}");
                break;
            }
        }
    }
}

/// Parses and dispatches a single JSON command line.
async fn dispatch_command(json: &str, event_tx: &IpcEventTx) {
    let cmd: IpcCommand = match serde_json::from_str(json) {
        Ok(c) => c,
        Err(e) => {
            warn!("IPC: invalid JSON command ({e}): {json}");
            return;
        }
    };

    match cmd {
        IpcCommand::UpdateReady { path, hash } => {
            info!("IPC: update_ready — path={path} hash={hash}");
            handle_update_ready(path, hash).await;
        }
        IpcCommand::HeartbeatAck => {
            info!("IPC: heartbeat_ack received");
            let _ = event_tx.send(IpcEvent::HeartbeatAck).await;
        }
        IpcCommand::TamperAlert { detail } => {
            error!("IPC: TAMPER ALERT — {detail}");
            let _ = event_tx.send(IpcEvent::TamperAlert(detail)).await;
        }
        IpcCommand::HealthOk { version } => {
            // health_ok arrives on this pipe only in the rollback-wait window,
            // which is managed inside handle_update_ready. An unexpected
            // health_ok outside that window is just logged.
            info!("IPC: health_ok received (version={version}) — no pending update swap");
        }
    }
}

// ── Update swap ───────────────────────────────────────────────────────────────

/// Full update swap sequence: verify → stop → swap → start → health → rollback.
async fn handle_update_ready(staged_path: String, expected_hash: String) {
    // Step 1: verify hash of staged binary.
    if let Err(e) = verify_staged_hash(&staged_path, &expected_hash).await {
        error!("update_ready: staged binary hash mismatch — aborting swap: {e}");
        return;
    }
    info!("update_ready: staged binary hash verified");

    // Step 2: stop current agent service.
    stop_agent_service().await;

    // Determine agent binary path.
    let current = agent_binary_path();
    let backup = agent_binary_path_old();

    // Step 3: rename current → .old
    if let Err(e) = tokio::fs::rename(&current, &backup).await {
        error!("update_ready: rename current→.old failed ({e}); restarting original");
        start_agent_service().await;
        return;
    }
    info!("update_ready: renamed current binary to .old");

    // Step 4: rename staged → current
    if let Err(e) = tokio::fs::rename(&staged_path, &current).await {
        error!("update_ready: rename staged→current failed ({e}); rolling back");
        rollback(&current, &backup).await;
        return;
    }
    info!("update_ready: staged binary moved to current path");

    // Step 5: start updated agent.
    start_agent_service().await;

    // Step 6: wait for health_ok (a fresh IPC connection from the new agent).
    info!("update_ready: waiting up to {}s for health confirmation", HEALTH_TIMEOUT.as_secs());
    match wait_for_health_ok().await {
        Ok(version) => {
            info!("update_ready: health confirmed — version={version}; removing .old");
            let _ = tokio::fs::remove_file(&backup).await;
        }
        Err(()) => {
            error!(
                "update_ready: no health_ok within {}s — rolling back",
                HEALTH_TIMEOUT.as_secs()
            );
            stop_agent_service().await;
            rollback(&current, &backup).await;
        }
    }
}

/// Verifies the SHA-256 of the staged file against `expected_hex`.
async fn verify_staged_hash(path: &str, expected_hex: &str) -> anyhow::Result<()> {
    use sha2::{Digest, Sha256};

    let data = tokio::fs::read(path).await?;
    let actual = hex::encode(Sha256::digest(&data));
    if actual.eq_ignore_ascii_case(expected_hex) {
        Ok(())
    } else {
        Err(anyhow::anyhow!(
            "expected {expected_hex} got {actual}"
        ))
    }
}

/// Rollback: rename .old → current and restart.
async fn rollback(current: &PathBuf, backup: &PathBuf) {
    if let Err(e) = tokio::fs::rename(backup, current).await {
        error!("rollback: rename .old→current failed: {e}");
    } else {
        info!("rollback: restored previous binary");
    }
    start_agent_service().await;
}

// ── Health-ok wait ────────────────────────────────────────────────────────────

/// Opens a *second* IPC listener on the same path and waits for a `health_ok`
/// message from the newly-started agent within [`HEALTH_TIMEOUT`].
///
/// Returns `Ok(version)` on success, `Err(())` on timeout or error.
async fn wait_for_health_ok() -> Result<String, ()> {
    tokio::time::timeout(HEALTH_TIMEOUT, receive_health_ok())
        .await
        .unwrap_or(Err(()))
}

/// Accepts one connection and reads lines until a `health_ok` is found.
async fn receive_health_ok() -> Result<String, ()> {
    #[cfg(target_os = "windows")]
    {
        use tokio::net::windows::named_pipe::{PipeMode, ServerOptions};
        use tokio::io::AsyncBufReadExt;

        let server = ServerOptions::new()
            .pipe_mode(PipeMode::Byte)
            .create(PIPE_NAME)
            .map_err(|_| ())?;
        server.connect().await.map_err(|_| ())?;

        let mut reader = BufReader::new(server);
        let mut line = String::new();
        loop {
            line.clear();
            if reader.read_line(&mut line).await.map_err(|_| ())? == 0 {
                return Err(());
            }
            if let Ok(IpcCommand::HealthOk { version }) = serde_json::from_str(line.trim()) {
                return Ok(version);
            }
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        use tokio::net::UnixListener;
        use tokio::io::AsyncBufReadExt;

        // Bind on a dedicated health socket to avoid conflicting with the main server.
        const HEALTH_SOCK: &str = "/tmp/personel-watchdog-health.sock";
        let _ = tokio::fs::remove_file(HEALTH_SOCK).await;
        let listener = UnixListener::bind(HEALTH_SOCK).map_err(|_| ())?;
        let (stream, _) = listener.accept().await.map_err(|_| ())?;

        let mut reader = BufReader::new(stream);
        let mut line = String::new();
        loop {
            line.clear();
            if reader.read_line(&mut line).await.map_err(|_| ())? == 0 {
                return Err(());
            }
            if let Ok(IpcCommand::HealthOk { version }) = serde_json::from_str(line.trim()) {
                return Ok(version);
            }
        }
    }
}

// ── OS helpers ────────────────────────────────────────────────────────────────

/// Returns the expected path of the running agent binary.
fn agent_binary_path() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        PathBuf::from(r"C:\Program Files\Personel\personel-agent.exe")
    }
    #[cfg(not(target_os = "windows"))]
    {
        PathBuf::from("/usr/local/bin/personel-agent")
    }
}

/// Returns the path for the rollback copy of the agent binary.
fn agent_binary_path_old() -> PathBuf {
    let mut p = agent_binary_path();
    let name = p.file_name().unwrap_or_default().to_string_lossy().into_owned();
    p.set_file_name(format!("{name}.old"));
    p
}

/// Stops the agent service.
async fn stop_agent_service() {
    info!("watchdog: stopping personel-agent service");

    #[cfg(target_os = "windows")]
    {
        let out = tokio::process::Command::new("sc")
            .args(["stop", "personel-agent"])
            .output()
            .await;
        match out {
            Ok(o) if o.status.success() => info!("watchdog: sc stop succeeded"),
            Ok(o) => warn!("watchdog: sc stop non-zero: {}", String::from_utf8_lossy(&o.stderr)),
            Err(e) => error!("watchdog: sc stop failed: {e}"),
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        // Dev: just a best-effort kill by name via sysinfo.
        use sysinfo::System;
        let mut sys = System::new();
        sys.refresh_processes();
        for (pid, _proc) in sys.processes() {
            if _proc.name() == "personel-agent" {
                _proc.kill();
                info!("watchdog: sent SIGKILL to personel-agent pid={pid}");
            }
        }
    }

    // Give the process a moment to actually stop.
    tokio::time::sleep(Duration::from_secs(2)).await;
}

/// Starts the agent service.
async fn start_agent_service() {
    info!("watchdog: starting personel-agent service");

    #[cfg(target_os = "windows")]
    {
        let out = tokio::process::Command::new("sc")
            .args(["start", "personel-agent"])
            .output()
            .await;
        match out {
            Ok(o) if o.status.success() => info!("watchdog: sc start succeeded"),
            Ok(o) => warn!("watchdog: sc start non-zero: {}", String::from_utf8_lossy(&o.stderr)),
            Err(e) => error!("watchdog: sc start failed: {e}"),
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        let _ = tokio::process::Command::new("personel-agent")
            .arg("--console")
            .spawn();
    }
}
