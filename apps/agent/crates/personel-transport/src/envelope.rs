//! Batch assembly, compression, and HMAC signing for event uploads.
//!
//! The `EventBatch` proto message supports an optional `batch_hmac` field for
//! integrity verification at the gateway. This module computes the HMAC using
//! HMAC-SHA-256 over the serialized batch bytes.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Wire gzip compression for large batches (use `flate2`).
//! - Implement HMAC-SHA-256 over prost-encoded `EventBatch` bytes.
//! - Provide `verify_batch_hmac` for gateway-side use (backend-developer).

use bytes::Bytes;
use prost::Message;
use sha2::{Digest, Sha256};

use personel_core::error::Result;
use personel_proto::v1::{EventBatch, Event};

/// Assembles a batch of proto events into an `EventBatch` with a SHA-256
/// integrity digest in `batch_hmac`.
///
/// Note: `batch_hmac` here is a SHA-256 of the batch payload bytes, not a
/// keyed MAC. Upgrading to HMAC requires a shared symmetric key negotiated
/// during the gRPC Hello/Welcome handshake — deferred to Phase 1 hardening.
///
/// # Errors
///
/// Returns an error if prost encoding fails.
pub fn build_batch(batch_id: u64, events: Vec<Event>) -> Result<EventBatch> {
    let mut batch = EventBatch {
        batch_id,
        events,
        batch_hmac: vec![],
    };

    // Compute a digest over the events bytes for integrity checking.
    let mut payload_bytes = Vec::with_capacity(batch.encoded_len());
    batch.encode(&mut payload_bytes)?;
    let digest: [u8; 32] = Sha256::digest(&payload_bytes).into();
    batch.batch_hmac = digest.to_vec();

    Ok(batch)
}
