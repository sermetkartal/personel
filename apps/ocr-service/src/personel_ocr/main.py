"""
FastAPI application factory and lifespan management for the OCR service.

Startup sequence:
  1. Configure structlog JSON logger.
  2. Instantiate TesseractEngine — check availability (lazy binary probe).
  3. Instantiate PaddleEngine — check availability (lazy import probe).
  4. Record engine availability in Prometheus gauges.
  5. Build OCRPipeline from available engines.
  6. Attach pipeline to app.state.

If NO engine is available:
  - app.state.pipeline is still set (pipeline is constructed regardless).
  - /readyz returns 'degraded'.
  - POST /v1/extract returns 503 for every request.
  - The service never crashes on startup due to missing engines.

Shutdown sequence:
  7. Log shutdown event (no stateful cleanup needed — engines are stateless).
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

from personel_ocr.config import get_settings
from personel_ocr.engines.paddle import PaddleEngine
from personel_ocr.engines.tesseract import TesseractEngine
from personel_ocr.logging import RequestIdMiddleware, configure_logging
from personel_ocr.metrics import ENGINE_AVAILABLE
from personel_ocr.pipeline import OCRPipeline
from personel_ocr.routes import extract_router, health_router

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Lifespan
# ---------------------------------------------------------------------------


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    settings = get_settings()
    configure_logging(settings.log_level)

    logger.info(
        "ocr_service.starting",
        version="0.1.0",
        bind=f"{settings.bind_host}:{settings.bind_port}",
        default_engine=settings.default_engine,
    )

    # --- Engine initialisation ---
    tesseract = TesseractEngine(
        tesseract_cmd=settings.tesseract_cmd,
        extra_config=settings.tesseract_config,
    )
    paddle = PaddleEngine(
        lang=settings.paddle_lang,
        use_gpu=settings.paddle_use_gpu,
    )

    # Probe availability (this triggers the lazy binary/import checks)
    t_avail = tesseract.is_available
    p_avail = paddle.is_available

    ENGINE_AVAILABLE.labels(engine="tesseract").set(1 if t_avail else 0)
    ENGINE_AVAILABLE.labels(engine="paddle").set(1 if p_avail else 0)

    logger.info(
        "ocr_service.engines",
        tesseract=t_avail,
        paddle=p_avail,
    )

    if not t_avail and not p_avail:
        logger.warning(
            "ocr_service.no_engines_available",
            note=(
                "No OCR engine found. Service starts in degraded mode. "
                "Install tesseract-ocr with tur+eng language packs."
            ),
        )

    # --- Pipeline ---
    pipeline = OCRPipeline(
        tesseract_engine=tesseract,
        paddle_engine=paddle,
        settings=settings,
    )
    app.state.pipeline = pipeline

    logger.info("ocr_service.ready")

    yield

    # --- Shutdown ---
    logger.info("ocr_service.shutting_down")
    logger.info("ocr_service.stopped")


# ---------------------------------------------------------------------------
# Application factory
# ---------------------------------------------------------------------------


def create_app() -> FastAPI:
    settings = get_settings()

    app = FastAPI(
        title="Personel OCR Service",
        description=(
            "Screenshot text extraction with KVKK-compliant PII redaction. "
            "Phase 2.8 — Personel Platform. "
            "KVKK note: OCR is OFF by default (module-state: disabled). "
            "Only enabled via opt-in ceremony following ADR 0013 pattern."
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

    # Request-ID propagation
    app.add_middleware(RequestIdMiddleware)

    # Routers
    app.include_router(extract_router)
    app.include_router(health_router)

    return app


# The ASGI app object consumed by uvicorn.
app = create_app()


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def _handle_signal(sig: int, frame: object) -> None:
    logger.info("ocr_service.signal_received", signal=sig)
    sys.exit(0)


def main() -> None:
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    settings = get_settings()
    configure_logging(settings.log_level)

    uvicorn.run(
        "personel_ocr.main:app",
        host=settings.bind_host,
        port=settings.bind_port,
        log_config=None,   # structlog handles logging
        access_log=False,
        workers=1,
    )


if __name__ == "__main__":
    main()
