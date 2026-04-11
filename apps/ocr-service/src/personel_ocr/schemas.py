"""
Pydantic v2 request/response schemas for the OCR extract API.
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Engine literals
# ---------------------------------------------------------------------------

EngineLiteral = Literal["tesseract", "paddle", "auto"]
EngineUsedLiteral = Literal["tesseract", "paddle"]


# ---------------------------------------------------------------------------
# Redaction summary
# ---------------------------------------------------------------------------


class RedactionEntry(BaseModel):
    """Summary of redacted PII items by kind."""

    kind: Literal["tckn", "iban", "credit_card", "phone", "email"]
    count: int = Field(..., ge=0)


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------


class ExtractRequest(BaseModel):
    """Single screenshot extraction request.

    The image is provided as a base64-encoded string.  The enricher that calls
    this service encodes the raw screenshot blob before sending.

    KVKK note: The enricher must only forward screenshots here when the 'ocr'
    module is enabled for the tenant and the screenshot is not sensitive-flagged.
    This service does not enforce that invariant — it is enforced upstream.
    """

    image_bytes: str = Field(
        ...,
        description="Base64-encoded screenshot bytes (JPEG, PNG, WebP, BMP).",
        min_length=4,
    )
    tenant_id: str = Field(
        ...,
        description="Tenant UUID — used for structured logging and metrics.",
    )
    endpoint_id: str = Field(
        ...,
        description="Endpoint UUID — for correlation with the originating agent.",
    )
    screenshot_id: str = Field(
        ...,
        description="Screenshot UUID — primary key in the screenshots table.",
    )
    engine_hint: EngineLiteral = Field(
        default="auto",
        description=(
            "Preferred OCR engine. 'auto' selects the first available engine "
            "in priority order: tesseract > paddle."
        ),
    )
    languages: list[str] = Field(
        default=["tr", "en"],
        min_length=1,
        description="ISO-639-1 language codes in order of priority.",
    )


class BatchExtractRequest(BaseModel):
    """Batch extraction request — up to batch_max_items screenshots."""

    items: list[ExtractRequest] = Field(
        ...,
        min_length=1,
        max_length=64,
        description="List of extraction requests (max 64 per batch).",
    )


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


class EngineResult(BaseModel):
    """Internal intermediate — not returned to callers directly."""

    text: str
    confidence: float = Field(..., ge=0.0, le=1.0)
    engine: EngineUsedLiteral
    language_detected: str
    word_count: int = Field(..., ge=0)


class ExtractResponse(BaseModel):
    """Extraction response — text has already been redacted."""

    text: str = Field(
        ...,
        description=(
            "Extracted and PII-redacted text. KVKK invariant: this field "
            "will never contain raw TCKN, IBAN, or credit card numbers."
        ),
    )
    confidence: float = Field(
        ...,
        ge=0.0,
        le=1.0,
        description="Mean word confidence across all extracted words, [0.0, 1.0].",
    )
    engine: EngineUsedLiteral = Field(
        ...,
        description="OCR engine that produced the result.",
    )
    language_detected: str = Field(
        ...,
        description="Dominant language detected by the engine (ISO-639-1 code).",
    )
    word_count: int = Field(..., ge=0, description="Number of words in the extracted text.")
    redactions: list[RedactionEntry] = Field(
        ...,
        description="Summary of PII items redacted, by kind.",
    )
    latency_ms: float = Field(
        ...,
        ge=0.0,
        description="End-to-end wall-clock latency for this extraction in milliseconds.",
    )


class BatchExtractResponse(BaseModel):
    """Response for a batch extraction request."""

    results: list[ExtractResponse] = Field(
        ...,
        description="Extraction results in the same order as input items.",
    )
    total: int = Field(..., ge=0)
    latency_ms: float = Field(..., ge=0.0)


# ---------------------------------------------------------------------------
# Health / readiness models
# ---------------------------------------------------------------------------


class HealthStatus(BaseModel):
    """GET /healthz — always ok if the process is alive."""

    status: Literal["ok"] = "ok"


class ReadyStatus(BaseModel):
    """GET /readyz — reflects whether at least one OCR engine is available."""

    status: Literal["ready", "degraded"]
    engines: dict[str, bool] = Field(
        ...,
        description="Map of engine_name -> is_available.",
    )
    primary_engine: str


# ---------------------------------------------------------------------------
# Error model (RFC 7807 lite)
# ---------------------------------------------------------------------------


class ErrorDetail(BaseModel):
    """Structured error response body."""

    error: str = Field(..., description="Error type slug")
    message: str = Field(..., description="Human-readable message")
    request_id: str | None = Field(default=None)
