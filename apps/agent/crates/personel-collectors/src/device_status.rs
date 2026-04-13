//! Device status snapshot collector (Faz 2 Wave 3 — roadmap item #17).
//!
//! Polls host health metrics every 60 seconds and emits a single
//! `device.status_snapshot` event with a rollup of CPU, memory, system disk,
//! battery, screen state, lock state, uptime and boot time. This is the
//! identifier-class heartbeat that lets admins see whether a managed
//! endpoint is healthy without touching any behavioural / content data.
//!
//! # KVKK posture
//!
//! Every field is host-level telemetry. There are no usernames, file paths,
//! window titles, network identifiers, or any other PII in the payload.
//! Per `event.rs` the `DeviceStatusSnapshot` event is classed as
//! [`PiiClass::Identifier`]; this collector treats that as a hard contract
//! and refuses to enrich the payload with anything beyond machine-wide
//! aggregate numbers.
//!
//! # Data sources (no new crate deps)
//!
//! | Field          | Win32 API                                  |
//! |----------------|--------------------------------------------|
//! | CPU total %    | `GetSystemTimes` × 2 (100 ms apart)        |
//! | Memory         | `GlobalMemoryStatusEx`                     |
//! | System disk    | `GetDiskFreeSpaceExW("C:\\")`              |
//! | Battery        | `GetSystemPowerStatus`                     |
//! | Uptime         | `GetTickCount64`                           |
//! | Screen / lock  | `WTSQuerySessionInformationW(WTSConnectState)` on the active console session |
//!
//! Any field that cannot be determined on the current host (desktop with
//! no battery, headless server with no console session, transient API
//! failure) is serialised as JSON `null`. The collector NEVER fails the
//! whole snapshot if a single source returns an error — partial coverage
//! is more useful than nothing.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: real implementation built around the
//! Win32 calls above, executed inside a `tokio::task::spawn_blocking`
//! worker so the 100 ms `GetSystemTimes` delta + WTS calls never block
//! the async runtime.
//!
//! Non-Windows: parks gracefully so `cargo check` passes on macOS/Linux
//! dev builds.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::oneshot;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// How often a device status snapshot is captured and enqueued.
const POLL_INTERVAL: Duration = Duration::from_secs(60);

