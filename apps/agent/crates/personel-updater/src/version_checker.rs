//! Agent-initiated auto-update version checker (Faz 4 #36).
//!
//! Periodically polls a manifest URL on the gateway / update server to
//! discover newer agent versions. When a newer version is found, the
//! package `.tar.gz` is downloaded and staged at
//! `<download_dir>\incoming\update.tar.gz` — the exact same path that the
//! server-pushed `UpdateNotify` apply flow watches.
//!
//! ## Design split: download vs apply
//!
//! This task ONLY downloads. It never calls [`crate::apply_update`].
//! Application happens via one of:
//!
//! 1. A server-pushed `UpdateNotify` message that tells the agent to run
//!    [`crate::apply_update`] against the already-staged file (Phase 1
//!    shipped path — see `personel-transport::client`).
//! 2. On next service restart, if a startup hook notices the staged file
//!    (Phase 2 follow-up).
//!
//! Separating the two is deliberate: the network-facing download is
//! fully automated (safe because the package must still pass Ed25519
//! signature + SHA-256 verification in `package.rs` before the swap),
//! but the cutover moment — when the running agent is replaced — stays
//! under operator / server control. KVKK + anti-tamper invariants are
//! enforced by [`crate::verify_update_package`], not here.
//!
//! ## Failure policy
//!
//! Every step is best-effort. HTTP errors, JSON parse errors, disk I/O
//! errors, and TLS errors all result in a `warn!` log + early return
//! from this tick; the next tick will retry. The checker loop itself
//! never exits on a transient failure — only a fatal `stop_rx` signal
//! or the TLS CA being unusable at startup stops it.

#![deny(unsafe_code)]

use std::cmp::Ordering;
use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use tokio::time::{interval, MissedTickBehavior};
use tracing::{debug, info, warn};

use crate::package::UpdateError;

// ── Config ────────────────────────────────────────────────────────────────────

/// Runtime configuration for [`run_version_checker`].
#[derive(Debug, Clone)]
pub struct VersionCheckerConfig {
    /// Current agent version — typically `env!("CARGO_PKG_VERSION")` captured
    /// at the call site so it reflects the actual running binary.
    pub current_version: String,
    /// Fully qualified URL to the manifest JSON document, e.g.
    /// `https://updates.personel.internal/agent/manifest.json`.
    pub manifest_url: String,
    /// Directory where `incoming/update.tar.gz` will be staged. The parent
    /// `incoming/` subdirectory is created if it does not exist.
    pub download_dir: PathBuf,
    /// Poll interval between version checks. Defaults to 6 hours if zero.
    pub check_interval: Duration,
    /// PEM-encoded TLS root CA bytes used to verify the manifest server. If
    /// `None`, the platform root store is used.
    pub tls_ca_pem: Option<Vec<u8>>,
}

impl VersionCheckerConfig {
    /// Default polling interval (6 hours) when [`Self::check_interval`] is
    /// zero.
    pub const DEFAULT_INTERVAL: Duration = Duration::from_secs(6 * 60 * 60);
}

/// Wire format returned by the update server.
///
/// `manifest_signature` is carried alongside for callers that want to
/// pre-stage the signature file; the authoritative signature check
/// happens later inside [`crate::verify_update_package`] against the
/// `manifest.sig` embedded in the downloaded `.tar.gz`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct UpdateManifest {
    /// Latest version the server is advertising.
    pub latest_version: String,
    /// Absolute URL to the `.tar.gz` update package.
    pub package_url: String,
    /// Optional detached manifest signature (hex or base64). Staged as
    /// `update.manifest.json.sig` when present.
    #[serde(default)]
    pub manifest_signature: Option<String>,
}

// ── Semver comparison ─────────────────────────────────────────────────────────

