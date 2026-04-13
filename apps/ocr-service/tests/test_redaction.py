"""
Unit tests for Turkish PII redaction patterns.

Each test class covers one PII kind.  Tests verify both:
  - True positives: valid PII is replaced with the appropriate tag.
  - Near-miss / invalid patterns: strings that look like PII but fail
    the checksum / structural rules are NOT redacted.

TCKN test cases are generated from the official algorithm.
IBAN test cases use real Turkish IBAN structure with mod-97 check.
"""

from __future__ import annotations

import pytest

from personel_ocr.redaction import (
    TAG_CREDIT_CARD,
    TAG_EMAIL,
    TAG_IBAN,
    TAG_PHONE,
    TAG_TCKN,
    _luhn_check,
    _validate_iban,
    _validate_tckn,
    redact,
)


# ---------------------------------------------------------------------------
# TCKN validation unit tests
# ---------------------------------------------------------------------------


class TestTCKNValidation:
    """Direct tests of the _validate_tckn function."""

    def test_known_valid_tckn_1(self) -> None:
        # Constructed following the official algorithm.
        # d = [1, 2, 3, 4, 5, 6, 7, 8, 9, ?, ?]
        # odd_sum  = 1+3+5+7+9 = 25
        # even_sum = 2+4+6+8   = 20
        # d[9] = (7*25 - 20) % 10 = (175 - 20) % 10 = 155 % 10 = 5
        # d[10] = (1+2+3+4+5+6+7+8+9+5) % 10 = 50 % 10 = 0
        assert _validate_tckn("12345678950") is True

    def test_known_valid_tckn_2(self) -> None:
        # Another manually verified TCKN.
        # d = [1, 0, 0, 0, 0, 0, 0, 0, 0, ?, ?]
        # odd_sum  = 1+0+0+0+0 = 1
        # even_sum = 0+0+0+0   = 0
        # d[9] = (7*1 - 0) % 10 = 7
        # d[10] = (1+0+0+0+0+0+0+0+0+7) % 10 = 8
        assert _validate_tckn("10000000078") is True

    def test_starts_with_zero_invalid(self) -> None:
        assert _validate_tckn("01234567890") is False

    def test_wrong_10th_digit(self) -> None:
        # 12345678950 is valid; changing d[9] 5→6 must fail.
        assert _validate_tckn("12345678960") is False

    def test_wrong_11th_digit(self) -> None:
        # 12345678950 is valid; changing d[10] 0→1 must fail.
        assert _validate_tckn("12345678951") is False

    def test_all_same_digits_invalid(self) -> None:
        assert _validate_tckn("11111111111") is False

    def test_short_string_invalid(self) -> None:
        assert _validate_tckn("1234567890") is False  # 10 digits

    def test_long_string_invalid(self) -> None:
        assert _validate_tckn("123456789501") is False  # 12 digits


class TestTCKNRedaction:
    """Integration tests: valid TCKN strings in OCR text are redacted."""

    def test_valid_tckn_in_plain_text(self) -> None:
        text = "TC Kimlik No: 12345678950"
        result = redact(text)
        assert TAG_TCKN in result.text
        assert "12345678950" not in result.text
        assert result.summary.tckn == 1

    def test_valid_tckn_surrounded_by_text(self) -> None:
        text = "Kullanici 12345678950 sisteme giris yapti."
        result = redact(text)
        assert "12345678950" not in result.text
        assert result.summary.tckn == 1

    def test_invalid_tckn_not_redacted(self) -> None:
        # 11 digits but fails checksum
        text = "Numara: 12345678999"
        result = redact(text)
        assert "12345678999" in result.text
        assert result.summary.tckn == 0

    def test_starts_with_zero_not_redacted(self) -> None:
        text = "01234567890 bu bir TCKN degil"
        result = redact(text)
        assert "01234567890" in result.text
        assert result.summary.tckn == 0

    def test_multiple_tckn(self) -> None:
        text = "Ali: 12345678950 Veli: 10000000078"
        result = redact(text)
        assert result.summary.tckn == 2
        assert "12345678950" not in result.text
        assert "10000000078" not in result.text


