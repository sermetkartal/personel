//! Clipboard *content* (DLP-gated) collector — **DORMANT by default per
//! [ADR 0013]**.
//!
//! [ADR 0013]: ../../../../../../docs/adr/0013-dlp-disabled-by-default.md
//!
//! # What this collector is
//!
//! A separate collector from [`crate::clipboard`]. The existing
//! `ClipboardCollector` always emits `clipboard.metadata` (format list, char
//! count, no content) and — when both `policy.collectors.clipboard_content`
//! AND `ctx.pe_dek.is_some()` — emits `clipboard.content_encrypted` as a
//! single envelope per change.
//!
//! `ClipboardContentRedactedCollector` adds a second, *parallel* lane
//! reserved for the **redacted-then-encrypted** content path used by the
//! Phase 2 DLP rule engine. It pre-redacts well-known Turkish & international
//! PII patterns (TCKN, IBAN, credit card numbers, email, phone) BEFORE the
//! ciphertext leaves the endpoint, so the DLP service in the data centre
//! never sees raw values for those classes — even though the underlying
//! envelope key (PE-DEK) would technically permit it.
//!
//! # ADR 0013 default-OFF guarantee
//!
//! Per ADR 0013 §"Default state at install", DLP is **disabled by default**.
//! The opt-in ceremony is `infra/scripts/dlp-enable.sh` and produces the
//! Vault Secret ID that allows the agent to provision a `pe_dek` at runtime.
//!
//! Without that ceremony:
//!
//! - `CollectorCtx::pe_dek` is `None`
//! - This collector starts, logs an explicit DLP-OFF banner citing ADR 0013,
//!   parks on its stop oneshot, and **emits zero events** for the entire
//!   lifetime of the agent process
//! - No clipboard listener is registered, no Win32 window is created, no
//!   plaintext ever leaves the message-pump thread (because there is no
//!   message pump)
//!
//! When DLP is enabled (`pe_dek == Some(...)`), the collector — in a future
//! Phase 2 wiring — will:
//!
//! 1. Subscribe to `WM_CLIPBOARDUPDATE` (same mechanism as
//!    [`crate::clipboard`])
//! 2. On each change, read `CF_UNICODETEXT`
//! 3. Apply [`apply_all_redactions`] to substitute `[TCKN]`, `[IBAN]`,
//!    `[CARD]`, `[EMAIL]`, `[PHONE]` markers
//! 4. AES-256-GCM encrypt the *redacted* string under PE-DEK
//! 5. Emit `clipboard.content_encrypted` with the redacted ciphertext and
//!    the list of rule names that matched
//!
//! Phase 1 implements steps 1-2 (start + DLP-off check + park) plus the
//! complete redaction helper library (steps 3 / Phase 2 prerequisites) so
//! the helpers are unit-testable today and ready to wire when DLP ships.
//!
//! # Why redact + encrypt and not just encrypt?
//!
//! Defence in depth. ADR 0013 already gives the DLP service exclusive
//! decryption authority via Vault PE-DEK delegation, but a compromised DLP
//! service or an over-scoped DLP rule could still expose raw TCKNs / cards.
//! Redacting at the endpoint means the endpoint is the *only* component
//! that ever holds the raw values, and only for the milliseconds between
//! `GetClipboardData` and the redaction pass.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;
use tracing::{info, warn};

use personel_core::error::Result;
use personel_crypto::Aes256Key;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

// ──────────────────────────────────────────────────────────────────────────────
// Collector
// ──────────────────────────────────────────────────────────────────────────────

