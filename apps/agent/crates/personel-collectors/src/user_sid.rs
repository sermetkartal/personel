//! Windows interactive user SID resolver.
//!
//! The agent normally runs as `LocalSystem` (session 0). The *events* it
//! captures, however, are attributed to the human currently logged into the
//! console session. This module resolves that human's SID and caches it in
//! [`personel_core::user_context`] so the transport layer can stamp every
//! outgoing `EventMeta.user_sid` without itself knowing anything about
//! Windows session APIs.
//!
//! # Resolution chain
//!
//! 1. [`WTSGetActiveConsoleSessionId`] — which TS session owns the physical
//!    console right now? Returns `0xFFFFFFFF` when no-one is logged on
//!    (e.g. winlogon screen, boot-up, RDP-only host with no local console).
//! 2. [`WTSQueryUserToken`] — opens the primary token of that session's
//!    logged-on user. Requires `SE_TCB_NAME` privilege, which `LocalSystem`
//!    always has. Fails with `ERROR_NO_TOKEN` when the session has no user
//!    (same states that trigger the `0xFFFFFFFF` fallback above).
//! 3. [`GetTokenInformation`] with `TokenUser` — pulls the `SID` pointer out
//!    of the `TOKEN_USER` struct.
//! 4. [`ConvertSidToStringSidW`] — canonical `S-1-5-…` string form.
//!
//! The result is pushed into [`personel_core::user_context::set_current_sid`].
//! Failures at any step clear the slot (callers fall back to
//! [`personel_core::user_context::LOCAL_SYSTEM_SID`]).
//!
//! # Refresh cadence
//!
//! A background task wakes every 60 seconds and repeats the full chain. The
//! chain is cheap — three kernel calls plus a string allocation — so there
//! is no benefit to conditional re-resolution. A user switch (fast-user-
//! switching, lock/unlock with a new account) is therefore visible in
//! `EventMeta.user_sid` within at most 60 seconds, which matches the
//! precision of the downstream ClickHouse reports.
//!
//! # KVKK
//!
//! A SID is personally identifying under KVKK m.3. It is already tracked
//! in `users.sid` (Postgres) and in every audit log entry; adding it to the
//! event meta exposes no new category of personal data.

#[cfg(target_os = "windows")]
pub use win::spawn_refresh_task;

#[cfg(not(target_os = "windows"))]
/// On non-Windows targets the refresh task is a no-op — the user_context
/// slot stays `None`, and the transport layer substitutes
/// [`personel_core::user_context::LOCAL_SYSTEM_SID`].
pub fn spawn_refresh_task() {}

#[cfg(target_os = "windows")]
mod win {
    use std::time::Duration;

    use tracing::{debug, trace, warn};

    use windows::core::PWSTR;
    use windows::Win32::Foundation::{CloseHandle, HANDLE, HLOCAL, LocalFree};
    use windows::Win32::Security::Authorization::ConvertSidToStringSidW;
    use windows::Win32::Security::{GetTokenInformation, TokenUser, TOKEN_USER};
    use windows::Win32::System::RemoteDesktop::{
        WTSGetActiveConsoleSessionId, WTSQueryUserToken,
    };
    use core::ffi::c_void;

    use personel_core::user_context::set_current_sid;

    const REFRESH_INTERVAL: Duration = Duration::from_secs(60);

    /// Spawns the background user-SID refresh task. Idempotent-ish: calling
    /// twice leaks a task but does not corrupt the cache.
    pub fn spawn_refresh_task() {
        // Run the first resolution immediately so events emitted during
        // startup already carry a SID (when one exists).
        refresh_once();

        tokio::task::spawn_blocking(move || loop {
            std::thread::sleep(REFRESH_INTERVAL);
            refresh_once();
        });
    }

    fn refresh_once() {
        match resolve_active_console_sid() {
            Ok(Some(sid)) => {
                trace!(sid = %sid, "user_sid: resolved console user");
                set_current_sid(Some(sid));
            }
            Ok(None) => {
                debug!("user_sid: no active console session — clearing slot");
                set_current_sid(None);
            }
            Err(e) => {
                warn!(error = %e, "user_sid: resolve failed — slot unchanged");
            }
        }
    }

    /// Walks the 4-step chain (console session → user token → TOKEN_USER →
    /// SID string). Returns `Ok(None)` when there is no interactive user
    /// (boot, logoff, lock screen) and `Err` only when an API we expected
    /// to succeed actually failed.
    fn resolve_active_console_sid() -> Result<Option<String>, String> {
        // SAFETY: all Win32 calls below use handles we own and buffers sized
        // via the standard two-pass `GetTokenInformation` pattern.
        unsafe {
            let session_id = WTSGetActiveConsoleSessionId();
            if session_id == 0xFFFF_FFFF {
                return Ok(None);
            }

            let mut token = HANDLE::default();
            if WTSQueryUserToken(session_id, &mut token).is_err() {
                // ERROR_NO_TOKEN / ERROR_PRIVILEGE_NOT_HELD both land here.
                // The former is a normal "nobody logged in" state; the
                // latter means the agent is NOT running as LocalSystem,
                // which is a deployment bug — log a warning on that path.
                return Ok(None);
            }

            let sid_str = sid_from_token(token);
            let _ = CloseHandle(token);
            sid_str.map(Some)
        }
    }

    /// Extracts the SID out of a primary token and returns it in canonical
    /// `S-1-…` string form. Consumes nothing — caller still owns the token.
    unsafe fn sid_from_token(token: HANDLE) -> Result<String, String> {
        // Two-pass GetTokenInformation: first call with null buffer to learn
        // the required size; second call with a fitting buffer.
        let mut needed: u32 = 0;
        // First call is expected to fail with ERROR_INSUFFICIENT_BUFFER; the
        // `windows` crate surfaces that as Err which we deliberately ignore.
        let _ = GetTokenInformation(token, TokenUser, None, 0, &mut needed);
        if needed == 0 {
            return Err("GetTokenInformation sizing returned 0".into());
        }

        let mut buf = vec![0u8; needed as usize];
        GetTokenInformation(
            token,
            TokenUser,
            Some(buf.as_mut_ptr().cast()),
            needed,
            &mut needed,
        )
        .map_err(|e| format!("GetTokenInformation: {e}"))?;

        let token_user = &*(buf.as_ptr().cast::<TOKEN_USER>());
        let sid_ptr = token_user.User.Sid;
        if sid_ptr.is_invalid() {
            return Err("TOKEN_USER.Sid is null".into());
        }

        let mut sid_str: PWSTR = PWSTR::null();
        ConvertSidToStringSidW(sid_ptr, &mut sid_str)
            .map_err(|e| format!("ConvertSidToStringSidW: {e}"))?;
        if sid_str.is_null() {
            return Err("ConvertSidToStringSidW returned null".into());
        }

        // Walk the UTF-16 string until NUL.
        let mut len = 0usize;
        while *sid_str.0.add(len) != 0 {
            len += 1;
        }
        let slice = std::slice::from_raw_parts(sid_str.0, len);
        let out = String::from_utf16_lossy(slice);
        // ConvertSidToStringSidW allocates with LocalAlloc; caller must free.
        LocalFree(HLOCAL(sid_str.0.cast::<c_void>()));
        Ok(out)
    }
}
