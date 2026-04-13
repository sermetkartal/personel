"""
FastAPI routers.

Routes:
  POST /v1/classify           — single item classification
  POST /v1/classify/batch     — batch classification (up to batch_max_items)
  GET  /healthz               — liveness probe (always 200 ok)
  GET  /readyz                — readiness probe (200 ready | 200 degraded)
  GET  /metrics               — Prometheus text exposition format

Dependency injection: the active classifier (LlamaClassifier or
FallbackClassifier) is retrieved via get_classifier(), which reads the
app state set in main.py startup.
"""

from __future__ import annotations

import json
import time
from typing import Annotated, AsyncGenerator

import structlog
from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import PlainTextResponse, StreamingResponse
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from personel_ml.classifier import BaseClassifier
from personel_ml.metrics import BATCH_SIZE, HTTP_REQUEST_LATENCY, HTTP_REQUEST_TOTAL
from personel_ml.schemas import (
    BatchRequest,
    BatchResponse,
    ClassifyItem,
    ClassifyResult,
    ErrorDetail,
    HealthStatus,
    ReadyStatus,
)

logger = structlog.get_logger(__name__)

# ---------------------------------------------------------------------------
# Dependency: retrieve the active classifier from app state
# ---------------------------------------------------------------------------


def get_classifier(request: Request) -> BaseClassifier:
    classifier: BaseClassifier | None = getattr(request.app.state, "classifier", None)
    if classifier is None:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Classifier not initialised",
        )
    return classifier


ClassifierDep = Annotated[BaseClassifier, Depends(get_classifier)]

# ---------------------------------------------------------------------------
# Routers
# ---------------------------------------------------------------------------

classify_router = APIRouter(prefix="/v1", tags=["classify"])
health_router = APIRouter(tags=["health"])


# ---------------------------------------------------------------------------
# POST /v1/classify
# ---------------------------------------------------------------------------


@classify_router.post(
    "/classify",
    response_model=ClassifyResult,
    responses={
        422: {"model": ErrorDetail, "description": "Validation error"},
        503: {"model": ErrorDetail, "description": "Service unavailable"},
    },
    summary="Classify a single activity item",
    description=(
        "Classifies app/URL activity into work | personal | distraction | unknown. "
        "KVKK note: only app_name, window_title, and URL hostname are accepted; "
        "no keystroke content or OCR text."
    ),
)
async def classify_single(
    item: ClassifyItem,
    classifier: ClassifierDep,
    request: Request,
) -> ClassifyResult:
    t0 = time.monotonic()
    log = logger.bind(
        app_name=item.app_name,
        backend=classifier.backend,
    )
    try:
        result = classifier.classify(item)
        elapsed = time.monotonic() - t0
        log.info(
            "classify.done",
            category=result.category,
            confidence=result.confidence,
            latency_ms=round(elapsed * 1000, 2),
        )
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/classify", status_code=200).inc()
        HTTP_REQUEST_LATENCY.labels(method="POST", path="/v1/classify").observe(elapsed)
        return result
    except Exception as exc:
        elapsed = time.monotonic() - t0
        log.error("classify.error", error=str(exc), latency_ms=round(elapsed * 1000, 2))
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/classify", status_code=500).inc()
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail={"error": "classify_failed", "message": str(exc)},
        ) from exc


# ---------------------------------------------------------------------------
# POST /v1/classify/batch
# ---------------------------------------------------------------------------


@classify_router.post(
    "/classify/batch",
    response_model=BatchResponse,
    responses={
        422: {"model": ErrorDetail, "description": "Validation error"},
    },
    summary="Classify a batch of activity items",
    description="Classify up to batch_max_items items in a single call.",
)
async def classify_batch(
    batch: BatchRequest,
    classifier: ClassifierDep,
    request: Request,
) -> BatchResponse:
    from personel_ml.config import get_settings

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
    results: list[ClassifyResult] = []
    errors = 0

    for item in batch.items:
        try:
            results.append(classifier.classify(item))
        except Exception as exc:
            logger.warning("batch.item_error", app_name=item.app_name, error=str(exc))
            errors += 1
            # Return unknown for failed items rather than aborting the batch
            results.append(
                ClassifyResult(
                    category="unknown",
                    confidence=0.0,
                    backend=classifier.backend,
                    model_version=classifier.model_version,
                )
            )

    elapsed = time.monotonic() - t0
    latency_ms = round(elapsed * 1000, 2)

    logger.info(
        "batch.done",
        total=len(batch.items),
        errors=errors,
        latency_ms=latency_ms,
        backend=classifier.backend,
    )
    HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/classify/batch", status_code=200).inc()
    HTTP_REQUEST_LATENCY.labels(method="POST", path="/v1/classify/batch").observe(elapsed)

    return BatchResponse(results=results, total=len(results), latency_ms=latency_ms)


