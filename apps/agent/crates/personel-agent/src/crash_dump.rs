//! Crash dump capture + deferred upload.
//!
//! Faz 4 Wave 1 #31 (Personel UAM).
//!
//! Two responsibilities:
//!
//! 1. **Unhandled-exception filter (Windows only)**: registered *before* the
//!    tokio runtime starts so that any panic that unwinds past the runtime, or
//!    any SEH exception from Win32 callbacks in collectors, produces a
//!    MiniDump file on disk. The filter is allocation-free and uses only the
//!    stack + pre-computed static buffers — heap state is undefined during a
//!    crash. We return `EXCEPTION_CONTINUE_SEARCH` so the OS still terminates
//!    the process: we want a real crash, not "crash recovery". The watchdog
//!    (sibling process, not this crate) is responsible for restarting the
//!    service.
//!
//! 2. **Pending dump uploader (async)**: on the next successful startup, any
//!    `.dmp` file in `%PROGRAMDATA%\Personel\agent\dumps\` is hashed,
//!    base64-encoded, wrapped in a small JSON payload and enqueued as a
//!    `Critical` `AgentTamperDetected` event. The enricher will eventually
//!    route crash dumps to the audit-worm bucket; for Phase 1 we piggyback on
//!    the tamper channel because the event kind enum doesn't have a dedicated
//!    variant and adding one would ripple through backend taxonomy + proto
//!    tables. After successful enqueue the file moves to `dumps\uploaded\`
//!    (not deleted — the queue DB could still be wiped in transit). A GC pass
//!    prunes `uploaded\` entries older than 7 days and un-uploaded dumps
//!    older than 30 days.
//!
//! The module is intentionally self-contained. Everything Windows-specific
//! lives behind `#[cfg(target_os = "windows")]`. On non-Windows the install
//! function is a no-op and the uploader still runs (for `--debug` dev on a
//! Linux/macOS host where a synthetic dump could in theory be dropped in).

// SAFETY: this module calls into raw Win32 exception + MiniDump APIs. All
// unsafe blocks are localised and justified inline. Required because the
// crate-level deny(unsafe_code) in main.rs forbids unsafe elsewhere.
#![allow(unsafe_code)]

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result};
use tokio::sync::oneshot;
use tracing::{debug, info, warn};

use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_queue::queue::EventQueue;

// ──────────────────────────────────────────────────────────────────────────────
// Paths
// ──────────────────────────────────────────────────────────────────────────────

/// Returns `%PROGRAMDATA%\Personel\agent\dumps` on Windows, or
/// `/var/lib/personel/agent/dumps` on other OSes (dev only).
fn dump_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        let program_data = std::env::var("ProgramData")
            .unwrap_or_else(|_| r"C:\ProgramData".to_string());
        PathBuf::from(program_data)
            .join("Personel")
            .join("agent")
            .join("dumps")
    }
    #[cfg(not(target_os = "windows"))]
    {
        PathBuf::from("/var/lib/personel/agent/dumps")
    }
}

/// Returns the `uploaded/` subdirectory used to retain already-uploaded dumps
/// (pending GC).
fn uploaded_dir() -> PathBuf {
    dump_dir().join("uploaded")
}

/// Ensures both the dump dir and the uploaded subdir exist. Idempotent.
fn ensure_dirs() -> std::io::Result<()> {
    let d = dump_dir();
    std::fs::create_dir_all(&d)?;
    std::fs::create_dir_all(d.join("uploaded"))?;
    Ok(())
}

// ──────────────────────────────────────────────────────────────────────────────
// Unhandled exception filter (Windows only)
// ──────────────────────────────────────────────────────────────────────────────

/// Installs the process-wide unhandled exception filter.
///
/// Must be called once, before the tokio runtime starts. Calling twice is
/// harmless (the second call just re-registers the same static function with
/// Win32).
///
/// On non-Windows this is a no-op that only ensures the dump dirs exist so
/// the async uploader has something to iterate (empty) on dev hosts.
pub fn install_unhandled_exception_filter() {
    if let Err(e) = ensure_dirs() {
        warn!(error = %e, "crash_dump: could not create dump dirs");
    }

    #[cfg(target_os = "windows")]
    {
        // SAFETY: SetUnhandledExceptionFilter is thread-safe and documented as
        // returning the previous filter. We discard the previous filter — we
        // do not chain to it, because we want EXCEPTION_CONTINUE_SEARCH to
        // hand control to the OS's default handler (which ultimately
        // terminates the process). The function pointer we install has static
        // lifetime and `extern "system"` ABI.
        unsafe {
            use windows::Win32::System::Diagnostics::Debug::{
                SetUnhandledExceptionFilter, LPTOP_LEVEL_EXCEPTION_FILTER,
            };
            let filter: LPTOP_LEVEL_EXCEPTION_FILTER = Some(unhandled_exception_filter);
            let _prev = SetUnhandledExceptionFilter(filter);
        }
        info!("crash_dump: unhandled exception filter installed");
    }
}

