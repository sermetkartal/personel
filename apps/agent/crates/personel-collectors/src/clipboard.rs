//! Clipboard monitor collector.
//!
//! Listens for clipboard changes via `AddClipboardFormatListener` on a
//! hidden message-only window (`HWND_MESSAGE`). On each `WM_CLIPBOARDUPDATE`:
//!
//! 1. Always emits `clipboard.metadata` (format list, char count, PII class).
//! 2. If `policy.collectors.clipboard_content && ctx.pe_dek.is_some()`,
//!    reads `CF_UNICODETEXT`, encrypts it with AES-256-GCM using the PE-DEK
//!    (ADR 0013 pattern), and emits `clipboard.content_encrypted`.
//!
//! The Win32 message loop runs on a dedicated OS thread (required by
//! `AddClipboardFormatListener`). Events are bridged to the async runtime
//! via an `std::sync::mpsc` channel.
//!
//! # ADR 0013 clipboard content gate
//!
//! Both conditions must be true for content to be emitted:
//! - `policy.collectors.clipboard_content == true`
//! - `ctx.pe_dek.is_some()` (PE-DEK provisioned via DLP ceremony)
//!
//! Clear-text clipboard content is **never** written to the queue.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full `AddClipboardFormatListener` implementation.
//! Non-Windows: parks gracefully so `cargo check` passes on macOS/Linux.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Clipboard monitor collector.
#[derive(Default)]
pub struct ClipboardCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl ClipboardCollector {
    /// Creates a new [`ClipboardCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ClipboardCollector {
    fn name(&self) -> &'static str {
        "clipboard"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["clipboard.metadata", "clipboard.content_encrypted"]
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
        info!("clipboard: AddClipboardFormatListener not supported on this platform — parking");
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
    use zeroize::Zeroizing;

    use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
    use windows::Win32::System::DataExchange::{
        AddClipboardFormatListener, CloseClipboard, GetClipboardData,
        OpenClipboard, RemoveClipboardFormatListener,
    };
    use windows::Win32::System::Memory::{GlobalLock, GlobalSize, GlobalUnlock};
    use windows::Win32::UI::WindowsAndMessaging::{
        CreateWindowExW, DefWindowProcW, DestroyWindow, DispatchMessageW,
        GetMessageW, PostMessageW, RegisterClassExW, HWND_MESSAGE, MSG,
        WM_APP, WM_CLIPBOARDUPDATE, WM_DESTROY, WNDCLASSEXW, WS_EX_NOACTIVATE,
    };
    use windows::core::PCWSTR;

    use personel_core::error::AgentError;
    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;
    use personel_crypto::envelope;

    use crate::CollectorCtx;

    // WM_APP + 1 used to signal the message loop to quit.
    const WM_PERSONEL_QUIT: u32 = WM_APP + 1;

    // CF_UNICODETEXT = 13
    const CF_UNICODETEXT: u32 = 13;

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<std::sync::atomic::AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("clipboard: starting (AddClipboardFormatListener)");

        // Encode class name as UTF-16 null-terminated.
        let class_name_w: Vec<u16> = "PersonelClipboardListener\0"
            .encode_utf16()
            .collect();
        let window_name_w: Vec<u16> = "PersonelClipboard\0".encode_utf16().collect();

        // SAFETY: We register a window class and create a message-only window.
        // All Win32 preconditions are met: class name is a null-terminated UTF-16
        // string, the window proc is a valid extern-system function, and the
        // HWND_MESSAGE parent is the documented value for message-only windows.
        let hwnd = unsafe {
            let wc = WNDCLASSEXW {
                cbSize: std::mem::size_of::<WNDCLASSEXW>() as u32,
                lpfnWndProc: Some(clipboard_wnd_proc),
                lpszClassName: PCWSTR(class_name_w.as_ptr()),
                ..Default::default()
            };
            RegisterClassExW(&wc);

            CreateWindowExW(
                WS_EX_NOACTIVATE,
                PCWSTR(class_name_w.as_ptr()),
                PCWSTR(window_name_w.as_ptr()),
                Default::default(),
                0,
                0,
                0,
                0,
                HWND_MESSAGE,
                None,
                None,
                None,
            )
        };

