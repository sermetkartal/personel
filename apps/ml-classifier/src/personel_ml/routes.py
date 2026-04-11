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

import time
from typing import Annotated

import structlog
from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import PlainTextResponse
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
            from personel_ml.schemas import ClassifyResult as CR

            results.append(
                CR(
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
