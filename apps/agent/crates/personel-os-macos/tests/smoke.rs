//! Smoke tests for `personel-os-macos`.
//!
//! These tests verify two invariants:
//!
//! 1. Every public API surface compiles and links on the host platform.
//! 2. Every stub function returns `Err(AgentError::Unsupported)` on
//!    non-macOS platforms (so the workspace build does not regress silently
//!    into panics or other error kinds).
//! 3. Platform-agnostic helpers (e.g. `service::generate_launch_daemon_plist`)
//!    produce correct output on all platforms.
//!
//! On macOS, tests that call macOS stubs verify the same `Unsupported` return
//! until Phase 2.2/2.3 replaces them with real implementations.

use personel_core::error::AgentError;
use personel_os_macos::{
    capture::ScCapture,
    es_bridge::EsBridgeClient,
    file_events::FsEventsStream,
    input::{foreground_window_info, last_input_idle_ms},
    keystore,
    network_extension::NetworkExtensionClient,
    service::{generate_launch_daemon_plist, is_launchd_context, run_as_launchd_service, LaunchdPlistConfig},
    tcc::{check_permission, TccPermission},
};

// ── input ──────────────────────────────────────────────────────────────────

#[test]
fn input_idle_ms_returns_unsupported() {
    let result = last_input_idle_ms();
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("last_input_idle_ms")),
        "expected Unsupported, got: {result:?}"
    );
}

#[test]
fn input_foreground_window_returns_unsupported() {
    let result = foreground_window_info();
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("foreground_window_info")),
        "expected Unsupported, got: {result:?}"
    );
}

// ── capture ────────────────────────────────────────────────────────────────

#[test]
fn capture_open_returns_unsupported() {
    let result = ScCapture::open(0);
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("capture")),
        "expected Unsupported, got: {result:?}"
    );
}

// ── file_events ────────────────────────────────────────────────────────────

#[test]
fn file_events_start_returns_unsupported() {
    let result = FsEventsStream::start(&["/tmp"], 1.0, |_events| {});
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("file_events")),
        "expected Unsupported, got: {result:?}"
    );
}

// ── network_extension ──────────────────────────────────────────────────────

#[test]
fn network_extension_connect_returns_unsupported() {
    let result = NetworkExtensionClient::connect("/var/run/personel-ne.sock");
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("network_extension")),
        "expected Unsupported, got: {result:?}"
    );
}

// ── tcc ────────────────────────────────────────────────────────────────────

#[test]
fn tcc_check_permission_returns_unsupported() {
    let result = check_permission(TccPermission::InputMonitoring);
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("tcc")),
        "expected Unsupported, got: {result:?}"
    );
}

#[test]
fn tcc_permission_service_strings_are_correct() {
    assert_eq!(TccPermission::ScreenRecording.service_string(), "kTCCServiceScreenCapture");
    assert_eq!(TccPermission::InputMonitoring.service_string(), "kTCCServiceListenEvent");
    assert_eq!(TccPermission::Accessibility.service_string(), "kTCCServiceAccessibility");
    assert_eq!(TccPermission::FullDiskAccess.service_string(), "kTCCServiceSystemPolicyAllFiles");
}

#[test]
fn tcc_mdm_grantable_only_full_disk_access() {
    assert!(TccPermission::FullDiskAccess.mdm_pre_grantable());
    assert!(!TccPermission::ScreenRecording.mdm_pre_grantable());
    assert!(!TccPermission::InputMonitoring.mdm_pre_grantable());
    assert!(!TccPermission::Accessibility.mdm_pre_grantable());
}

// ── es_bridge ──────────────────────────────────────────────────────────────

#[test]
fn es_bridge_connect_returns_unsupported() {
    let result = EsBridgeClient::connect("/var/run/personel-es.sock");
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("es_bridge")),
        "expected Unsupported, got: {result:?}"
    );
}

// ── service ────────────────────────────────────────────────────────────────

