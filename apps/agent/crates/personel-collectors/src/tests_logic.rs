//! Cross-platform unit tests for collector pure-logic functions.
//!
//! Windows API bağımlı collector döngüleri mock'lanamayacağından,
//! testable seams şunlardır:
//!
//! - **Sensitivity guard** — foreground app exclude listesi eşleşmesi
//!   (`screen.rs`'ten extract edildi)
//! - **PID diff** — process start/stop set farkı (`process_app.rs`'ten)
//! - **ADR 0013 gate** — pe_dek=None → metadata-only kararı (`keystroke.rs`'ten)
//! - **USB device path hashing** — SHA-256 hex (`usb.rs`'ten)
//! - **Clipboard metadata payload şekli** — JSON field doğrulaması
//! - **Print job metadata payload şekli** — JSON field doğrulaması
//! - **Idle threshold logic** — idle_ms >= threshold kararı
//! - **Window focus duration hesabı** — nanoseconds → milliseconds dönüşümü
//! - **Event kind string kontrolü** — collector `event_types()` non-empty
//! - **HealthSnapshot default** — sağlıklı başlangıç state
//!
//! Her test `#[cfg(test)]` altındadır ve macOS/Linux üzerinde çalışır.

// ──────────────────────────────────────────────────────────────────────────────
// Testable pure functions (re-exported / duplicated logic from collectors)
// ──────────────────────────────────────────────────────────────────────────────

/// Sensitivity guard: foreground app exe adı veya başlığı exclude listesinde mi?
///
/// `screen.rs` `capture_loop` içindeki aynı `skip` mantığının pure kopyasıdır.
/// Test edilebilirlik için buraya extract edildi.
pub(crate) fn sensitivity_guard_should_skip(
    exe_name: &str,
    window_title: &str,
    exclude_apps: &[String],
) -> bool {
    if exclude_apps.is_empty() {
        return false;
    }
    let exe_lower = exe_name.to_lowercase();
    let title_lower = window_title.to_lowercase();
    exclude_apps.iter().any(|app| {
        let a = app.to_lowercase();
        exe_lower.contains(&a) || title_lower.contains(&a)
    })
}

/// PID diff: `known_pids` ile `current_pids` arasında yeni ve kaybolan PID'leri döner.
///
/// `process_app.rs` `run_loop` içindeki set farkı mantığının pure kopyasıdır.
pub(crate) fn pid_diff(
    known: &std::collections::HashSet<u32>,
    current: &std::collections::HashSet<u32>,
) -> (Vec<u32>, Vec<u32>) {
    let started: Vec<u32> = current.difference(known).copied().collect();
    let stopped: Vec<u32> = known.difference(current).copied().collect();
    (started, stopped)
}

/// ADR 0013 gate: pe_dek ve policy flag kontrolü.
///
/// `keystroke.rs` `run_content_loop` başındaki `content_on` kararını mirror eder.
pub(crate) fn adr0013_content_enabled(pe_dek_present: bool, policy_content_enabled: bool) -> bool {
    pe_dek_present && policy_content_enabled
}

/// USB device path SHA-256 hash (hex string) hesaplar.
///
/// `usb.rs` Windows callback içindeki hash mantığının pure kopyasıdır.
pub(crate) fn hash_device_path(instance_path: &str) -> String {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    hasher.update(instance_path.as_bytes());
    hex::encode(hasher.finalize())
}

/// Idle threshold: idle_ms >= threshold_ms ise idle başlamış sayılır.
pub(crate) fn is_idle(idle_ms: u64, threshold_ms: u64) -> bool {
    idle_ms >= threshold_ms
}

/// Focus duration: nanosaniye farkından milisaniye üretir.
///
/// `window_title.rs` `prev_duration_ms` hesabının pure kopyasıdır.
pub(crate) fn focus_duration_ms(now_nanos: i64, last_change_nanos: i64) -> u64 {
    u64::try_from(now_nanos.saturating_sub(last_change_nanos)).unwrap_or(0) / 1_000_000
}

/// Clipboard metadata JSON payload şeklini doğrular (zorunlu field'lar mevcut mu?).
pub(crate) fn clipboard_meta_payload_valid(payload: &str) -> bool {
    let v: serde_json::Value = match serde_json::from_str(payload) {
        Ok(v) => v,
        Err(_) => return false,
    };
    v.get("has_text").is_some()
        && v.get("char_count").is_some()
        && v.get("content_encrypted").is_some()
}

