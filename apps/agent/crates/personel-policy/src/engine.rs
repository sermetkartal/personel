//! Policy cache, evaluation, and hot reload.
//!
//! The [`PolicyEngine`] owns the canonical policy bundle. Collectors receive
//! an `Arc<PolicyView>` which is cheap to clone and provides read-only access
//! to the current policy settings.
//!
//! On a `PolicyPush` from the server, `PolicyEngine::apply` is called. It
//! verifies the Ed25519 signature against the baked-in policy-signing public
//! key, then atomically swaps the inner `Arc<PolicyView>` so running
//! collectors pick up the new policy on their next policy check tick.

use std::sync::Arc;

use ed25519_dalek::{Signature, VerifyingKey};
use prost::Message;
use tokio::sync::watch;
use tracing::{error, info, warn};

use personel_core::error::{AgentError, Result};
use personel_proto::v1::{CollectorFlags, PolicyBundle, QueueSettings, ScreenshotSettings};

// ──────────────────────────────────────────────────────────────────────────────
// PolicyView — snapshot shared with collectors
// ──────────────────────────────────────────────────────────────────────────────

/// An immutable snapshot of the current policy bundle.
///
/// Cheap to clone (all fields are `Clone`). Collectors hold an
/// `Arc<PolicyView>` and subscribe to the change channel to get notified
/// when a new bundle is pushed.
#[derive(Debug, Clone)]
pub struct PolicyView {
    /// Monotonic version string assigned by the server.
    pub version: String,
    /// Which collectors are enabled.
    pub collectors: CollectorFlags,
    /// Screenshot interval and quality settings.
    pub screenshot: ScreenshotSettings,
    /// Queue capacity and upload batch settings.
    pub queue: QueueSettings,
    /// Idle threshold in seconds.
    pub idle_threshold_secs: u32,
    /// Whether keystroke content collection is enabled.
    pub keystroke_content_enabled: bool,
    /// App block list rules (exe_name_glob → rule_id).
    pub blocked_exe_globs: Vec<(String, String)>,
    /// URL block list rules (host_glob → rule_id).
    pub blocked_host_globs: Vec<(String, String)>,
}

/// Resolves a screenshot preset name to a full [`ScreenshotSettings`] value.
///
/// Presets are the canonical way administrators adjust capture footprint
/// without touching raw fields. The five preset triples (interval,
/// max_height, quality) are chosen for target payload sizes: minimal ~5 KB,
/// low ~10 KB, medium ~25 KB, high ~50 KB (default), max ~100 KB.
///
/// Unknown preset strings fall through to `"high"` defaults so misconfig
/// never disables capture.
#[must_use]
pub fn preset_screenshot(preset: &str) -> ScreenshotSettings {
    let (interval, max_h, quality): (u32, u32, u32) = match preset {
        "minimal" => (300, 540, 25),
        "low"     => (180, 720, 35),
        "medium"  => (120, 900, 50),
        "max"     => (30, 1440, 80),
        _         => (60, 1080, 65), // "high" — the production default
    };
    ScreenshotSettings {
        interval_seconds: interval,
        on_foreground_change: false,
        max_width: (max_h as f64 * 16.0 / 9.0) as u32, // 16:9 aspect, agent also computes from source
        max_height: max_h,
        quality_percent: quality,
        blur_when_off_hours: false,
        blur_exe_names: vec![],
        blur_host_patterns: vec![],
        exclude_apps: vec![],
    }
}

impl Default for PolicyView {
    fn default() -> Self {
        // Allow operators to override the boot-time default via a single
        // env var. The admin console / MSI installer writes this from the
        // tenant preference (see `apps/api/internal/tenant/screenshot_handler.go`
        // and `apps/console/src/app/[locale]/(app)/settings/general/`).
        let preset = std::env::var("PERSONEL_SCREENSHOT_PRESET")
            .ok()
            .unwrap_or_else(|| "high".to_string());
        let screenshot = preset_screenshot(&preset);
        Self {
            version: "default-v0".into(),
            collectors: CollectorFlags {
                process: true,
                window_focus: true,
                file: false,
                network: false,
                dns: false,
                usb: true,
                clipboard_meta: true,
                clipboard_content: false,
                keystroke_meta: true,
                keystroke_content: false,
                print: true,
                screenshot: true,
                screen_clip: false,
            },
            screenshot,
            queue: QueueSettings {
                max_bytes: 200 * 1024 * 1024,
                upload_batch_size: 100,
                upload_batch_interval_ms: 5_000,
            },
            idle_threshold_secs: 300,
            keystroke_content_enabled: false,
            blocked_exe_globs: vec![],
            blocked_host_globs: vec![],
        }
    }
}

