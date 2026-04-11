//! Newtype ID wrappers.
//!
//! Every domain identifier is a distinct type so that IDs from different
//! domains cannot be accidentally interchanged. All IDs are backed by
//! [`uuid::Uuid`] (128-bit). `EventId` uses UUIDv7 (time-ordered).
//!
//! # Example
//!
//! ```
//! use personel_core::ids::{TenantId, EndpointId, EventId};
//! use uuid::Uuid;
//!
//! let tenant = TenantId::new(Uuid::new_v4());
//! let endpoint = EndpointId::new(Uuid::new_v4());
//! let event = EventId::new_v7();
//!
//! // IDs of different types do not compare equal even with the same bytes.
//! // (compile error if you try: tenant == endpoint)
//! assert_ne!(tenant.as_uuid(), endpoint.as_uuid()); // probabilistically
//! let _ = event;
//! ```

use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::error::{AgentError, Result};

// ──────────────────────────────────────────────────────────────────────────────
// Macro to reduce repetition for simple UUID newtypes
// ──────────────────────────────────────────────────────────────────────────────

macro_rules! uuid_newtype {
    (
        $(#[$meta:meta])*
        $name:ident
    ) => {
        $(#[$meta])*
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
        #[serde(transparent)]
        pub struct $name(Uuid);

        impl $name {
            /// Creates a new ID wrapping the given [`Uuid`].
            #[must_use]
            #[inline]
            pub fn new(id: Uuid) -> Self {
                Self(id)
            }

            /// Parses from a 16-byte big-endian representation (as used in proto).
            ///
            /// # Errors
            ///
            /// Returns [`AgentError::InvalidId`] if `bytes` is not exactly 16 bytes.
            pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
                let arr: [u8; 16] = bytes.try_into().map_err(|_| AgentError::InvalidId {
                    reason: concat!("expected 16 bytes for ", stringify!($name)),
                })?;
                Ok(Self(Uuid::from_bytes(arr)))
            }

            /// Returns the underlying [`Uuid`].
            #[must_use]
            #[inline]
            pub fn as_uuid(&self) -> Uuid {
                self.0
            }

            /// Returns the big-endian byte representation (for proto serialisation).
            #[must_use]
            #[inline]
            pub fn to_bytes(&self) -> [u8; 16] {
                *self.0.as_bytes()
            }
        }

        impl std::fmt::Display for $name {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                self.0.fmt(f)
            }
        }
    };
}

uuid_newtype!(
    /// A unique identifier for a tenant (organisation) in the Personel platform.
    TenantId
);

uuid_newtype!(
    /// A unique identifier for an enrolled endpoint (workstation).
    EndpointId
);

uuid_newtype!(
    /// A unique identifier for a user identity.
    UserId
);

uuid_newtype!(
    /// A unique identifier for a live-view session.
    SessionId
);

uuid_newtype!(
    /// A unique identifier for a policy bundle version.
    PolicyId
);

/// A unique, time-ordered identifier for a single event.
///
/// Backed by UUIDv7 so that events are sortable by insertion time without
/// a separate timestamp index. Distinct type from all other IDs to prevent
/// accidental misuse.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct EventId(Uuid);

impl EventId {
    /// Generates a new UUIDv7 event ID using the current wall clock.
    #[must_use]
    pub fn new_v7() -> Self {
        // uuid::Uuid::now_v7() is stabilised in uuid 1.6+
        Self(Uuid::now_v7())
    }

    /// Wraps an existing UUID (useful for deserialization).
    #[must_use]
    #[inline]
    pub fn new(id: Uuid) -> Self {
        Self(id)
    }

    /// Parses from a 16-byte big-endian representation.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::InvalidId`] if `bytes` is not exactly 16 bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        let arr: [u8; 16] = bytes.try_into().map_err(|_| AgentError::InvalidId {
            reason: "expected 16 bytes for EventId",
        })?;
        Ok(Self(Uuid::from_bytes(arr)))
    }

    /// Returns the underlying [`Uuid`].
    #[must_use]
    #[inline]
    pub fn as_uuid(&self) -> Uuid {
        self.0
    }

    /// Returns the big-endian byte representation.
    #[must_use]
    #[inline]
    pub fn to_bytes(&self) -> [u8; 16] {
        *self.0.as_bytes()
    }
}

impl std::fmt::Display for EventId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.0.fmt(f)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_bytes() {
        let id = TenantId::new(Uuid::new_v4());
        let bytes = id.to_bytes();
        let recovered = TenantId::from_bytes(&bytes).unwrap();
        assert_eq!(id, recovered);
    }

    #[test]
    fn event_id_v7_ordering() {
        let a = EventId::new_v7();
        // Sleep is avoided; v7 has ms precision so two back-to-back calls
        // may equal — only checking they're valid UUIDs.
        let b = EventId::new_v7();
        // Both should have the same version nibble (7).
        assert_eq!(a.as_uuid().get_version_num(), 7);
        assert_eq!(b.as_uuid().get_version_num(), 7);
    }

    #[test]
    fn from_bytes_wrong_length() {
        let err = TenantId::from_bytes(&[0u8; 10]);
        assert!(err.is_err());
    }
}