/// Windows unhandled exception filter. Allocation-free, signal-safe.
///
/// This function is called by the Win32 structured-exception-handling machinery
/// when an unhandled exception walks off the top of the main thread's SEH
/// chain. The process state is undefined — we MUST NOT allocate, take locks,
/// log via `tracing`, or call into any code that might re-enter the panic
/// handler.
///
/// Strategy:
/// 1. Read `GetCurrentProcessId` and the current wall-clock seconds (via
///    `GetSystemTimeAsFileTime`, allocation-free, no locale).
/// 2. Format a UTF-16 filename into a fixed stack buffer using a tiny
///    hand-rolled integer-to-wchar routine (no std formatters, no itoa).
/// 3. Open the file with `CreateFileW`.
/// 4. Call `MiniDumpWriteDump` with `MiniDumpNormal`.
/// 5. Close the handle.
/// 6. Return `EXCEPTION_CONTINUE_SEARCH` so the OS default handler runs.
#[cfg(target_os = "windows")]
unsafe extern "system" fn unhandled_exception_filter(
    exception_info: *const windows::Win32::System::Diagnostics::Debug::EXCEPTION_POINTERS,
) -> i32 {
    use windows::core::PCWSTR;
    use windows::Win32::Foundation::{CloseHandle, GENERIC_WRITE, HANDLE};
    use windows::Win32::Storage::FileSystem::{
        CreateFileW, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, FILE_SHARE_MODE,
    };
    use windows::Win32::System::Diagnostics::Debug::{
        MiniDumpNormal, MiniDumpWriteDump, EXCEPTION_POINTERS,
        MINIDUMP_EXCEPTION_INFORMATION,
    };
    use windows::Win32::System::SystemInformation::GetSystemTimeAsFileTime;
    use windows::Win32::System::Threading::{
        GetCurrentProcess, GetCurrentProcessId, GetCurrentThreadId,
    };

    // No sharing — exclusive write on the dump file. In windows 0.54 the
    // share mode is a newtype around u32; FILE_SHARE_MODE(0) == none.
    const FILE_SHARE_NONE: FILE_SHARE_MODE = FILE_SHARE_MODE(0);

    // EXCEPTION_CONTINUE_SEARCH — let OS terminate after we capture the dump.
    // We want a real crash so the watchdog sees the process go down and
    // restarts it cleanly; swallowing the exception would leave the process
    // in an undefined state running corrupted memory.
    const EXCEPTION_CONTINUE_SEARCH: i32 = 0;

    // ── Build `%ProgramData%\Personel\agent\dumps\crash_<ts>_<pid>.dmp` ──
    //
    // We do NOT read the ProgramData env var here (that would allocate).
    // Instead the path is hard-coded as wide-literal string bytes. This means
    // if the machine has ProgramData redirected elsewhere the dump lands in
    // the default location — acceptable for a crash path.
    //
    // Layout in `filename_buf`:
    //   prefix ("C:\\ProgramData\\Personel\\agent\\dumps\\crash_")
    //   ts (up to 20 digits)
    //   '_'
    //   pid (up to 10 digits)
    //   suffix (".dmp")
    //   NUL terminator
    let mut filename_buf: [u16; 260] = [0; 260];
    let mut idx: usize = 0;

    // Prefix literal — kept short enough to fit MAX_PATH even with long pid/ts.
    // Use const &[u16] converted by hand. Each char pushed verbatim (ASCII).
    const PREFIX: &[u8] =
        b"C:\\ProgramData\\Personel\\agent\\dumps\\crash_";
    for &b in PREFIX {
        if idx + 1 >= filename_buf.len() {
            break;
        }
        filename_buf[idx] = b as u16;
        idx += 1;
    }

    // Timestamp (unix seconds, derived from Win32 FILETIME — 100ns intervals
    // since 1601-01-01). No allocation.
    let ts_unix_secs: u64 = {
        // SAFETY: GetSystemTimeAsFileTime takes no args and returns by value.
        // Infallible.
        let ft = GetSystemTimeAsFileTime();
        let hundred_ns = ((ft.dwHighDateTime as u64) << 32) | (ft.dwLowDateTime as u64);
        // Epoch delta: (1970-1601) in 100ns = 11644473600 * 10_000_000.
        const EPOCH_DELTA_100NS: u64 = 116_444_736_000_000_000;
        if hundred_ns >= EPOCH_DELTA_100NS {
            (hundred_ns - EPOCH_DELTA_100NS) / 10_000_000
        } else {
            0
        }
    };

    idx = append_u64(&mut filename_buf, idx, ts_unix_secs);
    if idx + 1 < filename_buf.len() {
        filename_buf[idx] = b'_' as u16;
        idx += 1;
    }

    // SAFETY: GetCurrentProcessId has no side effects.
    let pid = GetCurrentProcessId();
    idx = append_u64(&mut filename_buf, idx, pid as u64);

    // Suffix ".dmp"
    for &b in b".dmp" {
        if idx + 1 >= filename_buf.len() {
            break;
        }
        filename_buf[idx] = b as u16;
        idx += 1;
    }
    // NUL terminator — guaranteed because filename_buf was zero-initialised
    // and idx < filename_buf.len() - 1 at this point.
    if idx < filename_buf.len() {
        filename_buf[idx] = 0;
    }

    // SAFETY: filename_buf is a valid NUL-terminated UTF-16 string on the
    // stack. CreateFileW does not retain the pointer beyond the call.
    let file_handle: HANDLE = match CreateFileW(
        PCWSTR(filename_buf.as_ptr()),
        GENERIC_WRITE.0,
        FILE_SHARE_NONE,
        None,
        CREATE_ALWAYS,
        FILE_ATTRIBUTE_NORMAL,
        HANDLE::default(),
    ) {
        Ok(h) => h,
        Err(_) => {
            // Couldn't open dump file — nothing we can do from inside a
            // crashing process. Hand off to OS.
            return EXCEPTION_CONTINUE_SEARCH;
        }
    };

    // SAFETY: GetCurrentProcess returns a pseudo-handle that is always valid.
    let process: HANDLE = GetCurrentProcess();

    // Build the MINIDUMP_EXCEPTION_INFORMATION pointer if we have one.
    // NOTE: MiniDumpWriteDump uses the thread that was running when the SEH
    // fired via the ExceptionPointers; we pass GetCurrentThreadId for
    // ThreadId because docs require a real id (MiniDumpWriteDump reads the
    // context record for that thread).
    let exc_info = MINIDUMP_EXCEPTION_INFORMATION {
        ThreadId: GetCurrentThreadId(),
        // MINIDUMP_EXCEPTION_INFORMATION.ExceptionPointers is typed as *mut
        // for legacy reasons; MiniDumpWriteDump only reads through it so the
        // const-to-mut cast is safe.
        ExceptionPointers: exception_info as *mut EXCEPTION_POINTERS,
        ClientPointers: windows::Win32::Foundation::BOOL(0),
    };

    // SAFETY: all pointers are valid for the duration of the call. `process`
    // and `file_handle` are live handles. `exc_info` lives on this frame. The
    // dump type MiniDumpNormal is safe to pass. The function synchronously
    // writes to the file.
    let _ = MiniDumpWriteDump(
        process,
        pid,
        file_handle,
        MiniDumpNormal,
        Some(&exc_info as *const _),
        None,
        None,
    );

    // SAFETY: file_handle came from CreateFileW; CloseHandle takes ownership.
    let _ = CloseHandle(file_handle);

    EXCEPTION_CONTINUE_SEARCH
}