/// DLP-gated clipboard *content* collector with endpoint-side PII redaction.
///
/// **Dormant by default per ADR 0013.** See module documentation.
#[derive(Default)]
pub struct ClipboardContentRedactedCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl ClipboardContentRedactedCollector {
    /// Creates a new [`ClipboardContentRedactedCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for ClipboardContentRedactedCollector {
    fn name(&self) -> &'static str {
        "clipboard_content_redacted"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["clipboard.content_encrypted"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        // ADR 0013 gate — evaluated ONCE at startup.
        //
        // We deliberately read `pe_dek` once here instead of polling, because
        // ADR 0013 requires the DLP enable ceremony to restart the agent
        // process (Vault Secret ID is loaded at boot, not at runtime). If a
        // future ADR amendment relaxes that, this gate becomes a watch on
        // `ctx.policy_rx` — but until then the boot-time check is sufficient
        // and avoids accidentally racing partial PE-DEK provisioning.
        let pe_dek_present = ctx.pe_dek.is_some();

        let task = tokio::spawn(async move {
            if !pe_dek_present {
                // The single most important log line operators look for when
                // diagnosing "why isn't the DLP collector emitting?". Cite
                // ADR 0013 by name so a `grep "ADR 0013"` over agent logs
                // surfaces every dormant collector instance.
                info!(
                    target: "personel::collectors::clipboard_content_redacted",
                    "clipboard_content_redacted: DORMANT — DLP disabled per ADR 0013 (no PE-DEK provisioned). Run infra/scripts/dlp-enable.sh and restart the agent to activate.",
                );
                healthy.store(true, Ordering::Relaxed);
                let _ = (events, drops);
                let _ = stop_rx.await;
                return;
            }

            // Phase 2 wiring point. When DLP ships, the dormant branch above
            // is replaced by the active branch below — currently scaffolded
            // as a warn so any accidental enable surfaces immediately.
            //
            // The real implementation will:
            //   1. Spawn a `tokio::task::spawn_blocking` running the same
            //      Win32 message-only window pattern as `clipboard.rs`.
            //   2. On `WM_CLIPBOARDUPDATE` call `read_clipboard_text()`
            //      (shared helper, factor out from clipboard.rs).
            //   3. Pass the plaintext through `apply_all_redactions`.
            //   4. Encrypt the redacted bytes via `encrypt_for_dlp`.
            //   5. Build the JSON payload matching the documented schema
            //      (content_hash / nonce / ciphertext / dlp_rules_matched /
            //      preview_length / timestamp) and enqueue with
            //      `Priority::High`.
            warn!(
                target: "personel::collectors::clipboard_content_redacted",
                "clipboard_content_redacted: PE-DEK present but Phase 2 active loop not yet wired — parking",
            );
            healthy.store(true, Ordering::Relaxed);
            let _ = stop_rx.await;
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
// Redaction helpers (Phase 2 ready, fully unit-tested in Phase 1)
// ──────────────────────────────────────────────────────────────────────────────

/// Rule name constants used in the `dlp_rules_matched` payload field.
mod rule {
    pub const TCKN: &str = "tckn";
    pub const IBAN: &str = "iban";
    pub const CARD: &str = "card";
    pub const EMAIL: &str = "email";
    pub const PHONE: &str = "phone";
}

/// Replaces every Turkish national ID number (TCKN) in `input` with `[TCKN]`.
///
/// A TCKN is exactly 11 digits and satisfies the official checksum rules:
/// - 1st digit ≠ 0
/// - sum_of_first_10 % 10 == digit_11
/// - (7 * (d1+d3+d5+d7+d9) - (d2+d4+d6+d8)) % 10 == d10
///
/// Returns `(redacted_string, matched_at_least_once)`.
#[must_use]
pub fn redact_tckn(input: &str) -> (String, bool) {
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len());
    let mut matched = false;
    let mut i = 0;
    while i < bytes.len() {
        if i + 11 <= bytes.len() && bytes[i..i + 11].iter().all(u8::is_ascii_digit) {
            // Boundary check — make sure we are not in the middle of a
            // longer digit run (otherwise "123456789012345" would match
            // its first 11 chars wrongly).
            let left_ok = i == 0 || !bytes[i - 1].is_ascii_digit();
            let right_ok = i + 11 == bytes.len() || !bytes[i + 11].is_ascii_digit();
            if left_ok && right_ok && is_valid_tckn(&bytes[i..i + 11]) {
                out.push_str("[TCKN]");
                matched = true;
                i += 11;
                continue;
            }
        }
        out.push(bytes[i] as char);
        i += 1;
    }
    (out, matched)
}

fn is_valid_tckn(d: &[u8]) -> bool {
    if d.len() != 11 || d[0] == b'0' {
        return false;
    }
    let n: [u32; 11] = std::array::from_fn(|i| u32::from(d[i] - b'0'));
    let odd_sum = n[0] + n[2] + n[4] + n[6] + n[8];
    let even_sum = n[1] + n[3] + n[5] + n[7];
    let d10 = (odd_sum * 7 + 10 * 10 - even_sum) % 10;
    if d10 != n[9] {
        return false;
    }
    let total: u32 = n[..10].iter().sum();
    total % 10 == n[10]
}

/// Replaces every IBAN-shaped sequence in `input` with `[IBAN]`.
///
/// Heuristic: 2 letters + 2 digits + 11..30 alphanumerics, surrounded by
/// non-alphanumerics. This matches Turkish IBANs (TR + 24 chars = 26 total)
/// as well as international IBANs without locking us to a country list.
#[must_use]
pub fn redact_iban(input: &str) -> (String, bool) {
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len());
    let mut matched = false;
    let mut i = 0;
    while i < bytes.len() {
        let left_ok = i == 0 || !bytes[i - 1].is_ascii_alphanumeric();
        if left_ok
            && i + 15 <= bytes.len()
            && bytes[i].is_ascii_uppercase()
            && bytes[i + 1].is_ascii_uppercase()
            && bytes[i + 2].is_ascii_digit()
            && bytes[i + 3].is_ascii_digit()
        {
            // Walk forward over alphanumerics up to a max of 34 (IBAN cap).
            let mut end = i + 4;
            while end < bytes.len() && end - i < 34 && bytes[end].is_ascii_alphanumeric() {
                end += 1;
            }
            let len = end - i;
            // Real IBANs are 15..=34 chars total; we treat 15+ as a hit.
            if (15..=34).contains(&len) {
                out.push_str("[IBAN]");
                matched = true;
                i = end;
                continue;
            }
        }
        out.push(bytes[i] as char);
        i += 1;
    }
    (out, matched)
}

/// Replaces every Luhn-valid 13..19 digit credit card number with `[CARD]`.
///
/// Strips spaces and hyphens inside the candidate (so `4111-1111-1111-1111`
/// is recognised) but emits `[CARD]` once, replacing the entire run.
#[must_use]
pub fn redact_credit_card(input: &str) -> (String, bool) {
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len());
    let mut matched = false;
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i].is_ascii_digit() {
            // Try to greedily consume a card-shaped run starting here.
            let mut end = i;
            let mut digits: Vec<u8> = Vec::with_capacity(19);
            while end < bytes.len() && digits.len() < 19 {
                let b = bytes[end];
                if b.is_ascii_digit() {
                    digits.push(b - b'0');
                    end += 1;
                } else if (b == b' ' || b == b'-') && !digits.is_empty() && digits.len() < 19 {
                    end += 1;
                } else {
                    break;
                }
            }
            if (13..=19).contains(&digits.len()) && luhn_valid(&digits) {
                out.push_str("[CARD]");
                matched = true;
                i = end;
                continue;
            }
        }
        out.push(bytes[i] as char);
        i += 1;
    }
    (out, matched)
}

