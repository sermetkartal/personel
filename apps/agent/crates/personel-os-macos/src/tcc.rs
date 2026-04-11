//! TCC (Transparency, Consent, and Control) permission checker.
//!
//! Wraps `Security.framework` / `AuthorizationCopyRights` and the
//! `TCCAccessCheckIfGrantedForBundle` private SPI to query whether the agent
//! holds a specific TCC grant without triggering a user prompt.
//!
//! # Approach
//!
//! The public `Security.framework` API `AuthorizationCopyRights` can be used
//! to check certain rights, but TCC permissions (Screen Recording, Input
//! Monitoring, Accessibility, Full Disk Access) are not exposed through the
//! standard authorization database. The practical approaches are:
//!
//! 1. **Attempt the operation and observe failure**: e.g. attempt to enumerate
//!    `SCShareableContent`; if TCC is missing the call succeeds but returns no
//!    displayable content.
//!
//! 2. **`IOHIDCheckAccess` / `IOHIDRequestAccess`**: public API in IOKit for
//!    Input Monitoring TCC.
//!
//! 3. **`AXIsProcessTrustedWithOptions`**: public API for Accessibility TCC.
//!
//! 4. **Private `TCCAccessCheckIfGrantedForBundle` SPI**: not recommended for
//!    notarized app distribution (may fail notarization review).
//!
//! Phase 2.2 will use approaches 2 and 3. Approach 1 is the fallback for
//! Screen Recording and Full Disk Access.
//!
//! # Phase 2.1 status
//!
//! The types and constants are declared. `check_permission` returns
//! `Err(Unsupported)` on macOS and non-macOS alike.

use personel_core::error::{AgentError, Result};

/// A macOS TCC permission capability.
///
/// Each variant maps to a specific TCC service string as stored in
/// `/Library/Application Support/com.apple.TCC/TCC.db`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TccPermission {
    /// Screen Recording (`kTCCServiceScreenCapture`).
    /// Required for ScreenCaptureKit.
    ScreenRecording,
    /// Accessibility (`kTCCServiceAccessibility`).
    /// Required for Unicode keystroke content when DLP is enabled (ADR 0013).
    Accessibility,
    /// Input Monitoring (`kTCCServiceListenEvent`).
    /// Required for IOHIDManager keystroke count / idle time.
    InputMonitoring,
    /// Full Disk Access (`kTCCServiceSystemPolicyAllFiles`).
    /// Required for system-path FSEvents and ES coverage.
    FullDiskAccess,
}

impl TccPermission {
    /// Returns the TCC service string used in system databases and MDM profiles.
    #[must_use]
    pub fn service_string(self) -> &'static str {
        match self {
            Self::ScreenRecording => "kTCCServiceScreenCapture",
            Self::Accessibility => "kTCCServiceAccessibility",
            Self::InputMonitoring => "kTCCServiceListenEvent",
            Self::FullDiskAccess => "kTCCServiceSystemPolicyAllFiles",
        }
    }

    /// Returns whether this permission can be pre-granted via MDM PPPC profile.
    ///
    /// `false` means the user must grant it interactively through
    /// System Settings → Privacy & Security. See ADR 0015 §"MDM deployment".
    #[must_use]
    pub fn mdm_pre_grantable(self) -> bool {
        match self {
            // Full Disk Access can be pre-granted via PPPC MDM profile.
            Self::FullDiskAccess => true,
            // Screen Recording, Accessibility, Input Monitoring always require
            // the user to grant interactively.
            Self::ScreenRecording | Self::Accessibility | Self::InputMonitoring => false,
        }
    }
}

/// The result of a TCC permission check.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TccStatus {
    /// The permission has been granted by the user (or MDM).
    Granted,
    /// The permission has been explicitly denied by the user.
    Denied,
    /// The user has not yet been prompted for this permission.
    NotDetermined,
    /// The system TCC subsystem could not be queried (e.g. sandboxed context).
    Unknown,
}

/// Queries whether the running process holds a specific TCC permission.
///
/// On macOS this uses the appropriate public API for each permission kind:
/// - `Input Monitoring`: `IOHIDCheckAccess(kIOHIDRequestTypeListenEvent)`
/// - `Accessibility`: `AXIsProcessTrustedWithOptions(promptIfNotTrusted: false)`
/// - `Screen Recording` / `Full Disk Access`: operational probe (Phase 2.2).
///
/// This function **never triggers a user prompt**. Use `request_permission`
/// (Phase 2.2+) to prompt the user.
///
/// # Errors
///
/// - [`AgentError::Unsupported`] in Phase 2.1 on all platforms.
/// - Will return `Ok(TccStatus::Unknown)` in Phase 2.2+ if the OS returns an
///   unexpected code, never an `Err` (so the caller can degrade gracefully).
///
/// # Example
///
/// ```rust,no_run
/// use personel_os_macos::tcc::{check_permission, TccPermission};
///
/// match check_permission(TccPermission::InputMonitoring) {
///     Ok(status) => println!("status: {status:?}"),
///     Err(e) => println!("not available: {e}"),
/// }
/// ```
pub fn check_permission(permission: TccPermission) -> Result<TccStatus> {
    let _ = permission;

    #[cfg(target_os = "macos")]
    {
        // Phase 2.2: branch on `permission` and call the appropriate API.
        //
        // Input Monitoring example:
        //   use core_foundation::base::TCFType;
        //   let access = unsafe {
        //       IOHIDCheckAccess(kIOHIDRequestTypeListenEvent)
        //   };
        //   Ok(match access {
        //       kIOHIDAccessTypeGranted => TccStatus::Granted,
        //       kIOHIDAccessTypeDenied  => TccStatus::Denied,
        //       _                       => TccStatus::NotDetermined,
        //   })
        //
        // Accessibility example:
        //   let trusted = unsafe { AXIsProcessTrusted() };
        //   Ok(if trusted { TccStatus::Granted } else { TccStatus::Denied })
        //
        // SAFETY note for future implementor: IOHIDCheckAccess is a C
        // function in IOKit.framework; link with
        // `#[link(name = "IOKit", kind = "framework")]`. The return type is
        // `IOHIDAccessType` (u32 in practice). No memory ownership issues.
        Err(AgentError::Unsupported {
            os: "macos",
            component: "tcc::check_permission",
        })
    }

    #[cfg(not(target_os = "macos"))]
    {
        crate::stub::tcc::check_permission(permission)
    }
}

/// Returns `true` if ALL permissions in `required` are [`TccStatus::Granted`].
///
/// Convenience wrapper over [`check_permission`] for bulk-checking the
/// permission set needed to start a specific collector.
///
/// # Errors
///
/// Propagates the first error from `check_permission`.
pub fn all_granted(required: &[TccPermission]) -> Result<bool> {
    for &perm in required {
        match check_permission(perm)? {
            TccStatus::Granted => {}
            _ => return Ok(false),
        }
    }
    Ok(true)
}
