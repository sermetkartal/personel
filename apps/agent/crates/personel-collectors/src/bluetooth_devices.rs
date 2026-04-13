//! Bluetooth paired device inventory + diff collector (Faz 2 Wave 3 — #15).
//!
//! Polls the Windows Bluetooth stack every 30 seconds via
//! `BluetoothFindFirstDevice` / `BluetoothFindNextDevice` for **paired**
//! (authenticated + remembered) devices, computes a set diff against the
//! previous snapshot, and emits:
//!
//! - `bluetooth.device_paired`   when a Bluetooth address appears for the
//!   first time in the paired set
//! - `bluetooth.device_unpaired` when an address that was paired on the
//!   previous tick is no longer present
//!
//! # Baseline policy
//!
//! The very first poll **silently baselines** — every paired device is
//! recorded as already-known and produces no events. Diff events only fire
//! starting from the *second* poll. This avoids a noisy "everything just
//! paired" burst at agent startup.
//!
//! # KVKK
//!
//! Bluetooth MAC addresses are device identifiers, not personal content.
//! `szName` may contain a user-chosen label ("Ahmet'in iPhone'u") so it is
//! treated as identifier metadata, not sensitive content. No pairing keys,
//! link keys, or service data are touched.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: real `bluetoothapis.dll` enumeration.
//! Non-Windows: parks gracefully (`healthy=true`, no events) so `cargo
//! check` passes on macOS/Linux dev builds.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use personel_core::error::Result;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::EventId;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Poll cadence: every 30 seconds.
const POLL_INTERVAL: Duration = Duration::from_secs(30);

/// Bluetooth paired-device collector.
#[derive(Default)]
pub struct BluetoothDevicesCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl BluetoothDevicesCollector {
    /// Creates a new [`BluetoothDevicesCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for BluetoothDevicesCollector {
    fn name(&self) -> &'static str {
        "bluetooth_devices"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["bluetooth.device_paired", "bluetooth.device_unpaired"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, mut stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::spawn(async move {
            // Previous-tick paired set: address (u64) → cached display info.
            // Cached so unpair events can include the human-readable name
            // even though the device is gone from the OS at unpair time.
            let mut previous: std::collections::HashMap<u64, BtDevice> =
                std::collections::HashMap::new();
            let mut baselined = false;

            let mut ticker = tokio::time::interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

            info!("bluetooth_devices collector: started");

            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        match enumerate_paired_devices() {
                            Ok(current_list) => {
                                healthy.store(true, Ordering::Relaxed);

                                let current: std::collections::HashMap<u64, BtDevice> =
                                    current_list.into_iter().map(|d| (d.address, d)).collect();

                                if !baselined {
                                    debug!(
                                        count = current.len(),
                                        "bluetooth_devices: silent baseline (no events emitted)"
                                    );
                                    previous = current;
                                    baselined = true;
                                    continue;
                                }

                                // Newly paired: in current, not in previous.
                                for (addr, dev) in &current {
                                    if !previous.contains_key(addr) {
                                        let payload = build_payload(dev, &ctx);
                                        emit_event(
                                            &ctx,
                                            EventKind::BluetoothDevicePaired,
                                            &payload,
                                            &events,
                                            &drops,
                                        );
                                        debug!(
                                            address = %dev.address_str,
                                            name = %dev.name,
                                            "bluetooth device paired"
                                        );
                                    }
                                }

                                // Unpaired: in previous, not in current.
                                for (addr, dev) in &previous {
                                    if !current.contains_key(addr) {
                                        let payload = build_payload(dev, &ctx);
                                        emit_event(
                                            &ctx,
                                            EventKind::BluetoothDeviceUnpaired,
                                            &payload,
                                            &events,
                                            &drops,
                                        );
                                        debug!(
                                            address = %dev.address_str,
                                            name = %dev.name,
                                            "bluetooth device unpaired"
                                        );
                                    }
                                }

                                previous = current;
                            }
                            Err(BtError::NoHardwareOrEmpty) => {
                                // No radio installed OR no paired devices —
                                // both are perfectly valid steady states.
                                healthy.store(true, Ordering::Relaxed);
                                if !baselined {
                                    baselined = true;
                                    debug!("bluetooth_devices: empty baseline");
                                }
                                // If devices were previously known but the
                                // radio was disabled, treat them as unpaired
                                // events so audit reflects the disappearance.
                                if !previous.is_empty() {
                                    for dev in previous.values() {
                                        let payload = build_payload(dev, &ctx);
                                        emit_event(
                                            &ctx,
                                            EventKind::BluetoothDeviceUnpaired,
                                            &payload,
                                            &events,
                                            &drops,
                                        );
                                    }
                                    previous.clear();
                                }
                            }
                            Err(BtError::Unsupported) => {
                                healthy.store(true, Ordering::Relaxed);
                                info!(
                                    "bluetooth_devices: bluetoothapis.dll not available — parking"
                                );
                                let _ = (&mut stop_rx).await;
                                break;
                            }
                            Err(BtError::Os(msg)) => {
                                warn!("bluetooth_devices: enumeration error: {msg}");
                                healthy.store(false, Ordering::Relaxed);
                            }
                        }
                    }
                    _ = &mut stop_rx => {
                        info!("bluetooth_devices collector: stop requested");
                        break;
                    }
                }
            }
        });

        self.healthy.store(true, Ordering::Relaxed);
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
// Cross-platform data model
// ──────────────────────────────────────────────────────────────────────────────

