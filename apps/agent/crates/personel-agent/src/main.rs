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

mod anti_tamper;
mod config;
mod crash_dump;
#[cfg(target_os = "windows")]
mod health_pipe;
mod runtime;
mod service;
mod throttle;

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

    // Install the process-wide unhandled-exception filter BEFORE the tokio
    // runtime starts. On Windows this registers an SEH top-level filter that
    // writes a MiniDump to `%ProgramData%\Personel\agent\dumps\` on crash.
    // Faz 4 Wave 1 #31.
    crash_dump::install_unhandled_exception_filter();

    // Determine run mode as early as possible. We must defer all heavy
    // bring-up work (runtime build, config load, queue open, TLS channel
    // setup) until AFTER we have registered the SCM control handler —
    // otherwise Windows SCM kills us with error 1053 ("service did not
    // respond to start request in a timely fashion") when that deadline
    // (~30 s from process start) passes before we call
    // `service_dispatcher::start`.
    let is_service = !force_console && !debug && personel_platform::service::is_service_context();

    if is_service {
        // Windows service mode: the SCM owns the lifecycle.
        //
        // The ordering here is load-bearing: `os_run_as_service` calls
        // `service_dispatcher::start` which is the fast path Windows is
        // waiting for. All agent bring-up (runtime build, config load,
        // queue open, enrollment check, TLS channel, collector registry
        // spawn) is packaged into the body closure and executed AFTER the
        // SCM control handler has been registered and the service has
        // transitioned to Running.
        info!("starting in Windows service mode");

        #[cfg(target_os = "windows")]
        {
            os_run_as_service(move |shutdown_rx| {
                // Build the tokio runtime INSIDE the service body so the
                // thread creation + worker spawn does not count toward the
                // SCM start deadline.
                let rt = match runtime::build_runtime() {
                    Ok(rt) => rt,
                    Err(e) => {
                        tracing::error!(error = %e, "service body: build_runtime failed");
                        return;
                    }
                };

                // Load the agent config inside the body too — any file IO
                // or DPAPI unwrap here is safe because we are past the
                // SCM deadline.
                let data_dir = crate::config::default_data_dir();
                let agent_config = match crate::config::AgentConfig::load_or_default(&data_dir) {
                    Ok(c) => c,
                    Err(e) => {
                        tracing::error!(error = %e, "service body: load agent config failed");
                        return;
                    }
                };

                rt.block_on(async move {
                    if let Err(e) = service::run_agent(agent_config, shutdown_rx).await {
                        tracing::error!(error = %e, "service body: run_agent returned error");
                    }
                });
            })
            .context("Windows SCM dispatcher")?;
        }
        #[cfg(not(target_os = "windows"))]
        {
            // Non-Windows service fallback path. We still load the config
            // + build the runtime up-front since there is no SCM deadline.
            let data_dir = crate::config::default_data_dir();
            let agent_config = crate::config::AgentConfig::load_or_default(&data_dir)
                .context("load agent config")?;
            let rt = runtime::build_runtime().context("build tokio runtime")?;
            rt.block_on(async move {
                let (shutdown_tx, shutdown_rx) = oneshot::channel();
                tokio::spawn(runtime::wait_for_shutdown(shutdown_tx));
                service::run_agent(agent_config, shutdown_rx).await
            })
            .context("agent run_agent (non-windows service fallback)")?;
        }
    } else {
        // Console / debug mode — no SCM deadline, do the bring-up inline.
        info!("starting in console mode");
        let data_dir = crate::config::default_data_dir();
        let agent_config = crate::config::AgentConfig::load_or_default(&data_dir)
            .context("load agent config")?;
        let rt = runtime::build_runtime().context("build tokio runtime")?;
        rt.block_on(async move {
            let (shutdown_tx, shutdown_rx) = oneshot::channel();
            tokio::spawn(runtime::wait_for_shutdown(shutdown_tx));
            service::run_agent(agent_config, shutdown_rx).await
        })
        .context("agent run_agent")?;
    }

    Ok(())
}
