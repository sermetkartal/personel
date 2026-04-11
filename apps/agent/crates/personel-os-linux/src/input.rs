//! Linux input event collector.
//!
//! **Mechanism:** Reads raw `input_event` structs from `/dev/input/event*`
//! character devices using `libinput` (via the `input` crate, Phase 2.2) or
//! by direct device fd polling. This path sits *below* the display server,
//! meaning it works identically on X11 and Wayland sessions.
//!
//! **Collected data (Phase 2 scope):**
//! * Keystroke *count* and timing (idle detection). No keysyms, no content.
//! * Mouse move / click / scroll activity for idle-time classification.
//!
//! **What is NOT collected by default:**
//! * Keystroke content (keysyms/characters). Content capture requires the DLP
//!   opt-in ceremony (ADR 0013) and is only available on X11 (ADR 0016 §Wayland
//!   keystroke content gap).
//!
//! # Permissions
//!
//! The agent process must be a member of the `input` group, or hold
//! `CAP_DAC_READ_SEARCH`. The installer adds the `personel-agent` user to the
//! `input` group automatically. The systemd unit adds
//! `AmbientCapabilities=CAP_DAC_READ_SEARCH` as a fallback. See
//! `docs/adr/0016-linux-agent-architecture.md` §Input.
//!
//! # Phase 2.2 implementation plan
//!
//! Replace the stubs with a `libinput` context opened with
//! `libinput_path_create_context`. Enumerate `/dev/input/event*` nodes, add
//! those with `EV_KEY` capability (via `ioctl EVIOCGBIT`). Run an async poll
//! loop (tokio + `AsyncFd`) dispatching `libinput_dispatch` on readability.

use personel_core::error::{AgentError, Result};

/// Idle time and activity summary returned by the input collector.
#[derive(Debug, Clone)]
pub struct InputActivity {
    /// Milliseconds elapsed since the last keyboard or mouse event.
    pub idle_ms: u64,
    /// Keystroke events counted in the current sampling window.
    pub keystroke_count: u64,
    /// Mouse movement events counted in the current sampling window.
    pub mouse_move_count: u64,
}

/// Returns a snapshot of current input activity.
///
/// In Phase 2.2 this will poll `/dev/input/event*` via `libinput` and return
/// live idle time. In Phase 2.1 it returns `Err(Unsupported)`.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// In Phase 2.2 may return [`AgentError::Io`] if device nodes are inaccessible.
pub fn query_activity() -> Result<InputActivity> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "input::query_activity",
    })
}

/// Starts a background task that writes input activity samples into `sender`.
///
/// The task runs an async poll loop over all `EV_KEY`-capable `/dev/input/event*`
/// nodes. Each sample is emitted at the interval determined by the policy
/// `input_sample_interval_ms` field.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// In Phase 2.2 may return [`AgentError::CollectorStart`] if no input devices
/// are accessible (permissions failure or empty system).
pub fn start_collector(
    _sender: tokio::sync::mpsc::Sender<InputActivity>,
) -> Result<tokio::task::JoinHandle<()>> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "input::start_collector",
    })
}