/// One paired Bluetooth device snapshot.
#[derive(Debug, Clone)]
struct BtDevice {
    /// Native 48-bit BD_ADDR packed into the low 6 bytes of a u64.
    address: u64,
    /// Pretty-printed `XX:XX:XX:XX:XX:XX` form (uppercase, MSB-first).
    address_str: String,
    /// Friendly name from `szName`, lossy UTF-16 decoded, NUL-trimmed.
    name: String,
    /// Class of Device (24-bit COD field).
    class_of_device: u32,
    /// Coarse classification of `class_of_device` (`audio`, `peripheral`, …).
    device_class: &'static str,
    /// `fConnected` flag at the time of the snapshot.
    connected: bool,
    /// `stLastSeen` rendered as RFC3339 (UTC) when non-zero, else `None`.
    last_seen: Option<String>,
}

/// Enumeration error that `enumerate_paired_devices` can return.
#[derive(Debug)]
enum BtError {
    /// Bluetooth APIs are unavailable on this platform (non-Windows build).
    /// Only constructed by the non-Windows stub path; suppress the dead-
    /// code warning when building on Windows.
    #[allow(dead_code)]
    Unsupported,
    /// No paired devices OR no Bluetooth radio installed (steady state).
    NoHardwareOrEmpty,
    /// Any other Win32 error — string is human-readable.
    Os(String),
}

// ──────────────────────────────────────────────────────────────────────────────
// Pure helpers (cross-platform, unit-tested)
// ──────────────────────────────────────────────────────────────────────────────

/// Maps a 24-bit Class of Device (COD) value to a coarse category string.
///
/// Looks at the Major Device Class field (bits 12..=8 of the COD). Per the
/// Bluetooth SIG Assigned Numbers — Baseband:
///
/// | Major class | Hex pattern | Category    |
/// |-------------|-------------|-------------|
/// | Computer    | `0x01xxxx`  | `computer`  |
/// | Phone       | `0x02xxxx`  | `phone`     |
/// | Audio/Video | `0x04xxxx`  | `audio`     |
/// | Peripheral  | `0x05xxxx`  | `peripheral`|
/// | Other       | anything    | `other`     |
fn classify_cod(cod: u32) -> &'static str {
    let major = (cod >> 8) & 0x1F;
    match major {
        0x01 => "computer",
        0x02 => "phone",
        0x04 => "audio",
        0x05 => "peripheral",
        _ => "other",
    }
}

/// Formats a 48-bit Bluetooth address (packed low-6-bytes of `u64`,
/// little-endian byte order) as a canonical `XX:XX:XX:XX:XX:XX` string.
///
/// Windows reports the address as `ullLong` where the low byte is the LSB
/// of the BD_ADDR; the printed form puts the most-significant byte first.
fn format_addr(addr: u64) -> String {
    let b0 = ((addr >> 40) & 0xFF) as u8;
    let b1 = ((addr >> 32) & 0xFF) as u8;
    let b2 = ((addr >> 24) & 0xFF) as u8;
    let b3 = ((addr >> 16) & 0xFF) as u8;
    let b4 = ((addr >> 8) & 0xFF) as u8;
    let b5 = (addr & 0xFF) as u8;
    format!("{b0:02X}:{b1:02X}:{b2:02X}:{b3:02X}:{b4:02X}:{b5:02X}")
}

/// Internal SYSTEMTIME shim used by `systemtime_to_rfc3339` so the helper
/// can be unit-tested without depending on the `windows` crate on
/// non-Windows platforms.
#[derive(Debug, Clone, Copy)]
struct SystemTimeShim {
    year: u16,
    month: u16,
    day: u16,
    hour: u16,
    minute: u16,
    second: u16,
    millis: u16,
}