fn luhn_valid(digits: &[u8]) -> bool {
    let mut sum: u32 = 0;
    let n = digits.len();
    for (i, &d) in digits.iter().enumerate() {
        // Double every second digit from the right (i.e. positions of
        // parity (n - 1 - i) % 2 == 1).
        let from_right = n - 1 - i;
        if from_right % 2 == 1 {
            let doubled = u32::from(d) * 2;
            sum += if doubled > 9 { doubled - 9 } else { doubled };
        } else {
            sum += u32::from(d);
        }
    }
    sum % 10 == 0
}

/// Replaces every email-shaped substring in `input` with `[EMAIL]`.
///
/// Heuristic: a non-empty local part (letters/digits/`._%+-`), an `@`, a
/// non-empty domain with at least one dot and a 2+ char TLD.
#[must_use]
pub fn redact_email(input: &str) -> (String, bool) {
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len());
    let mut matched = false;
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'@' && i > 0 {
            // Walk back over the local part.
            let mut start = i;
            while start > 0 && is_local_char(bytes[start - 1]) {
                start -= 1;
            }
            // Walk forward over the domain.
            let mut end = i + 1;
            let mut saw_dot = false;
            let mut last_dot = 0usize;
            while end < bytes.len() && is_domain_char(bytes[end]) {
                if bytes[end] == b'.' {
                    saw_dot = true;
                    last_dot = end;
                }
                end += 1;
            }
            let tld_len = end.saturating_sub(last_dot + 1);
            if start < i && saw_dot && tld_len >= 2 {
                // Trim already-pushed local part bytes from `out`.
                let pushed = i - start;
                out.truncate(out.len() - pushed);
                out.push_str("[EMAIL]");
                matched = true;
                i = end;
                continue;
            }
        }
        out.push(bytes[i] as char);
        i += 1;
    }
    (out, matched)
}