/// Writes a u64 into a UTF-16 buffer at position `idx` using base-10 ASCII
/// digits. Returns the new index. Stops early if the buffer is about to
/// overflow (reserves 1 slot for the NUL terminator).
///
/// Allocation-free: uses a 20-byte stack scratch area.
#[cfg(target_os = "windows")]
fn append_u64(buf: &mut [u16; 260], mut idx: usize, mut val: u64) -> usize {
    if val == 0 {
        if idx + 1 < buf.len() {
            buf[idx] = b'0' as u16;
            idx += 1;
        }
        return idx;
    }
    let mut scratch: [u8; 20] = [0; 20];
    let mut n = 0usize;
    while val > 0 && n < scratch.len() {
        scratch[n] = (val % 10) as u8 + b'0';
        val /= 10;
        n += 1;
    }
    // scratch is reversed; write in reverse order.
    while n > 0 {
        n -= 1;
        if idx + 1 >= buf.len() {
            break;
        }
        buf[idx] = scratch[n] as u16;
        idx += 1;
    }
    idx
}

// ──────────────────────────────────────────────────────────────────────────────
// Pending dump uploader
// ──────────────────────────────────────────────────────────────────────────────

/// Maximum size of a dump we will read + base64 into the queue. Anything
/// larger is skipped with a warn (the operator can ship it out-of-band).
/// 50 MB raw → ~67 MB base64 — still under NATS default 64 MB max msg if
/// trimmed. The enricher may still reject oversized payloads; this is a
/// best-effort Phase 1 path.
const MAX_DUMP_INLINE_BYTES: u64 = 50 * 1024 * 1024;

