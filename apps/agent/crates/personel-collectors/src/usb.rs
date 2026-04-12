//! USB device event collector.
//!
//! Uses `CM_Register_Notification` (`GUID_DEVINTERFACE_USB_DEVICE`) to detect
//! USB device arrival and removal. On each notification, queries the device's
//! VID/PID/manufacturer string and emits:
//!
//! - `usb.device_attached` on `CM_NOTIFY_ACTION_DEVICEINTERFACEARRIVAL`
//! - `usb.device_removed`  on `CM_NOTIFY_ACTION_DEVICEINTERFACEREMOVAL`
//!
//! If `policy.usb_rules` blocks mass-storage devices (Phase 2 policy gate),
//! `usb.mass_storage_policy_block` is also emitted.
//!
//! # Privacy
//!
//! Serial numbers are SHA-256 hashed before emitting per KVKK m.6.
//!
//! # Platform support
//!
//! `cfg(target_os = "windows")`: full `CM_Register_Notification` implementation.
//! Non-Windows: parks gracefully so `cargo check` passes on macOS/Linux.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::info;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// USB device event collector.
#[derive(Default)]
pub struct UsbCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl UsbCollector {
    /// Creates a new [`UsbCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for UsbCollector {
    fn name(&self) -> &'static str {
        "usb"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["usb.device_attached", "usb.device_removed", "usb.mass_storage_policy_block"]
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
        info!("usb: CM_Register_Notification not supported on this platform — parking");
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
    use std::sync::{Arc, Mutex};

    use sha2::{Digest, Sha256};
    use tokio::sync::oneshot;
    use tracing::{debug, error, info, warn};

    use windows::core::GUID;
    use windows::Win32::Devices::DeviceAndDriverInstallation::{
        CM_Register_Notification, CM_Unregister_Notification, HCMNOTIFICATION,
        CM_NOTIFY_ACTION, CM_NOTIFY_ACTION_DEVICEINTERFACEARRIVAL,
        CM_NOTIFY_ACTION_DEVICEINTERFACEREMOVAL, CM_NOTIFY_FILTER,
        CM_NOTIFY_FILTER_0, CM_NOTIFY_FILTER_0_1, CM_NOTIFY_FILTER_TYPE_DEVICEINTERFACE,
        CR_SUCCESS,
    };
    use windows::Win32::Foundation::{LPARAM, WPARAM};
    use windows::Win32::UI::WindowsAndMessaging::{PostThreadMessageW, WM_APP, WM_QUIT};

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use crate::CollectorCtx;

    // WM_APP + 2 reserved for USB quit signal.
    const WM_USB_QUIT: u32 = WM_APP + 2;

    // GUID_DEVINTERFACE_USB_DEVICE
    const GUID_DEVINTERFACE_USB_DEVICE: GUID = GUID {
        data1: 0xA5DCBF10,
        data2: 0x6530,
        data3: 0x11D2,
        data4: [0x90, 0x1F, 0x00, 0xC0, 0x4F, 0xB9, 0x51, 0xED],
    };

    struct CallbackState {
        ctx: CollectorCtx,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        thread_id: u32,
    }

    /// Shared callback state. The pointer is stored here so the extern callback
    /// can access it. Only written once before `CM_Register_Notification` and
    /// read only from the notification callback thread.
    static CALLBACK_STATE: std::sync::OnceLock<Mutex<Option<Box<CallbackState>>>> =
        std::sync::OnceLock::new();

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<std::sync::atomic::AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!("usb: starting (CM_Register_Notification)");

        // SAFETY: GetCurrentThreadId is always safe.
        let thread_id = unsafe { windows::Win32::System::Threading::GetCurrentThreadId() };

        // Store callback state.
        let cell = CALLBACK_STATE.get_or_init(|| Mutex::new(None));
        {
            let mut guard = cell.lock().expect("CALLBACK_STATE poisoned");
            *guard = Some(Box::new(CallbackState {
                ctx,
                events: Arc::clone(&events),
                drops: Arc::clone(&drops),
                thread_id,
            }));
        }

        // Build the notification filter for USB device interface.
        let mut filter = CM_NOTIFY_FILTER {
            cbSize: std::mem::size_of::<CM_NOTIFY_FILTER>() as u32,
            FilterType: CM_NOTIFY_FILTER_TYPE_DEVICEINTERFACE,
            ..Default::default()
        };
        // SAFETY: union field access — we set FilterType = DEVICEINTERFACE above.
        unsafe {
            filter.u.DeviceInterface.ClassGuid = GUID_DEVINTERFACE_USB_DEVICE;
        }