fn is_local_char(b: u8) -> bool {
    b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'%' | b'+' | b'-')
}

fn is_domain_char(b: u8) -> bool {
    b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-')
}

/// Replaces every Turkish-shaped phone number in `input` with `[PHONE]`.
///
/// Recognised formats (after stripping spaces / `-` / `(` / `)` inside the
/// candidate run):
///
/// - `+90` followed by 10 digits          → 13 digits total incl. `+`
/// - `0` followed by 10 digits            → 11 digits total
/// - `5XX XXX XX XX` (mobile, 10 digits)  → 10 digits total
#[must_use]
pub fn redact_phone(input: &str) -> (String, bool) {
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len());
    let mut matched = false;
    let mut i = 0;
    while i < bytes.len() {
        // Candidate must start at a non-alphanumeric boundary.
        let left_ok = i == 0 || !bytes[i - 1].is_ascii_alphanumeric();
        if left_ok && (bytes[i] == b'+' || bytes[i].is_ascii_digit()) {
            let mut end = i;
            let mut digits: Vec<u8> = Vec::with_capacity(13);
            let mut saw_plus = false;
            if bytes[end] == b'+' {
                saw_plus = true;
                end += 1;
            }
            while end < bytes.len() && digits.len() < 13 {
                let b = bytes[end];
                if b.is_ascii_digit() {
                    digits.push(b - b'0');
                    end += 1;
                } else if matches!(b, b' ' | b'-' | b'(' | b')') && !digits.is_empty() {
                    end += 1;
                } else {
                    break;
                }
            }
            let right_ok = end == bytes.len() || !bytes[end].is_ascii_alphanumeric();
            let is_phone = right_ok
                && match (saw_plus, digits.as_slice()) {
                    (true, [9, 0, rest @ ..]) if rest.len() == 10 => true,
                    (false, [0, 5, _, ..]) if digits.len() == 11 => true,
                    (false, [5, _, ..]) if digits.len() == 10 => true,
                    _ => false,
                };
            if is_phone {
                out.push_str("[PHONE]");
                matched = true;
                i = end;
                continue;
            }
        }
        out.push(bytes[i] as char);
        i += 1;
    }
    (out, matched)
}

/// Applies every redaction helper in deterministic order and returns the
/// final string plus the names of the rules that matched at least once.
///
/// Order matters: TCKN runs first because an 11-digit TCKN starting with `5`
/// could otherwise be misread by the phone helper. Email runs before phone
/// because `+90...@example.com` would otherwise have its `+90` half
/// re-classified.
#[must_use]
pub fn apply_all_redactions(input: &str) -> (String, Vec<&'static str>) {
    let mut s = input.to_owned();
    let mut hits: Vec<&'static str> = Vec::new();

    let (next, m) = redact_tckn(&s);
    s = next;
    if m {
        hits.push(rule::TCKN);
    }
    let (next, m) = redact_iban(&s);
    s = next;
    if m {
        hits.push(rule::IBAN);
    }
    let (next, m) = redact_credit_card(&s);
    s = next;
    if m {
        hits.push(rule::CARD);
    }
    let (next, m) = redact_email(&s);
    s = next;
    if m {
        hits.push(rule::EMAIL);
    }
    let (next, m) = redact_phone(&s);
    s = next;
    if m {
        hits.push(rule::PHONE);
    }

    (s, hits)
}

