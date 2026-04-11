//! Workspace-level integration smoke tests.
//!
//! These tests verify that the key subsystems can be instantiated and interact
//! without panicking. They do NOT require a Windows environment.

use std::sync::Arc;

use personel_core::clock::FakeClock;
use personel_core::event::{EventKind, Priority};
use personel_core::ids::{EndpointId, TenantId};
use personel_crypto::envelope::{build_keystroke_aad, decrypt, encrypt};
use personel_policy::engine::PolicyEngine;
use personel_queue::queue::EventQueue;
use uuid::Uuid;
use zeroize::Zeroizing;

// ──────────────────────────────────────────────────────────────────────────────
// Core types
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn core_ids_round_trip() {
    let tenant = TenantId::new(Uuid::new_v4());
    let bytes = tenant.to_bytes();
    let recovered = TenantId::from_bytes(&bytes).unwrap();
    assert_eq!(tenant, recovered);
}

#[test]
fn event_kind_as_str_no_empty() {
    for kind in all_event_kinds() {
        assert!(!kind.as_str().is_empty(), "EventKind {:?} has empty as_str()", kind);
    }
}

#[test]
fn fake_clock_works() {
    let clock = FakeClock::new(0);
    clock.advance_secs(60);
    let ts = clock.now_proto();
    assert_eq!(ts.seconds, 60);
    assert_eq!(ts.nanos, 0);
}

// ──────────────────────────────────────────────────────────────────────────────
// Crypto
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn aes_gcm_encrypt_decrypt() {
    let key: personel_crypto::Aes256Key = Zeroizing::new([0x11u8; 32]);
    let endpoint_bytes = [0xAAu8; 16];
    let aad = build_keystroke_aad(&endpoint_bytes, 999);
    let plaintext = b"the quick brown fox";

    let envelope = encrypt(&key, aad, plaintext).unwrap();
    assert_eq!(envelope.ciphertext.len(), plaintext.len() + 16);

    let recovered = decrypt(&key, &envelope).unwrap();
    assert_eq!(recovered.as_slice(), plaintext);
}

#[test]
fn wrong_key_decrypt_fails() {
    let key: personel_crypto::Aes256Key = Zeroizing::new([0x22u8; 32]);
    let bad_key: personel_crypto::Aes256Key = Zeroizing::new([0x33u8; 32]);
    let aad = build_keystroke_aad(&[0u8; 16], 1);
    let envelope = encrypt(&key, aad, b"secret").unwrap();
    assert!(decrypt(&bad_key, &envelope).is_err());
}

// ──────────────────────────────────────────────────────────────────────────────
// Queue
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn queue_basic_flow() {
    let q = EventQueue::open_in_memory().unwrap();
    let event_id = vec![0x01u8; 16];

    q.enqueue(
        &event_id,
        "session.idle_start",
        Priority::Normal,
        1_000_000_000,
        1_000_000_001,
        b"proto_payload",
    )
    .unwrap();

    let stats = q.stats().unwrap();
    assert_eq!(stats.pending_count, 1);

    let batch = q.dequeue_batch(10, 1).unwrap();
    assert_eq!(batch.len(), 1);
    assert_eq!(batch[0].event_type, "session.idle_start");

    let deleted = q.ack_batch(1).unwrap();
    assert_eq!(deleted, 1);

    let stats2 = q.stats().unwrap();
    assert_eq!(stats2.pending_count, 0);
}

#[test]
fn queue_critical_events_survive_eviction() {
    let q = EventQueue::open_in_memory().unwrap();

    // Fill with low-priority events.
    for i in 0u8..5 {
        q.enqueue(&[i; 16], "file.read", Priority::Low, i as i64, i as i64, b"x")
            .unwrap();
    }
    // Add a critical event.
    q.enqueue(
        &[99u8; 16],
        "agent.tamper_detected",
        Priority::Critical,
        100,
        100,
        b"tamper",
    )
    .unwrap();

    q.evict(50).unwrap();

    let batch = q.dequeue_batch(100, 1).unwrap();
    assert!(
        batch.iter().any(|e| e.event_type == "agent.tamper_detected"),
        "tamper event must survive eviction"
    );
}

// ──────────────────────────────────────────────────────────────────────────────
// Policy engine
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn policy_engine_default_view() {
    let (engine, _rx) = PolicyEngine::new_unsigned();
    let view = engine.current();
    assert_eq!(view.version, "default-v0");
    assert!(view.collectors.process);
}

#[test]
fn policy_glob_matching() {
    use personel_proto::v1::{AppBlocklist, AppRule, PolicyBundle};

    let (engine, _rx) = PolicyEngine::new_unsigned();
    let bundle = PolicyBundle {
        version: "v1".into(),
        app_blocklist: Some(AppBlocklist {
            rules: vec![
                AppRule {
                    rule_id: "r1".into(),
                    exe_name_glob: "tor*.exe".into(),
                    ..Default::default()
                },
                AppRule {
                    rule_id: "r2".into(),
                    exe_name_glob: "*.game".into(),
                    ..Default::default()
                },
            ],
        }),
        ..Default::default()
    };
    engine.apply(bundle, &[]).unwrap();
    let view = engine.current();

    assert!(view.is_exe_blocked("torbrowser.exe").is_some());
    assert!(view.is_exe_blocked("quake.game").is_some());
    assert!(view.is_exe_blocked("chrome.exe").is_none());
}

// ──────────────────────────────────────────────────────────────────────────────
// Helper
// ──────────────────────────────────────────────────────────────────────────────

fn all_event_kinds() -> Vec<EventKind> {
    vec![
        EventKind::ProcessStart,
        EventKind::ProcessStop,
        EventKind::ProcessForegroundChange,
        EventKind::WindowTitleChanged,
        EventKind::WindowFocusLost,
        EventKind::SessionIdleStart,
        EventKind::SessionIdleEnd,
        EventKind::SessionLock,
        EventKind::SessionUnlock,
        EventKind::ScreenshotCaptured,
        EventKind::ScreenclipCaptured,
        EventKind::FileCreated,
        EventKind::FileRead,
        EventKind::FileWritten,
        EventKind::FileDeleted,
        EventKind::FileRenamed,
        EventKind::FileCopied,
        EventKind::ClipboardMetadata,
        EventKind::ClipboardContentEncrypted,
        EventKind::PrintJobSubmitted,
        EventKind::UsbDeviceAttached,
        EventKind::UsbDeviceRemoved,
        EventKind::UsbMassStoragePolicyBlock,
        EventKind::NetworkFlowSummary,
        EventKind::NetworkDnsQuery,
        EventKind::NetworkTlsSni,
        EventKind::KeystrokeWindowStats,
        EventKind::KeystrokeContentEncrypted,
        EventKind::AppBlockedByPolicy,
        EventKind::WebBlockedByPolicy,
        EventKind::AgentHealthHeartbeat,
        EventKind::AgentPolicyApplied,
        EventKind::AgentUpdateInstalled,
        EventKind::AgentTamperDetected,
        EventKind::LiveViewStarted,
        EventKind::LiveViewStopped,
    ]
}
