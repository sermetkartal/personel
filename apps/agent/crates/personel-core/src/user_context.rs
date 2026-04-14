//! Shared, process-wide cache of the current interactive user's SID.
//!
//! # Why this lives in `personel-core`
//!
//! `personel-transport` is the crate that wraps outgoing events into proto
//! frames and fills `EventMeta.user_sid`. `personel-collectors` is the crate
//! that actually knows how to resolve the SID on Windows (via
//! `WTSGetActiveConsoleSessionId` → `WTSQueryUserToken` →
//! `GetTokenInformation(TokenUser)` → `ConvertSidToStringSidW`). A direct
//! dependency from transport to collectors would be a cycle, so we route the
//! handshake through this tiny OS-agnostic slot:
//!
//! - Resolver (collectors, Windows-gated) calls [`set_current_sid`] every 60s.
//! - Consumer (transport, platform-neutral) calls [`current_sid`] at the
//!   moment each `EventMeta` is constructed.
//!
//! # Fallback
//!
//! If the slot has never been populated (resolver hasn't run yet, or the
//! agent is running in session 0 where no interactive user exists) the
//! consumer receives `None` and emits the literal fallback
//! `"S-1-5-18"` (LocalSystem) at its own discretion. See the transport
//! `stream_once` function in `personel-transport/src/client.rs`.
//!
//! # Thread safety
//!
//! Backed by a `RwLock<Option<String>>` wrapped in an [`OnceLock`]. Writes
//! happen once per minute; reads happen on every event (hundreds per second
//! under load). `RwLock` is the right primitive — readers never block each
//! other, writers take an exclusive lock briefly.

use std::sync::{OnceLock, RwLock};

/// Global singleton holding the latest known user SID. `None` until the
/// first successful resolution by the Windows refresh task.
static CURRENT_USER_SID: OnceLock<RwLock<Option<String>>> = OnceLock::new();

/// The conventional fallback SID used when no interactive user can be
/// resolved (session 0 service context, pre-logon boot, etc.).
///
/// `S-1-5-18` is the well-known SID for the `LocalSystem` account on every
/// Windows install since NT 4.0 and is stable across OS versions.
pub const LOCAL_SYSTEM_SID: &str = "S-1-5-18";

fn slot() -> &'static RwLock<Option<String>> {
    CURRENT_USER_SID.get_or_init(|| RwLock::new(None))
}

/// Stores the given SID string as the current interactive user.
///
/// Pass `None` to explicitly clear the slot (e.g. the resolver detected a
/// logoff and cannot resolve a new session yet). Pass `Some(sid)` with a
/// canonical `S-1-5-...` string otherwise.
///
/// Idempotent and cheap — the lock is held only for the swap.
pub fn set_current_sid(sid: Option<String>) {
    if let Ok(mut g) = slot().write() {
        *g = sid;
    }
}

/// Returns the most recently cached SID, or `None` if never set.
///
/// Callers that require a value (e.g. `EventMeta.user_sid` is non-nullable
/// downstream) should substitute [`LOCAL_SYSTEM_SID`] on `None`.
#[must_use]
pub fn current_sid() -> Option<String> {
    slot().read().ok().and_then(|g| g.clone())
}

/// Returns the cached SID or the [`LOCAL_SYSTEM_SID`] fallback — convenience
/// helper for call sites that always need a concrete string.
#[must_use]
pub fn current_sid_or_system() -> String {
    current_sid().unwrap_or_else(|| LOCAL_SYSTEM_SID.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    // Serialize tests because they share the global slot. Without this
    // guard parallel execution produces flakes when one test sets a SID
    // while another reads.
    static GUARD: Mutex<()> = Mutex::new(());

    #[test]
    fn set_and_get_roundtrip() {
        let _g = GUARD.lock().unwrap_or_else(|p| p.into_inner());
        set_current_sid(Some("S-1-5-21-111-222-333-1001".to_string()));
        assert_eq!(
            current_sid().as_deref(),
            Some("S-1-5-21-111-222-333-1001")
        );
        assert_eq!(current_sid_or_system(), "S-1-5-21-111-222-333-1001");
        set_current_sid(None); // leave the slot clean for the next test
    }

    #[test]
    fn clear_returns_none_and_falls_back() {
        let _g = GUARD.lock().unwrap_or_else(|p| p.into_inner());
        set_current_sid(Some("S-1-5-21-1".into()));
        set_current_sid(None);
        assert!(current_sid().is_none());
        assert_eq!(current_sid_or_system(), LOCAL_SYSTEM_SID);
    }
}