/// Compare two dotted-decimal semver strings.
///
/// Supported forms:
///
/// - `MAJOR.MINOR.PATCH`
/// - `MAJOR.MINOR.PATCH-PRE` where `PRE` is any non-empty suffix
///
/// Comparison order:
///
/// 1. Numeric major, then minor, then patch.
/// 2. If numeric triples are equal, a pre-release suffix sorts as
///    **less than** the unsuffixed version. (Two pre-release suffixes
///    compare lexicographically — sufficient for `rc1 < rc2 < rc10`
///    only if the tag widths match; this is documented and not a bug
///    for the agent's rc<->rc ordering needs.)
///
/// Unparseable components fall back to `0`, which means a malformed
/// candidate will never compare as "greater than" a sane current version.
#[must_use]
pub fn compare_semver(current: &str, candidate: &str) -> Ordering {
    let (cur_trip, cur_pre) = split_semver(current);
    let (cand_trip, cand_pre) = split_semver(candidate);

    match cur_trip.cmp(&cand_trip) {
        Ordering::Equal => {}
        non_eq => return non_eq,
    }

    // Numeric triples equal — pre-release rules:
    //   None vs None → Equal
    //   Some vs None → Less     (pre-release < release)
    //   None vs Some → Greater
    //   Some vs Some → lexicographic
    match (cur_pre, cand_pre) {
        (None, None) => Ordering::Equal,
        (Some(_), None) => Ordering::Less,
        (None, Some(_)) => Ordering::Greater,
        (Some(a), Some(b)) => a.cmp(&b),
    }
}

fn split_semver(v: &str) -> ((u64, u64, u64), Option<String>) {
    let trimmed = v.trim().trim_start_matches('v');
    let (core, pre) = match trimmed.split_once('-') {
        Some((core, pre)) => (core, Some(pre.to_string())),
        None => (trimmed, None),
    };
    let mut parts = core.split('.').map(|p| p.parse::<u64>().unwrap_or(0));
    let major = parts.next().unwrap_or(0);
    let minor = parts.next().unwrap_or(0);
    let patch = parts.next().unwrap_or(0);
    ((major, minor, patch), pre)
}

// ── Checker loop ──────────────────────────────────────────────────────────────

/// Run the periodic version checker until `stop_rx` is signalled.
///
/// Every `check_interval`, this task will:
///
/// 1. GET the manifest URL.
/// 2. If `latest_version > current_version`, GET the package URL and
///    stage it at `<download_dir>/incoming/update.tar.gz` via
///    write-to-`.tmp` + atomic rename.
/// 3. Optionally stage `update.manifest.json.sig` from
///    `manifest_signature`.
/// 4. Log and wait for the next tick.
///
/// # Errors
///
/// Returns `Err(UpdateError::Io)` only if the reqwest `Client` cannot be
/// constructed (bad TLS CA bytes, TLS backend initialisation failure).
/// Every per-tick error is swallowed + logged — the loop keeps running.
pub async fn run_version_checker(
    config: VersionCheckerConfig,
    mut stop_rx: oneshot::Receiver<()>,
) -> Result<(), UpdateError> {
    let client = build_client(config.tls_ca_pem.as_deref())?;

    let tick = if config.check_interval.is_zero() {
        VersionCheckerConfig::DEFAULT_INTERVAL
    } else {
        config.check_interval
    };

    let mut ticker = interval(tick);
    ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);

    info!(
        manifest_url = %config.manifest_url,
        current_version = %config.current_version,
        interval_secs = tick.as_secs(),
        "auto-update: version checker started"
    );

    loop {
        tokio::select! {
            _ = ticker.tick() => {
                if let Err(e) = check_once(&client, &config).await {
                    warn!(error = %e, "auto-update: tick failed");
                }
            }
            _ = &mut stop_rx => {
                info!("auto-update: version checker stop signal received");
                return Ok(());
            }
        }
    }
}

fn build_client(tls_ca_pem: Option<&[u8]>) -> Result<reqwest::Client, UpdateError> {
    let mut builder = reqwest::Client::builder()
        .user_agent(concat!("personel-agent/", env!("CARGO_PKG_VERSION")))
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(60));

    if let Some(pem) = tls_ca_pem {
        let cert = reqwest::Certificate::from_pem(pem)
            .map_err(|e| UpdateError::Io(format!("tls ca parse: {e}")))?;
        builder = builder.add_root_certificate(cert);
    }

    builder
        .build()
        .map_err(|e| UpdateError::Io(format!("reqwest build: {e}")))
}