/// Age at which an *uploaded* dump file is GC'd.
const UPLOADED_RETENTION: Duration = Duration::from_secs(7 * 24 * 60 * 60);

/// Age at which an un-uploaded dump file is considered abandoned and deleted
/// with a warn.
const ABANDONED_MAX_AGE: Duration = Duration::from_secs(30 * 24 * 60 * 60);

/// Top-level uploader task.
///
/// Spawned once at agent startup after the queue is ready. Runs the upload +
/// GC pass a single time, then waits on `stop_rx` so the task is cleanly
/// cancellable by the main agent loop. It does not keep polling — a new
/// crash is only discovered on the next boot, which is fine: uploading at
/// startup means the queue has freshly opened storage and the fastest route
/// to the gateway.
///
/// Errors during the pass are logged at warn and do not propagate — a crash
/// dump upload that fails must not gate the live agent.
pub async fn run_dump_uploader(
    queue: Arc<EventQueue>,
    stop_rx: oneshot::Receiver<()>,
) {
    // Run the scan off the async runtime's blocking thread pool so we don't
    // stall the reactor on large file reads + base64 encode.
    let queue_for_task = Arc::clone(&queue);
    let handle = tokio::task::spawn_blocking(move || {
        if let Err(e) = upload_pending_dumps(&queue_for_task) {
            warn!(error = %e, "crash_dump: upload pass failed");
        }
        if let Err(e) = gc_uploaded_dir() {
            warn!(error = %e, "crash_dump: gc pass failed");
        }
        if let Err(e) = gc_abandoned_dumps() {
            warn!(error = %e, "crash_dump: abandoned-dump gc failed");
        }
    });

    tokio::select! {
        _ = handle => {
            debug!("crash_dump: uploader pass complete");
        }
        _ = stop_rx => {
            info!("crash_dump: uploader cancelled on shutdown");
        }
    }
}

/// Scans the dump dir and enqueues every `.dmp` file it finds. On success
/// moves the file into `uploaded/`. Caller is responsible for running this
/// on a blocking thread.
fn upload_pending_dumps(queue: &EventQueue) -> Result<()> {
    let dir = dump_dir();
    ensure_dirs().context("ensure dump dirs")?;

    let read_dir = match std::fs::read_dir(&dir) {
        Ok(r) => r,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(anyhow::anyhow!("read_dir {}: {}", dir.display(), e)),
    };

    let mut found = 0usize;
    let mut uploaded = 0usize;
    let mut skipped = 0usize;

    for entry in read_dir.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let ext_matches = path
            .extension()
            .and_then(|e| e.to_str())
            .map(|e| e.eq_ignore_ascii_case("dmp"))
            .unwrap_or(false);
        if !ext_matches {
            continue;
        }
        found += 1;

        match upload_single_dump(queue, &path) {
            Ok(UploadOutcome::Uploaded) => uploaded += 1,
            Ok(UploadOutcome::Skipped(reason)) => {
                warn!(path = %path.display(), reason, "crash_dump: skipped");
                skipped += 1;
            }
            Err(e) => {
                warn!(path = %path.display(), error = %e, "crash_dump: upload failed");
            }
        }
    }

    if found > 0 {
        info!(found, uploaded, skipped, "crash_dump: pass complete");
    }
    Ok(())
}

