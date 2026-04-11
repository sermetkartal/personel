"""
FastAPI routers for the OCR service.

Routes:
  POST /v1/extract           — single screenshot extraction
  POST /v1/extract/batch     — batch extraction (up to batch_max_items)
  GET  /healthz              — liveness probe (always 200 ok)
  GET  /readyz               — readiness probe (ready | degraded)
  GET  /metrics              — Prometheus text exposition format

Engine availability:
  If no engine is available at request time, POST /v1/extract returns 503.
  The service always starts regardless of engine availability (/healthz stays 200).
"""

from __future__ import annotations

import time
from typing import Annotated

import structlog
from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import PlainTextResponse
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from personel_ocr.metrics import (
    BATCH_SIZE,
    EXTRACTIONS_TOTAL,
    EXTRACTION_LATENCY,
    HTTP_REQUEST_LATENCY,
    HTTP_REQUEST_TOTAL,
    REDACTIONS_TOTAL,
    WORD_COUNT,
)
from personel_ocr.pipeline import OCRPipeline
from personel_ocr.schemas import (
    BatchExtractRequest,
    BatchExtractResponse,
    ErrorDetail,
    ExtractRequest,
    ExtractResponse,
    HealthStatus,
    ReadyStatus,
)

logger = structlog.get_logger(__name__)

# ---------------------------------------------------------------------------
# Dependency: retrieve the active pipeline from app state
# ---------------------------------------------------------------------------


def get_pipeline(request: Request) -> OCRPipeline:
    pipeline: OCRPipeline | None = getattr(request.app.state, "pipeline", None)
    if pipeline is None:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail={"error": "service_unavailable", "message": "OCR pipeline not initialised"},
        )
    return pipeline


PipelineDep = Annotated[OCRPipeline, Depends(get_pipeline)]

# ---------------------------------------------------------------------------
# Routers
# ---------------------------------------------------------------------------

extract_router = APIRouter(prefix="/v1", tags=["extract"])
health_router = APIRouter(tags=["health"])


# ---------------------------------------------------------------------------
# POST /v1/extract
# ---------------------------------------------------------------------------


@extract_router.post(
    "/extract",
    response_model=ExtractResponse,
    responses={
        422: {"model": ErrorDetail, "description": "Validation error"},
        500: {"model": ErrorDetail, "description": "Extraction failed"},
        503: {"model": ErrorDetail, "description": "No OCR engine available"},
    },
    summary="Extract text from a screenshot",
    description=(
        "Runs the full OCR pipeline: preprocess → engine → postprocess → PII redaction. "
        "The response text will never contain raw TCKN, IBAN, or credit card numbers. "
        "KVKK note: the enricher must only call this endpoint when the 'ocr' module "
        "is enabled for the tenant and the screenshot is not sensitive-flagged."
    ),
)
async def extract_single(
    req: ExtractRequest,
    pipeline: PipelineDep,
    request: Request,
) -> ExtractResponse:
    t0 = time.monotonic()
    log = logger.bind(screenshot_id=req.screenshot_id, tenant_id=req.tenant_id)

    try:
        result = pipeline.extract(req)

        elapsed = time.monotonic() - t0
        # Record metrics
        EXTRACTIONS_TOTAL.labels(engine=result.engine, status="success").inc()
        EXTRACTION_LATENCY.labels(engine=result.engine).observe(elapsed)
        WORD_COUNT.observe(result.word_count)
        for entry in result.redactions:
            if entry.count > 0:
                REDACTIONS_TOTAL.labels(kind=entry.kind).inc(entry.count)

        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract", status_code=200).inc()
        HTTP_REQUEST_LATENCY.labels(method="POST", path="/v1/extract").observe(elapsed)

        log.info(
            "extract.done",
            word_count=result.word_count,
            engine=result.engine,
            latency_ms=result.latency_ms,
        )
        return result

    except ValueError as exc:
        # Bad input (invalid base64, undecodable image)
        elapsed = time.monotonic() - t0
        log.warning("extract.bad_input", error=str(exc), latency_ms=round(elapsed * 1000, 2))
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract", status_code=422).inc()
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail={"error": "invalid_input", "message": str(exc)},
        ) from exc

    except RuntimeError as exc:
        msg = str(exc)
        elapsed = time.monotonic() - t0
        # Check if it's an engine-unavailable error (503) vs generic pipeline error (500).
        if "not available" in msg.lower():
            log.warning("extract.engine_unavailable", error=msg)
            HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract", status_code=503).inc()
            EXTRACTIONS_TOTAL.labels(engine="none", status="error").inc()
            raise HTTPException(
                status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
                detail={"error": "engine_unavailable", "message": "No OCR engine is available"},
            ) from exc
        # Generic extraction error — do NOT echo any text back (KVKK invariant).
        log.error("extract.error", latency_ms=round(elapsed * 1000, 2))
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract", status_code=500).inc()
        EXTRACTIONS_TOTAL.labels(engine="unknown", status="error").inc()
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail={"error": "extraction_failed", "message": "OCR extraction failed"},
        ) from exc

    except Exception as exc:
        # Catch-all: never echo extracted text.
        elapsed = time.monotonic() - t0
        log.error("extract.unexpected_error", latency_ms=round(elapsed * 1000, 2))
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract", status_code=500).inc()
        EXTRACTIONS_TOTAL.labels(engine="unknown", status="error").inc()
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail={"error": "extraction_failed", "message": "OCR extraction failed"},
        ) from exc


