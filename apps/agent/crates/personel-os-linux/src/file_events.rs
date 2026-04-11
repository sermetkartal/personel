//! Filesystem event collector using `fanotify`.
//!
//! # Overview
//!
//! `fanotify` is a Linux kernel subsystem that delivers filesystem events to a
//! watching process. Unlike `inotify`, `fanotify` can be set to mark entire
//! mount points (`FAN_MARK_MOUNT`) rather than individual inodes, making it
//! suitable for broad filesystem telemetry without managing per-directory watch
//! descriptors.
//!
//! Events collected in Phase 2 scope:
//! * `FAN_OPEN` — file opened
//! * `FAN_CLOSE_WRITE` — file closed after write
//! * `FAN_MODIFY` — file modified
//! * `FAN_MOVE_SELF` — file/directory moved
//! * `FAN_OPEN_EXEC` — binary executed (very useful for process provenance)
//!
//! # CAPABILITY REQUIREMENT — `CAP_SYS_ADMIN`
//!
//! **`fanotify` with `FAN_MARK_MOUNT` requires `CAP_SYS_ADMIN`.**
//!
//! This is a Linux kernel restriction: mounting-point marks affect all
//! processes on the system, so the kernel gates the call behind the most
//! powerful non-root capability. The Personel agent is **not** run as root;
//! instead, the systemd unit grants a narrowly-scoped ambient capability:
//!
//! ```ini
//! # /etc/systemd/system/personel-agent.service.d/capabilities.conf
//! [Service]
//! AmbientCapabilities=CAP_SYS_ADMIN
//! CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_ADMIN
//! NoNewPrivileges=yes
//! ```
//!
//! Use [`check_fanotify_capability`] at startup to verify the capability is
//! present and emit a human-friendly error with setup instructions if not.
//!
//! # Fallback: `inotify`
//!
//! If `CAP_SYS_ADMIN` cannot be granted (hardened hosts, custom SELinux
//! policy), the collector degrades to `inotify`-based monitoring. `inotify` is
//! inode-scoped (no full-mount coverage), has kernel watch-descriptor limits
//! (`/proc/sys/fs/inotify/max_user_watches`), and misses filesystems mounted
//! after watch creation. The admin console marks endpoints operating in this
//! mode as **degraded**. See ADR 0016 §fanotify.
//!
//! # Phase 2.2 implementation plan
//!
//! 1. Open fanotify fd: `fanotify_init(FAN_CLASS_NOTIF | FAN_NONBLOCK, O_RDONLY)`
//! 2. Mark all mounts: `fanotify_mark(fd, FAN_MARK_ADD | FAN_MARK_MOUNT, events, AT_FDCWD, "/")`
//! 3. Run async poll loop (tokio `AsyncFd<OwnedFd>`) reading `fanotify_event_metadata`
//! 4. Resolve pid/exe from `event.pid` + `/proc/{pid}/exe` symlink
//! 5. Emit `FileEvent` structs to the collector channel

use personel_core::error::{AgentError, Result};

/// A filesystem event delivered by fanotify.
#[derive(Debug, Clone)]
pub struct FileEvent {
    /// Absolute path of the file affected, if resolvable from the fanotify fd.
    pub path: String,
    /// PID of the process that triggered the event.
    pub pid: u32,
    /// Bitmask of [`EventKind`] flags.
    pub kind: EventKind,
}

/// Event kind bitmask values.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EventKind {
    /// File was opened (`FAN_OPEN`).
    Open,
    /// File was closed after a write (`FAN_CLOSE_WRITE`).
    CloseWrite,
    /// File content was modified (`FAN_MODIFY`).
    Modify,
    /// File or directory was moved (`FAN_MOVE_SELF`).
    MoveSelf,
    /// A binary was executed (`FAN_OPEN_EXEC`).
    OpenExec,
}

/// Result of the `CAP_SYS_ADMIN` capability check.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CapabilityStatus {
    /// `CAP_SYS_ADMIN` is present in the effective capability set. `fanotify`
    /// mount-mark mode is available.
    Present,
    /// `CAP_SYS_ADMIN` is absent. The agent will fall back to `inotify` mode.
    /// See [`check_fanotify_capability`] for the human-friendly error message.
    Absent,
}

/// Checks whether the current process holds `CAP_SYS_ADMIN`.
///
/// Call this during collector initialisation. If it returns [`CapabilityStatus::Absent`]
/// log the returned error message and start the inotify fallback path.
///
/// # Degraded mode notice
///
/// When `CAP_SYS_ADMIN` is absent the file-event collector operates in
/// **degraded mode** (inotify). The admin console endpoint capability report
/// will include `{ "file_events_mode": "inotify_degraded" }` for this endpoint.
///
/// # Setup instructions (for administrators)
///
/// Grant the capability to the systemd unit with a drop-in:
///
/// ```sh
/// mkdir -p /etc/systemd/system/personel-agent.service.d/
/// cat > /etc/systemd/system/personel-agent.service.d/capabilities.conf <<'EOF'
/// [Service]
/// AmbientCapabilities=CAP_SYS_ADMIN
/// CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_ADMIN
/// EOF
/// systemctl daemon-reload
/// systemctl restart personel-agent
/// ```
///
/// On SELinux-enforcing systems also ensure the SELinux policy module
/// (`personel-agent.te`) has been loaded. See `docs/adr/0016` §Consequences.
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// In Phase 2.2 returns [`AgentError::Io`] only if the `capget(2)` syscall
/// fails unexpectedly (e.g. seccomp filter blocking it).
pub fn check_fanotify_capability() -> Result<CapabilityStatus> {
    // Phase 2.1 scaffold: Phase 2.2 will use nix::sys::prctl or read
    // /proc/self/status CapEff and test bit 21 (CAP_SYS_ADMIN).
    Err(AgentError::Unsupported {
        os: "linux",
        component: "file_events::check_fanotify_capability",
    })
}

/// Starts the fanotify filesystem event collector.
///
/// Marks all accessible mount points with `FAN_MARK_MOUNT` and streams
/// [`FileEvent`] values into `sender`. Automatically falls back to inotify
/// if [`check_fanotify_capability`] returns [`CapabilityStatus::Absent`].
///
/// # Errors
///
/// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
/// Phase 2.2: [`AgentError::CollectorStart`] if neither fanotify nor inotify
/// can be initialised.
pub fn start_collector(
    _sender: tokio::sync::mpsc::Sender<FileEvent>,
) -> Result<tokio::task::JoinHandle<()>> {
    Err(AgentError::Unsupported {
        os: "linux",
        component: "file_events::start_collector",
    })
}