enum UploadOutcome {
    Uploaded,
    Skipped(&'static str),
}

/// Reads, hashes, base64-encodes, and enqueues a single crash dump. On
/// success moves the file to `uploaded/`. Never deletes the source on
/// failure.
fn upload_single_dump(queue: &EventQueue, path: &Path) -> Result<UploadOutcome> {
    let metadata = std::fs::metadata(path).context("stat dump file")?;
    let size = metadata.len();
    if size == 0 {
        return Ok(UploadOutcome::Skipped("zero-byte dump"));
    }
    if size > MAX_DUMP_INLINE_BYTES {
        return Ok(UploadOutcome::Skipped("dump exceeds inline cap"));
    }

    let bytes = std::fs::read(path).context("read dump bytes")?;

    // SHA-256
    let sha_hex = {
        use sha2::{Digest, Sha256};
        let mut h = Sha256::new();
        h.update(&bytes);
        hex::encode(h.finalize())
    };

    // base64
    let dump_b64 = {
        use base64::engine::general_purpose::STANDARD;
        use base64::Engine as _;
        STANDARD.encode(&bytes)
    };

    // Parse captured_at from filename `crash_<ts>_<pid>.dmp` best-effort.
    let (captured_at_unix_secs, _parsed_pid) = parse_dump_filename(path);

    let filename = path
        .file_name()
        .and_then(|s| s.to_str())
        .unwrap_or("")
        .to_string();

    // JSON payload wire format. The enricher route adds the tenant/endpoint
    // envelope — we just need the crash-specific fields.
    let payload = serde_json::json!({
        "kind": "agent.crash_dump",
        "filename": filename,
        "size_bytes": size,
        "sha256": sha_hex,
        "captured_at_unix_secs": captured_at_unix_secs,
        "dump_bytes_base64": dump_b64,
        "notes": "Faz 4 #31 — inline crash dump via tamper channel (Phase 1)",
    });
    let payload_bytes = serde_json::to_vec(&payload).context("serialize crash payload")?;

    // Clock for occurred_at / enqueued_at. We use SystemTime here instead of
    // pulling in personel-core's SystemClock because this runs on a blocking
    // thread with no ctx handle — and the filename timestamp already carries
    // the true "moment of crash".
    let now_nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| i64::try_from(d.as_nanos()).unwrap_or(i64::MAX))
        .unwrap_or(0);

    let event_id = EventId::new_v7().to_bytes();

    queue
        .enqueue(
            &event_id,
            EventKind::AgentTamperDetected.as_str(),
            Priority::Critical,
            now_nanos,
            now_nanos,
            &payload_bytes,
        )
        .context("enqueue crash dump event")?;

    // On successful enqueue, MOVE to uploaded/ — don't delete, the queue may
    // still lose the row in transit.
    let target_dir = uploaded_dir();
    std::fs::create_dir_all(&target_dir).context("create uploaded dir")?;
    let target = target_dir.join(path.file_name().unwrap_or_default());
    // Use rename; fall back to copy+delete on cross-volume failures.
    if let Err(rename_err) = std::fs::rename(path, &target) {
        debug!(error = %rename_err, "rename failed; falling back to copy+delete");
        std::fs::copy(path, &target).context("copy dump to uploaded/")?;
        std::fs::remove_file(path).context("remove source dump after copy")?;
    }

    Ok(UploadOutcome::Uploaded)
}

/// Parses `crash_<unix_secs>_<pid>.dmp` → `(unix_secs, pid)`. Both fields are
/// best-effort; failures return `(0, 0)`.
fn parse_dump_filename(path: &Path) -> (u64, u64) {
    let stem = path
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("");
    // Expected: "crash_<ts>_<pid>"
    let rest = stem.strip_prefix("crash_").unwrap_or("");
    let mut parts = rest.split('_');
    let ts = parts
        .next()
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(0);
    let pid = parts
        .next()
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(0);
    (ts, pid)
}

/// Deletes files in `uploaded/` older than `UPLOADED_RETENTION`.
fn gc_uploaded_dir() -> Result<()> {
    let dir = uploaded_dir();
    let read_dir = match std::fs::read_dir(&dir) {
        Ok(r) => r,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(anyhow::anyhow!("read_dir {}: {}", dir.display(), e)),
    };
    let now = SystemTime::now();
    let mut removed = 0usize;
    for entry in read_dir.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let meta = match entry.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };
        let modified = match meta.modified() {
            Ok(t) => t,
            Err(_) => continue,
        };
        let age = now.duration_since(modified).unwrap_or(Duration::ZERO);
        if age > UPLOADED_RETENTION {
            if std::fs::remove_file(&path).is_ok() {
                removed += 1;
            }
        }
    }
    if removed > 0 {
        info!(removed, "crash_dump: gc'd uploaded/ entries");
    }
    Ok(())
}

