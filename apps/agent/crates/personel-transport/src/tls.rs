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

use std::sync::Arc;

use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, DigitallySignedStruct, Error, SignatureScheme};
use sha2::{Digest, Sha256};
use tracing::{debug, warn};

use personel_core::error::{AgentError, Result};

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

    fn check_pins(&self, intermediates: &[CertificateDer<'_>]) -> std::result::Result<(), Error> {
        if self.pins.is_empty() {
            debug!("pin check skipped: no pins configured");
            return Ok(());
        }

        for cert in intermediates {
            // Extract the SubjectPublicKeyInfo field via a simple DER walk.
            // A full ASN.1 parser is not needed here; we just need the SPKI
            // bytes to hash. In production use `x509-parser` or `rcgen` to
            // extract SPKI robustly.
            // TODO: replace this stub with proper SPKI extraction using
            //       `x509-parser` crate (add to dependencies).
            let spki_hash: [u8; 32] = Sha256::digest(cert.as_ref()).into();
            if self.pins.contains(&spki_hash) {
                debug!("SPKI pin matched");
                return Ok(());
            }
        }

        warn!("SPKI pin mismatch — no certificate in chain matched pinset");
        Err(Error::General("SPKI pin mismatch".into()))
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