// ──────────────────────────────────────────────────────────────────────────────
// Encryption placeholder (Phase 2)
// ──────────────────────────────────────────────────────────────────────────────

/// Encrypts `plaintext` under the supplied PE-DEK using the same AES-256-GCM
/// envelope used by `keystroke.content_encrypted`.
///
/// This wraps [`personel_crypto::envelope::encrypt`] so the DLP collector
/// never builds its own AAD format — every clipboard ciphertext travels
/// under the canonical `endpoint_id || seq` AAD shape and is byte-compatible
/// with the DLP service decryption path.
///
/// Currently *unused* in Phase 1: the active loop that calls this helper is
/// scaffolded behind `pe_dek_present` and is dormant per ADR 0013.
///
/// # Errors
///
/// Returns [`personel_core::error::AgentError::AeadError`] if the underlying
/// AES-GCM crate reports an encryption failure (should be impossible for
/// well-formed inputs).
#[allow(dead_code)]
pub(crate) fn encrypt_for_dlp(
    plaintext: &[u8],
    pe_dek: &Aes256Key,
    endpoint_id: &[u8; 16],
    seq: u64,
) -> Result<personel_crypto::envelope::CipherEnvelope> {
    let aad = personel_crypto::envelope::build_keystroke_aad(endpoint_id, seq);
    personel_crypto::envelope::encrypt(pe_dek, aad, plaintext)
}

