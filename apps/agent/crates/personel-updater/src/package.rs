//! Update package parsing + signature verification.
//!
//! A Personel update package is a `.tar.gz` archive containing:
//!
//! - `personel-agent.exe`
//! - `personel-watchdog.exe`
//! - `enroll.exe`
//! - `manifest.json`
//! - `manifest.sig`
//!
//! `manifest.json` schema:
//!
//! ```json
//! {
//!   "version": "1.2.3",
//!   "binaries": [
//!     { "name": "personel-agent.exe",    "sha256": "hex", "size": 4194304 },
//!     { "name": "personel-watchdog.exe", "sha256": "hex", "size":  524288 },
//!     { "name": "enroll.exe",            "sha256": "hex", "size":  262144 }
//!   ],
//!   "signed_by": "control-plane-signing"
//! }
//! ```
//!
//! `manifest.sig` is a raw 64-byte Ed25519 signature over the exact bytes
//! of `manifest.json` as stored in the archive (no canonicalisation — the
//! signer and the verifier agree to hash the file as-is).
//!
//! KVKK / anti-tamper rule: unsigned, wrong-key-signed, or payload-tampered
//! packages are rejected outright. There is no fallback. See
//! `docs/security/anti-tamper.md` §3.

#![deny(unsafe_code)]

use std::collections::HashMap;
use std::io::Read;
use std::path::{Path, PathBuf};

use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use flate2::read::GzDecoder;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tar::Archive;
use thiserror::Error;
use tracing::{debug, info};

// ── Error type ────────────────────────────────────────────────────────────────

/// Errors produced by the update package + swap subsystem.
///
/// Distinct from [`personel_core::error::AgentError`] so the update flow can
/// carry richer metadata (reasons, which stage failed) without polluting the
/// agent-wide error enum.
#[derive(Debug, Error)]
#[non_exhaustive]
pub enum UpdateError {
    /// Archive could not be opened / read / inflated.
    #[error("package I/O error: {0}")]
    Io(String),

    /// `manifest.json` could not be parsed as JSON.
    #[error("manifest parse error: {0}")]
    ManifestParse(String),

    /// `manifest.sig` was missing, malformed, or verified with the wrong key.
    #[error("manifest signature invalid")]
    Signature,

    /// A binary listed in the manifest was not present in the archive, or an
    /// extra unexpected file appeared.
    #[error("package contents mismatch: {0}")]
    ContentsMismatch(String),

    /// A binary's SHA-256 inside the archive did not match the manifest entry.
    #[error("binary '{name}' hash mismatch (expected {expected}, got {got})")]
    BinaryHash {
        /// Binary file name inside the archive.
        name: String,
        /// Expected SHA-256 hex from manifest.
        expected: String,
        /// Computed SHA-256 hex from archive contents.
        got: String,
    },

    /// The requested operation is not supported on the current platform
    /// (non-Windows swap calls).
    #[error("operation unsupported on this platform")]
    Unsupported,

    /// Rollback was triggered because some stage of `apply_update` failed.
    /// Contains a human-readable reason string.
    #[error("update rolled back: {0}")]
    Rollback(String),

    /// Rollback itself failed — system may be in an inconsistent state and
    /// manual repair is required.
    #[error("rollback failed ({stage}): {reason}")]
    RollbackFailed {
        /// Stage that was being rolled back.
        stage: &'static str,
        /// Underlying reason.
        reason: String,
    },
}

// ── Manifest schema ───────────────────────────────────────────────────────────

/// One entry in the manifest's `binaries` array.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ManifestBinary {
    /// File name inside the tar archive (e.g., `personel-agent.exe`).
    pub name: String,
    /// Lower-case hex SHA-256 of the binary's contents.
    pub sha256: String,
    /// Size in bytes. Informational; actual size is determined from the tar entry.
    pub size: u64,
}

/// Deserialised update manifest.
///
/// This is the package-level manifest — distinct from
/// [`crate::manifest::UpdateManifest`] which is the Phase 1 single-binary
/// manifest that the update poller fetches from the mirror.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PackageManifest {
    /// Semver of the update package.
    pub version: String,
    /// List of binary files contained in the package.
    pub binaries: Vec<ManifestBinary>,
    /// Key ID that signed the manifest (informational / audit trail).
    pub signed_by: String,
}

