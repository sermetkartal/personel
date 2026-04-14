//! Safe wrappers around Win32 user input APIs.
//!
//! - [`last_input_idle_ms`] — wraps `GetLastInputInfo`.
//! - [`foreground_window_info`] — wraps `GetForegroundWindow` +
//!   `GetWindowText` + `GetWindowThreadProcessId`.
//! - [`install_keyboard_hook`] — installs a `WH_KEYBOARD_LL` low-level hook
//!   and pumps messages on a dedicated OS thread. Returns a [`HookHandle`]
//!   whose `Drop` impl stops the pump and removes the hook.

use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::mpsc;

use windows::Win32::System::SystemInformation::GetTickCount64;
// GetLastInputInfo and LASTINPUTINFO are in Win32::UI::Input::KeyboardAndMouse,
// not in Win32::UI::WindowsAndMessaging (which only has the hook / message APIs).
use windows::Win32::UI::Input::KeyboardAndMouse::{GetLastInputInfo, LASTINPUTINFO};
use windows::Win32::UI::WindowsAndMessaging::{
    CallNextHookEx, DispatchMessageW, GetForegroundWindow, GetMessageW,
    GetWindowTextW, GetWindowThreadProcessId, PostThreadMessageW, SetWindowsHookExW, TranslateMessage,
    UnhookWindowsHookEx, HHOOK, KBDLLHOOKSTRUCT, MSG, WH_KEYBOARD_LL, WM_QUIT,
};
use windows::Win32::Foundation::{LPARAM, LRESULT, WPARAM};

use personel_core::error::{AgentError, Result};

// ──────────────────────────────────────────────────────────────────────────────
// Existing idle / foreground window APIs
// ──────────────────────────────────────────────────────────────────────────────

/// Information about the current foreground window.
#[derive(Debug, Clone)]
pub struct ForegroundWindowInfo {
    /// Window title (UTF-16 decoded to String).
    pub title: String,
    /// Owning process ID.
    pub pid: u32,
    /// Raw HWND value.
    pub hwnd: usize,
}

/// Returns the number of milliseconds since the last user input event.
///
/// # Errors
///
/// Returns [`AgentError::CollectorRuntime`] if `GetLastInputInfo` fails.
pub fn last_input_idle_ms() -> Result<u64> {
    // SAFETY: LASTINPUTINFO is a plain old data struct; we initialise it
    // correctly and the API writes into it atomically.
    let idle_ms = unsafe {
        let mut lii = LASTINPUTINFO {
            cbSize: std::mem::size_of::<LASTINPUTINFO>() as u32,
            dwTime: 0,
        };
        if !GetLastInputInfo(&mut lii).as_bool() {
            return Err(AgentError::CollectorRuntime {
                name: "idle",
                reason: "GetLastInputInfo returned false".into(),
            });
        }
        let tick_now = GetTickCount64();
        let last_input_tick = u64::from(lii.dwTime);
        let tick_now_32 = (tick_now & 0xFFFF_FFFF) as u64;
        if tick_now_32 >= last_input_tick {
            tick_now_32 - last_input_tick
        } else {
            tick_now_32 + 0x1_0000_0000 - last_input_tick
        }
    };
    Ok(idle_ms)
}