#[test]
fn service_is_launchd_context_returns_false_in_tests() {
    // In a test runner we are never launched by launchd.
    assert!(!is_launchd_context());
}

#[test]
fn service_run_as_launchd_returns_unsupported() {
    let (tx, _rx) = tokio::sync::oneshot::channel();
    let result = run_as_launchd_service(tx);
    assert!(
        matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("service")),
        "expected Unsupported, got: {result:?}"
    );
}

#[test]
fn service_plist_generator_produces_valid_xml() {
    let cfg = LaunchdPlistConfig {
        label: "com.personel.agent".to_string(),
        program: "/Applications/Personel.app/Contents/MacOS/personel-agent".to_string(),
        arguments: vec!["--service".to_string()],
        run_at_load: true,
        keep_alive: true,
        stdout_path: Some("/var/log/personel-agent.log".to_string()),
        stderr_path: Some("/var/log/personel-agent-err.log".to_string()),
    };

    let xml = generate_launch_daemon_plist(&cfg);

    // Must be a valid plist XML envelope.
    assert!(xml.starts_with("<?xml"), "missing XML declaration");
    assert!(xml.contains("<!DOCTYPE plist"), "missing DOCTYPE");
    assert!(xml.contains("<plist version=\"1.0\">"), "missing plist root");

    // Must contain all key fields.
    assert!(xml.contains("com.personel.agent"), "missing label");
    assert!(xml.contains("personel-agent"), "missing program");
    assert!(xml.contains("--service"), "missing argument");
    assert!(xml.contains("RunAtLoad"), "missing RunAtLoad key");
    assert!(xml.contains("KeepAlive"), "missing KeepAlive key");
    assert!(xml.contains("<true/>"), "RunAtLoad or KeepAlive should be true");
    assert!(xml.contains("/var/log/personel-agent.log"), "missing stdout path");
    assert!(xml.contains("/var/log/personel-agent-err.log"), "missing stderr path");
}

#[test]
fn service_plist_generator_optional_paths_absent_when_none() {
    let cfg = LaunchdPlistConfig {
        label: "com.personel.agent".to_string(),
        program: "/usr/bin/personel-agent".to_string(),
        arguments: vec![],
        run_at_load: false,
        keep_alive: false,
        stdout_path: None,
        stderr_path: None,
    };

    let xml = generate_launch_daemon_plist(&cfg);
    assert!(!xml.contains("StandardOutPath"), "should have no stdout key");
    assert!(!xml.contains("StandardErrorPath"), "should have no stderr key");
    assert!(xml.contains("<false/>"), "RunAtLoad/KeepAlive should be false");
}

// ── keystore ───────────────────────────────────────────────────────────────

#[test]
fn keystore_store_returns_unsupported_on_non_macos() {
    #[cfg(not(target_os = "macos"))]
    {
        let result = keystore::store("com.personel.agent", "pe-dek", b"test-key-material");
        assert!(
            matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("keystore")),
            "expected Unsupported, got: {result:?}"
        );
    }
    // On macOS this would attempt a real Keychain write; skip in this smoke suite.
    #[cfg(target_os = "macos")]
    {
        // Phase 2.2: add a real Keychain round-trip test with a unique
        // service name to avoid polluting the developer's Keychain.
        // For now, just assert the module compiles.
        let _ = keystore::KEYCHAIN_SERVICE;
        let _ = keystore::PEDEK_ACCOUNT;
    }
}

#[test]
fn keystore_load_returns_unsupported_on_non_macos() {
    #[cfg(not(target_os = "macos"))]
    {
        let result = keystore::load("com.personel.agent", "pe-dek");
        assert!(
            matches!(result, Err(AgentError::Unsupported { component, .. }) if component.contains("keystore")),
            "expected Unsupported, got: {result:?}"
        );
    }
    #[cfg(target_os = "macos")]
    {
        let _ = keystore::KEYCHAIN_SERVICE;
    }
}