/// One iteration of the checker. Errors here are non-fatal — the caller
/// logs and moves on to the next tick.
async fn check_once(
    client: &reqwest::Client,
    config: &VersionCheckerConfig,
) -> Result<(), UpdateError> {
    // 1. Fetch manifest
    let manifest = fetch_manifest(client, &config.manifest_url).await?;

    // 2. Semver gate
    match compare_semver(&config.current_version, &manifest.latest_version) {
        Ordering::Less => {
            info!(
                current = %config.current_version,
                latest = %manifest.latest_version,
                "auto-update: newer version available, downloading"
            );
        }
        Ordering::Equal | Ordering::Greater => {
            debug!(
                current = %config.current_version,
                latest = %manifest.latest_version,
                "auto-update: no update needed"
            );
            return Ok(());
        }
    }

    // 3. Download package to .tmp then atomic rename
    let incoming = config.download_dir.join("incoming");
    tokio::fs::create_dir_all(&incoming)
        .await
        .map_err(|e| UpdateError::Io(format!("create incoming dir: {e}")))?;

    let final_path = incoming.join("update.tar.gz");
    let tmp_path = incoming.join("update.tar.gz.tmp");

    // Best-effort cleanup of any leftover .tmp from a previous failed tick.
    let _ = tokio::fs::remove_file(&tmp_path).await;

    if let Err(e) = download_to(client, &manifest.package_url, &tmp_path).await {
        // Cleanup partial .tmp on failure so the next tick starts fresh.
        let _ = tokio::fs::remove_file(&tmp_path).await;
        return Err(e);
    }

    // Atomic rename (on Windows same-volume rename is atomic).
    tokio::fs::rename(&tmp_path, &final_path)
        .await
        .map_err(|e| UpdateError::Io(format!("rename tmp→final: {e}")))?;

    // 4. Optional signature side-car
    if let Some(sig) = manifest.manifest_signature.as_deref() {
        let sig_path = incoming.join("update.manifest.json.sig");
        if let Err(e) = tokio::fs::write(&sig_path, sig.as_bytes()).await {
            warn!(error = %e, "auto-update: signature side-car write failed (continuing)");
        }
    }

    info!(
        version = %manifest.latest_version,
        staged = %final_path.display(),
        "auto-update: package downloaded, awaiting UpdateNotify or next service restart to apply"
    );

    Ok(())
}

async fn fetch_manifest(
    client: &reqwest::Client,
    url: &str,
) -> Result<UpdateManifest, UpdateError> {
    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| UpdateError::Io(format!("manifest GET: {e}")))?;

    if !resp.status().is_success() {
        return Err(UpdateError::Io(format!(
            "manifest GET status {}",
            resp.status()
        )));
    }

    let bytes = resp
        .bytes()
        .await
        .map_err(|e| UpdateError::Io(format!("manifest body: {e}")))?;

    parse_manifest(&bytes)
}

fn parse_manifest(bytes: &[u8]) -> Result<UpdateManifest, UpdateError> {
    serde_json::from_slice::<UpdateManifest>(bytes)
        .map_err(|e| UpdateError::ManifestParse(e.to_string()))
}