// ── UpdateMetadata — the successful-verification result ──────────────────────

/// Metadata returned from [`verify_update_package`] on success.
///
/// `binary_paths` are the *source* paths inside the tar archive — they become
/// the source for the swap step in [`crate::swap::apply_update`] after the
/// package is extracted to disk.
#[derive(Debug, Clone)]
pub struct UpdateMetadata {
    /// Target version being rolled out.
    pub version: String,
    /// Map of archive-relative file name → absolute path on disk after
    /// extraction. Populated by [`verify_update_package`] which extracts
    /// binaries into a sibling directory of the package file.
    pub binary_paths: HashMap<String, PathBuf>,
    /// Full parsed manifest (for audit logging / UpdateAck payload).
    pub manifest: PackageManifest,
}

// ── Public API ────────────────────────────────────────────────────────────────

/// Verifies and extracts an update package.
///
/// Steps:
///
/// 1. Open the `.tar.gz` at `package_path` and inflate into memory.
/// 2. Collect `manifest.json`, `manifest.sig`, and each binary by name.
/// 3. Parse `VerifyingKey` from `expected_signature_pem` (accepts either a
///    raw 32-byte hex string or a PEM-wrapped block).
/// 4. Verify `manifest.sig` against `manifest.json` bytes with Ed25519.
/// 5. Parse `manifest.json` into [`PackageManifest`].
/// 6. Check archive contents exactly match the manifest's binary list (no
///    missing, no extras beyond manifest.json / manifest.sig).
/// 7. SHA-256 each binary's archive bytes and compare against the manifest
///    entry.
/// 8. Write each binary to `<package_parent>\.extracted\<name>` and record
///    the path in the returned [`UpdateMetadata`].
///
/// # Errors
///
/// Returns an [`UpdateError`] variant for each failure class — callers
/// wishing to log a single-line audit trail should use the `Display` impl,
/// which encodes the failure reason.
pub fn verify_update_package(
    package_path: &Path,
    expected_signature_pem: &str,
) -> Result<UpdateMetadata, UpdateError> {
    info!(?package_path, "verify_update_package: opening archive");

    // Step 1: open + inflate.
    let file =
        std::fs::File::open(package_path).map_err(|e| UpdateError::Io(format!("open: {e}")))?;
    let gz = GzDecoder::new(file);
    let mut archive = Archive::new(gz);

    // Step 2: collect entries into a name→bytes map. The tar crate is a
    // streaming reader; we buffer into memory because the update payload is
    // bounded (a few MB) and we need random access for hashing + signature
    // verification.
    let mut entries: HashMap<String, Vec<u8>> = HashMap::new();
    for entry_result in archive
        .entries()
        .map_err(|e| UpdateError::Io(format!("entries: {e}")))?
    {
        let mut entry = entry_result.map_err(|e| UpdateError::Io(format!("entry: {e}")))?;
        let path = entry
            .path()
            .map_err(|e| UpdateError::Io(format!("entry path: {e}")))?
            .to_path_buf();
        // Only accept flat entries (no subdirectories). Reject path
        // traversal attempts up front.
        let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
            return Err(UpdateError::ContentsMismatch(format!(
                "bad entry path: {path:?}"
            )));
        };
        if name != path.to_string_lossy() {
            return Err(UpdateError::ContentsMismatch(format!(
                "nested path rejected: {path:?}"
            )));
        }
        let mut buf = Vec::new();
        entry
            .read_to_end(&mut buf)
            .map_err(|e| UpdateError::Io(format!("read {name}: {e}")))?;
        debug!(name, bytes = buf.len(), "package entry");
        entries.insert(name.to_string(), buf);
    }

    // Step 3: extract manifest + sig.
    let manifest_bytes = entries
        .get("manifest.json")
        .ok_or_else(|| UpdateError::ContentsMismatch("manifest.json missing".into()))?
        .clone();
    let sig_bytes = entries
        .get("manifest.sig")
        .ok_or_else(|| UpdateError::ContentsMismatch("manifest.sig missing".into()))?
        .clone();

    // Step 4: parse public key.
    let verifying_key = parse_signing_key(expected_signature_pem)?;

    // Step 5: verify signature.
    let sig = parse_signature(&sig_bytes)?;
    verifying_key
        .verify(&manifest_bytes, &sig)
        .map_err(|_| UpdateError::Signature)?;
    info!("verify_update_package: manifest signature verified");

    // Step 6: parse manifest JSON.
    let manifest: PackageManifest = serde_json::from_slice(&manifest_bytes)
        .map_err(|e| UpdateError::ManifestParse(e.to_string()))?;

    // Step 7: contents match manifest exactly (no missing, no extras).
    let expected_names: std::collections::HashSet<&str> =
        manifest.binaries.iter().map(|b| b.name.as_str()).collect();
    let control_files: std::collections::HashSet<&str> =
        ["manifest.json", "manifest.sig"].into_iter().collect();
    for entry_name in entries.keys() {
        if !expected_names.contains(entry_name.as_str())
            && !control_files.contains(entry_name.as_str())
        {
            return Err(UpdateError::ContentsMismatch(format!(
                "unexpected archive entry: {entry_name}"
            )));
        }
    }

    // Step 8: hash each binary and stage to disk.
    let extract_dir = package_path
        .parent()
        .unwrap_or_else(|| Path::new("."))
        .join(".extracted");
    std::fs::create_dir_all(&extract_dir)
        .map_err(|e| UpdateError::Io(format!("create .extracted: {e}")))?;

    let mut binary_paths: HashMap<String, PathBuf> = HashMap::new();
    for bin in &manifest.binaries {
        let Some(bytes) = entries.get(&bin.name) else {
            return Err(UpdateError::ContentsMismatch(format!(
                "binary {} missing from archive",
                bin.name
            )));
        };
        let actual_hex = hex::encode(Sha256::digest(bytes));
        if !actual_hex.eq_ignore_ascii_case(&bin.sha256) {
            return Err(UpdateError::BinaryHash {
                name: bin.name.clone(),
                expected: bin.sha256.clone(),
                got: actual_hex,
            });
        }
        // Additional defence: advertised size must equal actual archive size.
        // Don't fail on zero (some binaries in tests); only fail when both
        // are non-zero and differ.
        if bin.size != 0 && bin.size != bytes.len() as u64 {
            return Err(UpdateError::ContentsMismatch(format!(
                "binary {} size {} does not match manifest {}",
                bin.name,
                bytes.len(),
                bin.size
            )));
        }
        let out_path = extract_dir.join(&bin.name);
        std::fs::write(&out_path, bytes)
            .map_err(|e| UpdateError::Io(format!("write {}: {e}", bin.name)))?;
        debug!(name = %bin.name, size = bytes.len(), "binary extracted");
        binary_paths.insert(bin.name.clone(), out_path);
    }

    info!(
        version = %manifest.version,
        count = manifest.binaries.len(),
        "verify_update_package: ok"
    );

    Ok(UpdateMetadata {
        version: manifest.version.clone(),
        binary_paths,
        manifest,
    })
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/// Parses the signing key from either a PEM-wrapped Ed25519 public key or a
/// bare 64-character hex string (32 raw bytes). Accepting both simplifies
/// test fixtures without compromising the production PEM path.
fn parse_signing_key(pem_or_hex: &str) -> Result<VerifyingKey, UpdateError> {
    let trimmed = pem_or_hex.trim();
    // Hex branch: exactly 64 hex chars → raw 32-byte key.
    if trimmed.len() == 64 && trimmed.chars().all(|c| c.is_ascii_hexdigit()) {
        let bytes = hex::decode(trimmed).map_err(|_| UpdateError::Signature)?;
        let arr: [u8; 32] = bytes.try_into().map_err(|_| UpdateError::Signature)?;
        return VerifyingKey::from_bytes(&arr).map_err(|_| UpdateError::Signature);
    }

    // PEM branch: strip header/footer and base64-decode.
    // We accept the standard "PUBLIC KEY" SPKI header OR the narrower
    // "ED25519 PUBLIC KEY" label some tooling emits.
    let body: String = trimmed
        .lines()
        .filter(|l| !l.starts_with("-----"))
        .collect::<Vec<_>>()
        .join("");
    let decoded = base64_decode_standard(&body).ok_or(UpdateError::Signature)?;

    // SPKI wraps the 32-byte key in an ASN.1 structure ~44 bytes long; the
    // last 32 bytes are the raw key. For narrow labels the body IS the
    // 32-byte key verbatim. Handle both by taking the trailing 32 bytes.
    if decoded.len() < 32 {
        return Err(UpdateError::Signature);
    }
    let key_bytes: [u8; 32] = decoded[decoded.len() - 32..]
        .try_into()
        .map_err(|_| UpdateError::Signature)?;
    VerifyingKey::from_bytes(&key_bytes).map_err(|_| UpdateError::Signature)
}