impl PolicyView {
    fn from_bundle(bundle: &PolicyBundle) -> Self {
        let collectors = bundle.collectors.clone().unwrap_or_default();
        let screenshot = bundle.screenshot.clone().unwrap_or_default();
        let queue = bundle.queue.clone().unwrap_or_default();
        let idle_threshold_secs = bundle
            .idle
            .as_ref()
            .map(|i| i.idle_threshold_seconds)
            .unwrap_or(300);
        let keystroke_content_enabled = bundle
            .keystroke
            .as_ref()
            .map(|k| k.content_enabled)
            .unwrap_or(false);

        let blocked_exe_globs = bundle
            .app_blocklist
            .as_ref()
            .map(|bl| {
                bl.rules
                    .iter()
                    .map(|r| (r.exe_name_glob.clone(), r.rule_id.clone()))
                    .collect()
            })
            .unwrap_or_default();

        let blocked_host_globs = bundle
            .url_blocklist
            .as_ref()
            .map(|bl| {
                bl.rules
                    .iter()
                    .map(|r| (r.host_glob.clone(), r.rule_id.clone()))
                    .collect()
            })
            .unwrap_or_default();

        Self {
            version: bundle.version.clone(),
            collectors,
            screenshot,
            queue,
            idle_threshold_secs,
            keystroke_content_enabled,
            blocked_exe_globs,
            blocked_host_globs,
        }
    }

    /// Returns `true` if `exe_name` matches any blocked application rule.
    ///
    /// Matching is case-insensitive glob (only `*` and `?` wildcards for now).
    #[must_use]
    pub fn is_exe_blocked(&self, exe_name: &str) -> Option<&str> {
        let lower = exe_name.to_lowercase();
        for (glob, rule_id) in &self.blocked_exe_globs {
            if glob_matches(&glob.to_lowercase(), &lower) {
                return Some(rule_id.as_str());
            }
        }
        None
    }

    /// Returns `true` if `host` matches any blocked URL rule.
    #[must_use]
    pub fn is_host_blocked(&self, host: &str) -> Option<&str> {
        let lower = host.to_lowercase();
        for (glob, rule_id) in &self.blocked_host_globs {
            if glob_matches(&glob.to_lowercase(), &lower) {
                return Some(rule_id.as_str());
            }
        }
        None
    }
}

/// Minimal glob matcher supporting `*` (any sequence) and `?` (any char).
fn glob_matches(pattern: &str, input: &str) -> bool {
    let pat: Vec<char> = pattern.chars().collect();
    let inp: Vec<char> = input.chars().collect();
    glob_match_inner(&pat, &inp)
}