# ---------------------------------------------------------------------------
# POST /v1/extract/batch
# ---------------------------------------------------------------------------


@extract_router.post(
    "/extract/batch",
    response_model=BatchExtractResponse,
    responses={
        422: {"model": ErrorDetail, "description": "Validation error"},
    },
    summary="Extract text from multiple screenshots",
    description="Batch extraction up to batch_max_items screenshots per call.",
)
async def extract_batch(
    batch: BatchExtractRequest,
    pipeline: PipelineDep,
    request: Request,
) -> BatchExtractResponse:
    from personel_ocr.config import get_settings  # noqa: PLC0415

    settings = get_settings()
    if len(batch.items) > settings.batch_max_items:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail={
                "error": "batch_too_large",
                "message": f"Batch size {len(batch.items)} exceeds maximum {settings.batch_max_items}",
            },
        )

    BATCH_SIZE.observe(len(batch.items))
    t0 = time.monotonic()
    results: list[ExtractResponse] = []

    for item in batch.items:
        try:
            results.append(pipeline.extract(item))
        except Exception as exc:
            logger.warning(
                "batch.item_error",
                screenshot_id=item.screenshot_id,
                # Never include exc text — it may contain partial OCR output
            )
            # Return a zero-result placeholder rather than aborting the batch
            from personel_ocr.schemas import RedactionEntry  # noqa: PLC0415

            results.append(
                ExtractResponse(
                    text="",
                    confidence=0.0,
                    engine="tesseract",
                    language_detected="",
                    word_count=0,
                    redactions=[
                        RedactionEntry(kind=k, count=0)  # type: ignore[arg-type]
                        for k in ("tckn", "iban", "credit_card", "phone", "email")
                    ],
                    latency_ms=0.0,
                )
            )

    elapsed = time.monotonic() - t0
    latency_ms = round(elapsed * 1000, 2)

    HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/extract/batch", status_code=200).inc()
    HTTP_REQUEST_LATENCY.labels(method="POST", path="/v1/extract/batch").observe(elapsed)

    return BatchExtractResponse(results=results, total=len(results), latency_ms=latency_ms)


# ---------------------------------------------------------------------------
# GET /healthz
# ---------------------------------------------------------------------------


@health_router.get(
    "/healthz",
    response_model=HealthStatus,
    summary="Liveness probe",
    description="Always returns 200 ok as long as the process is alive.",
)
async def healthz() -> HealthStatus:
    HTTP_REQUEST_TOTAL.labels(method="GET", path="/healthz", status_code=200).inc()
    return HealthStatus()


# ---------------------------------------------------------------------------
# GET /readyz
# ---------------------------------------------------------------------------


@health_router.get(
    "/readyz",
    response_model=ReadyStatus,
    summary="Readiness probe",
    description=(
        "Returns 'ready' if at least one OCR engine is available; "
        "'degraded' if no engine is available (Tesseract binary not installed)."
    ),
)
async def readyz(request: Request) -> ReadyStatus:
    pipeline: OCRPipeline | None = getattr(request.app.state, "pipeline", None)

    if pipeline is None:
        HTTP_REQUEST_TOTAL.labels(method="GET", path="/readyz", status_code=200).inc()
        return ReadyStatus(
            status="degraded",
            engines={"tesseract": False, "paddle": False},
            primary_engine="none",
        )

    engines = {
        "tesseract": pipeline._tesseract.is_available,
        "paddle": pipeline._paddle.is_available,
    }
    primary = "none"
    if engines["tesseract"]:
        primary = "tesseract"
    elif engines["paddle"]:
        primary = "paddle"

    ready_status = "ready" if primary != "none" else "degraded"

    HTTP_REQUEST_TOTAL.labels(method="GET", path="/readyz", status_code=200).inc()
    return ReadyStatus(status=ready_status, engines=engines, primary_engine=primary)


# ---------------------------------------------------------------------------
# GET /metrics
# ---------------------------------------------------------------------------


@health_router.get(
    "/metrics",
    summary="Prometheus metrics",
    description="Prometheus text exposition format.",
    response_class=PlainTextResponse,
    include_in_schema=False,
)
async def metrics() -> PlainTextResponse:
    data = generate_latest()
    return PlainTextResponse(content=data, media_type=CONTENT_TYPE_LATEST)
