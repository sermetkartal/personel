//! Safe wrappers around Win32 user input APIs.
//!
//! - [`last_input_idle_ms`] — wraps `GetLastInputInfo`.
//! - [`foreground_window_info`] — wraps `GetForegroundWindow` +
//!   `GetWindowText` + `GetWindowThreadProcessId`.

use windows::Win32::UI::WindowsAndMessaging::{
    GetForegroundWindow, GetLastInputInfo, GetWindowTextW, GetWindowThreadProcessId, LASTINPUTINFO,
};
use windows::Win32::System::SystemInformation::GetTickCount64;

use personel_core::error::{AgentError, Result};

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
        // GetTickCount64 wraps at ~49 days; GetLastInputInfo uses GetTickCount
        // (32-bit). Handle the wrap: if last_input_tick > tick_now (modulo
        // 32-bit) it means the 32-bit counter wrapped.
        let tick_now_32 = (tick_now & 0xFFFF_FFFF) as u64;
        if tick_now_32 >= last_input_tick {
            tick_now_32 - last_input_tick
        } else {
            // 32-bit wrap: add 2^32 to compensate.
            tick_now_32 + 0x1_0000_0000 - last_input_tick
        }
    };
    Ok(idle_ms)
}

/// Returns information about the currently active foreground window.
///
/// Returns `Ok(None)` if no foreground window is present (e.g., locked screen).
///
/// # Errors
///
/// Only returns `Err` on an internal buffer overflow, which cannot happen
/// with the fixed 512-char buffer.
pub fn foreground_window_info() -> Result<ForegroundWindowInfo> {
    // SAFETY: GetForegroundWindow returns NULL if no window is in the
    // foreground; we handle that case. The HWND is valid for the duration
    // of this function.
    unsafe {
        let hwnd = GetForegroundWindow();
        if hwnd.0 == 0 {
            return Ok(ForegroundWindowInfo {
                title: String::new(),
                pid: 0,
                hwnd: 0,
            });
        }

        // Read up to 512 UTF-16 code units.
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
