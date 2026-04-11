//! Update manifest parsing and Ed25519 signature verification.
//!
//! The manifest is a JSON document signed with Ed25519. The signing key's
//! public component is baked into the binary at build time. A manifest
//! with an invalid signature is rejected outright — no fallback.
//!
//! Manifest schema (JSON):
//! ```json
//! {
//!   "version": "1.2.3",
//!   "channel": "stable",
//!   "artifact_url": "/updates/personel-agent-1.2.3-x64.exe",
//!   "artifact_sha256": "hex",
//!   "signature": "base64url-ed25519-sig-over-canonical-fields",
//!   "signing_key_id": "update-signing-v1",
//!   "canary": false,
//!   "min_os_version": "10.0.19041"
//! }
//! ```

use ed25519_dalek::{Signature, VerifyingKey, Verifier};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use personel_core::error::{AgentError, Result};

/// A parsed and signature-verified update manifest.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateManifest {
    /// Target agent version string.
    pub version: String,
    /// Update channel (`"stable"` or `"canary"`).
    pub channel: String,
    /// Relative artifact URL on the update mirror.
    pub artifact_url: String,
    /// Expected SHA-256 of the downloaded artifact (hex string).
    pub artifact_sha256: String,
    /// Base64url-encoded Ed25519 signature.
    pub signature: String,
    /// Key ID for audit purposes.
    pub signing_key_id: String,
    /// Whether this is a canary release.
    #[serde(default)]
    pub canary: bool,
}

impl UpdateManifest {
    /// Parses a JSON manifest and verifies its Ed25519 signature.
    ///
    /// The signed message is the canonical UTF-8 JSON of all fields **except**
    /// `signature`, sorted by key name. This matches the server-side signing
    /// procedure.
    ///
    /// # Errors
    ///
    /// - [`AgentError::UpdateSignature`] if signature decoding or verification
    ///   fails.
    /// - [`AgentError::PolicyDeserialize`] if JSON parsing fails.
    pub fn parse_and_verify(json: &str, signing_pub_key: &VerifyingKey) -> Result<Self> {
        let manifest: Self = serde_json::from_str(json).map_err(|e| {
            AgentError::PolicyDeserialize(format!("manifest JSON parse error: {e}"))
        })?;

        // Build the canonical signed message: JSON without the `signature` field.
        let canonical = manifest.canonical_bytes();

        // Decode the base64url signature.
        let sig_bytes = base64url_decode(&manifest.signature)
            .ok_or(AgentError::UpdateSignature)?;
        let sig_arr: [u8; 64] = sig_bytes.try_into().map_err(|_| AgentError::UpdateSignature)?;
        let sig = Signature::from_bytes(&sig_arr);

        signing_pub_key
            .verify(&canonical, &sig)
            .map_err(|_| AgentError::UpdateSignature)?;

        Ok(manifest)
    }

    /// Returns the canonical bytes used for signature verification.
    ///
    /// This is the UTF-8 encoding of the JSON object with `version`, `channel`,
    /// `artifact_url`, `artifact_sha256`, `signing_key_id`, `canary` fields
    /// only, sorted by key.
    fn canonical_bytes(&self) -> Vec<u8> {
        // Construct a deterministic JSON object for signing.
        // serde_json preserves insertion order; we rely on alphanumeric key order.
        let canonical = serde_json::json!({
            "artifact_sha256": self.artifact_sha256,
            "artifact_url": self.artifact_url,
            "canary": self.canary,
            "channel": self.channel,
            "signing_key_id": self.signing_key_id,
            "version": self.version,
        });
        canonical.to_string().into_bytes()
    }

    /// Verifies that the SHA-256 of `artifact_bytes` matches `artifact_sha256`.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::ArtifactHash`] if the hash does not match.
    pub fn verify_artifact_hash(&self, artifact_bytes: &[u8]) -> Result<()> {
        let actual_hex = ::hex::encode(Sha256::digest(artifact_bytes));
        if actual_hex.eq_ignore_ascii_case(&self.artifact_sha256) {
            Ok(())
        } else {
            Err(AgentError::ArtifactHash)
        }
    }
}

fn base64url_decode(s: &str) -> Option<Vec<u8>> {
    // Simple base64url decoder (no padding).
    use std::collections::HashMap;
    let alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    let map: HashMap<char, u8> = alphabet.chars().enumerate().map(|(i, c)| (c, i as u8)).collect();

    let mut bits = 0u32;
    let mut bit_count = 0u32;
    let mut out = Vec::new();
    for c in s.chars() {
        let val = *map.get(&c)? as u32;
        bits = (bits << 6) | val;
        bit_count += 6;
        if bit_count >= 8 {
            bit_count -= 8;
            out.push(((bits >> bit_count) & 0xFF) as u8);
        }
    }
    Some(out)
}