impl SystemTimeShim {
    /// `true` if the SYSTEMTIME is the all-zeros sentinel that Windows uses
    /// for "never seen" / "never used".
    fn is_zero(self) -> bool {
        self.year == 0
            && self.month == 0
            && self.day == 0
            && self.hour == 0
            && self.minute == 0
            && self.second == 0
            && self.millis == 0
    }
}

/// Renders a Windows SYSTEMTIME (UTC) as an RFC3339 string.
///
/// Returns `None` if the SYSTEMTIME is the zero sentinel ("never seen") or
/// if any field is out of range (the Bluetooth stack occasionally returns
/// uninitialised structs for devices it has never connected to).
///
/// The renderer is intentionally permissive: it does *not* validate that
/// the date is real (e.g., Feb 31). The downstream consumer treats the
/// timestamp as informational metadata, not as an authoritative event time.
fn systemtime_to_rfc3339(st: SystemTimeShim) -> Option<String> {
    if st.is_zero() {
        return None;
    }
    if st.month == 0
        || st.month > 12
        || st.day == 0
        || st.day > 31
        || st.hour > 23
        || st.minute > 59
        || st.second > 59
    {
        return None;
    }
    Some(format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:03}Z",
        st.year, st.month, st.day, st.hour, st.minute, st.second, st.millis
    ))
}

/// Builds the JSON payload for a paired/unpaired event.
fn build_payload(dev: &BtDevice, ctx: &CollectorCtx) -> String {
    let now_nanos = ctx.clock.now_unix_nanos();
    let now_rfc3339 = nanos_to_rfc3339(now_nanos);
    let last_seen_field = match &dev.last_seen {
        Some(s) => format!("{s:?}"),
        None => "null".to_string(),
    };
    format!(
        r#"{{"address":{:?},"name":{:?},"device_class":{:?},"class_of_device":"0x{:06X}","last_seen":{},"connected":{},"timestamp":{:?}}}"#,
        dev.address_str,
        dev.name,
        dev.device_class,
        dev.class_of_device,
        last_seen_field,
        dev.connected,
        now_rfc3339,
    )
}

/// Best-effort RFC3339 rendering of a unix-nanos timestamp. Falls back to
/// the empty string on overflow — the queue still accepts the event.
fn nanos_to_rfc3339(nanos: i64) -> String {
    if nanos <= 0 {
        return String::new();
    }
    let secs = nanos / 1_000_000_000;
    let sub_ns = (nanos % 1_000_000_000) as u32;
    // Convert to UTC components without depending on `chrono`.
    let days = secs / 86_400;
    let mut rem_secs = secs % 86_400;
    let hour = rem_secs / 3600;
    rem_secs %= 3600;
    let minute = rem_secs / 60;
    let second = rem_secs % 60;

    // Civil-from-days algorithm (Howard Hinnant, public domain).
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = if m <= 2 { y + 1 } else { y };

    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:03}Z",
        year,
        m,
        d,
        hour,
        minute,
        second,
        sub_ns / 1_000_000,
    )
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
fn enumerate_paired_devices() -> std::result::Result<Vec<BtDevice>, BtError> {
    win::enumerate_paired_devices_impl()
}

