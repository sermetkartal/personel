//! Active window title and focus collector — X11 / Wayland dual adapter.
//!
//! At startup [`detect_session_type`] reads `$XDG_SESSION_TYPE` (and falls back
//! to checking `$WAYLAND_DISPLAY` / `$DISPLAY`) to determine which adapter to
//! construct. The top-level [`active_window_title`] function routes to the
//! appropriate adapter automatically.
//!
//! # X11 adapter (Phase 2.2)
//!
//! Uses `x11rb` to subscribe to `XCB_PROPERTY_NOTIFY` events on the root
//! window. When `_NET_ACTIVE_WINDOW` changes it reads `_NET_WM_NAME` (UTF-8)
//! or falls back to `WM_NAME` on the newly-focused window.
//!
//! # Wayland adapter — intentionally limited
//!
//! Wayland's security model deliberately provides **no standard protocol** for
//! one application to read another application's window title. This is a
//! privacy feature of the Wayland compositor architecture; it prevents any
//! background process from silently harvesting what a user is doing without
//! their knowledge.
//!
//! Per ADR 0016 §Display, Personel respects this boundary. The
//! [`WaylandAdapter`] therefore returns
//! `Err(AgentError::Unsupported { os: "wayland", component: "window_title" })`
//! unconditionally. Customers who require window title collection on Linux
//! **must use an X11 session**. This limitation is documented in the admin
//! console and counted as a privacy-positive capability for KVKK compliance
//! framing (Wayland users have stronger OS-level privacy guarantees).
//!
//! Phase 2.2 will explore compositor-specific extension APIs (GNOME Shell D-Bus
//! `org.gnome.Shell` / KDE `org.kde.KWin`) as opt-in adapters, but these will
//! require user-session supplementary processes and are strictly out of scope
//! for Phase 2.1.

use personel_core::error::{AgentError, Result};

/// Display server session type detected at runtime.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SessionType {
    /// X11/Xorg display server. Full feature set available.
    X11,
    /// Wayland compositor. Window title collection is unavailable by design;
    /// see module-level documentation.
    Wayland,
    /// Session type cannot be determined (headless server, VNC, etc.).
    Unknown,
}

/// Detects the current display server session type.
///
/// Resolution order:
/// 1. `$XDG_SESSION_TYPE` environment variable (set by PAM/logind on modern
///    distributions). Values: `"x11"` → [`SessionType::X11`],
///    `"wayland"` → [`SessionType::Wayland`].
/// 2. If `$XDG_SESSION_TYPE` is absent or `"mir"` / unrecognised:
///    - `$WAYLAND_DISPLAY` set → [`SessionType::Wayland`]
///    - `$DISPLAY` set → [`SessionType::X11`]
///    - Neither set → [`SessionType::Unknown`]
#[must_use]
pub fn detect_session_type() -> SessionType {
    match std::env::var("XDG_SESSION_TYPE")
        .as_deref()
        .unwrap_or("")
    {
        "x11" => return SessionType::X11,
        "wayland" => return SessionType::Wayland,
        _ => {}
    }
    if std::env::var("WAYLAND_DISPLAY").is_ok() {
        SessionType::Wayland
    } else if std::env::var("DISPLAY").is_ok() {
        SessionType::X11
    } else {
        SessionType::Unknown
    }
}

/// Information about the currently focused window.
#[derive(Debug, Clone)]
pub struct ActiveWindowInfo {
    /// Window title string (UTF-8). Empty if the compositor withholds it.
    pub title: String,
    /// PID of the owning process, if obtainable.
    pub pid: Option<u32>,
}

// ── X11 adapter ───────────────────────────────────────────────────────────────

/// X11 adapter for window title and focus collection.
///
/// Uses `x11rb` to observe `_NET_ACTIVE_WINDOW` / `_NET_WM_NAME` property
/// changes on the root window. Requires the `x11` feature flag and an active
/// `$DISPLAY`.
///
/// Phase 2.2 will implement [`X11Adapter::new`] and [`X11Adapter::poll`].
pub struct X11Adapter {
    _priv: (),
}