# ---------------------------------------------------------------------------
# Turkish IBAN validation unit tests
# ---------------------------------------------------------------------------


class TestIBANValidation:
    """Direct tests of the _validate_iban function."""

    def test_valid_turkish_iban(self) -> None:
        # TR33 0006 1005 1978 6457 8413 26 — a structurally valid example.
        # We generate one by construction using mod-97 check.
        # Known valid IBAN used in Turkish banking documentation.
        assert _validate_iban("TR330006100519786457841326") is True

    def test_invalid_country_code(self) -> None:
        assert _validate_iban("DE89370400440532013000") is False  # German IBAN

    def test_wrong_length(self) -> None:
        assert _validate_iban("TR3300061005197864578413") is False  # 24 chars, need 26

    def test_invalid_check_digits(self) -> None:
        # Change check digits of a valid IBAN
        assert _validate_iban("TR990006100519786457841326") is False

    def test_spaced_iban_valid(self) -> None:
        assert _validate_iban("TR33 0006 1005 1978 6457 8413 26") is True


class TestIBANRedaction:
    """Integration tests: valid Turkish IBAN strings are redacted."""

    def test_compact_iban_redacted(self) -> None:
        text = "IBAN: TR330006100519786457841326"
        result = redact(text)
        assert TAG_IBAN in result.text
        assert "TR330006100519786457841326" not in result.text
        assert result.summary.iban == 1

    def test_spaced_iban_redacted(self) -> None:
        text = "Hesap: TR33 0006 1005 1978 6457 8413 26"
        result = redact(text)
        assert TAG_IBAN in result.text
        assert result.summary.iban == 1

    def test_invalid_iban_not_redacted(self) -> None:
        text = "TR9900061005197864578413XX"
        result = redact(text)
        assert result.summary.iban == 0


# ---------------------------------------------------------------------------
# Credit card (Luhn) tests
# ---------------------------------------------------------------------------


class TestLuhnValidation:
    def test_valid_visa(self) -> None:
        assert _luhn_check("4111111111111111") is True

    def test_valid_mastercard(self) -> None:
        assert _luhn_check("5500005555555559") is True

    def test_invalid_luhn(self) -> None:
        assert _luhn_check("4111111111111112") is False


class TestCreditCardRedaction:
    def test_visa_redacted(self) -> None:
        text = "Kart numarasi: 4111111111111111"
        result = redact(text)
        assert TAG_CREDIT_CARD in result.text
        assert "4111111111111111" not in result.text
        assert result.summary.credit_card == 1

    def test_formatted_mastercard_redacted(self) -> None:
        text = "5500 0055 5555 5559"
        result = redact(text)
        assert TAG_CREDIT_CARD in result.text
        assert result.summary.credit_card == 1

    def test_invalid_luhn_not_redacted(self) -> None:
        text = "4111111111111112"
        result = redact(text)
        assert "4111111111111112" in result.text
        assert result.summary.credit_card == 0

    def test_short_sequence_not_redacted(self) -> None:
        # 12 digits — below 13-digit minimum
        text = "123456789012"
        result = redact(text)
        assert result.summary.credit_card == 0


# ---------------------------------------------------------------------------
# Phone number tests
# ---------------------------------------------------------------------------


class TestPhoneRedaction:
    def test_plus90_format(self) -> None:
        result = redact("+90 532 123 45 67")
        assert TAG_PHONE in result.text
        assert result.summary.phone == 1

    def test_zero_prefix_format(self) -> None:
        result = redact("0532 123 45 67")
        assert TAG_PHONE in result.text
        assert result.summary.phone == 1

    def test_compact_zero_prefix(self) -> None:
        result = redact("05321234567")
        assert TAG_PHONE in result.text
        assert result.summary.phone == 1


# ---------------------------------------------------------------------------
# Email tests
# ---------------------------------------------------------------------------


