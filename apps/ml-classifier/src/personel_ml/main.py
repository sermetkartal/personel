"""
FastAPI application factory and lifespan management.

Startup sequence:
  1. Configure structlog JSON logger.
  2. Attempt to load LlamaClassifier from the configured GGUF model path.
  3. On success: app.state.classifier = LlamaClassifier (ready mode).
  4. On any failure: app.state.classifier = FallbackClassifier (degraded mode).
     The service still starts; /readyz returns 'degraded'; all /classify calls
     return results with backend='fallback'.  HTTP 200 always — never 503 on
     model failure.  This is the ADR 0017 fallback contract.

Shutdown sequence:
  5. Unload the GGUF model from memory (if LlamaClassifier was active).
"""

from __future__ import annotations

import signal
import sys
from contextlib import asynccontextmanager
from typing import AsyncGenerator

import structlog
import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from personel_ml.classifier import FallbackClassifier, LlamaClassifier
from personel_ml.config import get_settings
from personel_ml.logging import RequestIdMiddleware, configure_logging
from personel_ml.routes import classify_router, health_router

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Lifespan
# ---------------------------------------------------------------------------


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    settings = get_settings()
    configure_logging(settings.log_level)

    logger.info(
        "ml_classifier.starting",
        version="0.1.0",
        model_path=settings.model_path,
        bind=f"{settings.bind_host}:{settings.bind_port}",
    )

    # --- Try to load LlamaClassifier ---
    llama_cls = LlamaClassifier(
        model_path=settings.model_path,
        model_version=settings.model_version,
        n_threads=settings.n_threads,
        n_ctx=settings.n_ctx,
        n_batch=settings.n_batch,
        n_gpu_layers=settings.n_gpu_layers,
        max_tokens=settings.max_tokens,
        temperature=settings.temperature,
        confidence_threshold=settings.confidence_threshold,
    )

    try:
        llama_cls.load()
        app.state.classifier = llama_cls
        logger.info("ml_classifier.backend", backend="llama", model_version=settings.model_version)
    except Exception as exc:
        logger.warning(
            "ml_classifier.fallback_mode",
            reason=str(exc),
            note="Service running in degraded mode; all classifications use rule-based fallback.",
        )
        app.state.classifier = FallbackClassifier(
            confidence_threshold=settings.confidence_threshold,
            model_version="fallback",
        )

    logger.info("ml_classifier.ready")

    yield

    # --- Shutdown ---
    logger.info("ml_classifier.shutting_down")
    if isinstance(app.state.classifier, LlamaClassifier):
        app.state.classifier.unload()
    logger.info("ml_classifier.stopped")


# ---------------------------------------------------------------------------
# Application factory
# ---------------------------------------------------------------------------


def create_app() -> FastAPI:
    settings = get_settings()

    app = FastAPI(
        title="Personel ML Classifier",
        description=(
            "Local LLM-based activity category classifier for Personel Platform Phase 2.3. "
            "Classifies (app_name, window_title, url) into work | personal | distraction | unknown. "
            "Runs entirely on-premises; no cloud egress. ADR 0017."
        ),
        version="0.1.0",
        docs_url="/docs",
        redoc_url="/redoc",
        openapi_url="/openapi.json",
        lifespan=lifespan,
    )

    # CORS — internal service only; very restrictive
    app.add_middleware(
        CORSMiddleware,
        allow_origins=[],
        allow_credentials=False,
        allow_methods=["GET", "POST"],
        allow_headers=["Content-Type", "X-Request-Id"],
    )

    # Request-ID propagation and structlog binding
    app.add_middleware(RequestIdMiddleware)

    # Routers
    app.include_router(classify_router)
    app.include_router(health_router)

    return app


# The ASGI app object consumed by uvicorn.
app = create_app()


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def _handle_signal(sig: int, frame: object) -> None:
    logger.info("ml_classifier.signal_received", signal=sig)
    sys.exit(0)


def main() -> None:
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    settings = get_settings()
    configure_logging(settings.log_level)

    uvicorn.run(
        "personel_ml.main:app",
        host=settings.bind_host,
        port=settings.bind_port,
        log_config=None,        # structlog handles logging; suppress uvicorn's own config
        access_log=False,       # access logging done by RequestIdMiddleware
        workers=1,              # single worker — llama.cpp model is not fork-safe
    )


if __name__ == "__main__":
    main()
