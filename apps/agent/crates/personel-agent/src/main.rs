//! `personel-agent` — main entry point.
//!
//! Supports two run modes:
//! 1. **Windows service** (default): launched by the SCM; delegates to
//!    `personel_os::service::run_as_service`.
//! 2. **Console / debug**: launched from a terminal; handles Ctrl-C.
//!
//! Usage:
//! ```text
//! personel-agent               # run as service or console depending on context
//! personel-agent --debug       # force console mode with verbose logging
//! ```

// Application-level binary: anyhow is allowed here.
#![deny(unsafe_code)]

use anyhow::{Context, Result};
use tokio::sync::oneshot;
use tracing::info;
use tracing_subscriber::EnvFilter;

mod config;
mod runtime;
mod service;

#[cfg(target_os = "windows")]
use personel_os::service::run_as_service as os_run_as_service;

fn main() -> Result<()> {
    // Parse minimal CLI args before tokio runtime starts.
    let args: Vec<String> = std::env::args().collect();
    let debug = args.iter().any(|a| a == "--debug");
    let force_console = args.iter().any(|a| a == "--console");

    // Initialise tracing. In service mode, use JSON for structured log ingestion.
    // In debug/console mode, use the pretty formatter.
    if debug || force_console {
        tracing_subscriber::fmt()
            .with_env_filter(EnvFilter::new("debug"))
            .with_target(true)
            .init();
    } else {
        tracing_subscriber::fmt()
            .json()
            .with_env_filter(EnvFilter::new(
                std::env::var("PERSONEL_LOG").unwrap_or_else(|_| "info".into()),
            ))
            .init();
    }

    info!(
        version = config::AGENT_VERSION,
        sha = config::AGENT_GIT_SHA,
        "personel-agent initialising"
    );

    let data_dir = crate::config::default_data_dir();
    let agent_config = crate::config::AgentConfig::load_or_default(&data_dir)
        .context("load agent config")?;

    let rt = runtime::build_runtime().context("build tokio runtime")?;

    // Determine run mode.
    let is_service = !force_console && !debug && personel_platform::service::is_service_context();

    if is_service {
        // Windows service mode: the SCM owns the lifecycle.
        // run_as_service blocks until the SCM sends SERVICE_CONTROL_STOP.
        info!("starting in Windows service mode");

        #[cfg(target_os = "windows")]
        {
            let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
            let (done_tx, done_rx) = oneshot::channel::<()>();
            rt.spawn(async move {
                let _ = service::run_agent(agent_config, shutdown_rx).await;
                let _ = done_tx.send(());
            });
            os_run_as_service(shutdown_tx)
                .context("Windows SCM dispatcher")?;
            let _ = rt.block_on(done_rx);
        }
        #[cfg(not(target_os = "windows"))]
        {
            rt.block_on(async move {
                let (shutdown_tx, shutdown_rx) = oneshot::channel();
                tokio::spawn(runtime::wait_for_shutdown(shutdown_tx));
                service::run_agent(agent_config, shutdown_rx).await
            })
            .context("agent run_agent (non-windows service fallback)")?;
        }
    } else {
        // Console / debug mode.
        info!("starting in console mode");
        rt.block_on(async move {
            let (shutdown_tx, shutdown_rx) = oneshot::channel();
            tokio::spawn(runtime::wait_for_shutdown(shutdown_tx));
            service::run_agent(agent_config, shutdown_rx).await
        })
        .context("agent run_agent")?;
    }

    Ok(())
}
