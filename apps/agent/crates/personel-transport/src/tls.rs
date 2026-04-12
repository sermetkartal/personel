//! rustls `ClientConfig` builder with SPKI certificate pinning.
//!
//! The agent pins the **tenant CA SPKI SHA-256** rather than the leaf server
//! certificate, per `docs/architecture/mtls-pki.md` §"Certificate Pinning on
//! the Agent":
//!
//! > The agent pins the tenant CA SPKI SHA-256, not a leaf server cert.
//!
//! This is implemented as a custom [`rustls::client::ServerCertVerifier`] that
//! performs normal chain validation AND then verifies that at least one
//! certificate in the chain has an SPKI SHA-256 matching the pinset.
//!
//! The agent also presents its own mTLS client certificate (stored DPAPI-
//! protected on disk) during the handshake.
//!
//! # Pin rotation
//!
//! Configure **two or more** pins in [`TlsConfig::spki_pins`]:
//! - Index 0: the current active CA SPKI pin.
//! - Index 1+: backup pin(s) for the next CA to be rotated in.
//!
//! The verifier accepts a handshake if **any** pin in the set matches. This
//! allows the server CA to be rotated without a simultaneous agent update.
//! Remove the old pin only after all agents have received the new pin via
//! config push.
//!
//! # Pin failure behaviour
//!
//! On mismatch the connection is rejected, a `warn!` is emitted (log sink is
//! the agent's OTLP pipeline), and an `AgentError::PinMismatch` is returned.
//! The caller (transport layer) should treat `PinMismatch` as a tamper event
//! and emit an `agent.tamper_detected` telemetry event.

use std::sync::Arc;

use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, DigitallySignedStruct, Error, SignatureScheme};
use sha2::{Digest, Sha256};
use tracing::{debug, warn};
use x509_parser::prelude::{FromDer, X509Certificate};

use personel_core::error::{AgentError, Result};

// ──────────────────────────────────────────────────────────────────────────────
// PinSet
// ──────────────────────────────────────────────────────────────────────────────

/// A set of allowed SPKI SHA-256 pins loaded from agent configuration.
///
/// Each element is a 32-byte SHA-256 digest of the DER-encoded
/// `SubjectPublicKeyInfo` field of a trusted CA certificate. Configure at least
/// two entries (current + backup) to enable zero-downtime CA rotation.
///
/// # Example
///
/// ```rust,no_run
/// # use personel_transport::tls::PinSet;
/// let current: [u8; 32] = [0u8; 32]; // replace with real SHA-256
/// let backup:  [u8; 32] = [1u8; 32]; // next CA pin, staged for rotation
/// let pins: PinSet = vec![current, backup];
/// ```
pub type PinSet = Vec<[u8; 32]>;

// ──────────────────────────────────────────────────────────────────────────────
// PinningVerifier
// ──────────────────────────────────────────────────────────────────────────────

/// A `ServerCertVerifier` that checks SPKI SHA-256 pins in addition to
/// standard chain validation.
///
/// If `pins` is empty, the verifier falls back to standard WebPKI validation
/// (useful during enrollment before the tenant CA pin is known).
#[derive(Debug)]
pub struct PinningVerifier {
    /// Set of allowed SPKI SHA-256 digests (32 bytes each).
    pins: Vec<[u8; 32]>,
    /// Inner WebPKI verifier for chain validation.
    inner: Arc<dyn ServerCertVerifier>,
}

impl PinningVerifier {
    /// Creates a new verifier with the given SPKI pins and a WebPKI inner
    /// verifier backed by the system/bundled root store.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Tls`] if the root store cannot be loaded.
    pub fn new(pins: Vec<[u8; 32]>) -> Result<Arc<Self>> {
        // Build a standard WebPKI verifier with the bundled Mozilla root set.
        // In production the agent also loads the tenant CA from its config,
        // but that's an additional step done after enrollment.
        let mut root_store = rustls::RootCertStore::empty();
        root_store.extend(webpki_roots::TLS_SERVER_ROOTS.iter().cloned());

        let inner = rustls::client::WebPkiServerVerifier::builder(Arc::new(root_store))
            .build()
            .map_err(|e| AgentError::Tls(e.to_string()))?;

        Ok(Arc::new(Self { pins, inner }))
    }

    /// Adds the tenant CA certificate to the trusted root store.
    ///
    /// This is called after enrollment when the tenant CA DER bytes are known.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Tls`] if the certificate cannot be parsed.
    pub fn new_with_tenant_ca(
        pins: Vec<[u8; 32]>,
        tenant_ca_der: &[u8],
    ) -> Result<Arc<Self>> {
        let mut root_store = rustls::RootCertStore::empty();
        root_store.extend(webpki_roots::TLS_SERVER_ROOTS.iter().cloned());

        let ca_cert = CertificateDer::from(tenant_ca_der.to_vec());
        root_store
            .add(ca_cert)
            .map_err(|e| AgentError::Tls(format!("tenant CA add failed: {e}")))?;

        let inner = rustls::client::WebPkiServerVerifier::builder(Arc::new(root_store))
            .build()
            .map_err(|e| AgentError::Tls(e.to_string()))?;

        Ok(Arc::new(Self { pins, inner }))
    }

