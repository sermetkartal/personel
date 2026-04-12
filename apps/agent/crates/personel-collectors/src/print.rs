//! Print job metadata collector.
//!
//! Uses `FindFirstPrinterChangeNotification` with `PRINTER_CHANGE_ADD_JOB` to
//! receive notifications when new print jobs are submitted. On each notification,
//! calls `EnumJobs` to read job metadata (document name, printer name, pages,
//! size, user) and emits `print.job_submitted`.
//!
//! No document content is captured — metadata only, per MVP scope.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full spooler notification implementation.
//! Non-Windows: parks gracefully so `cargo check` passes on macOS/Linux.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Print job metadata collector.
#[derive(Default)]
pub struct PrintCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl PrintCollector {
    /// Creates a new [`PrintCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for PrintCollector {
    fn name(&self) -> &'static str {
        "print"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["print.job_submitted"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

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
    windows::run(ctx, healthy, events, drops, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        info!("print: FindFirstPrinterChangeNotification not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows {
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::Arc;

    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};

    use windows::Win32::Foundation::{CloseHandle, HANDLE};
    use windows::Win32::Graphics::Printing::{
        ClosePrinter, EnumJobsW, FindClosePrinterChangeNotification,
        FindFirstPrinterChangeNotification, FindNextPrinterChangeNotification,
        OpenPrinterW, JOB_INFO_1W, PRINTER_CHANGE_ADD_JOB, PRINTER_DEFAULTS,
    };
    use windows::core::PWSTR;

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use crate::CollectorCtx;

    /// Wait up to 1 second for a printer change notification, then re-check stop.
    const WAIT_TIMEOUT_MS: u32 = 1_000;

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<std::sync::atomic::AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("print: starting (FindFirstPrinterChangeNotification)");

        // Open the default printer (NULL = default).
        // SAFETY: OpenPrinterW with null name and null defaults opens the default printer.
        let hprinter = unsafe {
            let mut hp = HANDLE::default();
            let ok = OpenPrinterW(
                PWSTR::null(),
                &mut hp,
                Some(std::ptr::null()),
            );
            if !ok.as_bool() || hp.is_invalid() {
                None
            } else {
                Some(hp)
            }
        };

        let hprinter = match hprinter {
            Some(h) => h,
            None => {
                // No default printer configured — this is normal in many environments.
                info!("print: no default printer found — collector will park");
                healthy.store(true, Ordering::Relaxed);
                let _ = stop_rx.blocking_recv();
                return;
            }
        };

        // Register for new-job notifications.
        // SAFETY: hprinter is a valid handle; PRINTER_CHANGE_ADD_JOB is a documented flag.
        let hchange = unsafe {
            FindFirstPrinterChangeNotification(hprinter, PRINTER_CHANGE_ADD_JOB, 0, None)
        };

        if hchange.is_invalid() {
            error!("print: FindFirstPrinterChangeNotification failed");
            healthy.store(false, Ordering::Relaxed);
            unsafe { let _ = ClosePrinter(hprinter); }
            let _ = stop_rx.blocking_recv();
            return;
        }

        healthy.store(true, Ordering::Relaxed);
        info!("print: registered for PRINTER_CHANGE_ADD_JOB notifications");

        // Poll loop: wait for notification with timeout so we can check stop.
        loop {
            if stop_rx.try_recv().is_ok() {
                break;
            }

            // SAFETY: WaitForSingleObject on a valid change notification handle.
            let wait_result = unsafe {
                windows::Win32::System::Threading::WaitForSingleObject(
                    HANDLE(hchange.0),
                    WAIT_TIMEOUT_MS,
                )
            };

            use windows::Win32::System::Threading::{WAIT_OBJECT_0, WAIT_TIMEOUT};

            match wait_result {
                WAIT_OBJECT_0 => {
                    // Drain job notifications.
                    let mut pdwchange: u32 = 0;
                    // SAFETY: FindNextPrinterChangeNotification with a valid handle.
                    let ok = unsafe {
                        FindNextPrinterChangeNotification(hchange, Some(&mut pdwchange), None, None)
                            .as_bool()
                    };
                    if ok && (pdwchange & PRINTER_CHANGE_ADD_JOB != 0) {
                        emit_jobs(&ctx, hprinter, &events, &drops);
                    }
                }
                WAIT_TIMEOUT => {
                    // Normal: check stop signal on next iteration.
                }
                _ => {
                    warn!("print: WaitForSingleObject returned unexpected value");
                    healthy.store(false, Ordering::Relaxed);
                    break;
                }
            }
        }

        // SAFETY: both handles are valid and must be closed.
        unsafe {
            let _ = FindClosePrinterChangeNotification(hchange);
            let _ = ClosePrinter(hprinter);
        }

        info!("print: stopped");
    }

    /// Reads current print jobs and emits `print.job_submitted` events.
    ///
    /// # Safety
    ///
    /// `hprinter` must be a valid printer handle.
    fn emit_jobs(
        ctx: &CollectorCtx,
        hprinter: HANDLE,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        // Query the buffer size needed for JOB_INFO_1W.
        let mut bytes_needed: u32 = 0;
        let mut jobs_returned: u32 = 0;

        // SAFETY: First call with null buffer to get required size.
        let _ = unsafe {
            EnumJobsW(
                hprinter,
                0,
                255,
                1,
                None,
                0,
                &mut bytes_needed,
                &mut jobs_returned,
            )
        };

        if bytes_needed == 0 {
            return;
        }

        let mut buf: Vec<u8> = vec![0u8; bytes_needed as usize];

        // SAFETY: Second call with properly sized buffer.
        let ok = unsafe {
            EnumJobsW(
                hprinter,
                0,
                255,
                1,
                Some(buf.as_mut_ptr()),
                bytes_needed,
                &mut bytes_needed,
                &mut jobs_returned,
            )
            .as_bool()
        };

        if !ok || jobs_returned == 0 {
            return;
        }

        // SAFETY: buf contains `jobs_returned` JOB_INFO_1W structs at its start.
        let jobs = unsafe {
            std::slice::from_raw_parts(
                buf.as_ptr() as *const JOB_INFO_1W,
                jobs_returned as usize,
            )
        };

        for job in jobs {
            // SAFETY: Wide strings in JOB_INFO_1W are null-terminated.
            let doc_name = unsafe { pwstr_to_string(job.pDocument) };
            let printer_name = unsafe { pwstr_to_string(job.pPrinterName) };
            let user_name = unsafe { pwstr_to_string(job.pUserName) };

            debug!(
                job_id = job.JobId,
                doc = %doc_name,
                printer = %printer_name,
                pages = job.TotalPages,
                "print job"
            );

            let payload = format!(
                r#"{{"job_id":{},"document_name":{:?},"printer_name":{:?},"user_name":{:?},"total_pages":{},"size_bytes":{}}}"#,
                job.JobId,
                doc_name,
                printer_name,
                user_name,
                job.TotalPages,
                job.Size,
            );

            let now = ctx.clock.now_unix_nanos();
            let id = EventId::new_v7().to_bytes();
            match ctx.queue.enqueue(
                &id,
                EventKind::PrintJobSubmitted.as_str(),
                Priority::Normal,
                now,
                now,
                payload.as_bytes(),
            ) {
                Ok(_) => {
                    events.fetch_add(1, Ordering::Relaxed);
                }
                Err(e) => {
                    warn!(error = %e, "print: queue error");
                    drops.fetch_add(1, Ordering::Relaxed);
                }
            }
        }
    }

    /// Converts a nullable `PWSTR` to an owned `String`.
    ///
    /// # Safety
    ///
    /// `ptr` must be null or point to a null-terminated UTF-16 string.
    unsafe fn pwstr_to_string(ptr: PWSTR) -> String {
        if ptr.is_null() {
            return String::new();
        }
        let mut len = 0usize;
        while *ptr.0.add(len) != 0 {
            len += 1;
        }
        let slice = std::slice::from_raw_parts(ptr.0, len);
        String::from_utf16_lossy(slice)
    }
}
