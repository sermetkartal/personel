//! ScreenCaptureKit wrapper — macOS screen capture abstraction.
//!
//! # macOS implementation plan (Phase 2.3)
//!
//! ScreenCaptureKit (macOS 13+) is the only non-deprecated screen capture API
//! on macOS 14+. The legacy `CGDisplayStream` path is removed in macOS 15.
//!
//! High-level design (Phase 2.3):
//!
//! 1. Call `SCShareableContent.getWithCompletionHandler` to enumerate displays.
//! 2. Build an `SCContentFilter` scoped to the primary display (or all).
//! 3. Create an `SCStreamConfiguration` (frame rate cap, pixel format BGRA).
//! 4. Start an `SCStream` with a Rust-backed `SCStreamOutput` delegate (via
//!    Swift shim or `objc` direct dispatch).
//! 5. Each `CMSampleBuffer` delivered to the delegate is converted to a
//!    `CapturedFrame` via `CVPixelBuffer` → raw bytes copy.
//!
//! For live view, the `CMSampleBuffer` is fed directly to VideoToolbox H.264
//! encoding (matches the Windows DXGI → H.264 pipeline in ADR 0015).
//!
//! # TCC permissions required
//!
//! **Screen Recording** (`com.apple.private.screencapturekit`) — user-grantable
//! only; cannot be pre-granted via MDM PPPC. Agent must prompt the user.
//!
//! # Phase 2.1 status
//!
//! All types are declared for API surface parity. All operations return
//! `Err(AgentError::Unsupported)`.

use personel_core::error::{AgentError, Result};

/// A single captured desktop frame.
///
/// Pixel data is BGRA-encoded, row-major, with stride equal to `width * 4`.
/// On macOS, frames originate from `CVPixelBuffer` delivered by `SCStream`.
#[derive(Debug)]
pub struct CapturedFrame {
    /// Raw BGRA pixel data. Length is `width * height * 4`.
    pub pixels: Vec<u8>,
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// Display index (0-based). Maps to `SCDisplay.displayID` on macOS.
    pub monitor_index: u32,
}

/// ScreenCaptureKit-backed screen capture session.
///
/// On macOS this wraps an `SCStream` (Phase 2.3). In Phase 2.1 construction
/// always fails with `AgentError::Unsupported`.
///
/// # Example
///
/// ```rust,no_run
/// use personel_os_macos::capture::ScCapture;
///
/// // This always returns Err(Unsupported) in Phase 2.1.
/// let session = ScCapture::open(0);
/// assert!(session.is_err());
/// ```
pub struct ScCapture {
    /// Display index this capture session is bound to.
    monitor_index: u32,
    /// Phase 2.1 marker — prevents direct construction outside this module.
    _private: (),
}

impl ScCapture {
    /// Opens a capture session for the given display index.
    ///
    /// Requires **Screen Recording TCC permission**. On macOS, if the
    /// permission is missing, the OS silently delivers black frames; this
    /// function will return `Err(CollectorStart)` in Phase 2.3+ when it
    /// detects the condition via `SCShareableContent`.
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1 (not yet implemented).
    /// - [`AgentError::CollectorStart`] in Phase 2.3+ if TCC permission is
    ///   missing or `SCShareableContent` enumeration fails.
    pub fn open(monitor_index: u32) -> Result<Self> {
        let _ = monitor_index;

        #[cfg(target_os = "macos")]
        {
            // Phase 2.3: call SCShareableContent.getWithCompletionHandler,
            // build SCContentFilter, create SCStream, start streaming.
            //
            // SAFETY note for future implementor: SCStream is an Objective-C
            // object. The output delegate (`SCStreamOutput`) must be retained
            // for the lifetime of the stream. Use a Box<T> pinned behind an
            // Arc or an explicit retain/release pair; do NOT let the delegate
            // be dropped while the stream is active.
            Err(AgentError::Unsupported {
                os: "macos",
                component: "capture::ScCapture::open",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            // The stub always returns Err(Unsupported); this branch is
            // compile-time dead code on macOS but must type-check everywhere.
            let _ = monitor_index;
            Err(AgentError::Unsupported {
                os: "non-macos",
                component: "capture::ScCapture::open",
            })
        }
    }

    /// Captures one frame synchronously.
    ///
    /// In the real Phase 2.3 implementation this returns the most recent
    /// `CMSampleBuffer` delivered by `SCStream`'s output callback.
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::CollectorRuntime`] in Phase 2.3+ if the stream has
    ///   stalled or TCC was revoked mid-session.
    pub fn capture_frame(&self) -> Result<CapturedFrame> {
        let _ = self.monitor_index;

        #[cfg(target_os = "macos")]
        {
            Err(AgentError::Unsupported {
                os: "macos",
                component: "capture::ScCapture::capture_frame",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            Err(AgentError::Unsupported {
                os: "non-macos",
                component: "capture::ScCapture::capture_frame",
            })
        }
    }
}
