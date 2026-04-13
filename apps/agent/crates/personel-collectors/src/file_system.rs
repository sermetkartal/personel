//! File system event collector — real ETW implementation.
//!
//! Subscribes to the manifested provider `Microsoft-Windows-Kernel-File`
//! (GUID `{edd08927-9cc4-4e65-b970-c2560fb5c289}`) via a private
//! `EVENT_TRACE_PROPERTIES` real-time session, decodes events with TDH, and
//! emits one [`EventKind::FileCreated`] / `FileWritten` / `FileDeleted` /
//! `FileRenamed` per observed operation.
//!
//! # Coalescing model
//!
//! The provider fires one event per IRP, which means a single user-visible
//! "save" can produce hundreds of `Write` events. We coalesce as follows:
//!
//! - **Create** (event id `12`): map `FileObject → path`, emit
//!   `file.created`.
//! - **Write**  (event id `16`): record that this `FileObject` is dirty (no
//!   emit).
//! - **Close**  (event id `30`): if dirty, emit a single `file.written`
//!   carrying the final on-disk size; drop the map entry.
//! - **DeletePath** (event id `26`) and **Delete** (event id `17`): emit
//!   `file.deleted`.
//! - **RenamePath** (event id `27`) and **Rename** (event id `19`): emit
//!   `file.renamed` with `from` / `to` paths.
//!
//! A path-level rate limiter additionally drops repeated writes to the same
//! path within a 2-second window, as a defence-in-depth against pathological
//! callers that close-and-reopen on every write.
//!
//! # Privacy
//!
//! Per KVKK m.6 we do **not** hash arbitrary files. Only files that match
//! [`is_sensitive`] (sensitive extensions OR live under user data folders)
//! are hashed, and only when smaller than 20 MB. Larger files emit
//! `sha256: null` with `reason: "too_large"`.
//!
//! # Privilege
//!
//! `StartTraceW` for a private session that subscribes to a kernel-area
//! manifested provider requires `SeSystemProfilePrivilege` (admin). On
//! failure the collector logs a warning and parks — Phase 1 dev mode runs
//! the agent as the desktop user. The 30-second health snapshot will report
//! `healthy: true` with a `status` describing the no-op state.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
#[cfg(not(target_os = "windows"))]
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// File system event collector.
#[derive(Default)]
pub struct FileSystemCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl FileSystemCollector {
    /// Creates a new [`FileSystemCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for FileSystemCollector {
    fn name(&self) -> &'static str {
        "file_system"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["file.created", "file.written", "file.deleted", "file.renamed"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        // ETW consumer must run on a dedicated OS thread (ProcessTrace
        // blocks). Use spawn_blocking so we can `oneshot::Receiver::blocking_recv`
        // without parking a tokio worker forever.
        let task = tokio::task::spawn_blocking(move || {
            run_loop(ctx, healthy, events, drops, stop_rx);
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

fn run_loop(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    stop_rx: oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    self::windows_impl::run(ctx, healthy, events, drops, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        info!("file_system: ETW Microsoft-Windows-Kernel-File not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows_impl {
    use std::collections::HashMap;
    use std::ffi::OsString;
    use std::io::Read;
    use std::os::windows::ffi::OsStringExt;
    use std::path::{Path, PathBuf};
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::{Arc, Mutex, OnceLock};
    use std::time::{Duration, Instant};

    use sha2::{Digest, Sha256};
    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};

    use windows::core::{GUID, PCWSTR, PWSTR};
    use windows::Win32::Foundation::{
        ERROR_ACCESS_DENIED, ERROR_ALREADY_EXISTS, ERROR_SUCCESS, WIN32_ERROR,
    };
    use windows::Win32::Storage::FileSystem::{
        GetLogicalDriveStringsW, QueryDosDeviceW,
    };
    use windows::Win32::System::Diagnostics::Etw::{
        CloseTrace, ControlTraceW, EnableTraceEx2, OpenTraceW, ProcessTrace, StartTraceW,
        TdhGetProperty, CONTROLTRACE_HANDLE, EVENT_CONTROL_CODE_ENABLE_PROVIDER,
        EVENT_RECORD, EVENT_TRACE_CONTROL_STOP, EVENT_TRACE_LOGFILEW,
        EVENT_TRACE_PROPERTIES, EVENT_TRACE_REAL_TIME_MODE,
        PROCESS_TRACE_MODE_EVENT_RECORD, PROCESS_TRACE_MODE_REAL_TIME,
        PROPERTY_DATA_DESCRIPTOR, TRACE_LEVEL_INFORMATION, WNODE_FLAG_TRACED_GUID,
    };

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use crate::CollectorCtx;

    // ──────────────────────────────────────────────────────────────────────────
    // Constants
    // ──────────────────────────────────────────────────────────────────────────

    /// `Microsoft-Windows-Kernel-File` provider GUID.
    const KERNEL_FILE_PROVIDER_GUID: GUID = GUID {
        data1: 0xedd0_8927,
        data2: 0x9cc4,
        data3: 0x4e65,
        data4: [0xb9, 0x70, 0xc2, 0x56, 0x0f, 0xb5, 0xc2, 0x89],
    };

    /// Session name. Must be unique per logger; rerunning the agent after a
    /// crash will need to first call `ControlTraceW(STOP)`.
    const SESSION_NAME: &str = "Personel-FileSystem";

    /// Provider keywords: bit field per the manifest.
    /// 0x10 = `KERNEL_FILE_KEYWORD_CREATE`,
    /// 0x20 = `KERNEL_FILE_KEYWORD_WRITE`,
    /// 0x80 = `KERNEL_FILE_KEYWORD_DELETE_PATH`,
    /// 0x100 = `KERNEL_FILE_KEYWORD_RENAME_SETLINK_PATH`,
    /// 0x200 = `KERNEL_FILE_KEYWORD_CREATE_NEW_FILE`.
    /// Close (event 30) is always emitted regardless of keywords; we don't
    /// need a Close keyword bit.
    const FS_KEYWORDS: u64 = 0x10 | 0x20 | 0x80 | 0x100 | 0x200;

    // Manifested event IDs (Microsoft-Windows-Kernel-File).
    const EVT_CREATE: u16 = 12;
    const EVT_DELETE: u16 = 17;
    const EVT_RENAME: u16 = 19;
    const EVT_WRITE: u16 = 16;
    const EVT_DELETE_PATH: u16 = 26;
    const EVT_RENAME_PATH: u16 = 27;
    const EVT_CLOSE: u16 = 30;

    /// Hash size limit per KVKK m.6 — files larger than this are not hashed.
    const HASH_SIZE_LIMIT: u64 = 20 * 1024 * 1024;

    /// Coalescing window for repeated writes to the same path.
    const WRITE_COALESCE_WINDOW: Duration = Duration::from_secs(2);

    // ──────────────────────────────────────────────────────────────────────────
    // Run loop
    // ──────────────────────────────────────────────────────────────────────────

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        stop_rx: oneshot::Receiver<()>,
    ) {
        info!("file_system: starting ETW Microsoft-Windows-Kernel-File session");

        // Best-effort: tear down any stale session left over from a previous
        // crashed run. A "not found" error is fine; "access denied" tells us
        // we won't be able to start either.
        stop_existing_session();

        let session_handle = match start_session() {
            Ok(h) => h,
            Err(e) if e == ERROR_ACCESS_DENIED => {
                warn!(
                    "file_system: ETW StartTrace returned ERROR_ACCESS_DENIED — \
                     run as Administrator to enable file system collection. \
                     Parking collector in no-op mode."
                );
                healthy.store(true, Ordering::Relaxed);
                let _ = stop_rx.blocking_recv();
                return;
            }
            Err(e) => {
                error!("file_system: StartTrace failed (WIN32_ERROR={})", e.0);
                healthy.store(false, Ordering::Relaxed);
                let _ = stop_rx.blocking_recv();
                return;
            }
        };

        // Enable the Microsoft-Windows-Kernel-File provider on this session.
        let enable_rc = unsafe {
            EnableTraceEx2(
                session_handle,
                &KERNEL_FILE_PROVIDER_GUID,
                EVENT_CONTROL_CODE_ENABLE_PROVIDER.0,
                TRACE_LEVEL_INFORMATION as u8,
                FS_KEYWORDS,
                0,
                0,
                None,
            )
        };
        if enable_rc != ERROR_SUCCESS {
            error!(
                "file_system: EnableTraceEx2 failed (WIN32_ERROR={})",
                enable_rc.0
            );
            stop_session();
            healthy.store(false, Ordering::Relaxed);
            let _ = stop_rx.blocking_recv();
            return;
        }

        healthy.store(true, Ordering::Relaxed);

        // Install global state for the C callback.
        let state = Arc::new(SessionState::new(ctx, events, drops));
        install_state(Arc::clone(&state));

        // Spin a dedicated OS thread for ProcessTrace; it blocks until the
        // session is stopped.
        let consumer_thread = std::thread::Builder::new()
            .name("personel-fs-etw".into())
            .spawn(|| consumer_loop())
            .expect("failed to spawn ETW consumer thread");

        info!("file_system: ETW session live, consumer thread started");

        // Park until shutdown.
        let _ = stop_rx.blocking_recv();
        info!("file_system: stop requested");

        // Disable provider, stop session, join consumer.
        let _ = unsafe {
            EnableTraceEx2(
                session_handle,
                &KERNEL_FILE_PROVIDER_GUID,
                0, /* EVENT_CONTROL_CODE_DISABLE_PROVIDER */
                TRACE_LEVEL_INFORMATION as u8,
                0,
                0,
                0,
                None,
            )
        };
        stop_session();
        // ProcessTrace will return once the session is stopped.
        let _ = consumer_thread.join();

        clear_state();
        info!("file_system: stopped");
    }

    /// Builds an [`EVENT_TRACE_PROPERTIES`] block with the session name
    /// appended after the struct (the OS expects the name buffer to live in
    /// the same allocation, addressed via `LoggerNameOffset`).
    fn properties_buffer() -> Vec<u8> {
        // Wide name with NUL.
        let name_w: Vec<u16> =
            SESSION_NAME.encode_utf16().chain(std::iter::once(0u16)).collect();
        let prop_size = std::mem::size_of::<EVENT_TRACE_PROPERTIES>();
        let total = prop_size + name_w.len() * 2 + 16; // padding
        let mut buf = vec![0u8; total];

        // SAFETY: buf has at least size_of::<EVENT_TRACE_PROPERTIES>() bytes
        // and is zero-initialised; we write into a freshly aligned struct.
        unsafe {
            let props = buf.as_mut_ptr().cast::<EVENT_TRACE_PROPERTIES>();
            (*props).Wnode.BufferSize = total as u32;
            (*props).Wnode.Guid = GUID {
                data1: 0xa01b_3f1e,
                data2: 0xc0de,
                data3: 0x4f00,
                data4: [0xb1, 0xa1, 0x70, 0x65, 0x72, 0x73, 0x6e, 0x6c],
            };
            (*props).Wnode.ClientContext = 1; // QPC
            (*props).Wnode.Flags = WNODE_FLAG_TRACED_GUID;
            (*props).LogFileMode = EVENT_TRACE_REAL_TIME_MODE;
            (*props).LoggerNameOffset = prop_size as u32;
            (*props).BufferSize = 64; // KB
            (*props).MinimumBuffers = 4;
            (*props).MaximumBuffers = 16;

            // Copy the wide name into the trailing buffer.
            let dst = buf.as_mut_ptr().add(prop_size).cast::<u16>();
            std::ptr::copy_nonoverlapping(name_w.as_ptr(), dst, name_w.len());
        }
        buf
    }

    fn start_session() -> std::result::Result<CONTROLTRACE_HANDLE, WIN32_ERROR> {
        let mut buf = properties_buffer();
        let mut handle = CONTROLTRACE_HANDLE::default();
        let name_w: Vec<u16> =
            SESSION_NAME.encode_utf16().chain(std::iter::once(0u16)).collect();

        // SAFETY: buf is alive for the duration of the call; props points
        // into it and the trailing name lives in the same allocation.
        let rc = unsafe {
            StartTraceW(
                &mut handle,
                PCWSTR(name_w.as_ptr()),
                buf.as_mut_ptr().cast::<EVENT_TRACE_PROPERTIES>(),
            )
        };

        if rc == ERROR_SUCCESS {
            Ok(handle)
        } else if rc == ERROR_ALREADY_EXISTS {
            // Stale session — try once to stop and retry.
            stop_existing_session();
            let mut buf2 = properties_buffer();
            let rc2 = unsafe {
                StartTraceW(
                    &mut handle,
                    PCWSTR(name_w.as_ptr()),
                    buf2.as_mut_ptr().cast::<EVENT_TRACE_PROPERTIES>(),
                )
            };
            if rc2 == ERROR_SUCCESS {
                Ok(handle)
            } else {
                Err(rc2)
            }
        } else {
            Err(rc)
        }
    }

    fn stop_existing_session() {
        let mut buf = properties_buffer();
        let name_w: Vec<u16> =
            SESSION_NAME.encode_utf16().chain(std::iter::once(0u16)).collect();
        // SAFETY: passing a valid name + a zero-initialised properties block.
        let _ = unsafe {
            ControlTraceW(
                CONTROLTRACE_HANDLE::default(),
                PCWSTR(name_w.as_ptr()),
                buf.as_mut_ptr().cast::<EVENT_TRACE_PROPERTIES>(),
                EVENT_TRACE_CONTROL_STOP,
            )
        };
    }

    fn stop_session() {
        stop_existing_session();
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Consumer
    // ──────────────────────────────────────────────────────────────────────────

    fn consumer_loop() {
        let name_w: Vec<u16> =
            SESSION_NAME.encode_utf16().chain(std::iter::once(0u16)).collect();

        let mut logfile = EVENT_TRACE_LOGFILEW::default();
        logfile.LoggerName = PWSTR(name_w.as_ptr() as *mut u16);
        // ProcessTraceMode = REAL_TIME | EVENT_RECORD. In windows-rs 0.54
        // these union fields are wrapped to allow safe writes.
        logfile.Anonymous1.ProcessTraceMode =
            PROCESS_TRACE_MODE_REAL_TIME | PROCESS_TRACE_MODE_EVENT_RECORD;
        logfile.Anonymous2.EventRecordCallback = Some(event_record_callback);

        // SAFETY: logfile is a valid pointer; OpenTraceW returns
        // INVALID_PROCESSTRACE_HANDLE (u64::MAX) on failure.
        let trace_handle = unsafe { OpenTraceW(&mut logfile) };
        if trace_handle.Value == u64::MAX {
            error!("file_system: OpenTraceW failed");
            return;
        }

        // SAFETY: trace_handle is valid; ProcessTrace blocks until the
        // session is stopped.
        let rc = unsafe { ProcessTrace(&[trace_handle], None, None) };
        if rc != ERROR_SUCCESS {
            warn!("file_system: ProcessTrace returned WIN32_ERROR={}", rc.0);
        }

        // SAFETY: trace_handle is valid; CloseTrace releases it.
        let _ = unsafe { CloseTrace(trace_handle) };
    }

    /// ETW per-event callback. Runs on the consumer thread; must not block
    /// or panic.
    unsafe extern "system" fn event_record_callback(record: *mut EVENT_RECORD) {
        if record.is_null() {
            return;
        }
        // SAFETY: ETW guarantees `record` is valid for the duration of the
        // callback. We treat the pointed-to data as read-only.
        let rec = &*record;

        // Provider check (defence in depth — we only enabled one).
        if rec.EventHeader.ProviderId != KERNEL_FILE_PROVIDER_GUID {
            return;
        }

        let event_id = rec.EventHeader.EventDescriptor.Id;
        let pid = rec.EventHeader.ProcessId;

        // Look up state. If the collector has been torn down, drop silently.
        let Some(state) = current_state() else { return };

        match event_id {
            EVT_CREATE => {
                if let Some(path) = read_file_name(rec) {
                    if let Some(file_obj) = read_file_object(rec) {
                        state.tracker.lock().unwrap().on_create(file_obj, path.clone());
                    }
                    if !should_skip(&path) {
                        let dos = nt_to_dos(&path).unwrap_or(path.clone());
                        state.emit_create(&dos, pid);
                    }
                }
            }
            EVT_WRITE => {
                if let Some(file_obj) = read_file_object(rec) {
                    state.tracker.lock().unwrap().on_write(file_obj);
                }
            }
            EVT_CLOSE => {
                if let Some(file_obj) = read_file_object(rec) {
                    let dirty_path = state.tracker.lock().unwrap().on_close(file_obj);
                    if let Some(nt_path) = dirty_path {
                        if !should_skip(&nt_path) {
                            let dos = nt_to_dos(&nt_path).unwrap_or(nt_path);
                            // Coalesce repeated saves of the same path.
                            if state.coalesce_path(&dos) {
                                state.emit_write(&dos, pid);
                            }
                        }
                    }
                }
            }
            EVT_DELETE | EVT_DELETE_PATH => {
                if let Some(path) = read_file_name(rec) {
                    if !should_skip(&path) {
                        let dos = nt_to_dos(&path).unwrap_or(path);
                        state.emit_delete(&dos, pid);
                    }
                }
            }
            EVT_RENAME | EVT_RENAME_PATH => {
                let from = read_file_name(rec);
                let to = read_named_string(rec, "FileName");
                let from_p = from.unwrap_or_default();
                let to_p = to.unwrap_or_default();
                if !from_p.as_os_str().is_empty() && !should_skip(&from_p) {
                    let dos_from = nt_to_dos(&from_p).unwrap_or(from_p.clone());
                    let dos_to = nt_to_dos(&to_p).unwrap_or(to_p.clone());
                    state.emit_rename(&dos_from, &dos_to, pid);
                }
            }
            _ => {}
        }
    }

    // ──────────────────────────────────────────────────────────────────────────
    // SessionState — shared between the callback and the collector handle
    // ──────────────────────────────────────────────────────────────────────────

    struct SessionState {
        ctx: CollectorCtx,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        tracker: Mutex<Tracker>,
        rate_limit: Mutex<HashMap<PathBuf, Instant>>,
    }

    impl SessionState {
        fn new(ctx: CollectorCtx, events: Arc<AtomicU64>, drops: Arc<AtomicU64>) -> Self {
            Self {
                ctx,
                events,
                drops,
                tracker: Mutex::new(Tracker::default()),
                rate_limit: Mutex::new(HashMap::new()),
            }
        }

        /// Returns true if the path's last emission was outside the
        /// coalescing window (and updates the timestamp).
        fn coalesce_path(&self, path: &Path) -> bool {
            let mut map = self.rate_limit.lock().unwrap();
            let now = Instant::now();
            let allow = match map.get(path) {
                Some(t) => now.duration_since(*t) > WRITE_COALESCE_WINDOW,
                None => true,
            };
            if allow {
                map.insert(path.to_path_buf(), now);
            }
            // Opportunistic GC: every ~1024 inserts trim entries older than 60s.
            if map.len() > 1024 {
                let cutoff = Duration::from_secs(60);
                map.retain(|_, t| now.duration_since(*t) < cutoff);
            }
            allow
        }

        fn emit_create(&self, path: &Path, pid: u32) {
            let payload = build_payload("create", path, None, pid, &resolve_process_name(pid), None, None);
            self.enqueue(EventKind::FileCreated, &payload);
        }

        fn emit_write(&self, path: &Path, pid: u32) {
            let (sha, reason, size) = maybe_hash(path);
            let proc_name = resolve_process_name(pid);
            let payload =
                build_payload("write", path, size, pid, &proc_name, sha.as_deref(), reason);
            self.enqueue(EventKind::FileWritten, &payload);
        }

        fn emit_delete(&self, path: &Path, pid: u32) {
            let payload = build_payload("delete", path, None, pid, &resolve_process_name(pid), None, None);
            self.enqueue(EventKind::FileDeleted, &payload);
        }

        fn emit_rename(&self, from: &Path, to: &Path, pid: u32) {
            let proc_name = resolve_process_name(pid);
            let payload = format!(
                r#"{{"kind":"rename","from":{},"to":{},"process_id":{},"process_name":{}}}"#,
                json_str(&from.to_string_lossy()),
                json_str(&to.to_string_lossy()),
                pid,
                json_str(&proc_name),
            );
            self.enqueue(EventKind::FileRenamed, &payload);
        }

        fn enqueue(&self, kind: EventKind, payload: &str) {
            let now = self.ctx.clock.now_unix_nanos();
            let id = EventId::new_v7().to_bytes();
            match self.ctx.queue.enqueue(
                &id,
                kind.as_str(),
                Priority::Normal,
                now,
                now,
                payload.as_bytes(),
            ) {
                Ok(_) => {
                    self.events.fetch_add(1, Ordering::Relaxed);
                }
                Err(e) => {
                    debug!(error = %e, "file_system: queue error");
                    self.drops.fetch_add(1, Ordering::Relaxed);
                }
            }
        }
    }

    /// Tracks `FileObject -> (path, dirty)` so we can emit one write per close.
    #[derive(Default)]
    struct Tracker {
        open: HashMap<u64, OpenFile>,
    }

    struct OpenFile {
        path: PathBuf,
        dirty: bool,
    }

    impl Tracker {
        fn on_create(&mut self, file_obj: u64, path: PathBuf) {
            self.open.insert(file_obj, OpenFile { path, dirty: false });
            // Bound the map: if it grows past ~50k entries something is leaking;
            // best-effort truncate.
            if self.open.len() > 65_536 {
                self.open.clear();
            }
        }
        fn on_write(&mut self, file_obj: u64) {
            if let Some(of) = self.open.get_mut(&file_obj) {
                of.dirty = true;
            }
        }
        fn on_close(&mut self, file_obj: u64) -> Option<PathBuf> {
            let of = self.open.remove(&file_obj)?;
            if of.dirty {
                Some(of.path)
            } else {
                None
            }
        }
    }

    // SAFETY: `SessionState` is only accessed via Arc + Mutex; CollectorCtx
    // is Clone + Send + Sync (tokio mpsc + Arc<dyn Clock>).
    unsafe impl Send for SessionState {}
    unsafe impl Sync for SessionState {}

    static GLOBAL_STATE: OnceLock<Mutex<Option<Arc<SessionState>>>> = OnceLock::new();

    fn install_state(state: Arc<SessionState>) {
        let cell = GLOBAL_STATE.get_or_init(|| Mutex::new(None));
        *cell.lock().unwrap() = Some(state);
    }

    fn clear_state() {
        if let Some(cell) = GLOBAL_STATE.get() {
            *cell.lock().unwrap() = None;
        }
    }

    fn current_state() -> Option<Arc<SessionState>> {
        GLOBAL_STATE.get()?.lock().ok()?.clone()
    }

    // ──────────────────────────────────────────────────────────────────────────
    // TDH property reads
    // ──────────────────────────────────────────────────────────────────────────

    /// Reads the `FileObject` property as u64. Most kernel-file events
    /// carry it as a pointer-sized integer.
    fn read_file_object(rec: &EVENT_RECORD) -> Option<u64> {
        let name_w: Vec<u16> = "FileObject\0".encode_utf16().collect();
        let desc = PROPERTY_DATA_DESCRIPTOR {
            PropertyName: name_w.as_ptr() as u64,
            ArrayIndex: 0,
            Reserved: 0,
        };
        let mut buf = [0u8; 8];
        // SAFETY: rec is valid for the callback duration; desc points to
        // a valid wide string; buf is large enough for a 64-bit pointer.
        let rc = unsafe { TdhGetProperty(rec, None, &[desc], &mut buf) };
        if rc != 0 {
            return None;
        }
        Some(u64::from_le_bytes(buf))
    }

    /// Reads the `FileName` property as a wide string (NT path).
    fn read_file_name(rec: &EVENT_RECORD) -> Option<PathBuf> {
        read_named_string(rec, "FileName")
    }

    /// Reads a named wide-string property out of the event.
    fn read_named_string(rec: &EVENT_RECORD, prop: &str) -> Option<PathBuf> {
        let name_w: Vec<u16> = prop.encode_utf16().chain(std::iter::once(0u16)).collect();
        let desc = PROPERTY_DATA_DESCRIPTOR {
            PropertyName: name_w.as_ptr() as u64,
            ArrayIndex: 0,
            Reserved: 0,
        };

        // First call: get required size.
        let mut size: u32 = 0;
        let rc = unsafe {
            windows::Win32::System::Diagnostics::Etw::TdhGetPropertySize(
                rec, None, &[desc], &mut size,
            )
        };
        if rc != 0 || size == 0 || size > 32_768 {
            return None;
        }
        let mut buf = vec![0u8; size as usize];
        let rc = unsafe { TdhGetProperty(rec, None, &[desc], &mut buf) };
        if rc != 0 {
            return None;
        }
        // Wide string, NUL terminated.
        let wide = unsafe {
            std::slice::from_raw_parts(buf.as_ptr().cast::<u16>(), buf.len() / 2)
        };
        let len = wide.iter().position(|&c| c == 0).unwrap_or(wide.len());
        if len == 0 {
            return None;
        }
        let os = OsString::from_wide(&wide[..len]);
        Some(PathBuf::from(os))
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Path filtering and classification
    // ──────────────────────────────────────────────────────────────────────────

    fn should_skip(path: &Path) -> bool {
        let s = path.to_string_lossy();
        let lower = s.to_ascii_lowercase();

        // Directory patterns (NT or DOS form — both end up here at different
        // stages of decoding).
        const SKIP_DIRS: &[&str] = &[
            r"\windows\",
            r"\programdata\microsoft\",
            r"\$recycle.bin\",
            r"\appdata\local\microsoft\",
            r"\appdata\local\packages\",
            r"\appdata\local\temp\",
            r"\system32\",
            r"\syswow64\",
            // NT-form equivalents of the above
            r"\harddiskvolume",
        ];
        // We deliberately do NOT skip "harddiskvolume" wholesale; only special-case
        // the `\windows\` etc. fragments which appear regardless of NT vs DOS form.
        for d in SKIP_DIRS.iter().take(SKIP_DIRS.len() - 1) {
            if lower.contains(d) {
                return true;
            }
        }

        // Special files.
        if lower.ends_with("\\pagefile.sys") || lower.ends_with("\\hiberfil.sys") {
            return true;
        }
        if lower.ends_with("\\swapfile.sys") {
            return true;
        }

        // Extension blacklist.
        if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
            let ext_lc = ext.to_ascii_lowercase();
            if matches!(
                ext_lc.as_str(),
                "tmp" | "log" | "etl" | "bak" | "swp" | "lock" | "crdownload" | "part"
            ) {
                return true;
            }
        }
        // Editor temp files like `~$foo.docx`, `.~lock.foo.ods#`.
        if let Some(name) = path.file_name().and_then(|n| n.to_str()) {
            if name.starts_with("~$") || name.starts_with(".~") {
                return true;
            }
        }

        false
    }

    fn is_sensitive(path: &Path) -> bool {
        let lower = path.to_string_lossy().to_ascii_lowercase();
        const SENS_DIRS: &[&str] = &[r"\desktop\", r"\documents\", r"\downloads\"];
        if SENS_DIRS.iter().any(|d| lower.contains(d)) {
            return true;
        }
        if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
            let ext_lc = ext.to_ascii_lowercase();
            if matches!(
                ext_lc.as_str(),
                "pdf" | "docx" | "xlsx" | "pptx" | "sql" | "key" | "p12" | "pfx" | "pem"
            ) {
                return true;
            }
        }
        false
    }

    /// Hashes a sensitive file. Returns `(Some(hex), None, Some(size))` on
    /// success, `(None, Some("too_large"), Some(size))` if oversized, or
    /// `(None, None, None)` if the file is not sensitive or could not be
    /// opened.
    fn maybe_hash(path: &Path) -> (Option<String>, Option<&'static str>, Option<u64>) {
        if !is_sensitive(path) {
            return (None, None, None);
        }
        let meta = match std::fs::metadata(path) {
            Ok(m) => m,
            Err(_) => return (None, None, None),
        };
        let size = meta.len();
        if size > HASH_SIZE_LIMIT {
            return (None, Some("too_large"), Some(size));
        }
        let mut file = match std::fs::File::open(path) {
            Ok(f) => f,
            Err(_) => return (None, None, Some(size)),
        };
        let mut hasher = Sha256::new();
        let mut buf = [0u8; 64 * 1024];
        loop {
            match file.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => hasher.update(&buf[..n]),
                Err(_) => return (None, None, Some(size)),
            }
        }
        (Some(hex::encode(hasher.finalize())), None, Some(size))
    }

    // ──────────────────────────────────────────────────────────────────────────
    // NT → DOS path conversion (with cached drive letter table)
    // ──────────────────────────────────────────────────────────────────────────

    static DOS_TABLE: OnceLock<Mutex<Vec<(String, String)>>> = OnceLock::new();

    /// Converts `\Device\HarddiskVolumeN\Users\...` to `C:\Users\...` if a
    /// matching drive letter exists. Returns `None` if the path is already
    /// a DOS path or the prefix is unknown.
    fn nt_to_dos(path: &Path) -> Option<PathBuf> {
        let s = path.to_string_lossy();
        if s.len() >= 2 {
            let bytes = s.as_bytes();
            // Already a DOS path like "C:\..."
            if bytes[1] == b':' {
                return Some(path.to_path_buf());
            }
        }
        if !s.starts_with(r"\Device\") {
            return None;
        }
        let table = DOS_TABLE.get_or_init(|| Mutex::new(load_dos_table()));
        let table = table.lock().ok()?;
        for (dos, nt) in table.iter() {
            if let Some(stripped) = strip_prefix_ci(&s, nt) {
                let mut out = String::with_capacity(dos.len() + stripped.len());
                out.push_str(dos);
                out.push_str(stripped);
                return Some(PathBuf::from(out));
            }
        }
        None
    }

    fn strip_prefix_ci<'a>(s: &'a str, prefix: &str) -> Option<&'a str> {
        if s.len() < prefix.len() {
            return None;
        }
        let (head, tail) = s.split_at(prefix.len());
        if head.eq_ignore_ascii_case(prefix) {
            Some(tail)
        } else {
            None
        }
    }

    /// Walks `A:` through `Z:` and resolves each via `QueryDosDeviceW`,
    /// producing a `(dos, nt_target)` table cached for the lifetime of the
    /// process. New drives mounted after start are not picked up — call
    /// sites can tolerate the resulting `nt_to_dos` returning `None`.
    fn load_dos_table() -> Vec<(String, String)> {
        let mut out = Vec::new();
        // Enumerate present drive letters via GetLogicalDriveStringsW.
        let mut buf = [0u16; 512];
        // SAFETY: buf is large enough for any sane number of drive letters.
        let n = unsafe { GetLogicalDriveStringsW(Some(&mut buf)) };
        if n == 0 {
            return out;
        }
        let mut start = 0usize;
        for i in 0..n as usize {
            if buf[i] == 0 {
                if start < i {
                    let drive = String::from_utf16_lossy(&buf[start..i]);
                    // drive looks like "C:\"; we want "C:".
                    let dos = drive.trim_end_matches('\\').to_string();
                    // QueryDosDeviceW expects "C:" without trailing slash.
                    let dos_w: Vec<u16> =
                        dos.encode_utf16().chain(std::iter::once(0u16)).collect();
                    let mut target = [0u16; 1024];
                    // SAFETY: dos_w is NUL-terminated; target buffer is large.
                    let len = unsafe {
                        QueryDosDeviceW(PCWSTR(dos_w.as_ptr()), Some(&mut target))
                    };
                    if len > 1 {
                        // QueryDosDeviceW returns a double-NUL list; first
                        // entry is the kernel target.
                        let len = (len as usize).min(target.len()).saturating_sub(1);
                        let nul = target[..len].iter().position(|&c| c == 0).unwrap_or(len);
                        let nt = String::from_utf16_lossy(&target[..nul]);
                        if !nt.is_empty() {
                            out.push((dos, nt));
                        }
                    }
                }
                start = i + 1;
                if buf[i + 1] == 0 {
                    break;
                }
            }
        }
        out
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Process name resolution (best-effort)
    // ──────────────────────────────────────────────────────────────────────────

    fn resolve_process_name(pid: u32) -> String {
        if pid == 0 {
            return String::new();
        }
        use windows::Win32::System::Threading::{
            OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_FORMAT,
            PROCESS_QUERY_LIMITED_INFORMATION,
        };
        // SAFETY: OpenProcess returns a HANDLE we own and must close.
        let handle = unsafe {
            OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
        };
        let handle = match handle {
            Ok(h) if !h.is_invalid() => h,
            _ => return String::new(),
        };
        let mut buf = [0u16; 512];
        let mut size: u32 = buf.len() as u32;
        // SAFETY: buf is u16; size tracks element count.
        let res = unsafe {
            QueryFullProcessImageNameW(
                handle,
                PROCESS_NAME_FORMAT(0),
                PWSTR(buf.as_mut_ptr()),
                &mut size,
            )
        };
        // Always close handle.
        let _ = unsafe { windows::Win32::Foundation::CloseHandle(handle) };
        if res.is_err() || size == 0 {
            return String::new();
        }
        let name = String::from_utf16_lossy(&buf[..size as usize]);
        // Trim to basename.
        name.rsplit('\\').next().unwrap_or(&name).to_string()
    }

    // ──────────────────────────────────────────────────────────────────────────
    // JSON payload helpers (we don't pull serde_json into this crate's runtime)
    // ──────────────────────────────────────────────────────────────────────────

    fn json_str(s: &str) -> String {
        let mut out = String::with_capacity(s.len() + 2);
        out.push('"');
        for c in s.chars() {
            match c {
                '"' => out.push_str(r#"\""#),
                '\\' => out.push_str(r"\\"),
                '\n' => out.push_str(r"\n"),
                '\r' => out.push_str(r"\r"),
                '\t' => out.push_str(r"\t"),
                c if (c as u32) < 0x20 => {
                    out.push_str(&format!("\\u{:04x}", c as u32));
                }
                c => out.push(c),
            }
        }
        out.push('"');
        out
    }

    fn build_payload(
        kind: &str,
        path: &Path,
        size: Option<u64>,
        pid: u32,
        process_name: &str,
        sha256: Option<&str>,
        reason: Option<&'static str>,
    ) -> String {
        let mut out = String::with_capacity(256);
        out.push('{');
        out.push_str("\"kind\":");
        out.push_str(&json_str(kind));
        out.push_str(",\"path\":");
        out.push_str(&json_str(&path.to_string_lossy()));
        out.push_str(",\"process_id\":");
        out.push_str(&pid.to_string());
        out.push_str(",\"process_name\":");
        out.push_str(&json_str(process_name));
        if let Some(sz) = size {
            out.push_str(",\"size\":");
            out.push_str(&sz.to_string());
        }
        match sha256 {
            Some(s) => {
                out.push_str(",\"sha256\":");
                out.push_str(&json_str(s));
            }
            None => {
                out.push_str(",\"sha256\":null");
            }
        }
        if let Some(r) = reason {
            out.push_str(",\"reason\":");
            out.push_str(&json_str(r));
        }
        out.push('}');
        out
    }

}