        let hwnd = match hwnd {
            Ok(h) if h.0 != 0 => h,
            _ => {
                error!("clipboard: CreateWindowExW failed");
                healthy.store(false, Ordering::Relaxed);
                let _ = stop_rx.blocking_recv();
                return;
            }
        };

        // Register for clipboard notifications.
        // SAFETY: hwnd is a valid message-only window created above.
        let registered = unsafe { AddClipboardFormatListener(hwnd).as_bool() };
        if !registered {
            error!("clipboard: AddClipboardFormatListener failed");
            healthy.store(false, Ordering::Relaxed);
            unsafe { let _ = DestroyWindow(hwnd); }
            let _ = stop_rx.blocking_recv();
            return;
        }

        healthy.store(true, Ordering::Relaxed);
        info!("clipboard: registered for WM_CLIPBOARDUPDATE");

        // Spawn a thread to watch for the shutdown signal and post WM_PERSONEL_QUIT.
        let hwnd_raw = hwnd.0 as isize;
        std::thread::spawn(move || {
            let _ = stop_rx.blocking_recv();
            // SAFETY: posting a user-defined message to a valid window handle.
            unsafe {
                let _ = PostMessageW(HWND(hwnd_raw), WM_PERSONEL_QUIT, WPARAM(0), LPARAM(0));
            }
        });

        // Message pump.
        // SAFETY: standard Win32 GetMessage / DispatchMessage loop.
        unsafe {
            let mut msg = MSG::default();
            loop {
                let ret = GetMessageW(&mut msg, hwnd, 0, 0);
                if !ret.as_bool() || msg.message == WM_PERSONEL_QUIT {
                    break;
                }
                if msg.message == WM_CLIPBOARDUPDATE {
                    on_clipboard_update(&ctx, &healthy, &events, &drops);
                }
                let _ = TranslateAndDispatch(&msg);
            }
            let _ = RemoveClipboardFormatListener(hwnd);
            let _ = DestroyWindow(hwnd);
        }

