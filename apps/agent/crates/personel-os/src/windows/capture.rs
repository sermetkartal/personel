//! DXGI Desktop Duplication capture wrapper.
//!
//! # TODO (Phase 1 implementation)
//!
//! - Implement `DxgiCapture` wrapping `IDXGIOutputDuplication::AcquireNextFrame`.
//! - Encode frames to WebP using the `image` or `webp` crate.
//! - Support multi-monitor enumeration.
//! - Implement `ScreenClipCapture` for short video clips via DXGI + H.264.

use personel_core::error::{AgentError, Result};

/// A single captured desktop frame.
pub struct CapturedFrame {
    /// Raw BGRA pixel data.
    pub pixels: Vec<u8>,
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// Monitor index (0-based).
    pub monitor_index: u32,
}

/// DXGI-based desktop capture (stub).
pub struct DxgiCapture {
    _private: (),
}

impl DxgiCapture {
    /// Opens a capture session for the given monitor index.
    ///
    /// # Errors
    ///
    /// Returns an error (stub always fails until implemented).
    pub fn open(monitor_index: u32) -> Result<Self> {
        let _ = monitor_index;
        Err(AgentError::CollectorStart {
            name: "screen",
            reason: "DXGI capture not yet implemented".into(),
        })
    }

    /// Captures one frame.
    ///
    /// # Errors
    ///
    /// Returns an error if the capture fails.
    pub fn capture_frame(&self) -> Result<CapturedFrame> {
        Err(AgentError::CollectorStart {
            name: "screen",
            reason: "DXGI capture not yet implemented".into(),
        })
    }
}
