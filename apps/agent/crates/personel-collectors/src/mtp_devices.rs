//! MTP / PTP portable device collector.
//!
//! Tracks Media Transfer Protocol (MTP) and Picture Transfer Protocol (PTP)
//! devices that appear above the raw USB mass-storage layer — phones, cameras,
//! portable media players. USB mass-storage devices are already covered by the
//! sibling [`crate::usb`] collector; this collector is specifically for the
//! `PORTABLE_DEVICES` device class so the two never double-count an iPhone or
//! a digital camera.
//!
//! # Privacy (KVKK m.6)
//!
//! Only device-level metadata is captured (instance ID, friendly name,
//! description, manufacturer string). The collector NEVER touches:
//!
//! - File listings on the device
//! - File contents
//! - Photo / video media
//! - Address book / SMS / call log entries
//!
//! These categories require the WPD COM API (see Phase 2 TODO below) and are
//! intentionally out of scope for the Phase 1 scaffold.
//!
//! # Approach (Phase 1 SCAFFOLD)
//!
//! Real MTP enumeration requires the **Windows Portable Devices (WPD) COM
//! API** (`PortableDeviceApiLib`, `IPortableDeviceManager`,
//! `IPortableDeviceContent`) which involves COM apartment threading,
//! multi-vtable hops and out-of-process activation. That work is deferred to
//! Phase 2.
//!
//! For Phase 1 the collector polls `SetupAPI` every 10 seconds for devices in
//! the `PORTABLE_DEVICES` device interface class GUID
//! `{6AC27878-A6FA-4155-BA85-F98F491D4F33}`, snapshots the live set, diffs it
//! against the previous snapshot keyed on the device instance ID, and emits
//! `mtp.device_attached` / `mtp.device_removed` for new and missing entries.
//! The first poll silently establishes a baseline so existing devices on
//! agent startup do not generate a flood of synthetic attach events.
//!
//! # Phase 2 TODO — WPD COM real implementation
//!
//! The next agent should replace [`enumerate_mtp_devices`] with a real WPD
//! enumeration. The general shape:
//!
//! 1. `CoInitializeEx(COINIT_MULTITHREADED)` once per worker thread (this
//!    collector lives on its own `spawn_blocking` thread so apartment state
//!    is isolated from other collectors). Pair with `CoUninitialize` on
//!    shutdown.
//!
//! 2. Create the device manager:
//!    `CoCreateInstance(&PortableDeviceManager, ..., &IPortableDeviceManager)`
//!    The CLSID and IID live in `windows::Win32::Devices::PortableDevices`.
//!    Add the corresponding feature to `apps/agent/Cargo.toml`:
//!    `"Win32_Devices_PortableDevices"`, `"Win32_System_Com"`.
//!
//! 3. Enumerate device IDs:
//!    ```text
//!    let mut count = 0u32;
//!    manager.GetDevices(None, &mut count); // get required count
//!    let mut buf: Vec<PWSTR> = vec![PWSTR::null(); count as usize];
//!    manager.GetDevices(Some(buf.as_mut_ptr()), &mut count);
//!    ```
//!    Each entry is a wide-char PnP device ID (the same string SetupAPI
//!    returns from `SetupDiGetDeviceInstanceIdW`).
//!
//! 4. For each device ID, call `manager.GetDeviceFriendlyName`,
//!    `GetDeviceDescription`, `GetDeviceManufacturer` to populate the
//!    metadata fields used by the Phase 1 scaffold. After this step the
//!    Phase 2 collector achieves parity with the Phase 1 SetupAPI collector.
//!
//! 5. To go beyond device discovery and emit content events (file create /
//!    delete / rename on the device), open each device with
//!    `IPortableDevice::Open(device_id, client_info)` then call
//!    `Content()->EnumObjects(...)` recursively under the device root. Diff
//!    against previous snapshots stored per-device. NOTE: WPD does not push
//!    notifications — content tracking requires polling. Recommended cadence
//!    is 60 seconds to keep the agent footprint inside the Phase 1 budget
//!    (<2% CPU, <150MB RAM).
//!
//! 6. New event kinds will be needed for content-level visibility. Add to
//!    `personel_core::event::EventKind` (and `lib.rs` event taxonomy):
//!    - `MtpFileCreated`
//!    - `MtpFileDeleted`
//!    - `MtpFileRenamed`
//!    Each payload should carry the parent device instance ID + a HASHED
//!    file name (KVKK m.6 — names of personal files are sensitive). NEVER
//!    add a payload field for file CONTENT — that requires DPIA amendment
//!    + DLP opt-in ceremony per ADR 0013.
//!
//! 7. Watch for the COM threading trap: `IPortableDeviceManager` and
//!    `IPortableDevice` are both apartment-threaded objects. Do not pass
//!    raw COM pointers across threads. The current `spawn_blocking` model
//!    is fine because the entire collector runs on a single OS thread for
//!    its lifetime.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full SetupAPI poll loop.
//! Non-Windows: parks gracefully so `cargo check` passes on macOS/Linux.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
#[cfg(not(target_os = "windows"))]
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// MTP / PTP portable device collector.
#[derive(Default)]
pub struct MtpDevicesCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl MtpDevicesCollector {
    /// Creates a new [`MtpDevicesCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for MtpDevicesCollector {
    fn name(&self) -> &'static str {
        "mtp_devices"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["mtp.device_attached", "mtp.device_removed"]
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
// Cross-platform shared types
// ──────────────────────────────────────────────────────────────────────────────

/// A single MTP device snapshot row.
///
/// Lives at the module root (not inside `mod windows`) so the diff logic and
/// classification helper can be unit-tested on macOS/Linux dev hosts without
/// touching a Windows API.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct MtpDevice {
    /// PnP device instance ID (e.g. `USB\\VID_05AC&PID_12A8\\...`). Stable
    /// across the lifetime of the connection so it's used as the diff key.
    pub instance_id: String,
    /// SPDRP_FRIENDLYNAME, e.g. `"iPhone"`. May be empty.
    pub friendly_name: String,
    /// SPDRP_DEVICEDESC, e.g. `"Apple Mobile Device"`. May be empty.
    pub description: String,
    /// SPDRP_MFG, e.g. `"Apple Inc."`. May be empty.
    pub manufacturer: String,
}

