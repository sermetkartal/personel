"""
Canonical OCR response shape for Faz 8 roadmap item #83.

The original ``ExtractResponse`` in ``schemas.py`` remains stable for existing
callers (enricher, smoke tests). This module introduces a second, richer shape
that is returned by the new ``/v1/ocr/extract`` and ``/v1/ocr/batch`` routes.

Shape:

.. code-block:: json

    {
      "request_id": "01HW4...",
      "backend": "tesseract" | "paddle" | "ensemble",
      "language": "tur" | "eng" | "auto",
      "text_redacted": "...",
      "confidence_overall": 0.82,
      "word_count": 47,
      "redaction_hits": [
        {"rule": "TCKN", "count": 1},
        {"rule": "IBAN", "count": 2}
      ],
      "processing_ms": 234,
      "words": [
        {"text": "[TCKN]", "confidence": 0.93}
      ]
    }

``words`` is only populated when the caller sets ``confidence_per_word=true``
on the request. The per-word field still flows through the redaction pass,
so low-confidence PII cannot leak to the consumer.

KVKK invariant: ``text_redacted`` + every ``word.text`` entry are the
post-redaction strings. Callers never see pre-redaction output.
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field

# Redaction rule names as exposed in the canonical response. Upper-case to
# match SIEM + compliance-report conventions (TCKN/IBAN are proper nouns).
CanonicalRule = Literal["TCKN", "IBAN", "CREDIT_CARD", "PHONE", "EMAIL"]

# Internal kind -> canonical rule name mapping.
KIND_TO_RULE: dict[str, str] = {
    "tckn": "TCKN",
    "iban": "IBAN",
    "credit_card": "CREDIT_CARD",
    "phone": "PHONE",
    "email": "EMAIL",
}

BackendLiteral = Literal["tesseract", "paddle", "ensemble"]


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------


class CanonicalExtractRequest(BaseModel):
    """Single-image extraction request for the canonical endpoint."""

    image_bytes: str = Field(
        ...,
        description="Base64-encoded image bytes (JPEG/PNG/WebP/BMP).",
        min_length=4,
    )
    tenant_id: str = Field(..., description="Tenant UUID for structured logging.")
    endpoint_id: str = Field(..., description="Endpoint UUID for correlation.")
    screenshot_id: str = Field(..., description="Source screenshot UUID.")
    backend_hint: Literal["tesseract", "paddle", "auto"] = Field(
        default="auto",
        description="Preferred backend. 'auto' picks the first available.",
    )
    language: Literal["tur", "eng", "auto"] = Field(
        default="auto",
        description="Target language. 'auto' enables tur+eng pack.",
    )
    confidence_per_word: bool = Field(
        default=False,
        description=(
            "When true, per-word post-redaction confidence values are returned "
            "under the `words` field. Lets consumers filter low-confidence text."
        ),
    )


class CanonicalBatchRequest(BaseModel):
    """Batch extraction request (max 50 items, <=10 MB total)."""

    items: list[CanonicalExtractRequest] = Field(
        ...,
        min_length=1,
        max_length=50,
        description=(
            "Batch of extraction requests. Soft cap 50 items. Total decoded "
            "payload is capped at 10 MB; oversize requests are rejected 413."
        ),
    )


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


class CanonicalRedactionHit(BaseModel):
    """Summary entry for a redaction rule that matched at least once."""

    rule: CanonicalRule
    count: int = Field(..., ge=1)


class CanonicalWord(BaseModel):
    """Per-word entry. ``text`` is always post-redaction."""

    text: str
    confidence: float = Field(..., ge=0.0, le=1.0)


class CanonicalExtractResponse(BaseModel):
    """New-shape response for /v1/ocr/extract.

    KVKK invariant reminder: ``text_redacted`` and ``words[*].text`` are the
    outputs of the redaction pass. The pipeline never returns pre-redaction
    content, even on error paths.
    """

    request_id: str
    backend: BackendLiteral
    language: str
    text_redacted: str = Field(
        ...,
        description=(
            "KVKK-safe extracted text. Will never contain raw TCKN, IBAN, "
            "credit card, phone, or email values."
        ),
    )
    confidence_overall: float = Field(..., ge=0.0, le=1.0)
    word_count: int = Field(..., ge=0)
    redaction_hits: list[CanonicalRedactionHit] = Field(default_factory=list)
    processing_ms: int = Field(..., ge=0)
    words: list[CanonicalWord] | None = Field(
        default=None,
        description=(
            "Present only when the request set `confidence_per_word=true`. "
            "Each entry is the post-redaction word text + its engine confidence."
        ),
    )


class CanonicalBatchResponse(BaseModel):
    """Batch response. Individual items may carry an error slot in future."""

    request_id: str
    total: int = Field(..., ge=0)
    processing_ms: int = Field(..., ge=0)
    results: list[CanonicalExtractResponse]