async fn download_to(
    client: &reqwest::Client,
    url: &str,
    dest: &std::path::Path,
) -> Result<(), UpdateError> {
    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| UpdateError::Io(format!("package GET: {e}")))?;

    if !resp.status().is_success() {
        return Err(UpdateError::Io(format!(
            "package GET status {}",
            resp.status()
        )));
    }

    let bytes = resp
        .bytes()
        .await
        .map_err(|e| UpdateError::Io(format!("package body: {e}")))?;

    tokio::fs::write(dest, &bytes)
        .await
        .map_err(|e| UpdateError::Io(format!("write tmp: {e}")))?;

    Ok(())
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    // ── semver ────────────────────────────────────────────────────────────────

    #[test]
    fn semver_equal() {
        assert_eq!(compare_semver("1.0.0", "1.0.0"), Ordering::Equal);
        assert_eq!(compare_semver("0.1.0", "0.1.0"), Ordering::Equal);
    }

    #[test]
    fn semver_patch_bump() {
        assert_eq!(compare_semver("1.0.0", "1.0.1"), Ordering::Less);
        assert_eq!(compare_semver("1.0.1", "1.0.0"), Ordering::Greater);
    }

    #[test]
    fn semver_minor_bump() {
        assert_eq!(compare_semver("1.0.5", "1.1.0"), Ordering::Less);
        assert_eq!(compare_semver("1.1.0", "1.0.9"), Ordering::Greater);
    }

    #[test]
    fn semver_major_bump() {
        assert_eq!(compare_semver("1.9.9", "2.0.0"), Ordering::Less);
        assert_eq!(compare_semver("2.0.0", "1.999.999"), Ordering::Greater);
    }

    #[test]
    fn semver_numeric_not_lexicographic() {
        // The classic lex bug: "1.10.0" > "1.9.0" only under numeric compare.
        assert_eq!(compare_semver("1.9.0", "1.10.0"), Ordering::Less);
        assert_eq!(compare_semver("1.10.0", "1.9.0"), Ordering::Greater);
        assert_eq!(compare_semver("0.2.10", "0.2.9"), Ordering::Greater);
    }

    #[test]
    fn semver_pre_release_less_than_release() {
        // Standard semver rule: 1.0.0-rc1 < 1.0.0
        assert_eq!(compare_semver("1.0.0-rc1", "1.0.0"), Ordering::Less);
        assert_eq!(compare_semver("1.0.0", "1.0.0-rc1"), Ordering::Greater);
    }

    #[test]
    fn semver_pre_release_both_sides() {
        assert_eq!(compare_semver("1.0.0-rc1", "1.0.0-rc1"), Ordering::Equal);
        assert_eq!(compare_semver("1.0.0-rc1", "1.0.0-rc2"), Ordering::Less);
        assert_eq!(compare_semver("1.0.0-rc2", "1.0.0-rc1"), Ordering::Greater);
    }

    #[test]
    fn semver_v_prefix_tolerated() {
        assert_eq!(compare_semver("v1.2.3", "1.2.3"), Ordering::Equal);
        assert_eq!(compare_semver("v1.2.3", "v1.2.4"), Ordering::Less);
    }

    #[test]
    fn semver_malformed_coerces_to_zero() {
        // Malformed candidate never "beats" a sane current.
        assert_eq!(compare_semver("1.0.0", "abc"), Ordering::Greater);
        assert_eq!(compare_semver("1.0.0", ""), Ordering::Greater);
    }

    #[test]
    fn semver_short_forms() {
        // "1" ≡ "1.0.0", "1.2" ≡ "1.2.0"
        assert_eq!(compare_semver("1", "1.0.0"), Ordering::Equal);
        assert_eq!(compare_semver("1.2", "1.2.0"), Ordering::Equal);
        assert_eq!(compare_semver("1.2", "1.2.1"), Ordering::Less);
    }

    // ── manifest parse ────────────────────────────────────────────────────────

    #[test]
    fn manifest_parse_happy() {
        let json = br#"{
            "latest_version": "1.2.3",
            "package_url": "https://example.invalid/pkg.tar.gz",
            "manifest_signature": "deadbeef"
        }"#;
        let m = parse_manifest(json).expect("parse ok");
        assert_eq!(m.latest_version, "1.2.3");
        assert_eq!(m.package_url, "https://example.invalid/pkg.tar.gz");
        assert_eq!(m.manifest_signature.as_deref(), Some("deadbeef"));
    }

    #[test]
    fn manifest_parse_no_signature_ok() {
        // manifest_signature is optional.
        let json = br#"{
            "latest_version": "2.0.0",
            "package_url": "https://example.invalid/pkg.tar.gz"
        }"#;
        let m = parse_manifest(json).expect("parse ok");
        assert_eq!(m.latest_version, "2.0.0");
        assert!(m.manifest_signature.is_none());
    }

    #[test]
    fn manifest_parse_malformed_rejected() {
        let cases: &[&[u8]] = &[
            b"not json at all",
            b"{}",
            br#"{"latest_version": "1.0.0"}"#, // missing package_url
            br#"{"package_url": "https://x/p.tgz"}"#, // missing latest_version
            b"",
        ];
        for (i, bad) in cases.iter().enumerate() {
            let r = parse_manifest(bad);
            assert!(
                matches!(r, Err(UpdateError::ManifestParse(_))),
                "case {i} should have rejected, got {r:?}"
            );
        }
    }

    // ── atomic staging (uses tempdir, no network) ─────────────────────────────

    #[tokio::test]
    async fn staging_dir_created_and_rename_atomic() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let incoming = tmp.path().join("incoming");
        tokio::fs::create_dir_all(&incoming).await.unwrap();

        let final_path = incoming.join("update.tar.gz");
        let tmp_path = incoming.join("update.tar.gz.tmp");

        // Write some fake bytes to .tmp.
        tokio::fs::write(&tmp_path, b"fake-package-bytes")
            .await
            .unwrap();
        assert!(tmp_path.exists());
        assert!(!final_path.exists());

        // Same-volume rename — atomic on Windows and POSIX.
        tokio::fs::rename(&tmp_path, &final_path).await.unwrap();

        assert!(!tmp_path.exists(), ".tmp should be gone after rename");
        assert!(final_path.exists(), "final path should exist");
        let contents = tokio::fs::read(&final_path).await.unwrap();
        assert_eq!(contents, b"fake-package-bytes");
    }

    #[tokio::test]
    async fn cleanup_removes_leftover_tmp() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let incoming = tmp.path().join("incoming");
        tokio::fs::create_dir_all(&incoming).await.unwrap();

        let tmp_path = incoming.join("update.tar.gz.tmp");
        tokio::fs::write(&tmp_path, b"stale").await.unwrap();
        assert!(tmp_path.exists());

        // Simulates the pre-download cleanup step in check_once.
        let _ = tokio::fs::remove_file(&tmp_path).await;
        assert!(!tmp_path.exists());
    }

    // ── HTTP error stub ───────────────────────────────────────────────────────

    #[tokio::test]
    async fn http_unreachable_host_returns_io_error() {
        // Not a real DNS name — reqwest must fail, and the error must map
        // into UpdateError::Io so the checker loop can warn+continue.
        let client = reqwest::Client::builder()
            .connect_timeout(Duration::from_millis(100))
            .timeout(Duration::from_millis(500))
            .build()
            .unwrap();

        let r =
            fetch_manifest(&client, "http://definitely-not-a-real-host.invalid/manifest.json")
                .await;
        assert!(matches!(r, Err(UpdateError::Io(_))));
    }

    #[tokio::test]
    async fn build_client_rejects_malformed_ca_pem() {
        // A PEM header with garbage inside the base64 body — reqwest's
        // Certificate::from_pem tolerates a fully non-PEM blob by parsing
        // zero certs, but a malformed body inside a valid header is a hard
        // parse error and surfaces as UpdateError::Io.
        let bad_pem = b"-----BEGIN CERTIFICATE-----\n@@@not-base64@@@\n-----END CERTIFICATE-----\n";
        let r = build_client(Some(bad_pem));
        assert!(
            matches!(r, Err(UpdateError::Io(_))),
            "expected Io error for malformed PEM body, got {r:?}"
        );
    }

    #[tokio::test]
    async fn build_client_ok_without_ca() {
        let r = build_client(None);
        assert!(r.is_ok());
    }

    // ── Stop signal ───────────────────────────────────────────────────────────

    #[tokio::test]
    async fn checker_exits_on_stop_signal() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let (stop_tx, stop_rx) = oneshot::channel();

        let config = VersionCheckerConfig {
            current_version: "1.0.0".to_string(),
            // Unreachable host → every tick warns + continues; stop signal
            // is what actually exits the loop.
            manifest_url: "http://definitely-not-a-real-host.invalid/m.json".to_string(),
            download_dir: tmp.path().to_path_buf(),
            check_interval: Duration::from_millis(50),
            tls_ca_pem: None,
        };

        let handle = tokio::spawn(run_version_checker(config, stop_rx));

        // Let at least one tick fire.
        tokio::time::sleep(Duration::from_millis(150)).await;

        stop_tx.send(()).expect("stop send");
        let result = tokio::time::timeout(Duration::from_secs(2), handle)
            .await
            .expect("checker did not exit within timeout")
            .expect("join ok");
        assert!(result.is_ok());
    }
}
