"""
Pydantic request/response schemas for the classify API.

All field names are snake_case JSON.  The output contract is locked per ADR 0017:
  {"category": "work|personal|distraction|unknown", "confidence": 0.0..1.0}
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Category enum (string literal union — avoids a real Enum for JSON compat)
# ---------------------------------------------------------------------------

CategoryLiteral = Literal["work", "personal", "distraction", "unknown"]

BackendLiteral = Literal["llama", "fallback"]


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------


class ClassifyItem(BaseModel):
    """Single activity item to classify.

    KVKK note: only metadata fields are accepted here — no keystroke content,
    no OCR text, no screenshot blobs. This is enforced structurally: the schema
    has no such fields.
    """

    app_name: str = Field(
        ...,
        min_length=1,
        max_length=512,
        examples=["chrome.exe", "Microsoft Excel", "Logo Tiger 3"],
        description="Application executable name or display name",
    )
    window_title: str = Field(
        default="",
        max_length=1024,
        examples=["Stack Overflow - How to use async in Python", "2026-Q1-Bütçe.xlsx"],
        description="Window title at capture time. Empty string if unavailable.",
    )
    url: str | None = Field(
        default=None,
        max_length=2048,
        examples=["stackoverflow.com", "mail.google.com"],
        description="URL hostname (SNI granularity). Null if not a browser event.",
    )


class BatchRequest(BaseModel):
    """Batch classification request."""

    items: list[ClassifyItem] = Field(
        ...,
        min_length=1,
        max_length=128,
        description="List of items to classify (max 128 per request)",
    )


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


class ClassifyResult(BaseModel):
    """Classification result for a single item.

    The output contract is ADR 0017 §'Input and output contract':
      - category: one of the four canonical values
      - confidence: [0.0, 1.0]; below threshold → category forced to 'unknown'
      - backend: which inference path produced the result
      - model_version: opaque version tag for audit / KVKK explainability
    """

    category: CategoryLiteral = Field(
        ...,
        description="Predicted activity category",
        examples=["work"],
    )
    confidence: float = Field(
        ...,
        ge=0.0,
        le=1.0,
        description="Model confidence [0.0, 1.0]",
        examples=[0.92],
    )
    backend: BackendLiteral = Field(
        ...,
        description="'llama' = local LLM inference; 'fallback' = rule-based fallback",
        examples=["llama"],
    )
    model_version: str = Field(
        ...,
        description="Model version tag (for audit and KVKK explainability)",
        examples=["llama-3.2-3b-q4_k_m"],
    )


class BatchResponse(BaseModel):
    """Response for a batch classification request."""

    results: list[ClassifyResult] = Field(
        ...,
        description="Classification results in the same order as input items",
    )
    total: int = Field(..., ge=0, description="Total number of items processed")
    latency_ms: float = Field(
        ...,
        ge=0.0,
        description="Wall-clock time for the entire batch in milliseconds",
    )


# ---------------------------------------------------------------------------
# Health / readiness models
# ---------------------------------------------------------------------------


class HealthStatus(BaseModel):
    """GET /healthz — always ok if the process is alive."""

    status: Literal["ok"] = "ok"


class ReadyStatus(BaseModel):
    """GET /readyz — reflects whether the model is loaded and ready."""

    status: Literal["ready", "degraded"]
    backend: BackendLiteral
    model_loaded: bool
    model_version: str


# ---------------------------------------------------------------------------
# Error model (RFC 7807 lite)
# ---------------------------------------------------------------------------


class ErrorDetail(BaseModel):
    """Structured error response body."""

    error: str = Field(..., description="Error type slug")
    message: str = Field(..., description="Human-readable message")
    request_id: str | None = Field(default=None)