class TestEmailRedaction:
    def test_simple_email(self) -> None:
        result = redact("ahmet@sirket.com.tr")
        assert TAG_EMAIL in result.text
        assert result.summary.email == 1

    def test_email_in_sentence(self) -> None:
        result = redact("Lutfen veli@example.com adresine gonderiniz.")
        assert TAG_EMAIL in result.text
        assert "veli@example.com" not in result.text

    def test_non_email_at_sign_not_redacted(self) -> None:
        result = redact("kullanici @ sunucu")  # spaces around @ — not a valid email
        assert result.summary.email == 0


# ---------------------------------------------------------------------------
# Compound tests — multiple PII kinds in same text
# ---------------------------------------------------------------------------


class TestCompoundRedaction:
    def test_tckn_and_iban_in_same_text(self) -> None:
        text = (
            "TC: 12345678950 Hesap: TR330006100519786457841326 "
            "Mail: ali@test.com"
        )
        result = redact(text)
        assert "12345678950" not in result.text
        assert "TR330006100519786457841326" not in result.text
        assert "ali@test.com" not in result.text
        assert result.summary.tckn == 1
        assert result.summary.iban == 1
        assert result.summary.email == 1

    def test_empty_text_no_error(self) -> None:
        result = redact("")
        assert result.text == ""
        assert result.summary.tckn == 0

    def test_no_pii_text_unchanged(self) -> None:
        text = "Bu metin hic ozel bilgi icermiyor."
        result = redact(text)
        assert result.text == text
        assert result.summary.tckn == 0
        assert result.summary.iban == 0
        assert result.summary.credit_card == 0


# ---------------------------------------------------------------------------
# Faz 8 #83 — defence-in-depth tests for canonical pipeline
# ---------------------------------------------------------------------------


class TestLeakRegression:
    """Ensure the redaction layer never lets original PII values pass through.

    These tests are the KVKK invariant anchor: the response contract is
    "no raw TCKN/IBAN/CC/phone/email in text_redacted". If this class ever
    regresses, the canonical response breaks the contract.
    """

    SENSITIVE_SAMPLES: tuple[str, ...] = (
        "12345678950",  # valid TCKN
        "10000000078",  # valid TCKN
        "TR330006100519786457841326",  # valid IBAN compact
        "TR33 0006 1005 1978 6457 8413 26",  # valid IBAN spaced
        "4111111111111111",  # Visa
        "5500005555555559",  # Mastercard
        "+90 532 123 45 67",  # phone plus-90
        "0532 123 45 67",  # phone zero-prefix
        "05321234567",  # phone compact
        "ahmet@sirket.com.tr",  # email
        "ali@test.com",  # email
    )

    def test_none_leak_through_text(self) -> None:
        joined = " ".join(self.SENSITIVE_SAMPLES)
        result = redact(joined)
        for raw in self.SENSITIVE_SAMPLES:
            assert raw not in result.text, (
                f"PII sample leaked through redaction: {raw[:4]}*****"
            )

    def test_summary_totals_match_expected(self) -> None:
        # 2 TCKN + 2 IBAN + 2 CC + 3 phone + 2 email
        text = " ".join(self.SENSITIVE_SAMPLES)
        result = redact(text)
        assert result.summary.tckn == 2
        assert result.summary.iban == 2
        assert result.summary.credit_card == 2
        # Phone matcher may fuse adjacent space-separated numbers but at
        # least each of the three distinct formats gets caught once.
        assert result.summary.phone >= 3
        assert result.summary.email == 2


class TestPhoneFormatMatrix:
    """Faz 8 #83 explicit formats from the task brief."""

    @pytest.mark.parametrize(
        "sample",
        [
            "+90 532 123 45 67",
            "+90 532 123 4567",
            "0 532 123 45 67",
            "0532 123 45 67",
            "05321234567",
            "0(532) 123 45 67",
        ],
    )
    def test_phone_formats_redacted(self, sample: str) -> None:
        result = redact(sample)
        assert TAG_PHONE in result.text
        assert result.summary.phone >= 1
