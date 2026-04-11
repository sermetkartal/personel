//! IOHIDManager input abstraction — foreground window and idle-time queries.
//!
//! # macOS implementation plan (Phase 2.3)
//!
//! - `foreground_window_info`: call `NSWorkspace.shared.frontmostApplication`
//!   via the `cocoa` crate bindings to obtain `NSRunningApplication`, then
//!   read `localizedName` (app display name) and `processIdentifier` (pid).
//!   Window title requires `CGWindowListCopyWindowInfo` with
//!   `kCGWindowListOptionOnScreenOnly`.
//!
//! - `last_input_idle_ms`: use `IOHIDCheckAccess` + `IOHIDRequestAccess` to
//!   verify Input Monitoring TCC permission, then query `IOHIDManager` for
//!   the most recent HID event timestamp. The difference with
//!   `CACurrentMediaTime()` gives the idle duration.
//!
//! # TCC permissions required
//!
//! - **Input Monitoring** (`com.apple.private.iohid.manager`) for idle time.
//! - **Accessibility** for Unicode keystroke content (only when DLP enabled —
//!   see ADR 0013 and ADR 0015 §"Keystroke capture").
//!
//! # Phase 2.1 status
//!
//! All public functions return `Err(AgentError::Unsupported)` on macOS and
//! on all other platforms.

use personel_core::error::{AgentError, Result};

/// Information about the currently active application window.
///
/// On macOS this maps to the frontmost `NSRunningApplication`. The `hwnd`
/// field is a Windows HWND equivalent; on macOS it is always `0` because
/// macOS does not expose a global window handle integer.
#[derive(Debug, Clone)]
pub struct ForegroundWindowInfo {
    /// Display name of the frontmost application (e.g. `"Safari"`).
    pub title: String,
    /// BSD process identifier of the frontmost application.
    pub pid: u32,
    /// Platform window handle. Always `0` on macOS; kept for API parity
    /// with the Windows `personel-os` crate.
    pub hwnd: usize,
}

/// Returns the number of milliseconds since the last user input event.
///
/// Uses `IOHIDManager` on macOS (requires Input Monitoring TCC permission).
///
/// # Errors
///
/// - [`AgentError::Unsupported`] on all platforms until Phase 2.3 implements
///   the real IOHIDManager path.
/// - Will return [`AgentError::CollectorRuntime`] if the TCC grant for Input
///   Monitoring is missing (Phase 2.3+).
pub fn last_input_idle_ms() -> Result<u64> {
    #[cfg(target_os = "macos")]
    {
        // Phase 2.3: replace with IOHIDManager query.
        //
        // use cocoa::base::nil;
        // use objc::runtime::Object;
        //
        // The IOHIDManager approach:
        //   1. IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone)
        //   2. IOHIDManagerSetDeviceMatching(manager, usage_page=kHIDPage_GenericDesktop)
        //   3. Register IOHIDManagerRegisterInputValueCallback
        //   4. Record last callback timestamp; delta with CACurrentMediaTime() = idle ms
        //
        // SAFETY note for future implementor: IOHIDManagerRef is a CF type
        // (opaque pointer). Ownership follows CF retain/release rules; wrap in
        // a RAII guard that calls IOHIDManagerClose + CFRelease on Drop.
        Err(AgentError::Unsupported {
            os: "macos",
            component: "input::last_input_idle_ms",
        })
    }

    #[cfg(not(target_os = "macos"))]
    {
        crate::stub::input::last_input_idle_ms()
    }
}

/// Returns information about the currently active foreground application.
///
/// On macOS, calls `NSWorkspace.shared.frontmostApplication` to obtain
/// the frontmost `NSRunningApplication` and reads its display name and PID.
///
/// # Errors
///
/// - [`AgentError::Unsupported`] on all platforms until Phase 2.3.
/// - Will return [`AgentError::CollectorRuntime`] if Accessibility permission
///   is unavailable and DLP mode requires content (Phase 2.3+).
pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
    #[cfg(target_os = "macos")]
    {
        // Phase 2.3: replace with real NSWorkspace call.
        //
        // Pseudo-code (requires cocoa + objc crates):
        //
        //   let workspace: *mut Object = msg_send![class!(NSWorkspace), sharedWorkspace];
        //   let app: *mut Object = msg_send![workspace, frontmostApplication];
        //   let name_ns: *mut Object = msg_send![app, localizedName];
        //   let pid: i32 = msg_send![app, processIdentifier];
        //   let title = nsstring_to_string(name_ns);
        //
        // SAFETY note for future implementor: NSWorkspace and NSRunningApplication
        // are Objective-C objects managed by ARC. When using the raw `objc` crate
        // without ARC, retain counts must be managed manually. `cocoa 0.25` does
        // not automate this. Wrap the result in an RAII guard or use
        // `autoreleasepool` from the `cocoa` crate.
        Err(AgentError::Unsupported {
            os: "macos",
            component: "input::foreground_window_info",
        })
    }

    #[cfg(not(target_os = "macos"))]
    {
        crate::stub::input::foreground_window_info()
    }
}
