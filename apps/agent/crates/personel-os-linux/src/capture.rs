//! Screen capture — X11 XShm adapter and Wayland portal adapter.
//!
//! The top-level [`capture_frame`] function detects the session type and
//! delegates to [`X11Adapter`] (XShm shared-memory capture) or
//! [`WaylandAdapter`] (`org.freedesktop.portal.ScreenCast`).
//!
//! # X11 capture (Phase 2.2)
//!
//! Uses `XCB-shm` (`x11rb` `shm` feature) for zero-copy shared-memory frame
//! capture. The MIT-SHM extension eliminates per-frame kernel copies and
//! achieves ~1% CPU at 1 Hz on a standard desktop. Fallback to `XGetImage`
//! if MIT-SHM is unavailable.
//!
//! # Wayland capture (Phase 2.2)
//!
//! Uses `org.freedesktop.portal.ScreenCast` D-Bus portal, which raises a
//! **user-visible consent dialog** on first use per session (GNOME/KDE can
//! persist the permission with "Remember for this session" or "Always"). The
//! DMA-BUF handle returned by the portal is read via the `wayland-client` crate
//! and a `wl_shm` fallback.
//!
//! # Feature flags
//!
//! | Feature     | Adapter enabled                       |
//! |-------------|---------------------------------------|
//! | `x11`       | [`X11Adapter`] (requires `x11rb`)     |
//! | `wayland`   | [`WaylandAdapter`] (requires `wayland-client`) |
//!
//! Without either feature the adapters compile as stubs returning `Unsupported`.

pub use crate::window_title::SessionType;
use personel_core::error::{AgentError, Result};

/// A single captured screen frame.
#[derive(Debug)]
pub struct CapturedFrame {
    /// Raw pixel data in BGRA8 format (same as DXGI on Windows).
    pub pixels: Vec<u8>,
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// Zero-based monitor/output index.
    pub monitor_index: u32,
}

// ── X11 adapter ───────────────────────────────────────────────────────────────

/// X11 screen capture adapter using XShm (MIT-SHM extension).
///
/// In Phase 2.2 this wraps an `x11rb` connection with the `shm` extension
/// enabled. Phase 2.1 returns [`AgentError::Unsupported`] from all methods.
pub struct X11Adapter {
    _priv: (),
}

impl X11Adapter {
    /// Opens an X11 connection and prepares the SHM segment for capture.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    /// Phase 2.2: [`AgentError::CollectorStart`] if MIT-SHM is unavailable or
    /// `$DISPLAY` is unset.
    pub fn open(_monitor: u32) -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "capture::X11Adapter::open",
        })
    }

    /// Captures one frame from the monitor this adapter was opened for.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    /// Phase 2.2: [`AgentError::Io`] on XCB errors.
    pub fn capture_frame(&self) -> Result<CapturedFrame> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "capture::X11Adapter::capture_frame",
        })
    }
}

// ── Wayland adapter ───────────────────────────────────────────────────────────

/// Wayland screen capture adapter using `org.freedesktop.portal.ScreenCast`.
///
/// Unlike the X11 path, the Wayland portal raises a user-visible consent dialog
/// the first time a session is created (unless the compositor has already
/// stored a persistent grant). Personel treats this as a feature: the user is
/// in control of what is captured.
///
/// Phase 2.2 will implement portal session negotiation, DMA-BUF fd reception,
/// and `wl_shm` fallback. Phase 2.1 returns [`AgentError::Unsupported`].
pub struct WaylandAdapter {
    _priv: (),
}

impl WaylandAdapter {
    /// Initiates a `ScreenCast` portal session.
    ///
    /// On first call (or after permission expiry) the compositor will show a
    /// consent dialog to the user. The `monitor_index` hint is passed to the
    /// portal as a preferred source but the user may override it.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    /// Phase 2.2: [`AgentError::CollectorStart`] if the portal D-Bus interface
    /// is unavailable, or the user denies consent.
    pub fn open(_monitor: u32) -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "capture::WaylandAdapter::open",
        })
    }

    /// Captures one frame via the portal DMA-BUF or `wl_shm` path.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    pub fn capture_frame(&self) -> Result<CapturedFrame> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "capture::WaylandAdapter::capture_frame",
        })
    }
}

// ── Top-level routing function ────────────────────────────────────────────────

/// Captures one frame from monitor `monitor_index`.
///
/// Detects the session type via [`crate::window_title::detect_session_type`]
/// and routes to [`X11Adapter`] or [`WaylandAdapter`] accordingly.
///
/// # Errors
///
/// * [`AgentError::Unsupported`] — Phase 2.1 scaffold, Wayland portal not
///   available, or unknown session type.
/// * [`AgentError::Io`] — X11 connection failure (Phase 2.2).
pub fn capture_frame(monitor_index: u32) -> Result<CapturedFrame> {
    match crate::window_title::detect_session_type() {
        SessionType::X11 => {
            let adapter = X11Adapter::open(monitor_index)?;
            adapter.capture_frame()
        }
        SessionType::Wayland => {
            let adapter = WaylandAdapter::open(monitor_index)?;
            adapter.capture_frame()
        }
        SessionType::Unknown => Err(AgentError::Unsupported {
            os: "linux",
            component: "capture::unknown_session",
        }),
    }
}