/// Computes a fixed-length stable digest for the redacted preview field of
/// the JSON payload. **Not a SHA-256** — the `sha2` crate is gated on
/// Windows-only in this crate's `Cargo.toml`, so we use a small stable
/// FNV-1a 64-bit fingerprint here. The DLP rule engine does not rely on
/// this digest for security; it exists only so the data-plane can detect
/// repeated identical clipboard contents without re-decrypting.
///
/// When the active loop ships and the `sha2` dep is promoted to a
/// cross-platform direct dep, replace this body with `Sha256::digest`.
#[allow(dead_code)]
pub(crate) fn content_digest_hex(input: &[u8]) -> String {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for &b in input {
        h ^= u64::from(b);
        h = h.wrapping_mul(0x0000_0100_0000_01B3);
    }
    format!("{h:016x}")
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    // ── TCKN ────────────────────────────────────────────────────────────────

    #[test]
    fn tckn_known_valid_is_redacted() {
        // 10000000146 is the canonical TC test value (passes both checks).
        let (out, hit) = redact_tckn("ID: 10000000146 ok");
        assert!(hit);
        assert_eq!(out, "ID: [TCKN] ok");
    }

    #[test]
    fn tckn_invalid_checksum_is_left_alone() {
        let (out, hit) = redact_tckn("ID: 12345678901 ok");
        assert!(!hit);
        assert_eq!(out, "ID: 12345678901 ok");
    }

    #[test]
    fn tckn_starting_with_zero_is_invalid() {
        let (out, hit) = redact_tckn("01234567890");
        assert!(!hit);
        assert_eq!(out, "01234567890");
    }

    #[test]
    fn tckn_inside_longer_digit_run_is_not_redacted() {
        // 12-digit run must not produce a partial match.
        let (out, hit) = redact_tckn("100000001460");
        assert!(!hit);
        assert_eq!(out, "100000001460");
    }

    // ── IBAN ────────────────────────────────────────────────────────────────

    #[test]
    fn iban_turkish_redacted() {
        let (out, hit) = redact_iban("Hesap TR330006100519786457841326 lütfen");
        assert!(hit);
        assert_eq!(out, "Hesap [IBAN] lütfen");
    }

    #[test]
    fn iban_too_short_left_alone() {
        let (out, hit) = redact_iban("XX12ABC");
        assert!(!hit);
        assert_eq!(out, "XX12ABC");
    }

    // ── Credit card ─────────────────────────────────────────────────────────

    #[test]
    fn card_visa_test_number_redacted() {
        let (out, hit) = redact_credit_card("Card 4111111111111111 ok");
        assert!(hit);
        assert_eq!(out, "Card [CARD] ok");
    }

    #[test]
    fn card_with_separators_redacted() {
        let (out, hit) = redact_credit_card("4111-1111-1111-1111");
        assert!(hit);
        assert_eq!(out, "[CARD]");
    }

    #[test]
    fn card_invalid_luhn_left_alone() {
        let (out, hit) = redact_credit_card("4111111111111112");
        assert!(!hit);
        assert_eq!(out, "4111111111111112");
    }

    // ── Email ───────────────────────────────────────────────────────────────

    #[test]
    fn email_simple_redacted() {
        let (out, hit) = redact_email("contact alice@example.com today");
        assert!(hit);
        assert_eq!(out, "contact [EMAIL] today");
    }

    #[test]
    fn email_with_plus_and_dot_redacted() {
        let (out, hit) = redact_email("alice.smith+test@sub.example.co");
        assert!(hit);
        assert_eq!(out, "[EMAIL]");
    }

    #[test]
    fn email_no_at_left_alone() {
        let (out, hit) = redact_email("not an email");
        assert!(!hit);
        assert_eq!(out, "not an email");
    }

    // ── Phone ───────────────────────────────────────────────────────────────

    #[test]
    fn phone_turkish_international_redacted() {
        let (out, hit) = redact_phone("Call +905551234567 now");
        assert!(hit);
        assert_eq!(out, "Call [PHONE] now");
    }

    #[test]
    fn phone_turkish_local_redacted() {
        let (out, hit) = redact_phone("05551234567");
        assert!(hit);
        assert_eq!(out, "[PHONE]");
    }

    #[test]
    fn phone_random_digits_left_alone() {
        let (out, hit) = redact_phone("123");
        assert!(!hit);
        assert_eq!(out, "123");
    }

    // ── apply_all ───────────────────────────────────────────────────────────

    #[test]
    fn apply_all_handles_mixed_input() {
        let s = "ID 10000000146, IBAN TR330006100519786457841326, \
                 card 4111-1111-1111-1111, mail a@b.co, tel +905551234567";
        let (out, hits) = apply_all_redactions(s);
        assert!(out.contains("[TCKN]"), "out = {out}");
        assert!(out.contains("[IBAN]"), "out = {out}");
        assert!(out.contains("[CARD]"), "out = {out}");
        assert!(out.contains("[EMAIL]"), "out = {out}");
        assert!(out.contains("[PHONE]"), "out = {out}");
        assert_eq!(hits.len(), 5);
        assert!(hits.contains(&"tckn"));
        assert!(hits.contains(&"iban"));
        assert!(hits.contains(&"card"));
        assert!(hits.contains(&"email"));
        assert!(hits.contains(&"phone"));
    }

    #[test]
    fn apply_all_clean_input_leaves_empty_hits() {
        let (out, hits) = apply_all_redactions("hello world");
        assert_eq!(out, "hello world");
        assert!(hits.is_empty());
    }

    // ── Digest ──────────────────────────────────────────────────────────────

    #[test]
    fn digest_is_stable() {
        let a = content_digest_hex(b"abc");
        let b = content_digest_hex(b"abc");
        assert_eq!(a, b);
        assert_eq!(a.len(), 16);
    }

    #[test]
    fn digest_differs_for_different_inputs() {
        let a = content_digest_hex(b"abc");
        let b = content_digest_hex(b"abd");
        assert_ne!(a, b);
    }
}
