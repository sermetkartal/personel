"""
FastAPI application entry point — Personel UBA Detector.

Startup:
  1. Configure structlog
  2. Connect to ClickHouse (read-only)
  3. Attach ClickHouseClient to app.state
  4. Register routers

Shutdown:
  1. Close ClickHouse connection

Background scoring job structure is included here as a stub.
Real scheduling (APScheduler or Celery) is Phase 2.7 work.
"""

from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
from typing import AsyncGenerator

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from personel_uba import KVKK_DISCLAIMER, __version__
from personel_uba.clickhouse import ClickHouseClient
from personel_uba.config import get_settings
from personel_uba.logging import configure_logging, get_logger
from personel_uba.routes import health_router, uba_router

logger = get_logger(__name__)


# ---------------------------------------------------------------------------
# Application lifespan
# ---------------------------------------------------------------------------


@asynccontextmanager
async def lifespan(application: FastAPI) -> AsyncGenerator[None, None]:
    """Manage startup and shutdown of shared resources."""
    settings = get_settings()
    configure_logging(settings.log_level)

    logger.info(
        "uba_service_starting",
        version=__version__,
        phase="2.6",
        kvkk_advisory=KVKK_DISCLAIMER,
    )

    # Connect to ClickHouse (read-only)
    ch_client = ClickHouseClient(
        host=settings.clickhouse_host,
        port=settings.clickhouse_port,
        database=settings.clickhouse_database,
        username=settings.clickhouse_user,
        password=settings.clickhouse_password.get_secret_value(),
        secure=settings.clickhouse_secure,
    )
    try:
        ch_client.connect()
        logger.info("clickhouse_connected", host=settings.clickhouse_host)
    except Exception as exc:  # noqa: BLE001
        # Degraded mode: service starts but readyz will report clickhouse=fail
        logger.warning(
            "clickhouse_connect_failed",
            error=str(exc),
            note="Service running in degraded mode — ClickHouse unavailable",
        )

    application.state.clickhouse = ch_client

    # Faz 8 #84 — real ClickHouse feature extractor for POST /v1/uba/score
    # Shares the CH connection details with the read-only ClickHouseClient
    # but uses its own clickhouse-connect client so parameterised queries
    # can be issued without contaminating the legacy client.
    try:
        from personel_uba.clickhouse_extractor import ClickHouseFeatureExtractor  # noqa: PLC0415

        application.state.feature_extractor = ClickHouseFeatureExtractor(
            host=settings.clickhouse_host,
            port=settings.clickhouse_port,
            database=settings.clickhouse_database,
            username=settings.clickhouse_user,
            password=settings.clickhouse_password.get_secret_value(),
            secure=settings.clickhouse_secure,
        )
        logger.info("feature_extractor_initialised")
    except Exception as exc:  # noqa: BLE001
        logger.warning("feature_extractor_init_failed", error=str(exc))
        application.state.feature_extractor = None

    # Phase 2.6: detector is not yet populated at startup; /v1/uba/score
    # falls back to anomaly_score=0.0 until a trained model is attached.
    application.state.detector = None

    # Background scoring job (Phase 2.7: wire up APScheduler here)
    background_task: asyncio.Task | None = None
    # background_task = asyncio.create_task(_scoring_loop(settings, ch_client))

    yield

    # Shutdown
    if background_task is not None:
        background_task.cancel()
        try:
            await background_task
        except asyncio.CancelledError:
            pass

    ch_client.close()

    extractor = getattr(application.state, "feature_extractor", None)
    if extractor is not None:
        try:
            extractor.close()
        except Exception:  # noqa: BLE001
            pass

    logger.info("uba_service_stopped")


# ---------------------------------------------------------------------------
# Application factory
# ---------------------------------------------------------------------------


def create_app() -> FastAPI:
    settings = get_settings()

    application = FastAPI(
        title="Personel UBA Detector",
        description=(
            "User Behavior Analytics / Insider Threat Anomaly Detection Service.\n\n"
            f"**KVKK m.11/g Advisory Notice:** {KVKK_DISCLAIMER}"
        ),
        version=__version__,
        docs_url="/v1/uba/docs",
        redoc_url="/v1/uba/redoc",
        openapi_url="/v1/uba/openapi.json",
        lifespan=lifespan,
    )

    # CORS — admin console origin only; configure via env in production
    application.add_middleware(
        CORSMiddleware,
        allow_origins=["http://localhost:3000"],
        allow_credentials=False,
        allow_methods=["GET", "POST"],
        allow_headers=["Content-Type", "Authorization"],
    )

    # Routers
    application.include_router(uba_router)
    application.include_router(health_router)

    return application


app = create_app()


# ---------------------------------------------------------------------------
# Background scoring job stub (Phase 2.7)
# ---------------------------------------------------------------------------


async def _scoring_loop(settings: object, ch: ClickHouseClient) -> None:
    """
    Phase 2.7: Hourly background job that recomputes scores for all active users.

    Structure only — not connected to real feature extraction or model inference
    in Phase 2.6. Wire up in Phase 2.7 with:
      1. Query active users from ClickHouse
      2. Extract features via features.py
      3. Score via scoring.py
      4. Write uba_scores rows via ch.write_score()
      5. Notify DPO feed for scores > 0.9 (advisory only)
    """
    import asyncio  # noqa: PLC0415

    while True:
        try:
            # Phase 2.7: implement real scoring batch here
            logger.debug("scoring_loop_tick", status="stub_phase_2.7")
            await asyncio.sleep(3600)  # 1 hour
        except asyncio.CancelledError:
            break
        except Exception as exc:  # noqa: BLE001
            logger.error("scoring_loop_error", error=str(exc))
            await asyncio.sleep(60)  # backoff


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------


def run() -> None:
    """Entry point for `personel-uba` command."""
    import uvicorn  # noqa: PLC0415

    settings = get_settings()
    uvicorn.run(
        "personel_uba.main:app",
        host="0.0.0.0",  # noqa: S104
        port=settings.port,
        log_config=None,  # structlog handles logging
    )


if __name__ == "__main__":
    run()