#[cfg(not(target_os = "windows"))]
fn enumerate_paired_devices() -> std::result::Result<Vec<BtDevice>, BtError> {
    Err(BtError::Unsupported)
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod win {
    use super::{
        classify_cod, format_addr, systemtime_to_rfc3339, BtDevice, BtError, SystemTimeShim,
    };

    use windows::Win32::Devices::Bluetooth::{
        BluetoothFindDeviceClose, BluetoothFindFirstDevice, BluetoothFindNextDevice,
        BLUETOOTH_DEVICE_INFO, BLUETOOTH_DEVICE_SEARCH_PARAMS,
    };
    use windows::Win32::Foundation::{BOOL, HANDLE, SYSTEMTIME};

    /// Real implementation: enumerate paired devices via bluetoothapis.dll.
    ///
    /// Translates "no paired devices" / "no radio installed" into
    /// `BtError::NoHardwareOrEmpty` so the calling collector can treat
    /// those steady states as healthy.
    pub(super) fn enumerate_paired_devices_impl() -> std::result::Result<Vec<BtDevice>, BtError> {
        let mut search_params = BLUETOOTH_DEVICE_SEARCH_PARAMS {
            dwSize: std::mem::size_of::<BLUETOOTH_DEVICE_SEARCH_PARAMS>() as u32,
            fReturnAuthenticated: BOOL(1),
            fReturnRemembered: BOOL(1),
            fReturnUnknown: BOOL(0),
            fReturnConnected: BOOL(1),
            fIssueInquiry: BOOL(0),
            cTimeoutMultiplier: 0,
            hRadio: HANDLE::default(),
        };
        let mut info = BLUETOOTH_DEVICE_INFO {
            dwSize: std::mem::size_of::<BLUETOOTH_DEVICE_INFO>() as u32,
            ..Default::default()
        };

        let h_find = unsafe { BluetoothFindFirstDevice(&search_params, &mut info) };
        let h_find = match h_find {
            Ok(h) => h,
            Err(e) => {
                // ERROR_NO_MORE_ITEMS (259) → empty paired set.
                // ERROR_INVALID_PARAMETER from missing radio also lands here.
                let code = e.code().0 as u32;
                if code == 0x8007_0103 // HRESULT_FROM_WIN32(ERROR_NO_MORE_ITEMS)
                    || code == 0x8007_0057 // HRESULT_FROM_WIN32(ERROR_INVALID_PARAMETER)
                    || code == 0x8007_007A // HRESULT_FROM_WIN32(ERROR_INSUFFICIENT_BUFFER)
                    || code == 0x8007_0490
                // HRESULT_FROM_WIN32(ERROR_NOT_FOUND)
                {
                    return Err(BtError::NoHardwareOrEmpty);
                }
                return Err(BtError::Os(format!("BluetoothFindFirstDevice: {e}")));
            }
        };

        let mut out = Vec::new();
        out.push(decode_device(&info));

        loop {
            let mut next = BLUETOOTH_DEVICE_INFO {
                dwSize: std::mem::size_of::<BLUETOOTH_DEVICE_INFO>() as u32,
                ..Default::default()
            };
            let step = unsafe { BluetoothFindNextDevice(h_find, &mut next) };
            match step {
                Ok(()) => out.push(decode_device(&next)),
                Err(e) => {
                    let code = e.code().0 as u32;
                    if code == 0x8007_0103 {
                        // ERROR_NO_MORE_ITEMS — clean end of enumeration.
                        break;
                    }
                    // Close handle then surface the error.
                    let _ = unsafe { BluetoothFindDeviceClose(h_find) };
                    return Err(BtError::Os(format!("BluetoothFindNextDevice: {e}")));
                }
            }
        }

        let _ = unsafe { BluetoothFindDeviceClose(h_find) };
        let _ = &mut search_params; // keep alive across the call
        Ok(out)
    }

    fn decode_device(info: &BLUETOOTH_DEVICE_INFO) -> BtDevice {
        // Read the BLUETOOTH_ADDRESS union — `ullLong` is the canonical
        // 64-bit packed form (low 6 bytes hold the BD_ADDR).
        let addr_u64 = unsafe { info.Address.Anonymous.ullLong };

        // szName is a fixed [u16; 248] NUL-terminated wide string.
        let name_slice = &info.szName[..];
        let nul = name_slice.iter().position(|&c| c == 0).unwrap_or(name_slice.len());
        let name = String::from_utf16_lossy(&name_slice[..nul]);

        let st_shim = systemtime_to_shim(info.stLastSeen);

        BtDevice {
            address: addr_u64,
            address_str: format_addr(addr_u64),
            name,
            class_of_device: info.ulClassofDevice,
            device_class: classify_cod(info.ulClassofDevice),
            connected: info.fConnected.as_bool(),
            last_seen: systemtime_to_rfc3339(st_shim),
        }
    }

    fn systemtime_to_shim(st: SYSTEMTIME) -> SystemTimeShim {
        SystemTimeShim {
            year: st.wYear,
            month: st.wMonth,
            day: st.wDay,
            hour: st.wHour,
            minute: st.wMinute,
            second: st.wSecond,
            millis: st.wMilliseconds,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Event emit
// ──────────────────────────────────────────────────────────────────────────────

fn emit_event(
    ctx: &CollectorCtx,
    kind: EventKind,
    payload: &str,
    events: &Arc<AtomicU64>,
    drops: &Arc<AtomicU64>,
) {
    let now = ctx.clock.now_unix_nanos();
    let id = EventId::new_v7().to_bytes();
    match ctx.queue.enqueue(
        &id,
        kind.as_str(),
        Priority::Normal,
        now,
        now,
        payload.as_bytes(),
    ) {
        Ok(_) => {
            events.fetch_add(1, Ordering::Relaxed);
        }
        Err(e) => {
            error!(error = %e, "bluetooth_devices: queue error");
            drops.fetch_add(1, Ordering::Relaxed);
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests (pure logic, runnable on every host)
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn st(y: u16, m: u16, d: u16, h: u16, mi: u16, s: u16, ms: u16) -> SystemTimeShim {
        SystemTimeShim { year: y, month: m, day: d, hour: h, minute: mi, second: s, millis: ms }
    }

    #[test]
    fn classify_cod_audio() {
        // AirPods-style audio headset: 0x240404 → major class 0x04 → audio.
        assert_eq!(classify_cod(0x0024_0404), "audio");
        assert_eq!(classify_cod(0x0000_0418), "audio");
    }

    #[test]
    fn classify_cod_peripheral_kbd_mouse() {
        // Major class 0x05 (peripheral) — keyboard, mouse, joystick.
        assert_eq!(classify_cod(0x0000_2540), "peripheral");
        assert_eq!(classify_cod(0x0000_0580), "peripheral");
    }

    #[test]
    fn classify_cod_phone() {
        // Major class 0x02 — smartphone.
        assert_eq!(classify_cod(0x0058_020C), "phone");
    }

    #[test]
    fn classify_cod_computer() {
        // Major class 0x01 — laptop.
        assert_eq!(classify_cod(0x0010_010C), "computer");
    }

    #[test]
    fn classify_cod_other_unknown_major() {
        assert_eq!(classify_cod(0x0000_0000), "other");
        // Major class 0x06 (imaging) → not in our coarse map → other.
        assert_eq!(classify_cod(0x0000_0680), "other");
        // Major class 0x07 (wearable) → other.
        assert_eq!(classify_cod(0x0000_0704), "other");
    }

    #[test]
    fn format_addr_canonical_msb_first() {
        // BD_ADDR AA:BB:CC:DD:EE:FF packed LSB-first into ullLong.
        // ullLong layout: byte0=FF, byte1=EE, …, byte5=AA → high bytes of
        // the u64 carry the most significant address bytes when shifted.
        let packed: u64 = 0x0000_AABB_CCDD_EEFF;
        assert_eq!(format_addr(packed), "AA:BB:CC:DD:EE:FF");
    }

    #[test]
    fn format_addr_zero() {
        assert_eq!(format_addr(0), "00:00:00:00:00:00");
    }

    #[test]
    fn format_addr_pads_low_bytes() {
        // 0x01:02:03:04:05:06 must pad each byte to two hex digits.
        let packed: u64 = 0x0000_0102_0304_0506;
        assert_eq!(format_addr(packed), "01:02:03:04:05:06");
    }

    #[test]
    fn systemtime_zero_returns_none() {
        assert!(systemtime_to_rfc3339(st(0, 0, 0, 0, 0, 0, 0)).is_none());
    }

    #[test]
    fn systemtime_normal_renders_rfc3339() {
        // 2026-04-13 14:23:45.678 UTC
        let out = systemtime_to_rfc3339(st(2026, 4, 13, 14, 23, 45, 678))
            .expect("non-zero SYSTEMTIME must render");
        assert_eq!(out, "2026-04-13T14:23:45.678Z");
    }

    #[test]
    fn systemtime_pads_single_digit_fields() {
        let out = systemtime_to_rfc3339(st(2024, 1, 5, 3, 7, 9, 4))
            .expect("non-zero SYSTEMTIME must render");
        assert_eq!(out, "2024-01-05T03:07:09.004Z");
    }

    #[test]
    fn systemtime_invalid_month_returns_none() {
        assert!(systemtime_to_rfc3339(st(2026, 13, 1, 0, 0, 0, 0)).is_none());
        assert!(systemtime_to_rfc3339(st(2026, 0, 1, 0, 0, 0, 0)).is_none());
    }

    #[test]
    fn systemtime_invalid_hour_returns_none() {
        assert!(systemtime_to_rfc3339(st(2026, 4, 13, 24, 0, 0, 0)).is_none());
    }

    #[test]
    fn systemtime_invalid_day_returns_none() {
        assert!(systemtime_to_rfc3339(st(2026, 4, 0, 12, 0, 0, 0)).is_none());
        assert!(systemtime_to_rfc3339(st(2026, 4, 32, 12, 0, 0, 0)).is_none());
    }

    #[test]
    fn nanos_to_rfc3339_epoch() {
        // 2026-04-13 00:00:00 UTC = 1_775_001_600 unix seconds.
        let out = nanos_to_rfc3339(1_775_001_600 * 1_000_000_000);
        assert_eq!(out, "2026-04-13T00:00:00.000Z");
    }

    #[test]
    fn nanos_to_rfc3339_negative_is_empty() {
        assert!(nanos_to_rfc3339(-1).is_empty());
        assert!(nanos_to_rfc3339(0).is_empty());
    }
}