/// Print job metadata JSON payload şeklini doğrular.
pub(crate) fn print_job_payload_valid(payload: &str) -> bool {
    let v: serde_json::Value = match serde_json::from_str(payload) {
        Ok(v) => v,
        Err(_) => return false,
    };
    v.get("job_id").is_some()
        && v.get("document_name").is_some()
        && v.get("printer_name").is_some()
        && v.get("total_pages").is_some()
        && v.get("size_bytes").is_some()
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    // ── Screen sensitivity guard ──────────────────────────────────────────────

    #[test]
    fn sensitivity_guard_skips_excluded_exe() {
        let exclude = vec!["keepass".to_string(), "1password".to_string()];
        assert!(
            sensitivity_guard_should_skip("KeePass.exe", "KeePass Password Safe", &exclude),
            "exe match (case-insensitive) should skip"
        );
    }

    #[test]
    fn sensitivity_guard_skips_excluded_title() {
        let exclude = vec!["private browsing".to_string()];
        assert!(
            sensitivity_guard_should_skip("firefox.exe", "Firefox - Private Browsing", &exclude),
            "title match should skip"
        );
    }

    #[test]
    fn sensitivity_guard_does_not_skip_unlisted_app() {
        let exclude = vec!["keepass".to_string()];
        assert!(
            !sensitivity_guard_should_skip("chrome.exe", "Google Chrome", &exclude),
            "unlisted app should not be skipped"
        );
    }

    #[test]
    fn sensitivity_guard_empty_list_never_skips() {
        assert!(
            !sensitivity_guard_should_skip("anything.exe", "Any Title", &[]),
            "empty exclude list must never skip"
        );
    }

    #[test]
    fn sensitivity_guard_partial_match_in_exe() {
        // "pass" matches "1password.exe" via contains
        let exclude = vec!["pass".to_string()];
        assert!(sensitivity_guard_should_skip("1password.exe", "1Password", &exclude));
    }

    // ── Process PID diff ──────────────────────────────────────────────────────

    #[test]
    fn pid_diff_detects_new_process() {
        let known: HashSet<u32> = [1, 2, 3].into_iter().collect();
        let current: HashSet<u32> = [1, 2, 3, 4, 5].into_iter().collect();
        let (started, stopped) = pid_diff(&known, &current);
        let mut started_sorted = started.clone();
        started_sorted.sort_unstable();
        assert_eq!(started_sorted, vec![4, 5]);
        assert!(stopped.is_empty());
    }

    #[test]
    fn pid_diff_detects_stopped_process() {
        let known: HashSet<u32> = [1, 2, 3].into_iter().collect();
        let current: HashSet<u32> = [1, 3].into_iter().collect();
        let (started, stopped) = pid_diff(&known, &current);
        assert!(started.is_empty());
        assert_eq!(stopped, vec![2]);
    }

    #[test]
    fn pid_diff_no_change_returns_empty() {
        let known: HashSet<u32> = [10, 20, 30].into_iter().collect();
        let current = known.clone();
        let (started, stopped) = pid_diff(&known, &current);
        assert!(started.is_empty());
        assert!(stopped.is_empty());
    }

    // ── ADR 0013 gate ─────────────────────────────────────────────────────────

    #[test]
    fn adr0013_no_dek_means_metadata_only() {
        // pe_dek=None → content disabled regardless of policy flag
        assert!(!adr0013_content_enabled(false, true));
    }

    #[test]
    fn adr0013_policy_false_means_metadata_only() {
        // pe_dek=Some BUT policy says false → content disabled
        assert!(!adr0013_content_enabled(true, false));
    }

    #[test]
    fn adr0013_both_true_enables_content() {
        assert!(adr0013_content_enabled(true, true));
    }

    #[test]
    fn adr0013_both_false_means_metadata_only() {
        assert!(!adr0013_content_enabled(false, false));
    }

    // ── USB device path hashing ───────────────────────────────────────────────

    #[test]
    fn usb_hash_is_hex_sha256_length() {
        let hash = hash_device_path(r"\\?\USB#VID_1234&PID_5678#ABCD1234#{...}");
        // SHA-256 hex = 64 chars
        assert_eq!(hash.len(), 64, "SHA-256 hex must be 64 chars");
        assert!(hash.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn usb_hash_different_paths_different_hashes() {
        let h1 = hash_device_path(r"\\?\USB#VID_1111");
        let h2 = hash_device_path(r"\\?\USB#VID_2222");
        assert_ne!(h1, h2);
    }

    #[test]
    fn usb_hash_empty_path_deterministic() {
        // Empty path should still produce a valid hash, not panic
        let h1 = hash_device_path("");
        let h2 = hash_device_path("");
        assert_eq!(h1, h2);
    }

    // ── Idle threshold ────────────────────────────────────────────────────────

    #[test]
    fn idle_threshold_exactly_at_boundary() {
        // At threshold: idle
        assert!(is_idle(300_000, 300_000));
    }

    #[test]
    fn idle_below_threshold_is_active() {
        assert!(!is_idle(299_999, 300_000));
    }

    #[test]
    fn idle_well_above_threshold() {
        assert!(is_idle(600_000, 300_000));
    }

    // ── Window focus duration ─────────────────────────────────────────────────

    #[test]
    fn focus_duration_one_second() {
        let now = 2_000_000_000i64; // 2 s in nanos
        let last = 1_000_000_000i64; // 1 s in nanos
        assert_eq!(focus_duration_ms(now, last), 1000);
    }

    #[test]
    fn focus_duration_zero_when_same_tick() {
        let ts = 5_000_000_000i64;
        assert_eq!(focus_duration_ms(ts, ts), 0);
    }

    #[test]
    fn focus_duration_saturates_at_zero_for_negative_diff() {
        // Clock jitter: last > now should not underflow
        assert_eq!(focus_duration_ms(100, 200), 0);
    }

    // ── Clipboard metadata payload shape ──────────────────────────────────────

    #[test]
    fn clipboard_meta_valid_payload() {
        let payload = r#"{"has_text":true,"char_count":42,"content_encrypted":false}"#;
        assert!(clipboard_meta_payload_valid(payload));
    }

    #[test]
    fn clipboard_meta_missing_field_is_invalid() {
        let payload = r#"{"has_text":true,"char_count":10}"#;
        assert!(!clipboard_meta_payload_valid(payload));
    }

    #[test]
    fn clipboard_meta_empty_json_is_invalid() {
        assert!(!clipboard_meta_payload_valid("{}"));
    }

    // ── Print job metadata payload shape ──────────────────────────────────────

    #[test]
    fn print_job_valid_payload() {
        let payload = r#"{
            "job_id": 1,
            "document_name": "report.pdf",
            "printer_name": "HP LaserJet",
            "user_name": "alice",
            "total_pages": 5,
            "size_bytes": 204800
        }"#;
        assert!(print_job_payload_valid(payload));
    }

    #[test]
    fn print_job_missing_size_bytes_invalid() {
        let payload =
            r#"{"job_id":1,"document_name":"x","printer_name":"p","total_pages":1}"#;
        assert!(!print_job_payload_valid(payload));
    }

    // ── Collector trait surface (event_types non-empty) ───────────────────────

    #[test]
    fn idle_collector_event_types_non_empty() {
        use crate::idle::IdleCollector;
        use crate::Collector;
        let c = IdleCollector::new();
        assert!(!c.event_types().is_empty());
        assert!(c.event_types().contains(&"session.idle_start"));
        assert!(c.event_types().contains(&"session.idle_end"));
    }

    #[test]
    fn window_title_collector_event_types() {
        use crate::window_title::WindowTitleCollector;
        use crate::Collector;
        let c = WindowTitleCollector::new();
        assert!(c.event_types().contains(&"window.title_changed"));
    }

    #[test]
    fn process_app_collector_event_types() {
        use crate::process_app::ProcessAppCollector;
        use crate::Collector;
        let c = ProcessAppCollector::new();
        let types = c.event_types();
        assert!(types.contains(&"process.start"));
        assert!(types.contains(&"process.stop"));
    }

    #[test]
    fn clipboard_collector_event_types() {
        use crate::clipboard::ClipboardCollector;
        use crate::Collector;
        let c = ClipboardCollector::new();
        assert!(c.event_types().contains(&"clipboard.metadata"));
        assert!(c.event_types().contains(&"clipboard.content_encrypted"));
    }

    #[test]
    fn usb_collector_event_types() {
        use crate::usb::UsbCollector;
        use crate::Collector;
        let c = UsbCollector::new();
        assert!(c.event_types().contains(&"usb.device_attached"));
    }

    #[test]
    fn print_collector_event_types() {
        use crate::print::PrintCollector;
        use crate::Collector;
        let c = PrintCollector::new();
        assert!(c.event_types().contains(&"print.job_submitted"));
    }

    #[test]
    fn keystroke_meta_collector_event_types() {
        use crate::keystroke::KeystrokeMetaCollector;
        use crate::Collector;
        let c = KeystrokeMetaCollector::new();
        assert!(c.event_types().contains(&"keystroke.window_stats"));
    }

    #[test]
    fn keystroke_content_collector_event_types() {
        use crate::keystroke::KeystrokeContentCollector;
        use crate::Collector;
        let c = KeystrokeContentCollector::new();
        assert!(c.event_types().contains(&"keystroke.content_encrypted"));
    }

    // ── HealthSnapshot default ────────────────────────────────────────────────

    #[test]
    fn health_snapshot_default_is_zero() {
        use crate::HealthSnapshot;
        let snap = HealthSnapshot::default();
        // Default collector başlangıçta sağlıklı olmak zorunda değil —
        // ama test sadece Default trait'inin panik üretmediğini doğrular.
        assert_eq!(snap.events_since_last, 0);
        assert_eq!(snap.drops_since_last, 0);
    }

    // ── Screen collector name ─────────────────────────────────────────────────

    #[test]
    fn screen_collector_name_is_screen() {
        use crate::screen::ScreenCollector;
        use crate::Collector;
        let c = ScreenCollector::new();
        assert_eq!(c.name(), "screen");
    }
}