/// Deletes un-uploaded `.dmp` files in the main dump dir older than
/// `ABANDONED_MAX_AGE`. This runs AFTER the upload pass, so anything left is
/// genuinely stuck (enqueue failed repeatedly or the file is corrupt).
fn gc_abandoned_dumps() -> Result<()> {
    let dir = dump_dir();
    let read_dir = match std::fs::read_dir(&dir) {
        Ok(r) => r,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(anyhow::anyhow!("read_dir {}: {}", dir.display(), e)),
    };
    let now = SystemTime::now();
    let mut removed = 0usize;
    for entry in read_dir.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let ext_matches = path
            .extension()
            .and_then(|e| e.to_str())
            .map(|e| e.eq_ignore_ascii_case("dmp"))
            .unwrap_or(false);
        if !ext_matches {
            continue;
        }
        let meta = match entry.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };
        let modified = match meta.modified() {
            Ok(t) => t,
            Err(_) => continue,
        };
        let age = now.duration_since(modified).unwrap_or(Duration::ZERO);
        if age > ABANDONED_MAX_AGE {
            warn!(
                path = %path.display(),
                age_days = age.as_secs() / 86400,
                "crash_dump: abandoning stuck dump (> 30 days, enqueue never succeeded)"
            );
            let _ = std::fs::remove_file(&path);
            removed += 1;
        }
    }
    if removed > 0 {
        warn!(removed, "crash_dump: abandoned stuck dumps");
    }
    Ok(())
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_dump_filename_happy() {
        let p = PathBuf::from("crash_1728000000_4242.dmp");
        let (ts, pid) = parse_dump_filename(&p);
        assert_eq!(ts, 1_728_000_000);
        assert_eq!(pid, 4242);
    }

    #[test]
    fn parse_dump_filename_malformed() {
        let p = PathBuf::from("something_else.dmp");
        let (ts, pid) = parse_dump_filename(&p);
        assert_eq!(ts, 0);
        assert_eq!(pid, 0);
    }

    #[test]
    fn dump_dir_not_empty_path() {
        let d = dump_dir();
        assert!(d.components().count() > 1);
        assert!(d.ends_with("dumps"));
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn append_u64_zero() {
        let mut buf: [u16; 260] = [0; 260];
        let idx = append_u64(&mut buf, 0, 0);
        assert_eq!(idx, 1);
        assert_eq!(buf[0], b'0' as u16);
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn append_u64_large() {
        let mut buf: [u16; 260] = [0; 260];
        let idx = append_u64(&mut buf, 0, 1_728_000_000);
        let s: String = buf[..idx].iter().map(|&c| c as u8 as char).collect();
        assert_eq!(s, "1728000000");
    }

    /// Exercises the real Win32 MiniDumpWriteDump call against the current
    /// process — NOT inside an exception context, so we pass null for the
    /// exception info. This verifies our Win32 bindings compile and the
    /// feature flag coverage is right. The generated file is deleted at the
    /// end of the test so CI runners stay clean.
    #[cfg(target_os = "windows")]
    #[test]
    fn minidump_write_dump_current_process() {
        use windows::core::PCWSTR;
        use windows::Win32::Foundation::{CloseHandle, GENERIC_WRITE, HANDLE};
        use windows::Win32::Storage::FileSystem::{
            CreateFileW, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, FILE_SHARE_MODE,
        };
        use windows::Win32::System::Diagnostics::Debug::{
            MiniDumpNormal, MiniDumpWriteDump,
        };
        use windows::Win32::System::Threading::{GetCurrentProcess, GetCurrentProcessId};

        let tmp = std::env::temp_dir().join("personel-test-minidump.dmp");
        // Build wide path + NUL.
        let mut w: Vec<u16> = tmp.to_string_lossy().encode_utf16().collect();
        w.push(0);

        unsafe {
            let handle: HANDLE = CreateFileW(
                PCWSTR(w.as_ptr()),
                GENERIC_WRITE.0,
                FILE_SHARE_MODE(0),
                None,
                CREATE_ALWAYS,
                FILE_ATTRIBUTE_NORMAL,
                HANDLE::default(),
            )
            .expect("CreateFileW");
            let process = GetCurrentProcess();
            let pid = GetCurrentProcessId();
            MiniDumpWriteDump(
                process,
                pid,
                handle,
                MiniDumpNormal,
                None,
                None,
                None,
            )
            .expect("MiniDumpWriteDump");
            let _ = CloseHandle(handle);
        }

        let meta = std::fs::metadata(&tmp).expect("dump file must exist");
        assert!(meta.len() > 0, "dump file must be non-empty");
        let _ = std::fs::remove_file(&tmp);
    }
}