        info!("clipboard: stopped");
    }

    /// Handles a `WM_CLIPBOARDUPDATE` notification.
    ///
    /// # Safety
    ///
    /// Called from the Win32 message pump; all clipboard APIs are called on
    /// the same thread that owns the clipboard listener window.
    fn on_clipboard_update(
        ctx: &CollectorCtx,
        healthy: &Arc<std::sync::atomic::AtomicBool>,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        healthy.store(true, Ordering::Relaxed);

        let policy = ctx.policy();
        let content_enabled =
            policy.collectors.clipboard_content && ctx.pe_dek.is_some();

        // Read clipboard text length for metadata.
        // SAFETY: OpenClipboard/GetClipboardData/CloseClipboard is standard usage.
        let (char_count, has_text) = unsafe { read_clipboard_char_count() };

        debug!(char_count, has_text, content_enabled, "clipboard update");

        // Always emit metadata.
        let meta_payload = format!(
            r#"{{"has_text":{},"char_count":{},"content_encrypted":{}}}"#,
            has_text,
            char_count,
            content_enabled && has_text,
        );
        enqueue(
            ctx,
            EventKind::ClipboardMetadata,
            Priority::Normal,
            &meta_payload,
            events,
            drops,
        );

        // ADR 0013: only emit encrypted content when both gates are open.
        if content_enabled && has_text && char_count > 0 {
            if let Some(dek) = &ctx.pe_dek {
                // SAFETY: we re-open clipboard to read CF_UNICODETEXT.
                let plaintext_opt = unsafe { read_clipboard_text() };
                if let Some(plaintext) = plaintext_opt {
                    let ep = ctx.endpoint_id.to_bytes();
                    let aad = envelope::build_keystroke_aad(&ep, 0);
                    match envelope::encrypt(dek, aad, plaintext.as_slice()) {
                        Ok(env) => {
                            // Plaintext Zeroizing<Vec<u8>> is dropped here.
                            drop(plaintext);
                            let content_payload = format!(
                                r#"{{"char_count":{},"nonce":"{}","ciphertext":"{}","aad":"{}"}}"#,
                                char_count,
                                hex::encode(env.nonce),
                                hex::encode(&env.ciphertext),
                                hex::encode(&env.aad),
                            );
                            enqueue(
                                ctx,
                                EventKind::ClipboardContentEncrypted,
                                Priority::High,
                                &content_payload,
                                events,
                                drops,
                            );
                        }
                        Err(e) => {
                            drop(plaintext);
                            error!(error = %e, "clipboard: encryption failed — content dropped");
                            drops.fetch_add(1, Ordering::Relaxed);
                        }
                    }
                }
            }
        }
    }

    /// Returns `(char_count, has_text)` for the current clipboard contents.
    ///
    /// # Safety
    ///
    /// Caller must be on the message-pump thread. Opens and closes the clipboard.
    unsafe fn read_clipboard_char_count() -> (usize, bool) {
        if !OpenClipboard(None).as_bool() {
            return (0, false);
        }
        let hdata = GetClipboardData(CF_UNICODETEXT);
        let result = match hdata {
            Ok(h) if h.0 != 0 => {
                let size = GlobalSize(windows::Win32::Foundation::HANDLE(h.0));
                // Size is in bytes; UTF-16 chars are 2 bytes each (minus null terminator).
                let chars = size.saturating_sub(2) / 2;
                (chars, true)
            }
            _ => (0, false),
        };
        let _ = CloseClipboard();
        result
    }

    /// Reads CF_UNICODETEXT from the clipboard and returns it as a Zeroizing UTF-8 string bytes.
    ///
    /// Returns `None` if the clipboard cannot be opened or does not contain text.
    ///
    /// # Safety
    ///
    /// Caller must be on the message-pump thread. Properly locks and unlocks the
    /// global memory handle returned by `GetClipboardData`.
    unsafe fn read_clipboard_text() -> Option<Zeroizing<Vec<u8>>> {
        if !OpenClipboard(None).as_bool() {
            return None;
        }

        let hdata = GetClipboardData(CF_UNICODETEXT).ok()?;
        if hdata.0 == 0 {
            let _ = CloseClipboard();
            return None;
        }

        let ptr = GlobalLock(windows::Win32::Foundation::HANDLE(hdata.0));
        if ptr.is_null() {
            let _ = CloseClipboard();
            return None;
        }

        let size = GlobalSize(windows::Win32::Foundation::HANDLE(hdata.0));
        // UTF-16 chars: each char is 2 bytes, last 2 bytes are null terminator.
        let char_count = size.saturating_sub(2) / 2;

        let result = if char_count > 0 {
            let slice = std::slice::from_raw_parts(ptr as *const u16, char_count);
            let utf8 = String::from_utf16_lossy(slice);
            let mut buf = Zeroizing::new(utf8.into_bytes());
            // Ensure the backing Vec is fully owned so Zeroizing can wipe it.
            Some(buf)
        } else {
            None
        };

        GlobalUnlock(windows::Win32::Foundation::HANDLE(hdata.0));
        let _ = CloseClipboard();
        result
    }

    /// Minimal translate+dispatch shim used inside the message loop.
    ///
    /// # Safety
    ///
    /// Called with a valid `MSG` from `GetMessageW`.
    #[allow(non_snake_case)]
    unsafe fn TranslateAndDispatch(msg: &MSG) {
        use windows::Win32::UI::WindowsAndMessaging::{DispatchMessageW, TranslateMessage};
        let _ = TranslateMessage(msg);
        DispatchMessageW(msg);
    }

    /// Default window procedure for the clipboard listener window.
    ///
    /// # Safety
    ///
    /// Standard `extern "system"` Win32 window procedure. Delegates all
    /// messages to `DefWindowProcW`.
    unsafe extern "system" fn clipboard_wnd_proc(
        hwnd: HWND,
        msg: u32,
        wparam: WPARAM,
        lparam: LPARAM,
    ) -> LRESULT {
        if msg == WM_DESTROY {
            return LRESULT(0);
        }
        DefWindowProcW(hwnd, msg, wparam, lparam)
    }

    fn enqueue(
        ctx: &CollectorCtx,
        kind: EventKind,
        priority: Priority,
        payload: &str,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let now = ctx.clock.now_unix_nanos();
        let id = EventId::new_v7().to_bytes();
        match ctx.queue.enqueue(&id, kind.as_str(), priority, now, now, payload.as_bytes()) {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
            }
            Err(e) => {
                warn!(error = %e, "clipboard: queue error");
                drops.fetch_add(1, Ordering::Relaxed);
            }
        }
    }
}