fn glob_match_inner(pat: &[char], inp: &[char]) -> bool {
    match (pat.first(), inp.first()) {
        (None, None) => true,
        (Some(&'*'), _) => {
            // Match zero characters.
            glob_match_inner(&pat[1..], inp)
            // Or consume one character and try again.
            || (!inp.is_empty() && glob_match_inner(pat, &inp[1..]))
        }
        (Some(&'?'), Some(_)) => glob_match_inner(&pat[1..], &inp[1..]),
        (Some(p), Some(i)) if p == i => glob_match_inner(&pat[1..], &inp[1..]),
        _ => false,
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// PolicyEngine
// ──────────────────────────────────────────────────────────────────────────────

/// Manages the live policy bundle and distributes updates to collectors.
///
/// The engine is shared via `Arc`; collectors subscribe to the
/// `tokio::sync::watch` channel to receive `Arc<PolicyView>` updates.
pub struct PolicyEngine {
    /// Sender half — updated when a new bundle is applied.
    tx: watch::Sender<Arc<PolicyView>>,
    /// Baked-in Ed25519 verifying key for policy bundle signatures.
    /// `None` in test mode (signature verification skipped).
    signing_key: Option<VerifyingKey>,
}

impl PolicyEngine {
    /// Creates a new engine with the default policy and a baked-in signing key.
    ///
    /// `signing_key_bytes` must be 32 bytes (Ed25519 compressed public key).
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::PolicySignature`] if `signing_key_bytes` is not
    /// a valid Ed25519 compressed point.
    pub fn new(signing_key_bytes: &[u8; 32]) -> Result<(Arc<Self>, watch::Receiver<Arc<PolicyView>>)> {
        let verifying_key = VerifyingKey::from_bytes(signing_key_bytes)
            .map_err(|_| AgentError::PolicySignature)?;
        let (tx, rx) = watch::channel(Arc::new(PolicyView::default()));
        Ok((
            Arc::new(Self { tx, signing_key: Some(verifying_key) }),
            rx,
        ))
    }

    /// Creates an engine that skips signature verification.
    ///
    /// **Only use in tests or during development.** Production code must use
    /// [`new`] with the real signing key.
    #[must_use]
    pub fn new_unsigned() -> (Arc<Self>, watch::Receiver<Arc<PolicyView>>) {
        let (tx, rx) = watch::channel(Arc::new(PolicyView::default()));
        (Arc::new(Self { tx, signing_key: None }), rx)
    }

    /// Returns the current policy view.
    #[must_use]
    pub fn current(&self) -> Arc<PolicyView> {
        self.tx.borrow().clone()
    }

    /// Applies a new policy bundle, verifying its signature.
    ///
    /// On success, sends the new `PolicyView` to all subscribers.
    ///
    /// # Errors
    ///
    /// - [`AgentError::PolicySignature`] if signature verification fails.
    /// - [`AgentError::PolicyDeserialize`] if bundle decoding fails.
    pub fn apply(&self, bundle: PolicyBundle, signature: &[u8]) -> Result<()> {
        // Verify Ed25519 signature over canonical prost encoding of bundle.
        if let Some(ref vk) = self.signing_key {
            if signature.is_empty() {
                return Err(AgentError::PolicySignature);
            }
            let sig_bytes: [u8; 64] = signature.try_into().map_err(|_| {
                AgentError::PolicyDeserialize("signature must be 64 bytes".into())
            })?;
            let sig = Signature::from_bytes(&sig_bytes);

            // Canonical encoding: prost encode the bundle to bytes.
            let mut encoded = Vec::with_capacity(bundle.encoded_len());
            bundle.encode(&mut encoded)?;

            use ed25519_dalek::Verifier;
            vk.verify(&encoded, &sig).map_err(|e| {
                warn!("policy signature verification failed: {e}");
                AgentError::PolicySignature
            })?;
        }

        let version = bundle.version.clone();
        let view = Arc::new(PolicyView::from_bundle(&bundle));

        // Atomic send — all subscribers see the new view on their next borrow.
        self.tx.send(view).map_err(|_| {
            error!("policy watch channel closed; no subscribers?");
            AgentError::Internal("policy watch channel closed".into())
        })?;

        info!(version, "policy applied");
        Ok(())
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn make_bundle(version: &str) -> PolicyBundle {
        PolicyBundle {
            version: version.into(),
            tenant_id: None,
            endpoint_id: None,
            collectors: Some(CollectorFlags {
                process: true,
                window_focus: true,
                ..Default::default()
            }),
            app_blocklist: Some(personel_proto::v1::AppBlocklist {
                rules: vec![personel_proto::v1::AppRule {
                    rule_id: "r1".into(),
                    exe_name_glob: "tor*.exe".into(),
                    ..Default::default()
                }],
            }),
            ..Default::default()
        }
    }

    #[test]
    fn apply_updates_view() {
        let (engine, mut rx) = PolicyEngine::new_unsigned();
        let bundle = make_bundle("v2");
        engine.apply(bundle, &[]).unwrap();
        rx.mark_changed();
        let view = rx.borrow_and_update().clone();
        assert_eq!(view.version, "v2");
    }

    #[test]
    fn glob_block_match() {
        let (engine, _rx) = PolicyEngine::new_unsigned();
        let bundle = make_bundle("v1");
        engine.apply(bundle, &[]).unwrap();
        let view = engine.current();
        assert!(view.is_exe_blocked("TorBrowser.exe").is_some());
        assert!(view.is_exe_blocked("chrome.exe").is_none());
    }

    #[test]
    fn glob_wildcard() {
        assert!(glob_matches("*.exe", "chrome.exe"));
        assert!(glob_matches("tor*.exe", "torbrowser.exe"));
        assert!(!glob_matches("tor*.exe", "chrome.exe"));
        assert!(glob_matches("?hrome.exe", "chrome.exe"));
    }
}