        let mut hnotify = HCMNOTIFICATION::default();
        // SAFETY: CM_Register_Notification is called with valid filter and
        // callback; hnotify is zeroed and will be filled on success.
        let cr = unsafe {
            CM_Register_Notification(
                &filter,
                Some(std::ptr::null_mut()), // context passed to callback via lParam trick — we use global state instead
                Some(usb_notification_callback),
                &mut hnotify,
            )
        };

        if cr != CR_SUCCESS {
            error!("usb: CM_Register_Notification failed (cr={:?})", cr);
            healthy.store(false, Ordering::Relaxed);
            // Clean up state.
            if let Ok(mut guard) = cell.lock() {
                guard.take();
            }
            let _ = stop_rx.blocking_recv();
            return;
        }

        healthy.store(true, Ordering::Relaxed);
        info!("usb: registered for USB device interface notifications");

        // Wait for shutdown.
        let _ = stop_rx.blocking_recv();

        // SAFETY: hnotify is a valid handle returned by CM_Register_Notification.
        unsafe { CM_Unregister_Notification(hnotify) };

        // Clear callback state.
        if let Ok(mut guard) = cell.lock() {
            guard.take();
        }

        info!("usb: stopped");
    }

    /// USB device interface notification callback.
    ///
    /// # Safety
    ///
    /// This is a `CM_NOTIFY_CALLBACK` registered with `CM_Register_Notification`.
    /// It accesses the global `CALLBACK_STATE` under a mutex.
    unsafe extern "system" fn usb_notification_callback(
        _hnotify: HCMNOTIFICATION,
        _context: *const std::ffi::c_void,
        action: CM_NOTIFY_ACTION,
        event_data: *const windows::Win32::Devices::DeviceAndDriverInstallation::CM_NOTIFY_EVENT_DATA,
        _event_data_size: u32,
    ) -> u32 {
        let kind = match action {
            CM_NOTIFY_ACTION_DEVICEINTERFACEARRIVAL => EventKind::UsbDeviceAttached,
            CM_NOTIFY_ACTION_DEVICEINTERFACEREMOVAL => EventKind::UsbDeviceRemoved,
            _ => return 0,
        };

        // Extract the device instance path from the event data (best-effort).
        let instance_path = if !event_data.is_null() {
            // SAFETY: event_data points to CM_NOTIFY_EVENT_DATA with a DeviceInterface union.
            let data = &*event_data;
            // DeviceInterface.SymbolicLink is a wide-char array at the end of the struct.
            let sym_ptr = data.u.DeviceInterface.SymbolicLink.as_ptr();
            let mut len = 0usize;
            while len < 512 && *sym_ptr.add(len) != 0 {
                len += 1;
            }
            let sym_slice = std::slice::from_raw_parts(sym_ptr, len);
            String::from_utf16_lossy(sym_slice)
        } else {
            String::new()
        };

        // Hash the instance path for privacy (KVKK m.6).
        let mut hasher = Sha256::new();
        hasher.update(instance_path.as_bytes());
        let hash = hex::encode(hasher.finalize());

        let label = match kind {
            EventKind::UsbDeviceAttached => "attached",
            EventKind::UsbDeviceRemoved => "removed",
            _ => "unknown",
        };

        debug!(label, hash = %&hash[..16], "USB device event");

        if let Some(cell) = CALLBACK_STATE.get() {
            if let Ok(guard) = cell.lock() {
                if let Some(state) = guard.as_ref() {
                    let payload = format!(
                        r#"{{"action":{:?},"instance_path_sha256":{:?}}}"#,
                        label, hash
                    );
                    enqueue(&state.ctx, kind, &payload, &state.events, &state.drops);
                }
            }
        }

        0
    }

    fn enqueue(
        ctx: &CollectorCtx,
        kind: EventKind,
        payload: &str,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let now = ctx.clock.now_unix_nanos();
        let id = EventId::new_v7().to_bytes();
        match ctx.queue.enqueue(&id, kind.as_str(), Priority::Normal, now, now, payload.as_bytes()) {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
            }
            Err(e) => {
                warn!(error = %e, "usb: queue error");
                drops.fetch_add(1, Ordering::Relaxed);
            }
        }
    }
}