impl X11Adapter {
    /// Opens an X11 connection to `$DISPLAY` and subscribes to focus-change
    /// notifications.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    /// In Phase 2.2 may return [`AgentError::Io`] on connection failure, or
    /// [`AgentError::CollectorStart`] if `_NET_WM_NAME` is unsupported.
    pub fn new() -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "window_title::X11Adapter::new",
        })
    }

    /// Polls for the currently active window title.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    pub fn poll(&self) -> Result<ActiveWindowInfo> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "window_title::X11Adapter::poll",
        })
    }
}

// ── Wayland adapter ───────────────────────────────────────────────────────────

/// Wayland adapter for window focus.
///
/// # Design decision — window title is UNAVAILABLE on Wayland
///
/// The Wayland protocol intentionally provides no mechanism for one client to
/// inspect another client's window title or focused state. This is a
/// deliberate security boundary in the Wayland architecture: compositors (e.g.
/// Mutter/GNOME Shell, KWin/KDE) do not expose per-client metadata to
/// unprivileged third parties.
///
/// [`WaylandAdapter::active_window`] therefore always returns
/// `Err(AgentError::Unsupported { os: "wayland", component: "window_title" })`.
///
/// Customers who require window title collection on Linux endpoints **must
/// configure their fleet to use X11 sessions** (or XWayland with
/// backwards-compatible EWMH properties). This is a documented limitation in
/// ADR 0016 and the Personel admin console surfaces it as a capability flag
/// (`window_title_available: false`) for Wayland-session endpoints.
pub struct WaylandAdapter {
    _priv: (),
}

impl WaylandAdapter {
    /// Creates a Wayland adapter. Never produces a usable window title;
    /// call sites should prefer [`X11Adapter`] where available.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    pub fn new() -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "window_title::WaylandAdapter::new",
        })
    }

    /// Returns the active window information.
    ///
    /// # Wayland security model
    ///
    /// This function **always** returns
    /// `Err(AgentError::Unsupported { os: "wayland", component: "window_title" })`.
    ///
    /// Wayland compositors do not expose inter-client window metadata to
    /// unprivileged processes. This is by design and Personel respects this
    /// boundary. See [`WaylandAdapter`] documentation for the full rationale.
    ///
    /// # Errors
    ///
    /// Always returns [`AgentError::Unsupported`].
    pub fn active_window(&self) -> Result<ActiveWindowInfo> {
        // INTENTIONAL: Wayland's security model prevents window title access.
        // This is NOT a temporary scaffold — this is the correct behaviour.
        // See ADR 0016 §Display and WaylandAdapter documentation.
        Err(AgentError::Unsupported {
            os: "wayland",
            component: "window_title",
        })
    }
}

// ── Top-level routing function ────────────────────────────────────────────────

/// Returns the title of the currently focused window.
///
/// Detects the session type via [`detect_session_type`] and routes to the
/// appropriate adapter. On Wayland this always returns `Err(Unsupported)`.
///
/// # Errors
///
/// * [`AgentError::Unsupported`] — Wayland session or Phase 2.1 scaffold.
/// * [`AgentError::Io`] — X11 connection failure (Phase 2.2).
pub fn active_window_title() -> Result<ActiveWindowInfo> {
    match detect_session_type() {
        SessionType::X11 => {
            let adapter = X11Adapter::new()?;
            adapter.poll()
        }
        SessionType::Wayland => {
            // Wayland cannot provide window titles — return Unsupported
            // with explicit component tag so callers can distinguish this
            // from a transient failure.
            Err(AgentError::Unsupported {
                os: "wayland",
                component: "window_title",
            })
        }
        SessionType::Unknown => Err(AgentError::Unsupported {
            os: "linux",
            component: "window_title::unknown_session",
        }),
    }
}