/// Returns information about the currently active foreground window.
///
/// Returns an empty `ForegroundWindowInfo` (all zero/empty) if no foreground
/// window is present (e.g., locked screen).
///
/// # Errors
///
/// Only returns `Err` on internal buffer overflow, which cannot happen with
/// the fixed 512-char buffer.
pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
    // SAFETY: GetForegroundWindow returns NULL for no window; we handle it.
    // HWND is valid for the duration of this function.
    unsafe {
        let hwnd = GetForegroundWindow();
        if hwnd.0 == 0 {
            return Ok(ForegroundWindowInfo { title: String::new(), pid: 0, hwnd: 0 });
        }

        let mut buf = [0u16; 512];
        let len = GetWindowTextW(hwnd, &mut buf);
        let title = if len > 0 {
            String::from_utf16_lossy(&buf[..len as usize])
        } else {
            String::new()
        };

        let mut pid: u32 = 0;
        GetWindowThreadProcessId(hwnd, Some(&mut pid));

        Ok(ForegroundWindowInfo { title, pid, hwnd: hwnd.0 as usize })
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Low-level keyboard hook
// ──────────────────────────────────────────────────────────────────────────────

/// A raw keystroke event from the `WH_KEYBOARD_LL` hook.
///
/// Contains only VK codes, scan codes, and flags — **no character
/// interpretation**. The keystroke collector is responsible for deciding
/// whether to buffer counts (metadata mode) or encrypt content (DLP mode).
#[derive(Debug, Clone, Copy)]
pub struct KeyEvent {
    /// Virtual-key code (e.g. `VK_A`, `VK_RETURN`).
    pub vk_code: u32,
    /// Hardware scan code.
    pub scan_code: u32,
    /// Hook flags (`LLKHF_*` bitmask from `KBDLLHOOKSTRUCT.flags`).
    pub flags: u32,
    /// `GetTickCount64()` milliseconds at the time the hook fired.
    pub timestamp_ms: u64,
}

/// Thread ID of the hook's message-pump thread, used to post `WM_QUIT`.
///
/// Stored as a global because the hook callback is a bare `extern "system"
/// fn` and cannot capture any state.
static HOOK_THREAD_ID: AtomicU32 = AtomicU32::new(0);

// ── Hook callback diagnostics (Faz 8 item #3) ───────────────────────────────
//
// The WH_KEYBOARD_LL callback runs on a raw OS thread inside the message
// pump. Logging macros (`info!`/`warn!`) are unsafe to call from there
// because tracing subscribers can allocate, contend locks, or re-enter
// code that has its own thread-locals. Instead we bump lock-free atomic
// counters on every hook event and let a non-hook thread (the keystroke
// meta collector's progress logger) print deltas every 10 seconds.
//
// Interpretation matrix (from `cargo log`):
//   cb_fired == 0                             → hook never fires; check
//                                                 SetWindowsHookExW install
//                                                 path or HOOK_THREAD_ID.
//   cb_fired > 0 && cb_send_err > 0           → receiver dropped (mpsc
//                                                 channel gone).
//   cb_fired > 0 && cb_lock_miss > 0          → Mutex contention with
//                                                 the uninstall path.
//   cb_fired > 0 && cb_no_cell > 0            → init ordering bug,
//                                                 KEY_EVENT_TX unset
//                                                 when callback fires.
//   cb_fired > 0 && cb_send_ok > 0 && run_meta
//     reports keys_since_flush == 0           → run_meta drain loop or
//                                                 `ev.flags & 0x80` filter
//                                                 bug.
//
// Counters are `AtomicU64::fetch_add(Relaxed)` which is a handful of
// nanoseconds on x86 — negligible vs. the existing mpsc send.

/// Total number of WH_KEYBOARD_LL callback invocations since agent start.
pub static HOOK_CB_FIRED: AtomicU64 = AtomicU64::new(0);
/// Number of key events successfully sent into the meta collector's rx.
pub static HOOK_CB_SEND_OK: AtomicU64 = AtomicU64::new(0);
/// Number of send failures (mpsc receiver dropped).
pub static HOOK_CB_SEND_ERR: AtomicU64 = AtomicU64::new(0);
/// Number of callback invocations where the sender Mutex was contended.
pub static HOOK_CB_LOCK_MISS: AtomicU64 = AtomicU64::new(0);
/// Number of callback invocations where KEY_EVENT_TX had no sender yet
/// (init ordering race — should be rare after the hook install path
/// returns).
pub static HOOK_CB_NO_CELL: AtomicU64 = AtomicU64::new(0);

/// Public getter for the hook's message-pump thread ID. Used by the
/// keystroke meta collector to include it in progress logs — a zero
/// value means the hook thread never started or has exited.
pub fn hook_thread_id() -> u32 {
    HOOK_THREAD_ID.load(Ordering::Acquire)
}

/// Newtype wrapper that allows a raw `*mut mpsc::Sender<KeyEvent>` to be stored
/// in a `static`.
///
/// # Safety invariant
///
/// The wrapped pointer is `Some(Box::into_raw(tx))` only while the hook thread
/// is running and the `HHOOK` handle is live.  All writes happen under the
/// enclosing `Mutex`, and reads occur only from the single hook-callback thread
/// while the mutex is held.  There is therefore no data race.
struct SenderPtr(*mut mpsc::Sender<KeyEvent>);

// SAFETY: SenderPtr is only ever accessed under the Mutex<Option<SenderPtr>>
// lock.  The pointer is written before the hook is installed and cleared after
// UnhookWindowsHookEx returns.  No two threads ever access the pointed-to
// Sender concurrently, so both Send and Sync are sound.
unsafe impl Send for SenderPtr {}
unsafe impl Sync for SenderPtr {}

/// The sender end of the key-event channel, set by the hook thread before
/// installing the hook and cleared on teardown.
///
/// Using a raw pointer behind an `AtomicU32` sentinel is unavoidable here
/// because Win32 hook callbacks cannot carry closures. The pointer is:
/// - written once before `SetWindowsHookExW` (sequenced-before the callback)
/// - read only from the hook callback thread (no data race)
/// - cleared after `UnhookWindowsHookEx` returns (before the thread exits)
///
/// # Safety invariant
///
/// The inner `SenderPtr` is `Some(SenderPtr(Box::into_raw(tx)))` only while the
/// hook thread is running and the `HHOOK` handle is live. The hook callback
/// casts it to `*mut mpsc::Sender<KeyEvent>` and calls `(*p).send(...)`.
/// The hook thread owns the pointer and drops it after unhook.
static KEY_EVENT_TX: std::sync::OnceLock<std::sync::Mutex<Option<SenderPtr>>> =
    std::sync::OnceLock::new();

/// SAFETY: The raw pointer is only accessed from the hook callback (single OS
/// thread) and from the hook-install thread (only while no callback is live).
unsafe impl Send for HookHandle {}
unsafe impl Sync for HookHandle {}

/// Handle to a running keyboard hook.
///
/// Dropping this handle stops the message pump (`WM_QUIT`) and removes the
/// hook (`UnhookWindowsHookEx`). The background thread joins automatically
/// because `PostThreadMessageW(WM_QUIT)` causes `GetMessageW` to return
/// `false`, ending the pump loop.
pub struct HookHandle {
    _thread: std::thread::JoinHandle<()>,
}

impl Drop for HookHandle {
    fn drop(&mut self) {
        let tid = HOOK_THREAD_ID.load(Ordering::Acquire);
        if tid != 0 {
            // SAFETY: WM_QUIT is a well-defined message; thread is alive.
            unsafe {
                let _ = PostThreadMessageW(tid, WM_QUIT, WPARAM(0), LPARAM(0));
            }
        }
        // _thread is joined when the JoinHandle is dropped. We do NOT call
        // join() here to avoid blocking Drop callers; the OS will clean up
        // the thread resources when it exits naturally after WM_QUIT.
    }
}

/// Installs a low-level keyboard hook (`WH_KEYBOARD_LL`) on a dedicated OS
/// thread and returns a [`HookHandle`].
///
/// `WH_KEYBOARD_LL` requires a message pump on the installing thread, so this
/// function spawns a `std::thread` (not a tokio task) and drives the pump with
/// `GetMessageW`. Key events are sent to the caller via `tx`.
///
/// The hook is removed when the returned `HookHandle` is dropped.
///
/// # Errors
///
/// Returns `AgentError::CollectorStart` if `SetWindowsHookExW` fails.
pub fn install_keyboard_hook(tx: mpsc::Sender<KeyEvent>) -> Result<HookHandle> {
    // Channel to propagate hook installation result from the hook thread.
    let (ready_tx, ready_rx) = mpsc::channel::<std::result::Result<(), String>>();

    // Store the sender in the global so the callback can reach it.
    let cell = KEY_EVENT_TX.get_or_init(|| std::sync::Mutex::new(None));
    {
        let mut guard = cell.lock().expect("KEY_EVENT_TX poisoned");
        // SAFETY: We box the sender so it has a stable heap address. The
        // pointer is valid until the hook thread clears it after unhook.
        *guard = Some(SenderPtr(Box::into_raw(Box::new(tx))));
    }

    let thread = std::thread::Builder::new()
        .name("personel-kbd-hook".into())
        .spawn(move || {
            // Record this thread's ID for WM_QUIT posting.
            // SAFETY: GetCurrentThreadId() is always safe.
            let tid = unsafe { windows::Win32::System::Threading::GetCurrentThreadId() };
            HOOK_THREAD_ID.store(tid, Ordering::Release);

            // Install the hook. NULL hModule + NULL hwnd = global hook on
            // all threads in the current desktop, which is what WH_KEYBOARD_LL
            // requires (it ignores the thread ID parameter anyway).
            // SAFETY: `keyboard_hook_proc` is a valid extern-system function.
            let hhook = unsafe {
                SetWindowsHookExW(WH_KEYBOARD_LL, Some(keyboard_hook_proc), None, 0)
            };
            match hhook {
                Ok(h) => {
                    let _ = ready_tx.send(Ok(()));
                    // Message pump — required for WH_KEYBOARD_LL to fire.
                    // SAFETY: standard Win32 message pump pattern.
                    unsafe {
                        let mut msg = MSG::default();
                        while GetMessageW(&mut msg, None, 0, 0).as_bool() {
                            let _ = TranslateMessage(&msg);
                            DispatchMessageW(&msg);
                        }
                        // Pump exited: remove hook, clear sender pointer.
                        let _ = UnhookWindowsHookEx(h);
                    }
                }
                Err(e) => {
                    let _ = ready_tx.send(Err(format!("SetWindowsHookExW failed: {e}")));
                }
            }

            // Clear the sender so no further events can be sent after unhook.
            if let Some(cell) = KEY_EVENT_TX.get() {
                if let Ok(mut guard) = cell.lock() {
                    if let Some(SenderPtr(ptr)) = guard.take() {
                        // SAFETY: we own the Box from Box::into_raw above.
                        unsafe { drop(Box::from_raw(ptr)) };
                    }
                }
            }
            HOOK_THREAD_ID.store(0, Ordering::Release);
        })
        .map_err(|e| AgentError::CollectorStart {
            name: "keystroke",
            reason: format!("failed to spawn hook thread: {e}"),
        })?;

    // Wait for the hook thread to confirm installation.
    ready_rx
        .recv()
        .map_err(|_| AgentError::CollectorStart {
            name: "keystroke",
            reason: "hook thread exited before signalling ready".into(),
        })?
        .map_err(|reason| AgentError::CollectorStart { name: "keystroke", reason })?;

    Ok(HookHandle { _thread: thread })
}

/// Low-level keyboard hook callback (`WH_KEYBOARD_LL`).
///
/// # Safety
///
/// This is an `extern "system"` callback registered with Win32. It is called
/// from the hook thread's message pump. We access `KEY_EVENT_TX` under a
/// mutex and send the event, then delegate to the next hook in the chain.
unsafe extern "system" fn keyboard_hook_proc(
    n_code: i32,
    w_param: WPARAM,
    l_param: LPARAM,
) -> LRESULT {
    // nCode < 0 means we must not process the message.
    if n_code >= 0 {
        // Cast lParam to KBDLLHOOKSTRUCT.
        // SAFETY: Win32 guarantees lParam points to a KBDLLHOOKSTRUCT for
        // WH_KEYBOARD_LL when nCode == HC_ACTION (0).
        let kbd = &*(l_param.0 as *const KBDLLHOOKSTRUCT);
        let ts  = GetTickCount64();

        let ev = KeyEvent {
            vk_code:      kbd.vkCode,
            scan_code:    kbd.scanCode,
            flags:        kbd.flags.0,
            timestamp_ms: ts,
        };

        // Bump the fire counter FIRST so "hook installed but fires zero
        // times" is distinguishable from "hook never fired at all".
        HOOK_CB_FIRED.fetch_add(1, Ordering::Relaxed);

        // Send to the collector. Track every outcome with atomic counters
        // so the meta collector's progress log can distinguish silent
        // drop from genuine zero-key sessions.
        match KEY_EVENT_TX.get() {
            None => {
                HOOK_CB_NO_CELL.fetch_add(1, Ordering::Relaxed);
            }
            Some(cell) => match cell.try_lock() {
                Err(_) => {
                    HOOK_CB_LOCK_MISS.fetch_add(1, Ordering::Relaxed);
                }
                Ok(guard) => {
                    if let Some(ref sp) = *guard {
                        // SAFETY: sp.0 is valid while the hook is
                        // installed; this callback only fires while the
                        // hook thread is alive.
                        match (*sp.0).send(ev) {
                            Ok(_) => {
                                HOOK_CB_SEND_OK.fetch_add(1, Ordering::Relaxed);
                            }
                            Err(_) => {
                                HOOK_CB_SEND_ERR.fetch_add(1, Ordering::Relaxed);
                            }
                        }
                    } else {
                        HOOK_CB_NO_CELL.fetch_add(1, Ordering::Relaxed);
                    }
                }
            },
        }
    }

    CallNextHookEx(HHOOK::default(), n_code, w_param, l_param)
}