# ---------------------------------------------------------------------------
# POST /v1/classify/stream  (Faz 8 #82)
# ---------------------------------------------------------------------------
#
# Newline-delimited JSON (NDJSON) streaming endpoint. Clients send a
# ClassifyItem on each request (one-shot like /v1/classify), but the
# server responds with a stream of `application/x-ndjson` rows — one
# ClassifyResult per line — over a single HTTP connection.
#
# The practical benefit for the enricher: with HTTP/2 keep-alive, a single
# long-lived stream avoids per-request TCP/TLS amortisation cost when the
# pipeline is emitting 100+ events/second. The batch endpoint still works
# for one-shot fan-out, but streaming is lower-latency.
#
# This endpoint accepts the same BatchRequest body as /v1/classify/batch,
# but emits results incrementally instead of waiting for the full batch.
# HTTP status is always 200; errors per item are encoded inline as NDJSON
# rows with `{"error": "..."}`.


@classify_router.post(
    "/classify/stream",
    summary="Stream classification results as NDJSON",
    description=(
        "Classify a batch of items and stream ClassifyResult JSON rows, one "
        "per newline, as they are produced. Lower per-request overhead than "
        "the batch endpoint when the caller holds a long-lived HTTP/2 "
        "connection open. Response media type: application/x-ndjson."
    ),
    responses={
        200: {"content": {"application/x-ndjson": {}}, "description": "NDJSON stream"},
        422: {"model": ErrorDetail, "description": "Validation error"},
    },
)
async def classify_stream(
    batch: BatchRequest,
    classifier: ClassifierDep,
    request: Request,
) -> StreamingResponse:
    from personel_ml.config import get_settings

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

    async def _row_generator() -> AsyncGenerator[bytes, None]:
        t0 = time.monotonic()
        produced = 0
        for item in batch.items:
            # Cooperative cancellation: if the client disconnected, stop
            # burning CPU on classifications nobody will read.
            if await request.is_disconnected():
                logger.info("stream.client_disconnect", produced=produced)
                break
            try:
                result = classifier.classify(item)
                line = result.model_dump_json() + "\n"
            except Exception as exc:
                logger.warning(
                    "stream.item_error",
                    app_name=item.app_name,
                    error=str(exc),
                )
                line = json.dumps({"error": "classify_failed", "message": str(exc)}) + "\n"
            produced += 1
            yield line.encode("utf-8")

        elapsed = time.monotonic() - t0
        HTTP_REQUEST_TOTAL.labels(method="POST", path="/v1/classify/stream", status_code=200).inc()
        HTTP_REQUEST_LATENCY.labels(method="POST", path="/v1/classify/stream").observe(elapsed)
        logger.info(
            "stream.done",
            total=len(batch.items),
            produced=produced,
            backend=classifier.backend,
            latency_ms=round(elapsed * 1000, 2),
        )

    return StreamingResponse(
        _row_generator(),
        media_type="application/x-ndjson",
        headers={"X-Classifier-Backend": classifier.backend},
    )


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
        "Returns 'ready' if the LLM model is loaded; 'degraded' if the service is "
        "running in fallback mode (rule-based classifier only)."
    ),
)
async def readyz(request: Request) -> ReadyStatus:
    classifier: BaseClassifier | None = getattr(request.app.state, "classifier", None)
    model_loaded = classifier is not None and classifier.is_loaded
    backend = classifier.backend if classifier else "fallback"
    model_version = classifier.model_version if classifier else "fallback"
    status_str = "ready" if backend == "llama" and model_loaded else "degraded"

    HTTP_REQUEST_TOTAL.labels(method="GET", path="/readyz", status_code=200).inc()
    return ReadyStatus(
        status=status_str,
        backend=backend,
        model_loaded=model_loaded,
        model_version=model_version,
    )


# ---------------------------------------------------------------------------
# GET /metrics
# ---------------------------------------------------------------------------


@health_router.get(
    "/metrics",
    summary="Prometheus metrics",
    description="Prometheus text exposition format. Scraped by the monitoring stack.",
    response_class=PlainTextResponse,
    include_in_schema=False,
)
async def metrics() -> PlainTextResponse:
    data = generate_latest()
    return PlainTextResponse(content=data, media_type=CONTENT_TYPE_LATEST)
