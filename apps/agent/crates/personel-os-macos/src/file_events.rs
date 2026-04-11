//! File system event abstraction — FSEvents + Endpoint Security Framework.
//!
//! # macOS implementation plan (Phase 2.3/2.4)
//!
//! ADR 0015 specifies a dual-source design:
//!
//! - **FSEvents** (`CoreServices.framework FSEvents API`): high-volume, coalesced
//!   events for user directories (`~/Documents`, `~/Downloads`, `~/Desktop`).
//!   Cheap because the kernel FSEvents daemon coalesces writes into batches.
//!   Does NOT see `/private` or system volumes reliably.
//!
//! - **Endpoint Security Framework** (via the Swift ES helper process in
//!   `es_bridge`): system-wide coverage for privileged paths. Delivers
//!   `ES_EVENT_TYPE_NOTIFY_OPEN`, `NOTIFY_CLOSE`, `NOTIFY_WRITE`,
//!   `NOTIFY_RENAME`, `NOTIFY_UNLINK`, `NOTIFY_CREATE`.
//!
//! De-duplication of events from both sources is by `(inode, timestamp_ms,
//! event_type)` in the caller (personel-collectors).
//!
//! ## FSEvents implementation sketch (Phase 2.3)
//!
//! ```text
//! FSEventStreamCreate(
//!     allocator,
//!     callback_fn,          // fn(*const c_void, FSEventStreamRef, usize,
//!                           //     *mut *const c_char, *const FSEventStreamEventFlags,
//!                           //     *const FSEventStreamEventId) -> ()
//!     &context,
//!     paths_to_watch,       // CFArray of CFString
//!     kFSEventStreamEventIdSinceNow,
//!     latency_seconds = 0.5,
//!     kFSEventStreamCreateFlagFileEvents | kFSEventStreamCreateFlagNoDefer,
//! )
//! ```
//!
//! # TCC permissions required
//!
//! - **Full Disk Access** for system-path FSEvents coverage (MDM pre-grantable).
//! - **Endpoint Security** client entitlement (Apple-approved; Phase 2 kickoff).
//!
//! # Phase 2.1 status
//!
//! All types are declared; all operations return `Err(AgentError::Unsupported)`.

use personel_core::error::{AgentError, Result};

/// A file system event from either FSEvents or the ES bridge.
#[derive(Debug, Clone)]
pub struct FileEvent {
    /// Absolute path of the affected file or directory.
    pub path: String,
    /// Kind of event.
    pub kind: FileEventKind,
    /// Milliseconds since Unix epoch when the event was observed.
    pub timestamp_ms: u64,
    /// Inode number of the affected file (used for dedup).
    pub inode: u64,
    /// Process ID that triggered the event, if known.
    pub pid: Option<u32>,
}

/// The kind of file system event.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FileEventKind {
    /// A new file or directory was created.
    Create,
    /// An existing file was opened for reading.
    Open,
    /// An existing file was written to or closed after writing.
    Write,
    /// A file or directory was renamed or moved.
    Rename,
    /// A file or directory was deleted.
    Delete,
    /// A file was mounted (volume mount point).
    Mount,
    /// A file was unmounted.
    Unmount,
    /// An unrecognised or coalesced event type.
    Other,
}

/// A handle to an active FSEvents stream for a set of watched paths.
///
/// Phase 2.3 will wrap `FSEventStreamRef` behind this type. In Phase 2.1
/// construction always returns `Err(AgentError::Unsupported)`.
pub struct FsEventsStream {
    _private: (),
}

impl FsEventsStream {
    /// Start watching `paths` for file events.
    ///
    /// The `callback` receives a batch of events each time FSEvents coalesces
    /// them (up to `latency_secs` delay).
    ///
    /// # Errors
    ///
    /// - [`AgentError::Unsupported`] in Phase 2.1.
    /// - [`AgentError::CollectorStart`] in Phase 2.3+ if `FSEventStreamCreate`
    ///   or `FSEventStreamStart` fails, or if the paths vector is empty.
    pub fn start<F>(_paths: &[&str], _latency_secs: f64, _callback: F) -> Result<Self>
    where
        F: Fn(Vec<FileEvent>) + Send + 'static,
    {
        #[cfg(target_os = "macos")]
        {
            // Phase 2.3: call FSEventStreamCreate, schedule on a background
            // CFRunLoop thread, call FSEventStreamStart.
            //
            // SAFETY note for future implementor: `FSEventStreamRef` is a
            // CoreFoundation opaque pointer. Ownership is transferred to the
            // caller on creation; release with `FSEventStreamInvalidate` +
            // `FSEventStreamRelease` in Drop. The callback pointer must remain
            // valid for the lifetime of the stream; store it in a Box and pin
            // the address in FSEventStreamContext.info.
            Err(AgentError::Unsupported {
                os: "macos",
                component: "file_events::FsEventsStream::start",
            })
        }

        #[cfg(not(target_os = "macos"))]
        {
            crate::stub::file_events::FsEventsStream::start(_paths, _latency_secs, _callback)
        }
    }
}

impl Drop for FsEventsStream {
    fn drop(&mut self) {
        // Phase 2.3: FSEventStreamInvalidate(stream); FSEventStreamRelease(stream);
    }
}
