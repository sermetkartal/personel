//! macOS launchd service integration.
//!
//! Provides helpers for:
//!
//! 1. **Plist generation**: building a `launchd` plist XML string for either
//!    `LaunchDaemons` (root, persistent, boot-time) or `LaunchAgents`
//!    (per-user, login-time).
//!
//! 2. **Context detection**: determining whether the process was launched by
//!    `launchd` (as opposed to an interactive terminal session).
//!
//! 3. **Service registration** (Phase 2.2): invoking `launchctl bootstrap` and
//!    `launchctl enable` at install time via the `postinstall` script.
//!
//! # launchd plist for personel-agent
//!
//! ADR 0015 §"Installer and deployment" specifies the plist is installed to
//! `/Library/LaunchDaemons/com.personel.agent.plist` for the boot-time daemon.
//!
//! # Phase 2.1 status
//!
//! - `generate_launch_daemon_plist`: fully implemented (string builder, no
//!   platform dependency).
//! - `is_launchd_context`: stub returning `false`.
//! - `run_as_launchd_service`: stub returning `Err(Unsupported)`.

use personel_core::error::{AgentError, Result};
use tokio::sync::oneshot;

/// Configuration for generating a launchd plist file.
#[derive(Debug, Clone)]
pub struct LaunchdPlistConfig {
    /// The `Label` key — reverse-DNS identifier, e.g. `com.personel.agent`.
    pub label: String,
    /// Absolute path to the agent executable, e.g. `/Applications/Personel.app/Contents/MacOS/personel-agent`.
    pub program: String,
    /// Command-line arguments passed to `program` (not including argv[0]).
    pub arguments: Vec<String>,
    /// If `true`, the plist is written to `LaunchDaemons` (root, boot-time).
    /// If `false`, it targets `LaunchAgents` (per-user, login-time).
    pub run_at_load: bool,
    /// If `true`, launchd will restart the service if it exits unexpectedly.
    pub keep_alive: bool,
    /// Standard output log path, e.g. `/var/log/personel-agent.log`.
    pub stdout_path: Option<String>,
    /// Standard error log path.
    pub stderr_path: Option<String>,
}

/// Generates a launchd plist XML string from `config`.
///
/// The returned string is ready to be written to disk and loaded by
/// `launchctl bootstrap system <plist>` in the installer's postinstall script.
///
/// This function is **always available** on all platforms (it is a pure string
/// builder). It is called by the installer generator tooling and by tests.
///
/// # Example
///
/// ```rust
/// use personel_os_macos::service::{generate_launch_daemon_plist, LaunchdPlistConfig};
///
/// let cfg = LaunchdPlistConfig {
///     label: "com.personel.agent".to_string(),
///     program: "/Applications/Personel.app/Contents/MacOS/personel-agent".to_string(),
///     arguments: vec!["--service".to_string()],
///     run_at_load: true,
///     keep_alive: true,
///     stdout_path: Some("/var/log/personel-agent.log".to_string()),
///     stderr_path: Some("/var/log/personel-agent-err.log".to_string()),
/// };
/// let xml = generate_launch_daemon_plist(&cfg);
/// assert!(xml.contains("com.personel.agent"));
/// assert!(xml.contains("RunAtLoad"));
/// assert!(xml.contains("KeepAlive"));
/// ```
#[must_use]
pub fn generate_launch_daemon_plist(config: &LaunchdPlistConfig) -> String {
    let args_xml: String = config
        .arguments
        .iter()
        .map(|a| format!("\t\t<string>{a}</string>\n"))
        .collect();

    let stdout_xml = config
        .stdout_path
        .as_deref()
        .map(|p| format!("\t<key>StandardOutPath</key>\n\t<string>{p}</string>\n"))
        .unwrap_or_default();

    let stderr_xml = config
        .stderr_path
        .as_deref()
        .map(|p| format!("\t<key>StandardErrorPath</key>\n\t<string>{p}</string>\n"))
        .unwrap_or_default();

    let run_at_load_val = if config.run_at_load { "<true/>" } else { "<false/>" };
    let keep_alive_val = if config.keep_alive { "<true/>" } else { "<false/>" };

    format!(
        r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{label}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{program}</string>
{args_xml}	</array>
	<key>RunAtLoad</key>
	{run_at_load_val}
	<key>KeepAlive</key>
	{keep_alive_val}
{stdout_xml}{stderr_xml}</dict>
</plist>
"#,
        label = config.label,
        program = config.program,
    )
}

/// Returns `true` if the current process was launched by `launchd`.
///
/// On macOS a heuristic is used: if `getppid()` returns `1` (launchd is PID 1
/// on macOS), the process was launched by launchd.
///
/// # Platform note
///
/// Always returns `false` on non-macOS. On macOS returns `false` in Phase 2.1
/// (stub). Phase 2.2 will enable the `getppid` check.
#[must_use]
pub fn is_launchd_context() -> bool {
    #[cfg(target_os = "macos")]
    {
        // Phase 2.2: unsafe { libc::getppid() == 1 }
        // SAFETY: getppid() is always safe; it has no preconditions and cannot fail.
        false
    }

    #[cfg(not(target_os = "macos"))]
    {
        false
    }
}

/// Runs the current process as a launchd-managed service.
///
/// Blocks until a shutdown signal is sent on `shutdown_tx`. On macOS this
/// installs a signal handler for `SIGTERM` (the signal launchd sends on
/// `launchctl stop`) and bridges it to the tokio oneshot channel.
///
/// # Errors
///
/// - [`AgentError::Unsupported`] in Phase 2.1.
/// - [`AgentError::Internal`] in Phase 2.2+ if signal handler registration
///   fails.
pub fn run_as_launchd_service(_shutdown_tx: oneshot::Sender<()>) -> Result<()> {
    #[cfg(target_os = "macos")]
    {
        // Phase 2.2: install SIGTERM handler, block, forward to shutdown_tx.
        Err(AgentError::Unsupported {
            os: "macos",
            component: "service::run_as_launchd_service",
        })
    }

    #[cfg(not(target_os = "macos"))]
    {
        crate::stub::service::run_as_launchd_service(_shutdown_tx)
    }
}
