//! Tokio runtime setup and graceful shutdown signal handling.
//!
//! Worker count is capped at 4 to stay within the <2% CPU budget on a
//! typical corporate laptop. The collector tasks are mostly I/O bound
//! (waiting for OS events), so 4 workers is ample.

use tokio::runtime::{Builder, Runtime};
use tokio::signal;
use tokio::sync::oneshot;
use tracing::info;

use anyhow::Result;

/// Maximum tokio worker threads. Conservative for an agent context.
const MAX_WORKER_THREADS: usize = 4;

/// Builds the tokio multi-thread runtime with a conservative thread count.
///
/// # Errors
///
/// Returns an error if the runtime cannot be created (rare system failure).
pub fn build_runtime() -> Result<Runtime> {
    let rt = Builder::new_multi_thread()
        .worker_threads(MAX_WORKER_THREADS)
        .thread_name("personel-worker")
        .enable_all()
        .build()?;
    Ok(rt)
}

/// Waits for Ctrl-C or `SIGTERM` and sends a shutdown signal.
///
/// The `shutdown_tx` is consumed when the signal fires.
pub async fn wait_for_shutdown(shutdown_tx: oneshot::Sender<()>) {
    // On Windows, SIGTERM doesn't exist but we handle Ctrl-C.
    #[cfg(unix)]
    let sigterm = async {
        let mut sig = signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler");
        sig.recv().await;
    };
    #[cfg(not(unix))]
    let sigterm = std::future::pending::<()>();

    tokio::select! {
        _ = signal::ctrl_c() => {
            info!("Ctrl-C received; initiating shutdown");
        }
        _ = sigterm => {
            info!("SIGTERM received; initiating shutdown");
        }
    }

    // Signal the rest of the system to shut down.
    let _ = shutdown_tx.send(());
}