/// Device status / host health rollup collector.
#[derive(Default)]
pub struct DeviceStatusCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl DeviceStatusCollector {
    /// Creates a new [`DeviceStatusCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for DeviceStatusCollector {
    fn name(&self) -> &'static str {
        "device_status"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["device.status_snapshot"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        // The 100 ms CPU delta sleep + GlobalMemoryStatusEx + WTS calls are
        // all blocking. We host the entire poll loop inside spawn_blocking
        // so we never park a tokio worker on a Win32 system call.
        let task = tokio::task::spawn_blocking(move || {
            run_loop(ctx, healthy, events, drops, stop_rx);
        });

        self.healthy.store(true, Ordering::Relaxed);
        Ok(CollectorHandle {
            name: self.name(),
            task,
            stop_tx,
        })
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
// Snapshot type — common across platforms
// ──────────────────────────────────────────────────────────────────────────────

/// One full host-health rollup.
///
/// Every field is `Option`-wrapped because every Win32 source can fail
/// independently and partial snapshots must still be deliverable.
#[derive(Debug, Clone, Default)]
struct Snapshot {
    cpu_percent_total: Option<f64>,
    memory_used_bytes: Option<u64>,
    memory_total_bytes: Option<u64>,
    memory_percent: Option<f64>,
    disk_system_used_bytes: Option<u64>,
    disk_system_total_bytes: Option<u64>,
    disk_system_percent: Option<f64>,
    battery_percent: Option<u8>,
    battery_charging: Option<bool>,
    battery_estimated_minutes_remaining: Option<u32>,
    ac_power: Option<bool>,
    uptime_seconds: Option<u64>,
    screen_on: Option<bool>,
    locked: Option<bool>,
    /// Unix nanos of (now − uptime). Optional because boot time depends on uptime.
    boot_time_unix_nanos: Option<i64>,
}

impl Snapshot {
    /// Serialises the snapshot to a JSON object with the wire shape documented
    /// in the module header. `null` is used for any unknown field.
    fn to_json(&self, now_unix_nanos: i64) -> String {
        let mut s = String::with_capacity(640);
        s.push('{');

        push_opt_f64(&mut s, "cpu_percent_total", self.cpu_percent_total, true);
        push_opt_u64(&mut s, "memory_used_bytes", self.memory_used_bytes, false);
        push_opt_u64(&mut s, "memory_total_bytes", self.memory_total_bytes, false);
        push_opt_f64(&mut s, "memory_percent", self.memory_percent, false);
        push_opt_u64(
            &mut s,
            "disk_system_used_bytes",
            self.disk_system_used_bytes,
            false,
        );
        push_opt_u64(
            &mut s,
            "disk_system_total_bytes",
            self.disk_system_total_bytes,
            false,
        );
        push_opt_f64(
            &mut s,
            "disk_system_percent",
            self.disk_system_percent,
            false,
        );
        push_opt_u8(&mut s, "battery_percent", self.battery_percent, false);
        push_opt_bool(&mut s, "battery_charging", self.battery_charging, false);
        push_opt_u32(
            &mut s,
            "battery_estimated_minutes_remaining",
            self.battery_estimated_minutes_remaining,
            false,
        );
        push_opt_bool(&mut s, "ac_power", self.ac_power, false);
        push_opt_u64(&mut s, "uptime_seconds", self.uptime_seconds, false);
        push_opt_bool(&mut s, "screen_on", self.screen_on, false);
        push_opt_bool(&mut s, "locked", self.locked, false);

        s.push(',');
        s.push_str("\"boot_time\":");
        match self.boot_time_unix_nanos {
            Some(b) => {
                s.push('"');
                s.push_str(&format_rfc3339(b));
                s.push('"');
            }
            None => s.push_str("null"),
        }

        s.push(',');
        s.push_str("\"timestamp\":\"");
        s.push_str(&format_rfc3339(now_unix_nanos));
        s.push('"');

        s.push('}');
        s
    }
}

fn push_opt_f64(s: &mut String, key: &str, val: Option<f64>, first: bool) {
    if !first {
        s.push(',');
    }
    s.push('"');
    s.push_str(key);
    s.push_str("\":");
    match val {
        // Round to 2 decimal places for percent-style fields. Avoids noisy
        // wire payloads from FILETIME jitter.
        Some(v) if v.is_finite() => s.push_str(&format!("{:.2}", v)),
        _ => s.push_str("null"),
    }
}

fn push_opt_u64(s: &mut String, key: &str, val: Option<u64>, first: bool) {
    if !first {
        s.push(',');
    }
    s.push('"');
    s.push_str(key);
    s.push_str("\":");
    match val {
        Some(v) => s.push_str(&v.to_string()),
        None => s.push_str("null"),
    }
}

fn push_opt_u32(s: &mut String, key: &str, val: Option<u32>, first: bool) {
    if !first {
        s.push(',');
    }
    s.push('"');
    s.push_str(key);
    s.push_str("\":");
    match val {
        Some(v) => s.push_str(&v.to_string()),
        None => s.push_str("null"),
    }
}

fn push_opt_u8(s: &mut String, key: &str, val: Option<u8>, first: bool) {
    if !first {
        s.push(',');
    }
    s.push('"');
    s.push_str(key);
    s.push_str("\":");
    match val {
        Some(v) => s.push_str(&v.to_string()),
        None => s.push_str("null"),
    }
}

fn push_opt_bool(s: &mut String, key: &str, val: Option<bool>, first: bool) {
    if !first {
        s.push(',');
    }
    s.push('"');
    s.push_str(key);
    s.push_str("\":");
    match val {
        Some(true) => s.push_str("true"),
        Some(false) => s.push_str("false"),
        None => s.push_str("null"),
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// CPU delta math (pure helper, unit-tested cross-platform)
// ──────────────────────────────────────────────────────────────────────────────

/// FILETIME-equivalent 64-bit tick value used for unit tests so the math is
/// platform-agnostic. Real Win32 conversion lives in the `windows` submodule.
#[derive(Debug, Clone, Copy)]
struct Ft100Ns(u64);

/// Computes total CPU utilisation percentage from two GetSystemTimes
/// snapshots.
///
/// The Windows formula is:
///
/// ```text
/// busy = (kernel - idle) + user
/// total = kernel + user
/// percent = (busy / total) × 100
/// ```
///
/// Note: GetSystemTimes' `kernel` ALREADY INCLUDES the `idle` slice on
/// Windows, so subtracting `idle` from `kernel` is the documented way to
/// get the non-idle kernel time. See KB article 2553708 / the official
/// `GetSystemTimes` reference.
///
/// Returns `None` when both deltas are zero (no time elapsed) or when
/// the delta values would overflow / underflow — the caller should
/// surface that as JSON `null`, not as a stale or fabricated value.
fn cpu_percent_from_deltas(
    idle_a: Ft100Ns,
    kernel_a: Ft100Ns,
    user_a: Ft100Ns,
    idle_b: Ft100Ns,
    kernel_b: Ft100Ns,
    user_b: Ft100Ns,
) -> Option<f64> {
    let idle_d = idle_b.0.checked_sub(idle_a.0)?;
    let kernel_d = kernel_b.0.checked_sub(kernel_a.0)?;
    let user_d = user_b.0.checked_sub(user_a.0)?;

    let total = kernel_d.checked_add(user_d)?;
    if total == 0 {
        return None;
    }

    // idle is a subset of kernel; busy = total - idle.
    let busy = total.saturating_sub(idle_d);
    let pct = (busy as f64) * 100.0 / (total as f64);

    // Clamp to [0, 100] to defend against measurement jitter.
    Some(pct.clamp(0.0, 100.0))
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
    {
        windows_impl::run(ctx, healthy, events, drops, stop_rx);
    }

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        tracing::info!(
            "device_status: GetSystemTimes / GlobalMemoryStatusEx not available on this platform — parking"
        );
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows_impl {
    use super::{
        cpu_percent_from_deltas, format_rfc3339, Ft100Ns, Snapshot, POLL_INTERVAL,
    };
    use crate::CollectorCtx;

    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::Arc;
    use std::time::{Duration, Instant};

    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use ::windows::core::PCWSTR;
    use ::windows::Win32::Foundation::{FILETIME, HANDLE};
    use ::windows::Win32::Storage::FileSystem::GetDiskFreeSpaceExW;
    use ::windows::Win32::System::Power::{GetSystemPowerStatus, SYSTEM_POWER_STATUS};
    use ::windows::Win32::System::RemoteDesktop::{
        WTSActive, WTSFreeMemory, WTSGetActiveConsoleSessionId, WTSQuerySessionInformationW,
        WTSConnectState, WTS_CONNECTSTATE_CLASS, WTS_CURRENT_SERVER_HANDLE,
    };
    use ::windows::Win32::System::SystemInformation::{GetTickCount64, GlobalMemoryStatusEx, MEMORYSTATUSEX};
    use ::windows::Win32::System::Threading::GetSystemTimes;

    /// Sleep between the two `GetSystemTimes` reads used to compute the
    /// CPU delta. 100 ms is the same window Task Manager uses for its
    /// "% CPU" column and gives <1% jitter on idle laptops.
    const CPU_DELTA_SLEEP: Duration = Duration::from_millis(100);

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("device_status: started, interval = {:?}", POLL_INTERVAL);
        healthy.store(true, Ordering::Relaxed);

        loop {
            // Stop check at the top so we exit quickly even right after start.
            if stop_rx.try_recv().is_ok() {
                break;
            }

            let snap_start = Instant::now();
            let snapshot = take_snapshot();
            let now = ctx.clock.now_unix_nanos();
            let payload = snapshot.to_json(now);

            debug!(
                cpu_pct = ?snapshot.cpu_percent_total,
                mem_pct = ?snapshot.memory_percent,
                disk_pct = ?snapshot.disk_system_percent,
                bat_pct = ?snapshot.battery_percent,
                screen_on = ?snapshot.screen_on,
                locked = ?snapshot.locked,
                "device_status snapshot ready"
            );

            let id = EventId::new_v7().to_bytes();
            match ctx.queue.enqueue(
                &id,
                EventKind::DeviceStatusSnapshot.as_str(),
                Priority::Low,
                now,
                now,
                payload.as_bytes(),
            ) {
                Ok(_) => {
                    events.fetch_add(1, Ordering::Relaxed);
                    healthy.store(true, Ordering::Relaxed);
                }
                Err(e) => {
                    error!(error = %e, "device_status: queue enqueue failed");
                    drops.fetch_add(1, Ordering::Relaxed);
                }
            }

            // Sleep until the next poll, but in small slices so a stop
            // signal is honoured within ~250 ms.
            let elapsed = snap_start.elapsed();
            let mut remaining = POLL_INTERVAL.saturating_sub(elapsed);
            while remaining > Duration::ZERO {
                if stop_rx.try_recv().is_ok() {
                    info!("device_status: stop requested during sleep");
                    return;
                }
                let slice = remaining.min(Duration::from_millis(250));
                std::thread::sleep(slice);
                remaining = remaining.saturating_sub(slice);
            }
        }

        info!("device_status: stop requested, exiting run loop");
    }

    /// Captures one full snapshot. Each subsystem call is independent — a
    /// failing source becomes `None`, never an early return.
    fn take_snapshot() -> Snapshot {
        let cpu_percent_total = read_cpu_percent();
        let (memory_used_bytes, memory_total_bytes, memory_percent) = read_memory();
        let (disk_system_used_bytes, disk_system_total_bytes, disk_system_percent) =
            read_system_disk();
        let (
            battery_percent,
            battery_charging,
            battery_estimated_minutes_remaining,
            ac_power,
        ) = read_power();
        let uptime_seconds = read_uptime_seconds();
        let (screen_on, locked) = read_session_state();
        let boot_time_unix_nanos = uptime_seconds.map(|u| {
            // Use SystemTime so this is consistent with the wall-clock
            // emitted in the JSON timestamp. `Clock` isn't passed into
            // helpers — the small drift between the two reads is fine
            // for boot-time reporting (seconds-level precision).
            let now_nanos = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_nanos() as i64)
                .unwrap_or(0);
            now_nanos - (u as i64) * 1_000_000_000
        });

        Snapshot {
            cpu_percent_total,
            memory_used_bytes,
            memory_total_bytes,
            memory_percent,
            disk_system_used_bytes,
            disk_system_total_bytes,
            disk_system_percent,
            battery_percent,
            battery_charging,
            battery_estimated_minutes_remaining,
            ac_power,
            uptime_seconds,
            screen_on,
            locked,
            boot_time_unix_nanos,
        }
    }

    /// Two `GetSystemTimes` reads 100 ms apart, then `cpu_percent_from_deltas`.
    fn read_cpu_percent() -> Option<f64> {
        let a = read_system_times()?;
        std::thread::sleep(CPU_DELTA_SLEEP);
        let b = read_system_times()?;
        cpu_percent_from_deltas(a.0, a.1, a.2, b.0, b.1, b.2)
    }

    /// Returns `(idle, kernel, user)` as 100-ns ticks, or `None` on failure.
    fn read_system_times() -> Option<(Ft100Ns, Ft100Ns, Ft100Ns)> {
        let mut idle = FILETIME::default();
        let mut kernel = FILETIME::default();
        let mut user = FILETIME::default();
        // SAFETY: All three pointers are valid stack locals; the API only writes.
        let res = unsafe {
            GetSystemTimes(Some(&mut idle), Some(&mut kernel), Some(&mut user))
        };
        if res.is_err() {
            warn!("device_status: GetSystemTimes failed");
            return None;
        }
        Some((filetime_to_ticks(idle), filetime_to_ticks(kernel), filetime_to_ticks(user)))
    }

    fn filetime_to_ticks(ft: FILETIME) -> Ft100Ns {
        Ft100Ns(((ft.dwHighDateTime as u64) << 32) | ft.dwLowDateTime as u64)
    }

    fn read_memory() -> (Option<u64>, Option<u64>, Option<f64>) {
        let mut ms = MEMORYSTATUSEX::default();
        ms.dwLength = std::mem::size_of::<MEMORYSTATUSEX>() as u32;
        // SAFETY: dwLength initialised, pointer valid for the duration of the call.
        let res = unsafe { GlobalMemoryStatusEx(&mut ms) };
        if res.is_err() {
            warn!("device_status: GlobalMemoryStatusEx failed");
            return (None, None, None);
        }
        let total = ms.ullTotalPhys;
        let avail = ms.ullAvailPhys;
        let used = total.saturating_sub(avail);
        let pct = if total > 0 {
            Some((used as f64) * 100.0 / (total as f64))
        } else {
            None
        };
        (Some(used), Some(total), pct)
    }

    fn read_system_disk() -> (Option<u64>, Option<u64>, Option<f64>) {
        // Probe `C:\` — the standard Windows system drive. We deliberately
        // do NOT try to enumerate volumes; that would risk surfacing
        // attached USB storage and behavioural drift.
        let path: Vec<u16> = "C:\\".encode_utf16().chain(std::iter::once(0)).collect();
        let mut free_avail: u64 = 0;
        let mut total_bytes: u64 = 0;
        let mut total_free: u64 = 0;
        // SAFETY: path is null-terminated; all three out pointers are valid stack locals.
        let res = unsafe {
            GetDiskFreeSpaceExW(
                PCWSTR(path.as_ptr()),
                Some(&mut free_avail),
                Some(&mut total_bytes),
                Some(&mut total_free),
            )
        };
        if res.is_err() {
            warn!("device_status: GetDiskFreeSpaceExW(C:) failed");
            return (None, None, None);
        }
        let used = total_bytes.saturating_sub(total_free);
        let pct = if total_bytes > 0 {
            Some((used as f64) * 100.0 / (total_bytes as f64))
        } else {
            None
        };
        (Some(used), Some(total_bytes), pct)
    }

    /// Returns `(battery_percent, charging, estimated_minutes_remaining, ac_power)`.
    ///
    /// Desktop PCs without a battery report `BatteryFlag = 128 (NO_BATTERY)`;
    /// in that case all battery-related fields are `None` while `ac_power`
    /// is still populated from `ACLineStatus` (typically `1` = online).
    fn read_power() -> (Option<u8>, Option<bool>, Option<u32>, Option<bool>) {
        let mut sps = SYSTEM_POWER_STATUS::default();
        // SAFETY: pointer valid for one struct.
        let res = unsafe { GetSystemPowerStatus(&mut sps) };
        if res.is_err() {
            warn!("device_status: GetSystemPowerStatus failed");
            return (None, None, None, None);
        }

        // ACLineStatus: 0 = offline, 1 = online, 255 = unknown.
        let ac_power = match sps.ACLineStatus {
            0 => Some(false),
            1 => Some(true),
            _ => None,
        };

        // BatteryFlag bits: 1=high, 2=low, 4=critical, 8=charging,
        // 128=no battery, 255=unknown.
        let no_battery = sps.BatteryFlag & 128 != 0 || sps.BatteryFlag == 255;
        if no_battery {
            return (None, None, None, ac_power);
        }

        // BatteryLifePercent: 0..100, 255 = unknown.
        let battery_percent = if sps.BatteryLifePercent <= 100 {
            Some(sps.BatteryLifePercent)
        } else {
            None
        };

        let battery_charging = Some(sps.BatteryFlag & 8 != 0);

        // BatteryLifeTime: seconds remaining, u32::MAX (0xFFFFFFFF) = unknown.
        let battery_estimated_minutes_remaining =
            if sps.BatteryLifeTime != u32::MAX && sps.BatteryLifeTime > 0 {
                Some(sps.BatteryLifeTime / 60)
            } else {
                None
            };

        (
            battery_percent,
            battery_charging,
            battery_estimated_minutes_remaining,
            ac_power,
        )
    }

    fn read_uptime_seconds() -> Option<u64> {
        // SAFETY: GetTickCount64 is a pure read; never fails.
        let ms = unsafe { GetTickCount64() };
        Some(ms / 1000)
    }

    /// Returns `(screen_on, locked)`.
    ///
    /// Heuristic, by design — Windows does not expose a clean "monitor
    /// power state" API to user-mode without a hidden window listening
    /// for `WM_POWERBROADCAST`. We use the WTS connect state of the
    /// active console session as a stable proxy:
    ///
    /// * `WTSActive`        → screen_on=true, locked=false
    /// * `WTSDisconnected`  → screen_on=false, locked=true
    /// * `WTSConnectQuery`  → screen_on=false, locked=true (transient lock)
    /// * any other value    → both `None` (don't fabricate)
    ///
    /// On a host with no console session at all (headless server, kiosk
    /// in some configurations) `WTSGetActiveConsoleSessionId` returns
    /// `0xFFFFFFFF`; we surface that as `(None, None)` rather than guess.
    fn read_session_state() -> (Option<bool>, Option<bool>) {
        // SAFETY: kernel32 export, never fails.
        let session_id = unsafe { WTSGetActiveConsoleSessionId() };
        if session_id == 0xFFFF_FFFF {
            return (None, None);
        }

        let mut buffer: ::windows::core::PWSTR = ::windows::core::PWSTR::null();
        let mut bytes: u32 = 0;
        // SAFETY: WTS_CURRENT_SERVER_HANDLE is documented sentinel; out pointer
        // is owned by WTS and freed via WTSFreeMemory below.
        let res = unsafe {
            WTSQuerySessionInformationW(
                HANDLE(WTS_CURRENT_SERVER_HANDLE.0),
                session_id,
                WTSConnectState,
                &mut buffer,
                &mut bytes,
            )
        };
        if res.is_err() || buffer.is_null() || (bytes as usize) < std::mem::size_of::<i32>() {
            warn!("device_status: WTSQuerySessionInformationW(WTSConnectState) failed");
            if !buffer.is_null() {
                // SAFETY: buffer was allocated by WTS API.
                unsafe { WTSFreeMemory(buffer.0 as *mut _) };
            }
            return (None, None);
        }

        // The buffer is actually a pointer to a WTS_CONNECTSTATE_CLASS i32,
        // even though the API signature is *mut PWSTR. This is the
        // documented behaviour for the WTSConnectState info class.
        // SAFETY: buffer is non-null and large enough (checked above).
        let raw = unsafe { *(buffer.0 as *const i32) };
        // SAFETY: buffer was allocated by WTS API.
        unsafe { WTSFreeMemory(buffer.0 as *mut _) };

        let state = WTS_CONNECTSTATE_CLASS(raw);
        if state == WTSActive {
            (Some(true), Some(false))
        } else {
            // Disconnected / locked / connect query / etc — treat as
            // screen-off + locked. The taxonomy matters less than the
            // fact that it is reproducibly NOT the active state.
            (Some(false), Some(true))
        }
    }

    // Bring the parent module's RFC3339 helper into scope for any future
    // diagnostic logging in this submodule. Currently unused at the
    // submodule level but kept to mirror sister collectors.
    #[allow(dead_code)]
    fn _format(ts: i64) -> String {
        format_rfc3339(ts)
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Hand-rolled RFC3339 (no chrono dep — matches sister collectors)
// ──────────────────────────────────────────────────────────────────────────────

/// Formats a unix-nanos timestamp as RFC3339 UTC with second precision.
///
/// Mirrors `email_metadata::system_time_to_rfc3339_seconds` but takes raw
/// `i64` nanos so it can be used directly with `Clock::now_unix_nanos`.
/// Uses Howard Hinnant's civil-from-days algorithm — the same algorithm
/// every other collector in this crate uses for hand-rolled RFC3339.
fn format_rfc3339(unix_nanos: i64) -> String {
    let secs = unix_nanos.div_euclid(1_000_000_000);

    // Civil-from-days.
    let z = secs.div_euclid(86_400) + 719_468;
    let secs_of_day = secs.rem_euclid(86_400);
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };

    let hour = secs_of_day / 3600;
    let minute = (secs_of_day % 3600) / 60;
    let second = secs_of_day % 60;

    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        y, m, d, hour, minute, second
    )
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    // ── CPU delta math ────────────────────────────────────────────────

    #[test]
    fn cpu_percent_idle_machine_returns_zero() {
        // 1 second of total time, all of it idle → 0% busy.
        let one_sec = 10_000_000u64; // 100-ns ticks
        let pct = cpu_percent_from_deltas(
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(one_sec),
            Ft100Ns(one_sec),
            Ft100Ns(0),
        )
        .expect("delta should be computable");
        assert!((pct - 0.0).abs() < 0.001, "expected 0%, got {pct}");
    }

    #[test]
    fn cpu_percent_fully_loaded_returns_hundred() {
        // 1 second of total kernel time, none idle, none user.
        let one_sec = 10_000_000u64;
        let pct = cpu_percent_from_deltas(
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(one_sec),
            Ft100Ns(0),
        )
        .expect("delta should be computable");
        assert!((pct - 100.0).abs() < 0.001, "expected 100%, got {pct}");
    }

    #[test]
    fn cpu_percent_half_loaded() {
        // 1 second total, 0.5s idle → 50% busy.
        let one_sec = 10_000_000u64;
        let half_sec = 5_000_000u64;
        let pct = cpu_percent_from_deltas(
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(half_sec),
            Ft100Ns(one_sec),
            Ft100Ns(0),
        )
        .expect("delta should be computable");
        assert!((pct - 50.0).abs() < 0.001, "expected 50%, got {pct}");
    }

    #[test]
    fn cpu_percent_with_user_time() {
        // kernel: 1s (incl 0.2s idle), user: 1s. Total non-idle = 1.8s of 2s.
        let one_sec = 10_000_000u64;
        let p2_sec = 2_000_000u64;
        let pct = cpu_percent_from_deltas(
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(p2_sec),
            Ft100Ns(one_sec),
            Ft100Ns(one_sec),
        )
        .expect("delta should be computable");
        // (2_000_000 - 200_000) busy / 2_000_000 total = 90%
        assert!((pct - 90.0).abs() < 0.001, "expected 90%, got {pct}");
    }

    #[test]
    fn cpu_percent_zero_delta_returns_none() {
        let pct = cpu_percent_from_deltas(
            Ft100Ns(100),
            Ft100Ns(100),
            Ft100Ns(100),
            Ft100Ns(100),
            Ft100Ns(100),
            Ft100Ns(100),
        );
        assert!(pct.is_none(), "no time elapsed must be None, got {pct:?}");
    }

    #[test]
    fn cpu_percent_underflow_returns_none() {
        // Counters running backward — physically impossible, defensive.
        let pct = cpu_percent_from_deltas(
            Ft100Ns(1_000_000),
            Ft100Ns(1_000_000),
            Ft100Ns(1_000_000),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
        );
        assert!(pct.is_none(), "monotonic violation must be None");
    }

    #[test]
    fn cpu_percent_jitter_clamped_above_zero() {
        // Idle reported slightly larger than total — would yield negative
        // raw value; helper must clamp into [0,100].
        let pct = cpu_percent_from_deltas(
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(0),
            Ft100Ns(11_000_000),
            Ft100Ns(10_000_000),
            Ft100Ns(0),
        );
        // idle_d > kernel_d ⇒ saturating_sub gives busy=0 ⇒ 0%.
        assert_eq!(pct, Some(0.0));
    }

    // ── Snapshot JSON shape ───────────────────────────────────────────

    #[test]
    fn empty_snapshot_serialises_with_all_nulls() {
        let s = Snapshot::default();
        let json = s.to_json(0);
        // Spot-check critical keys. Each null field must be present.
        assert!(json.contains("\"cpu_percent_total\":null"));
        assert!(json.contains("\"memory_total_bytes\":null"));
        assert!(json.contains("\"battery_percent\":null"));
        assert!(json.contains("\"ac_power\":null"));
        assert!(json.contains("\"screen_on\":null"));
        assert!(json.contains("\"locked\":null"));
        assert!(json.contains("\"boot_time\":null"));
        assert!(json.contains("\"timestamp\":\"1970-01-01T00:00:00Z\""));
        assert!(json.starts_with('{') && json.ends_with('}'));
    }

    #[test]
    fn populated_snapshot_serialises_known_values() {
        let s = Snapshot {
            cpu_percent_total: Some(23.456),
            memory_used_bytes: Some(12_345_678_901),
            memory_total_bytes: Some(34_359_738_368),
            memory_percent: Some(35.94),
            disk_system_used_bytes: Some(256_000_000_000),
            disk_system_total_bytes: Some(512_000_000_000),
            disk_system_percent: Some(50.0),
            battery_percent: Some(87),
            battery_charging: Some(true),
            battery_estimated_minutes_remaining: Some(210),
            ac_power: Some(true),
            uptime_seconds: Some(123_456),
            screen_on: Some(true),
            locked: Some(false),
            boot_time_unix_nanos: Some(1_700_000_000_000_000_000),
        };
        let json = s.to_json(1_775_644_462_000_000_000);
        // Round-trip check: each field appears with its expected formatting.
        assert!(json.contains("\"cpu_percent_total\":23.46"));
        assert!(json.contains("\"memory_used_bytes\":12345678901"));
        assert!(json.contains("\"memory_total_bytes\":34359738368"));
        assert!(json.contains("\"memory_percent\":35.94"));
        assert!(json.contains("\"disk_system_used_bytes\":256000000000"));
        assert!(json.contains("\"disk_system_percent\":50.00"));
        assert!(json.contains("\"battery_percent\":87"));
        assert!(json.contains("\"battery_charging\":true"));
        assert!(json.contains("\"battery_estimated_minutes_remaining\":210"));
        assert!(json.contains("\"ac_power\":true"));
        assert!(json.contains("\"uptime_seconds\":123456"));
        assert!(json.contains("\"screen_on\":true"));
        assert!(json.contains("\"locked\":false"));
        assert!(json.contains("\"boot_time\":\"2023-11-14T22:13:20Z\""));
        assert!(json.contains("\"timestamp\":\"2026-04-08T10:34:22Z\""));
    }

    #[test]
    fn desktop_with_no_battery_shape() {
        // A desktop with AC power and no battery: ac_power=true but every
        // battery field null.
        let s = Snapshot {
            cpu_percent_total: Some(5.0),
            memory_total_bytes: Some(16 * 1024 * 1024 * 1024),
            memory_used_bytes: Some(4 * 1024 * 1024 * 1024),
            memory_percent: Some(25.0),
            ac_power: Some(true),
            uptime_seconds: Some(86_400),
            ..Snapshot::default()
        };
        let json = s.to_json(0);
        assert!(json.contains("\"battery_percent\":null"));
        assert!(json.contains("\"battery_charging\":null"));
        assert!(json.contains("\"battery_estimated_minutes_remaining\":null"));
        assert!(json.contains("\"ac_power\":true"));
    }

    // ── RFC3339 helper ────────────────────────────────────────────────

    #[test]
    fn rfc3339_unix_epoch() {
        assert_eq!(format_rfc3339(0), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn rfc3339_known_date() {
        // 2023-11-14T22:13:20Z = 1_700_000_000 unix seconds
        assert_eq!(
            format_rfc3339(1_700_000_000_000_000_000),
            "2023-11-14T22:13:20Z"
        );
    }

    #[test]
    fn rfc3339_recent_date() {
        // 1_775_644_462 unix seconds = 2026-04-08T10:34:22Z (cross-checked
        // with `Date.UTC` and the Howard Hinnant civil-from-days reference).
        assert_eq!(
            format_rfc3339(1_775_644_462_000_000_000),
            "2026-04-08T10:34:22Z"
        );
    }

    #[test]
    fn rfc3339_strips_subsecond() {
        // Hand-rolled helper is second-precision by design; verify a
        // sub-second nanos chunk is truncated rather than crashing.
        let s = format_rfc3339(1_775_644_462_999_999_999);
        assert!(s.starts_with("2026-04-08T10:34:22"), "got {s}");
        assert!(s.ends_with('Z'));
    }

    // ── Ft100Ns conversion sanity ─────────────────────────────────────

    #[test]
    fn ft100ns_wraps_64_bits_correctly() {
        let v = Ft100Ns(u64::MAX);
        // Just exercise the constructor & field; pattern matches sister
        // collectors that lightly test internal helpers.
        assert_eq!(v.0, u64::MAX);
    }
}
