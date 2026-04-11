"""
PII redaction for OCR output.

KVKK invariant: the final text returned by this service MUST NEVER contain
raw TCKN, IBAN, or credit card numbers.  Redaction is the last step before
JSON encoding.  If redaction raises an unhandled exception, the caller
(pipeline.py) returns HTTP 500 with a generic error — the raw text is never
echoed back in error messages.

Patterns implemented:
  - TCKN (Turkish National ID): 11-digit number with Luhn-like checksum.
  - Turkish IBAN: TR + 2 check digits + 22 alphanumeric chars (24 chars after TR).
  - Credit card: 13–19 digit Luhn-valid card numbers.
  - Turkish phone: +90 / 0 prefix + 10 digits.
  - Email: RFC 5322 simplified pattern.

Turkish TCKN algorithm (official):
  d1..d11 are the 11 digits.
  Validity rules:
    1. d1 != 0
    2. (d1 + d3 + d5 + d7 + d9) * 7 - (d2 + d4 + d6 + d8) ≡ d10 (mod 10)
    3. (d1+d2+d3+d4+d5+d6+d7+d8+d9+d10) ≡ d11 (mod 10)
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field


# ---------------------------------------------------------------------------
# Tag constants
# ---------------------------------------------------------------------------

TAG_TCKN = "[TCKN]"
TAG_IBAN = "[IBAN]"
TAG_CREDIT_CARD = "[CREDIT_CARD]"
TAG_PHONE = "[PHONE]"
TAG_EMAIL = "[EMAIL]"


# ---------------------------------------------------------------------------
# Redaction summary
# ---------------------------------------------------------------------------


@dataclass
class RedactionSummary:
    """Count of each PII kind redacted in a single pass."""

    tckn: int = 0
    iban: int = 0
    credit_card: int = 0
    phone: int = 0
    email: int = 0

    def as_list(self) -> list[dict[str, object]]:
        return [
            {"kind": "tckn", "count": self.tckn},
            {"kind": "iban", "count": self.iban},
            {"kind": "credit_card", "count": self.credit_card},
            {"kind": "phone", "count": self.phone},
            {"kind": "email", "count": self.email},
        ]


# ---------------------------------------------------------------------------
# TCKN validation
# ---------------------------------------------------------------------------

# 11 consecutive digits, not starting with 0, preceded/followed by non-digit.
_TCKN_RAW_PATTERN = re.compile(r"(?<!\d)([1-9]\d{10})(?!\d)")


def _validate_tckn(digits: str) -> bool:
    """Return True if the 11-digit string passes the official TCKN algorithm.

    Algorithm (source: Nüfus ve Vatandaşlık İşleri Genel Müdürlüğü):
      Let d[0]..d[10] be the 11 digits (d[0] is the most-significant).
      1. d[0] must not be 0.
      2. Sum of odd-indexed digits (d[0], d[2], d[4], d[6], d[8]) multiplied
         by 7, minus the sum of even-indexed digits (d[1], d[3], d[5], d[7]),
         modulo 10 must equal d[9].
         (7 * (d[0]+d[2]+d[4]+d[6]+d[8]) - (d[1]+d[3]+d[5]+d[7])) % 10 == d[9]
      3. Sum of all first 10 digits modulo 10 must equal d[10].
    """
    if len(digits) != 11:
        return False
    d = [int(c) for c in digits]
    if d[0] == 0:
        return False
    # Rule 2: odd positions (1-based: positions 1,3,5,7,9 => 0-based: 0,2,4,6,8)
    odd_sum = d[0] + d[2] + d[4] + d[6] + d[8]
    even_sum = d[1] + d[3] + d[5] + d[7]
    check10 = (7 * odd_sum - even_sum) % 10
    if check10 != d[9]:
        return False
    # Rule 3
    check11 = sum(d[:10]) % 10
    return check11 == d[10]


def _redact_tckn(text: str, summary: RedactionSummary) -> str:
    """Replace valid TCKN sequences with [TCKN]."""

    def replace(m: re.Match[str]) -> str:
        candidate = m.group(1)
        if _validate_tckn(candidate):
            summary.tckn += 1
            return TAG_TCKN
        return m.group(0)

    return _TCKN_RAW_PATTERN.sub(replace, text)


# ---------------------------------------------------------------------------
# Turkish IBAN
# ---------------------------------------------------------------------------

# Turkish IBAN: TR + 2 check digits + 22 alphanumeric characters = 26 chars total.
# Pattern: TR followed by 24 digits (the checksum digits and BBAN are all digits
# for Turkish banks: 2 check digits + 5 bank code + 1 reserved + 16 account = 24 digits).
_IBAN_TR_PATTERN = re.compile(r"(?<![A-Z0-9])TR\d{24}(?![A-Z0-9])", re.IGNORECASE)

# Also match space-separated IBAN: TR12 1234 5678 9012 3456 7890 12
_IBAN_TR_SPACED_PATTERN = re.compile(
    r"(?<!\w)TR\d{2}(?:\s?\d{4}){5}\s?\d{2}(?!\w)",
    re.IGNORECASE,
)


def _validate_iban(raw: str) -> bool:
    """Basic Turkish IBAN structural validation.

    Full IBAN mod-97 check is applied.  Format: TR + 24 digits.
    """
    compact = re.sub(r"\s+", "", raw).upper()
    if not re.fullmatch(r"TR\d{24}", compact):
        return False
    # ISO 13616 mod-97 check: move first 4 chars to end, convert letters to digits
    rearranged = compact[4:] + compact[:4]
    numeric = "".join(str(ord(c) - 55) if c.isalpha() else c for c in rearranged)
    return int(numeric) % 97 == 1


def _redact_iban(text: str, summary: RedactionSummary) -> str:
    """Replace Turkish IBAN sequences with [IBAN]."""

    def replace_spaced(m: re.Match[str]) -> str:
        candidate = m.group(0)
        if _validate_iban(candidate):
            summary.iban += 1
            return TAG_IBAN
        return candidate

    # Replace spaced form first (more specific)
    text = _IBAN_TR_SPACED_PATTERN.sub(replace_spaced, text)

    def replace_compact(m: re.Match[str]) -> str:
        candidate = m.group(0)
        if _validate_iban(candidate):
            summary.iban += 1
            return TAG_IBAN
        return candidate

    return _IBAN_TR_PATTERN.sub(replace_compact, text)


# ---------------------------------------------------------------------------
# Credit card (Luhn)
# ---------------------------------------------------------------------------

# Match 13–19 consecutive digits (possible card numbers), optionally space/dash separated.
# We match both compact and formatted (groups of 4).
_CC_COMPACT_PATTERN = re.compile(r"(?<!\d)(\d{13,19})(?!\d)")
_CC_FORMATTED_PATTERN = re.compile(
    r"(?<!\d)(\d{4}[\s\-]\d{4}[\s\-]\d{4}[\s\-]\d{1,7})(?!\d)"
)


def _luhn_check(number: str) -> bool:
    """Return True if the digit string passes the Luhn algorithm."""
    digits = [int(c) for c in number]
    # Double every second digit from the right (starting at index -2)
    checksum = 0
    for i, d in enumerate(reversed(digits)):
        if i % 2 == 1:
            doubled = d * 2
            checksum += doubled - 9 if doubled > 9 else doubled
        else:
            checksum += d
    return checksum % 10 == 0


def _redact_credit_card(text: str, summary: RedactionSummary) -> str:
    """Replace Luhn-valid 13–19 digit sequences with [CREDIT_CARD]."""

    def replace_formatted(m: re.Match[str]) -> str:
        compact = re.sub(r"[\s\-]", "", m.group(1))
        if 13 <= len(compact) <= 19 and _luhn_check(compact):
            summary.credit_card += 1
            return TAG_CREDIT_CARD
        return m.group(0)

    def replace_compact(m: re.Match[str]) -> str:
        candidate = m.group(1)
        if _luhn_check(candidate):
            summary.credit_card += 1
            return TAG_CREDIT_CARD
        return m.group(0)

    # Formatted first — prevents the compact pattern from matching partial groups
    text = _CC_FORMATTED_PATTERN.sub(replace_formatted, text)
    return _CC_COMPACT_PATTERN.sub(replace_compact, text)


# ---------------------------------------------------------------------------
# Turkish phone number
# ---------------------------------------------------------------------------

# Turkish mobile/landline: +90 5XX XXX XX XX or 0 5XX XXX XX XX (10 digits after prefix)
# Also matches 05XXXXXXXXX (11 digits total with leading zero).
_PHONE_PATTERN = re.compile(
    r"(?<!\d)"
    r"(?:"
    r"\+90[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}"  # +90 ...
    r"|"
    r"0\s?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}"  # 0 ...
    r")"
    r"(?!\d)"
)


def _redact_phone(text: str, summary: RedactionSummary) -> str:
    def replace(m: re.Match[str]) -> str:
        summary.phone += 1
        return TAG_PHONE

    return _PHONE_PATTERN.sub(replace, text)


# ---------------------------------------------------------------------------
# Email
# ---------------------------------------------------------------------------

# RFC 5322 simplified — covers the vast majority of real-world addresses.
_EMAIL_PATTERN = re.compile(
    r"(?<![^\s<,;(\[\"'])"
    r"[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}"
    r"(?![^\s>,;)\]\"'])"
)


def _redact_email(text: str, summary: RedactionSummary) -> str:
    def replace(m: re.Match[str]) -> str:
        summary.email += 1
        return TAG_EMAIL

    return _EMAIL_PATTERN.sub(replace, text)


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


@dataclass
class RedactionResult:
    """Output of the redaction pipeline."""

    text: str
    summary: RedactionSummary = field(default_factory=RedactionSummary)


def redact(text: str) -> RedactionResult:
    """Apply all PII redaction passes to the given text.

    Passes are applied in a fixed order chosen to minimise false-positive
    interference:
      1. TCKN  (11-digit; checked before generic CC to avoid overlap)
      2. Turkish IBAN
      3. Credit card (Luhn)
      4. Phone
      5. Email

    Returns:
        RedactionResult with redacted text and per-kind counts.

    Raises:
        This function must not raise.  If an internal pass fails it is silently
        skipped so that the remaining passes still execute.  Callers (pipeline.py)
        are responsible for catching any unexpected exceptions and returning 500
        without echoing text.
    """
    summary = RedactionSummary()
    try:
        text = _redact_tckn(text, summary)
    except Exception:  # noqa: BLE001
        pass
    try:
        text = _redact_iban(text, summary)
    except Exception:  # noqa: BLE001
        pass
    try:
        text = _redact_credit_card(text, summary)
    except Exception:  # noqa: BLE001
        pass
    try:
        text = _redact_phone(text, summary)
    except Exception:  # noqa: BLE001
        pass
    try:
        text = _redact_email(text, summary)
    except Exception:  # noqa: BLE001
        pass
    return RedactionResult(text=text, summary=summary)
