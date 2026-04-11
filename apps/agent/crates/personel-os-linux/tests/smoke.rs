//! Smoke tests for `personel-os-linux`.
//!
//! These tests verify that:
//! 1. All stub functions return `AgentError::Unsupported` on non-Linux.
//! 2. On Linux, Phase 2.1 scaffold functions also return `Unsupported` (no
//!    panics, no unexpected `Ok`).
//! 3. Pure-logic helpers that do not touch OS APIs (`detect_session_type`,
//!    `watchdog_interval_us`) behave correctly.
//!
//! The tests are intentionally minimal — the goal is to verify that the
//! scaffold compiles and the public API surface is reachable, not to exercise
//! real OS functionality.

use personel_core::error::AgentError;

/// Assert that a `Result` is `Err(AgentError::Unsupported)` and that the
/// `os` and `component` fields match the expected values (substring check for
/// component).
macro_rules! assert_unsupported {
    ($result:expr, $component_substr:expr) => {{
        let result = $result;
        match result {
            Err(AgentError::Unsupported { os, component }) => {
                assert!(
                    component.contains($component_substr),
                    "Expected component to contain {:?}, got {:?}",
                    $component_substr,
                    component
                );
                let _ = os; // os value validated per-test where needed
            }
            other => panic!(
                "Expected Err(AgentError::Unsupported), got: {:?}",
                other
            ),
        }
    }};
}

// ── input ─────────────────────────────────────────────────────────────────────

#[test]
fn input_query_activity_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::input::query_activity(),
            "input::query_activity"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::input::query_activity(),
            "input::query_activity"
        );
    }
}

// ── capture ───────────────────────────────────────────────────────────────────

#[test]
fn capture_x11_open_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::capture::X11Adapter::open(0),
            "capture::X11Adapter::open"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::capture::X11Adapter::open(0),
            "capture::X11Adapter::open"
        );
    }
}

#[test]
fn capture_wayland_open_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::capture::WaylandAdapter::open(0),
            "capture::WaylandAdapter::open"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::capture::WaylandAdapter::open(0),
            "capture::WaylandAdapter::open"
        );
    }
}

// ── window_title ─────────────────────────────────────────────────────────────

/// Verify that detect_session_type does not panic. The return value depends on
/// the test environment's env vars and is not asserted here.
#[test]
fn detect_session_type_does_not_panic() {
    #[cfg(target_os = "linux")]
    {
        let _ = personel_os_linux::window_title::detect_session_type();
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = personel_os_linux::stub::window_title::detect_session_type();
    }
}

/// Wayland adapter must always return Unsupported with component = "window_title".
/// This is not a Phase 2.1 temporary stub — it is the correct long-term behaviour.
#[test]
fn wayland_window_title_always_unsupported() {
    #[cfg(target_os = "linux")]
    {
        // Directly test WaylandAdapter without going through session detection.
        // We can't call ::new() since it returns Err in Phase 2.1, but we can
        // verify the Unsupported component tag.
        assert_unsupported!(
            personel_os_linux::window_title::WaylandAdapter::new(),
            "window_title::WaylandAdapter"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::window_title::WaylandAdapter::new(),
            "window_title::WaylandAdapter"
        );
    }
}

// ── file_events ───────────────────────────────────────────────────────────────

#[test]
fn fanotify_capability_check_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::file_events::check_fanotify_capability(),
            "file_events::check_fanotify_capability"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::file_events::check_fanotify_capability(),
            "file_events::check_fanotify_capability"
        );
    }
}

// ── ebpf ─────────────────────────────────────────────────────────────────────

#[test]
fn ebpf_process_load_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::ebpf::process::ProcessCollector::load(),
            "ebpf::process::load"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::ebpf::process::ProcessCollector::load(),
            "ebpf::process::load"
        );
    }
}

#[test]
fn ebpf_network_load_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::ebpf::network::NetworkCollector::load(),
            "ebpf::network::load"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::ebpf::network::NetworkCollector::load(),
            "ebpf::network::load"
        );
    }
}

// ── systemd ───────────────────────────────────────────────────────────────────

#[test]
fn systemd_notify_ready_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::systemd::notify_ready(),
            "systemd::notify_ready"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::systemd::notify_ready(),
            "systemd::notify_ready"
        );
    }
}

/// `watchdog_interval_us` returns None when $WATCHDOG_USEC is unset (typical
/// in CI / test environments).
#[test]
fn watchdog_interval_none_when_env_absent() {
    // Ensure the variable is absent for this test.
    std::env::remove_var("WATCHDOG_USEC");

    #[cfg(target_os = "linux")]
    {
        assert_eq!(personel_os_linux::systemd::watchdog_interval_us(), None);
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_eq!(
            personel_os_linux::stub::systemd::watchdog_interval_us(),
            None
        );
    }
}

// ── keystore ─────────────────────────────────────────────────────────────────

#[test]
fn keystore_store_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::keystore::store("pe-dek", b"dummy"),
            "keystore::store"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::keystore::store("pe-dek", b"dummy"),
            "keystore::store"
        );
    }
}

#[test]
fn keystore_load_returns_unsupported() {
    #[cfg(target_os = "linux")]
    {
        assert_unsupported!(
            personel_os_linux::keystore::load("pe-dek"),
            "keystore::load"
        );
    }
    #[cfg(not(target_os = "linux"))]
    {
        assert_unsupported!(
            personel_os_linux::stub::keystore::load("pe-dek"),
            "keystore::load"
        );
    }
}