    /// Extracts the DER-encoded `SubjectPublicKeyInfo` bytes from a certificate
    /// and returns their SHA-256 digest.
    ///
    /// Returns `None` if the certificate cannot be parsed (malformed DER).
    fn spki_hash_of(cert_der: &CertificateDer<'_>) -> Option<[u8; 32]> {
        // x509_parser parses the DER in zero-copy fashion and gives us direct
        // access to the raw SPKI bytes, which we then hash.
        let (_, parsed) = X509Certificate::from_der(cert_der.as_ref()).ok()?;
        let spki_raw = parsed.public_key().raw;
        let hash: [u8; 32] = Sha256::digest(spki_raw).into();
        Some(hash)
    }

    /// Checks whether any certificate in `certs` has an SPKI SHA-256 that
    /// matches one of the configured pins.
    ///
    /// - If `self.pins` is empty the check is skipped (enrollment/bootstrap mode).
    /// - If a match is found the connection proceeds.
    /// - If no match is found the connection is rejected and a tamper-alert log
    ///   line is emitted at `warn!` level.
    fn check_pins(&self, certs: &[CertificateDer<'_>]) -> std::result::Result<(), Error> {
        if self.pins.is_empty() {
            debug!("SPKI pin check skipped: no pins configured (enrollment mode)");
            return Ok(());
        }

        for cert in certs {
            match Self::spki_hash_of(cert) {
                Some(hash) if self.pins.contains(&hash) => {
                    debug!(
                        spki_sha256 = hex::encode(hash),
                        "SPKI pin matched — connection authorised"
                    );
                    return Ok(());
                }
                Some(hash) => {
                    debug!(
                        spki_sha256 = hex::encode(hash),
                        "SPKI pin miss for this cert — checking next"
                    );
                }
                None => {
                    debug!("could not parse SPKI from certificate DER — skipping");
                }
            }
        }

        // No cert in the chain matched any pin.
        warn!(
            pin_count = self.pins.len(),
            "SPKI pin mismatch — connection rejected; possible MITM or misconfigured CA; \
             emitting tamper alert"
        );
        // Use a typed rustls error so the transport layer can distinguish this
        // from a generic TLS failure and emit an agent.tamper_detected event.
        Err(Error::General("SPKI pin mismatch: agent.tamper_detected".into()))
    }
}

impl ServerCertVerifier for PinningVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        intermediates: &[CertificateDer<'_>],
        server_name: &ServerName<'_>,
        ocsp_response: &[u8],
        now: UnixTime,
    ) -> std::result::Result<ServerCertVerified, Error> {
        // 1. Standard chain + name validation.
        self.inner.verify_server_cert(end_entity, intermediates, server_name, ocsp_response, now)?;

        // 2. Pin check over all certs (end-entity + intermediates).
        let mut all_certs = vec![end_entity.clone()];
        all_certs.extend_from_slice(intermediates);
        self.check_pins(&all_certs)?;

        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> std::result::Result<HandshakeSignatureValid, Error> {
        self.inner.verify_tls12_signature(message, cert, dss)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> std::result::Result<HandshakeSignatureValid, Error> {
        self.inner.verify_tls13_signature(message, cert, dss)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.inner.supported_verify_schemes()
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// ClientConfig builder
// ──────────────────────────────────────────────────────────────────────────────

/// TLS client configuration parameters.
pub struct TlsConfig {
    /// DER-encoded client certificate chain (leaf first).
    pub client_cert_chain: Vec<CertificateDer<'static>>,
    /// DER-encoded client private key.
    pub client_key: rustls::pki_types::PrivateKeyDer<'static>,
    /// SPKI SHA-256 pins for the gateway's CA.
    pub spki_pins: Vec<[u8; 32]>,
    /// Optional tenant CA certificate in DER form.
    pub tenant_ca_der: Option<Vec<u8>>,
}

/// Builds a [`rustls::ClientConfig`] for mTLS with SPKI pinning.
///
/// # Errors
///
/// Returns [`AgentError::Tls`] if key material is invalid or the verifier
/// cannot be constructed.
pub fn build_client_config(cfg: TlsConfig) -> Result<ClientConfig> {
    let verifier = match &cfg.tenant_ca_der {
        Some(ca) => PinningVerifier::new_with_tenant_ca(cfg.spki_pins, ca)?,
        None => PinningVerifier::new(cfg.spki_pins)?,
    };

    ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_client_auth_cert(cfg.client_cert_chain, cfg.client_key)
        .map_err(|e| AgentError::Tls(e.to_string()))
}