fn parse_signature(bytes: &[u8]) -> Result<Signature, UpdateError> {
    if bytes.len() != 64 {
        return Err(UpdateError::Signature);
    }
    let arr: [u8; 64] = bytes.try_into().map_err(|_| UpdateError::Signature)?;
    Ok(Signature::from_bytes(&arr))
}

/// Minimal standard-base64 decoder (accepts padding, tolerates whitespace).
/// Duplicated from `manifest.rs::base64url_decode` with a different alphabet;
/// keeping both is cheaper than pulling in a `base64` crate dependency.
fn base64_decode_standard(s: &str) -> Option<Vec<u8>> {
    const ALPHABET: &[u8; 64] =
        b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut map = [255u8; 256];
    for (i, &b) in ALPHABET.iter().enumerate() {
        map[b as usize] = i as u8;
    }
    let mut out = Vec::with_capacity(s.len() * 3 / 4);
    let mut bits: u32 = 0;
    let mut bit_count: u32 = 0;
    for b in s.bytes() {
        if b == b'=' || b.is_ascii_whitespace() {
            continue;
        }
        let v = map[b as usize];
        if v == 255 {
            return None;
        }
        bits = (bits << 6) | u32::from(v);
        bit_count += 6;
        if bit_count >= 8 {
            bit_count -= 8;
            out.push(((bits >> bit_count) & 0xFF) as u8);
        }
    }
    Some(out)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signer, SigningKey};
    use flate2::{write::GzEncoder, Compression};
    use rand::rngs::OsRng;

    /// Helper: build a .tar.gz package in memory and write it to `dst`.
    /// Returns the (PackageManifest JSON bytes, signing key) used.
    fn build_test_package(
        dst: &Path,
        version: &str,
        tamper: TamperMode,
    ) -> (Vec<u8>, SigningKey) {
        let signing_key = SigningKey::generate(&mut OsRng);

        // Build binaries first (so we know real sizes / hashes).
        let agent_bytes = b"fake agent binary v1".to_vec();
        let watchdog_bytes = b"fake watchdog binary v1".to_vec();
        let enroll_bytes = b"fake enroll binary v1".to_vec();

        let manifest = PackageManifest {
            version: version.into(),
            binaries: vec![
                ManifestBinary {
                    name: "personel-agent.exe".into(),
                    sha256: hex::encode(Sha256::digest(&agent_bytes)),
                    size: agent_bytes.len() as u64,
                },
                ManifestBinary {
                    name: "personel-watchdog.exe".into(),
                    sha256: hex::encode(Sha256::digest(&watchdog_bytes)),
                    size: watchdog_bytes.len() as u64,
                },
                ManifestBinary {
                    name: "enroll.exe".into(),
                    sha256: hex::encode(Sha256::digest(&enroll_bytes)),
                    size: enroll_bytes.len() as u64,
                },
            ],
            signed_by: "control-plane-signing".into(),
        };
        let mut manifest_json = serde_json::to_vec(&manifest).unwrap();
        if matches!(tamper, TamperMode::TamperManifest) {
            // Corrupt the JSON *after* signing — signature must fail.
            manifest_json.extend_from_slice(b"\n// tampered");
        }

        // Sign the ORIGINAL (pre-tamper) bytes.
        let original_json = serde_json::to_vec(&manifest).unwrap();
        let sig = signing_key.sign(&original_json);
        let sig_bytes = sig.to_bytes().to_vec();

        // Build the tar.gz.
        let file = std::fs::File::create(dst).unwrap();
        let gz = GzEncoder::new(file, Compression::default());
        let mut tar = tar::Builder::new(gz);

        let add = |tar: &mut tar::Builder<_>, name: &str, content: &[u8]| {
            let mut header = tar::Header::new_gnu();
            header.set_path(name).unwrap();
            header.set_size(content.len() as u64);
            header.set_mode(0o644);
            header.set_cksum();
            tar.append(&header, content).unwrap();
        };

        let agent_payload = if matches!(tamper, TamperMode::TamperBinary) {
            b"tampered agent!!!!!!".to_vec()
        } else {
            agent_bytes
        };
        add(&mut tar, "personel-agent.exe", &agent_payload);
        add(&mut tar, "personel-watchdog.exe", &watchdog_bytes);
        add(&mut tar, "enroll.exe", &enroll_bytes);
        add(&mut tar, "manifest.json", &manifest_json);
        add(&mut tar, "manifest.sig", &sig_bytes);

        tar.into_inner().unwrap().finish().unwrap();

        (original_json, signing_key)
    }

    #[derive(Clone, Copy)]
    enum TamperMode {
        None,
        TamperManifest,
        TamperBinary,
    }

    fn vk_hex(sk: &SigningKey) -> String {
        hex::encode(sk.verifying_key().to_bytes())
    }

    #[test]
    fn verify_happy_path() {
        let tmp = tempfile::tempdir().unwrap();
        let pkg = tmp.path().join("update.tar.gz");
        let (_, sk) = build_test_package(&pkg, "1.2.3", TamperMode::None);

        let metadata = verify_update_package(&pkg, &vk_hex(&sk)).expect("verify ok");
        assert_eq!(metadata.version, "1.2.3");
        assert_eq!(metadata.binary_paths.len(), 3);
        assert!(metadata.binary_paths.contains_key("personel-agent.exe"));
        for path in metadata.binary_paths.values() {
            assert!(path.exists(), "extracted binary should exist");
        }
    }

    #[test]
    fn verify_rejects_tampered_manifest() {
        let tmp = tempfile::tempdir().unwrap();
        let pkg = tmp.path().join("update.tar.gz");
        let (_, sk) = build_test_package(&pkg, "1.2.3", TamperMode::TamperManifest);
        let err = verify_update_package(&pkg, &vk_hex(&sk)).unwrap_err();
        assert!(
            matches!(err, UpdateError::Signature),
            "expected Signature error, got {err:?}"
        );
    }

    #[test]
    fn verify_rejects_tampered_binary() {
        let tmp = tempfile::tempdir().unwrap();
        let pkg = tmp.path().join("update.tar.gz");
        let (_, sk) = build_test_package(&pkg, "1.2.3", TamperMode::TamperBinary);
        let err = verify_update_package(&pkg, &vk_hex(&sk)).unwrap_err();
        // Either the binary size check or the hash check must catch it.
        assert!(
            matches!(err, UpdateError::BinaryHash { .. } | UpdateError::ContentsMismatch(_)),
            "expected binary hash/size mismatch, got {err:?}"
        );
    }

    #[test]
    fn verify_rejects_wrong_key() {
        let tmp = tempfile::tempdir().unwrap();
        let pkg = tmp.path().join("update.tar.gz");
        let (_, _real_sk) = build_test_package(&pkg, "1.2.3", TamperMode::None);
        let wrong_key = SigningKey::generate(&mut OsRng);
        let err = verify_update_package(&pkg, &vk_hex(&wrong_key)).unwrap_err();
        assert!(matches!(err, UpdateError::Signature));
    }

    #[test]
    fn parse_signing_key_hex_round_trip() {
        let sk = SigningKey::generate(&mut OsRng);
        let hex_key = hex::encode(sk.verifying_key().to_bytes());
        let parsed = parse_signing_key(&hex_key).unwrap();
        assert_eq!(parsed.to_bytes(), sk.verifying_key().to_bytes());
    }

    #[test]
    fn parse_signature_wrong_length_rejected() {
        let err = parse_signature(&[0u8; 10]).unwrap_err();
        assert!(matches!(err, UpdateError::Signature));
    }

    #[test]
    fn package_manifest_roundtrip() {
        let m = PackageManifest {
            version: "0.9.9".into(),
            binaries: vec![ManifestBinary {
                name: "enroll.exe".into(),
                sha256: "aa".into(),
                size: 42,
            }],
            signed_by: "k".into(),
        };
        let json = serde_json::to_string(&m).unwrap();
        let back: PackageManifest = serde_json::from_str(&json).unwrap();
        assert_eq!(m, back);
    }
}