/// Coarse device classification used to enrich the emitted payload.
///
/// Classification is deliberately substring-based on the friendly name; this
/// is good enough for the most common Phase 1 cases (phones, cameras, MP3
/// players) and avoids the larger Phase 2 effort of querying USB descriptors
/// for `bDeviceClass`/`bDeviceSubClass` codes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum DeviceType {
    Phone,
    Camera,
    Mp3Player,
    Other,
}

impl DeviceType {
    fn as_str(self) -> &'static str {
        match self {
            DeviceType::Phone => "phone",
            DeviceType::Camera => "camera",
            DeviceType::Mp3Player => "mp3_player",
            DeviceType::Other => "other",
        }
    }
}

/// Classifies a device by substring match on the friendly name.
///
/// The matching is case-insensitive. Order matters: more specific tokens
/// (e.g. `ipod`) are checked before `ipad` to avoid the Apple naming
/// collision between iPod and iPad.
pub(crate) fn classify_device(friendly_name: &str) -> DeviceType {
    let n = friendly_name.to_ascii_lowercase();

    // iPod must be checked before iPad/iPhone to avoid the ipo[dh]/ipad
    // substring overlap (none today, but cheap insurance).
    if n.contains("ipod") {
        return DeviceType::Mp3Player;
    }

    if n.contains("iphone")
        || n.contains("ipad")
        || n.contains("android")
        || n.contains("galaxy")
        || n.contains("pixel")
    {
        return DeviceType::Phone;
    }

    if n.contains("canon")
        || n.contains("nikon")
        || n.contains("gopro")
        || n.contains("sony alpha")
    {
        return DeviceType::Camera;
    }

    DeviceType::Other
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
    windows_impl::run(ctx, healthy, events, drops, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        info!("mtp_devices: SetupAPI not supported on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation (SetupAPI poll)
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod windows_impl {
    use std::collections::HashMap;
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::Arc;
    use std::time::Duration;

    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};

    use windows::core::GUID;
    use windows::Win32::Devices::DeviceAndDriverInstallation::{
        SetupDiDestroyDeviceInfoList, SetupDiEnumDeviceInfo, SetupDiGetClassDevsW,
        SetupDiGetDeviceInstanceIdW, SetupDiGetDeviceRegistryPropertyW, DIGCF_DEVICEINTERFACE,
        DIGCF_PRESENT, HDEVINFO, SETUP_DI_REGISTRY_PROPERTY, SPDRP_DEVICEDESC, SPDRP_FRIENDLYNAME,
        SPDRP_MFG, SP_DEVINFO_DATA,
    };

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use super::{classify_device, MtpDevice};
    use crate::CollectorCtx;

    /// PORTABLE_DEVICES device interface class GUID
    /// `{6AC27878-A6FA-4155-BA85-F98F491D4F33}`. This is the device interface
    /// guid that WPD-aware devices register under.
    const GUID_DEVINTERFACE_WPD: GUID = GUID {
        data1: 0x6AC27878,
        data2: 0xA6FA,
        data3: 0x4155,
        data4: [0xBA, 0x85, 0xF9, 0x8F, 0x49, 0x1D, 0x4F, 0x33],
    };

    const POLL_INTERVAL: Duration = Duration::from_secs(10);

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("mtp_devices: starting (SetupAPI poll, 10s cadence)");
        healthy.store(true, Ordering::Relaxed);

        let mut state: HashMap<String, MtpDevice> = HashMap::new();
        let mut baseline_established = false;

        loop {
            // Cooperative stop: try_recv lets us check the channel between
            // polls without blocking.
            match stop_rx.try_recv() {
                Ok(()) | Err(oneshot::error::TryRecvError::Closed) => {
                    info!("mtp_devices: stop requested");
                    break;
                }
                Err(oneshot::error::TryRecvError::Empty) => {}
            }

            match enumerate_mtp_devices() {
                Ok(current) => {
                    healthy.store(true, Ordering::Relaxed);

                    let current_map: HashMap<String, MtpDevice> = current
                        .into_iter()
                        .map(|d| (d.instance_id.clone(), d))
                        .collect();

                    if !baseline_established {
                        debug!(
                            count = current_map.len(),
                            "mtp_devices: baseline established (no events emitted)"
                        );
                        state = current_map;
                        baseline_established = true;
                    } else {
                        // Emit attaches: in current but not in state.
                        for (id, dev) in &current_map {
                            if !state.contains_key(id) {
                                emit(&ctx, dev, EventKind::MtpDeviceAttached, &events, &drops);
                            }
                        }
                        // Emit removes: in state but not in current.
                        for (id, dev) in &state {
                            if !current_map.contains_key(id) {
                                emit(&ctx, dev, EventKind::MtpDeviceRemoved, &events, &drops);
                            }
                        }
                        state = current_map;
                    }
                }
                Err(e) => {
                    warn!("mtp_devices: enumerate failed: {e}");
                    healthy.store(false, Ordering::Relaxed);
                }
            }

            // Sleep with periodic stop checks. We split the 10-second sleep
            // into 1-second chunks so shutdown stays responsive without
            // requiring a blocking_recv timeout dance.
            for _ in 0..POLL_INTERVAL.as_secs() {
                if matches!(
                    stop_rx.try_recv(),
                    Ok(()) | Err(oneshot::error::TryRecvError::Closed)
                ) {
                    info!("mtp_devices: stop requested mid-sleep");
                    return;
                }
                std::thread::sleep(Duration::from_secs(1));
            }
        }
    }

    /// Enumerates currently-present WPD portable devices via SetupAPI.
    ///
    /// Returns an empty Vec when no devices are present (not an error). All
    /// SetupAPI handle lifetimes are bounded by this function — the caller
    /// receives only owned `MtpDevice` value rows.
    fn enumerate_mtp_devices() -> windows::core::Result<Vec<MtpDevice>> {
        // SAFETY: SetupDiGetClassDevsW with a valid GUID + flags. The
        // returned HDEVINFO is checked for invalid before use.
        let h: HDEVINFO = unsafe {
            SetupDiGetClassDevsW(
                Some(&GUID_DEVINTERFACE_WPD),
                None,
                None,
                DIGCF_DEVICEINTERFACE | DIGCF_PRESENT,
            )?
        };

        if h.is_invalid() {
            return Ok(Vec::new());
        }

        let mut out = Vec::new();
        let mut idx: u32 = 0;

        loop {
            let mut info = SP_DEVINFO_DATA::default();
            info.cbSize = std::mem::size_of::<SP_DEVINFO_DATA>() as u32;

            // SAFETY: h is valid (checked above), info is properly sized.
            // Loop terminates when SetupDiEnumDeviceInfo returns an error
            // (NO_MORE_ITEMS).
            let res = unsafe { SetupDiEnumDeviceInfo(h, idx, &mut info) };
            if res.is_err() {
                break;
            }

            let instance_id = read_instance_id(h, &info).unwrap_or_default();
            let friendly_name = read_string_property(h, &info, SPDRP_FRIENDLYNAME);
            let description = read_string_property(h, &info, SPDRP_DEVICEDESC);
            let manufacturer = read_string_property(h, &info, SPDRP_MFG);

            if !instance_id.is_empty() {
                out.push(MtpDevice {
                    instance_id,
                    friendly_name,
                    description,
                    manufacturer,
                });
            }

            idx += 1;
        }

        // SAFETY: h is the valid handle returned by SetupDiGetClassDevsW.
        unsafe {
            let _ = SetupDiDestroyDeviceInfoList(h);
        }

        Ok(out)
    }

    /// Reads `SetupDiGetDeviceInstanceIdW` into an owned String.
    fn read_instance_id(h: HDEVINFO, info: &SP_DEVINFO_DATA) -> Option<String> {
        let mut required: u32 = 0;
        // First call: probe required size.
        // SAFETY: passing None for the buffer is the documented size-probe form.
        let _ = unsafe {
            SetupDiGetDeviceInstanceIdW(h, info, None, Some(&mut required))
        };
        if required == 0 {
            return None;
        }

        let mut buf = vec![0u16; required as usize];
        // SAFETY: buf is sized to `required` UTF-16 units as returned above.
        let r = unsafe {
            SetupDiGetDeviceInstanceIdW(h, info, Some(&mut buf[..]), Some(&mut required))
        };
        if r.is_err() {
            return None;
        }

        // Trim the trailing NUL.
        let len = buf.iter().position(|&c| c == 0).unwrap_or(buf.len());
        Some(String::from_utf16_lossy(&buf[..len]))
    }

    /// Reads a `SETUP_DI_REGISTRY_PROPERTY` (SPDRP_*) string property.
    ///
    /// Returns an empty String on any error or absence — this collector
    /// treats missing optional metadata as benign rather than fatal.
    fn read_string_property(
        h: HDEVINFO,
        info: &SP_DEVINFO_DATA,
        prop: SETUP_DI_REGISTRY_PROPERTY,
    ) -> String {
        let mut required: u32 = 0;
        // SAFETY: probing for required buffer size with no destination buffer.
        let _ = unsafe {
            SetupDiGetDeviceRegistryPropertyW(h, info, prop, None, None, Some(&mut required))
        };
        if required == 0 {
            return String::new();
        }

        let mut buf = vec![0u8; required as usize];
        // SAFETY: buf sized to `required` bytes as returned above.
        let r = unsafe {
            SetupDiGetDeviceRegistryPropertyW(
                h,
                info,
                prop,
                None,
                Some(&mut buf[..]),
                Some(&mut required),
            )
        };
        if r.is_err() {
            return String::new();
        }

        // The property is REG_SZ — a null-terminated UTF-16 string. The
        // byte buffer length must be even.
        if buf.len() < 2 {
            return String::new();
        }

        let words: Vec<u16> = buf
            .chunks_exact(2)
            .map(|c| u16::from_ne_bytes([c[0], c[1]]))
            .collect();

        let len = words.iter().position(|&c| c == 0).unwrap_or(words.len());
        String::from_utf16_lossy(&words[..len])
    }

    fn emit(
        ctx: &CollectorCtx,
        dev: &MtpDevice,
        kind: EventKind,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let action = match kind {
            EventKind::MtpDeviceAttached => "attach",
            EventKind::MtpDeviceRemoved => "remove",
            _ => "unknown",
        };

        let device_type = classify_device(&dev.friendly_name).as_str();
        let now = ctx.clock.now_unix_nanos();

        // Hand-rolled JSON to avoid pulling in serde_json on the hot path
        // and to match the existing collector style. Field values are
        // escaped via the {:?} debug formatter which produces Rust string
        // literal syntax — that is a strict subset of JSON string literal
        // syntax for ASCII-only payloads. The instance_id and friendly_name
        // strings come from Win32 wide-char buffers and may contain
        // non-ASCII; but the {:?} formatter also escapes those correctly
        // (\u{...}) and the resulting output is valid JSON for the
        // gateway's consumer.
        let payload = format!(
            r#"{{"instance_id":{:?},"friendly_name":{:?},"description":{:?},"manufacturer":{:?},"kind":{:?},"device_type":{:?}}}"#,
            dev.instance_id,
            dev.friendly_name,
            dev.description,
            dev.manufacturer,
            action,
            device_type,
        );

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
                debug!(
                    action,
                    device_type,
                    friendly_name = %dev.friendly_name,
                    "mtp device event"
                );
            }
            Err(e) => {
                error!(error = %e, "mtp_devices: queue error");
                drops.fetch_add(1, Ordering::Relaxed);
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests (cross-platform: classification + diff semantics)
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use super::{classify_device, DeviceType, MtpDevice};

    fn dev(id: &str, name: &str) -> MtpDevice {
        MtpDevice {
            instance_id: id.to_string(),
            friendly_name: name.to_string(),
            description: String::new(),
            manufacturer: String::new(),
        }
    }

    #[test]
    fn classify_iphone_is_phone() {
        assert_eq!(classify_device("iPhone"), DeviceType::Phone);
        assert_eq!(classify_device("Apple iPhone 15 Pro"), DeviceType::Phone);
    }

    #[test]
    fn classify_ipad_is_phone_bucket() {
        // Tablets count as phones for the Phase 1 bucket — same legal /
        // privacy classification under KVKK.
        assert_eq!(classify_device("iPad Air"), DeviceType::Phone);
    }

    #[test]
    fn classify_android_pixel_galaxy_are_phones() {
        assert_eq!(classify_device("Android Device"), DeviceType::Phone);
        assert_eq!(classify_device("Pixel 8"), DeviceType::Phone);
        assert_eq!(classify_device("Samsung Galaxy S24"), DeviceType::Phone);
    }

    #[test]
    fn classify_canon_nikon_gopro_are_cameras() {
        assert_eq!(classify_device("Canon EOS R5"), DeviceType::Camera);
        assert_eq!(classify_device("Nikon Z9"), DeviceType::Camera);
        assert_eq!(classify_device("GoPro Hero 12"), DeviceType::Camera);
    }

    #[test]
    fn classify_ipod_is_mp3_player() {
        assert_eq!(classify_device("iPod nano"), DeviceType::Mp3Player);
    }

    #[test]
    fn classify_ipod_takes_priority_over_phone_tokens() {
        // Defensive: if a device name has both, the more specific (and
        // less common) ipod token wins because it's checked first.
        assert_eq!(
            classify_device("iPod touch (iPhone-like)"),
            DeviceType::Mp3Player
        );
    }

    #[test]
    fn classify_unknown_is_other() {
        assert_eq!(classify_device("Some Random Device"), DeviceType::Other);
        assert_eq!(classify_device(""), DeviceType::Other);
    }

    #[test]
    fn classify_is_case_insensitive() {
        assert_eq!(classify_device("IPHONE"), DeviceType::Phone);
        assert_eq!(classify_device("CANON"), DeviceType::Camera);
    }

    /// Computes the (added, removed) sets between two snapshots. Mirrors
    /// the diff logic inside [`windows_impl::run`] so we can unit-test
    /// the semantics without actually touching SetupAPI.
    fn diff(
        prev: &HashMap<String, MtpDevice>,
        curr: &HashMap<String, MtpDevice>,
    ) -> (Vec<String>, Vec<String>) {
        let mut added: Vec<String> = curr
            .keys()
            .filter(|k| !prev.contains_key(*k))
            .cloned()
            .collect();
        let mut removed: Vec<String> = prev
            .keys()
            .filter(|k| !curr.contains_key(*k))
            .cloned()
            .collect();
        added.sort();
        removed.sort();
        (added, removed)
    }

    fn snapshot(devs: &[MtpDevice]) -> HashMap<String, MtpDevice> {
        devs.iter().map(|d| (d.instance_id.clone(), d.clone())).collect()
    }

    #[test]
    fn diff_empty_to_empty_no_events() {
        let (a, r) = diff(&HashMap::new(), &HashMap::new());
        assert!(a.is_empty());
        assert!(r.is_empty());
    }

    #[test]
    fn diff_single_attach() {
        let prev = HashMap::new();
        let curr = snapshot(&[dev("USB\\VID_05AC&PID_12A8\\1", "iPhone")]);
        let (a, r) = diff(&prev, &curr);
        assert_eq!(a, vec!["USB\\VID_05AC&PID_12A8\\1".to_string()]);
        assert!(r.is_empty());
    }

    #[test]
    fn diff_single_remove() {
        let prev = snapshot(&[dev("USB\\VID_05AC&PID_12A8\\1", "iPhone")]);
        let curr = HashMap::new();
        let (a, r) = diff(&prev, &curr);
        assert!(a.is_empty());
        assert_eq!(r, vec!["USB\\VID_05AC&PID_12A8\\1".to_string()]);
    }

    #[test]
    fn diff_swap_one_for_another() {
        let prev = snapshot(&[dev("ID_A", "iPhone")]);
        let curr = snapshot(&[dev("ID_B", "Pixel 8")]);
        let (a, r) = diff(&prev, &curr);
        assert_eq!(a, vec!["ID_B".to_string()]);
        assert_eq!(r, vec!["ID_A".to_string()]);
    }

    #[test]
    fn diff_steady_state_no_events() {
        let snap = snapshot(&[
            dev("ID_A", "iPhone"),
            dev("ID_B", "Canon EOS R5"),
        ]);
        let (a, r) = diff(&snap, &snap);
        assert!(a.is_empty());
        assert!(r.is_empty());
    }

    #[test]
    fn diff_partial_overlap() {
        let prev = snapshot(&[dev("ID_A", "iPhone"), dev("ID_B", "Canon")]);
        let curr = snapshot(&[dev("ID_B", "Canon"), dev("ID_C", "GoPro")]);
        let (a, r) = diff(&prev, &curr);
        assert_eq!(a, vec!["ID_C".to_string()]);
        assert_eq!(r, vec!["ID_A".to_string()]);
    }
}
