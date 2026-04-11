//! Agent configuration: enrollment state, paths, and feature flags.
//!
//! Configuration is layered:
//! 1. Compiled-in defaults (this module).
//! 2. On-disk TOML file at `%PROGRAMDATA%\Personel\agent\config.toml`.
//! 3. Policy bundle pushed from server (overrides relevant fields at runtime).
//!
//! The on-disk file must NOT contain secret material. Secrets (PE-DEK, agent
//! private key) are stored as DPAPI blobs in separate files and are never
//! written to config.toml.

use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use personel_core::error::{AgentError, Result};
use personel_core::ids::{EndpointId, TenantId};

/// Compiled-in agent version (set by build.rs from git).
pub const AGENT_VERSION: &str = env!("CARGO_PKG_VERSION");
/// Git SHA for diagnostics (set by build.rs; "dev" in local builds).
pub const AGENT_GIT_SHA: &str = env!("AGENT_GIT_SHA");

/// Fully-qualified default base directory for agent data.
///
/// Falls back to the current directory on non-Windows for dev builds.
#[must_use]
pub fn default_data_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        PathBuf::from(r"C:\ProgramData\Personel\agent")
    }
    #[cfg(not(target_os = "windows"))]
    {
        std::env::current_dir()
            .unwrap_or_else(|_| PathBuf::from("/tmp/personel-agent"))
            .join("personel-agent-data")
    }
}

/// Persistent enrollment state written to disk after first enrollment.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnrollmentConfig {
    /// Tenant UUID (set after enrollment).
    pub tenant_id: String,
    /// Endpoint UUID (set after enrollment).
    pub endpoint_id: String,
    /// Gateway URL (e.g., `https://gw.personel.example:443`).
    pub gateway_url: String,
    /// Tenant CA SPKI SHA-256 pins (hex-encoded, set after enrollment).
    pub spki_pins: Vec<String>,
    /// Agent cert path (DER-encoded, DPAPI-protected separately).
    pub cert_path: PathBuf,
    /// Agent private key path (DER-encoded DPAPI blob).
    pub key_path: PathBuf,
}

/// Top-level agent configuration (on-disk TOML).
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct AgentConfig {
    /// Enrollment state (absent before first enrollment).
    pub enrollment: Option<EnrollmentConfig>,
    /// Override for data directory (usually inferred from binary path).
    pub data_dir: Option<PathBuf>,
    /// Whether to run in verbose debug logging mode.
    #[serde(default)]
    pub debug_logging: bool,
}

impl AgentConfig {
    /// Loads configuration from the standard path, or returns defaults if the
    /// file does not exist yet (pre-enrollment state).
    ///
    /// # Errors
    ///
    /// Returns an error only if the config file exists but cannot be parsed.
    pub fn load_or_default(data_dir: &std::path::Path) -> Result<Self> {
        let path = data_dir.join("config.toml");
        if !path.exists() {
            return Ok(Self::default());
        }
        let contents = std::fs::read_to_string(&path).map_err(AgentError::Io)?;
        toml::from_str(&contents)
            .map_err(|e| AgentError::Config(format!("config.toml parse error: {e}")))
    }

    /// Saves the configuration to the standard path.
    ///
    /// # Errors
    ///
    /// Returns an error if the file cannot be written.
    pub fn save(&self, data_dir: &std::path::Path) -> Result<()> {
        let path = data_dir.join("config.toml");
        let contents = toml::to_string_pretty(self)
            .map_err(|e| AgentError::Config(format!("config serialise error: {e}")))?;
        std::fs::write(path, contents).map_err(AgentError::Io)
    }

    /// Returns parsed `TenantId`.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Config`] if enrollment is absent or the ID is invalid.
    pub fn tenant_id(&self) -> Result<TenantId> {
        let enroll = self.enrollment.as_ref().ok_or_else(|| {
            AgentError::Config("agent not enrolled; run `enroll` first".into())
        })?;
        let uuid = enroll.tenant_id.parse().map_err(|e| {
            AgentError::Config(format!("invalid tenant_id in config: {e}"))
        })?;
        Ok(TenantId::new(uuid))
    }

    /// Returns parsed `EndpointId`.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Config`] if enrollment is absent or the ID is invalid.
    pub fn endpoint_id(&self) -> Result<EndpointId> {
        let enroll = self.enrollment.as_ref().ok_or_else(|| {
            AgentError::Config("agent not enrolled; run `enroll` first".into())
        })?;
        let uuid = enroll.endpoint_id.parse().map_err(|e| {
            AgentError::Config(format!("invalid endpoint_id in config: {e}"))
        })?;
        Ok(EndpointId::new(uuid))
    }
}
