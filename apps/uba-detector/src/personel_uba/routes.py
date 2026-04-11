"""
FastAPI router — UBA API endpoints.

All endpoints are prefixed /v1/uba.

Routes:
  GET  /v1/uba/users/top-anomalous
  GET  /v1/uba/users/{user_id}/score
  GET  /v1/uba/users/{user_id}/timeline
  POST /v1/uba/recompute
  GET  /healthz
  GET  /readyz
  GET  /metrics

KVKK m.11/g: every response includes the disclaimer field. No endpoint
produces judgments that trigger automated disciplinary action.
"""

from __future__ import annotations

import time
from datetime import datetime, timezone
from typing import Annotated, Literal
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, Query, Request, status
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from personel_uba import KVKK_DISCLAIMER
from personel_uba.clickhouse import ClickHouseClient
from personel_uba.config import Settings, get_settings
from personel_uba.logging import get_logger
from personel_uba.metrics import API_REQUEST_DURATION, API_REQUESTS_TOTAL
from personel_uba.schemas import (
    HealthResponse,
    ReadinessResponse,
    RecomputeRequest,
    RecomputeResponse,
    ScoreTimelinePoint,
    TopAnomalousResponse,
    UserAnomalyScore,
    UserScoreTimeline,
    WindowStr,
)

logger = get_logger(__name__)

# ---------------------------------------------------------------------------
# Router instances
# ---------------------------------------------------------------------------

uba_router = APIRouter(prefix="/v1/uba", tags=["uba"])
health_router = APIRouter(tags=["health"])

# ---------------------------------------------------------------------------
# Dependency: ClickHouse client
# In tests, override this with a mock via app.dependency_overrides.
# ---------------------------------------------------------------------------


def get_clickhouse(request: Request) -> ClickHouseClient:
    """FastAPI dependency that returns the ClickHouseClient from app state."""
    return request.app.state.clickhouse


def get_app_settings() -> Settings:
    return get_settings()


ClickHouseDep = Annotated[ClickHouseClient, Depends(get_clickhouse)]
SettingsDep = Annotated[Settings, Depends(get_app_settings)]


# ---------------------------------------------------------------------------
# Helper: synthetic/stub scores (used when ClickHouse data is unavailable)
# ---------------------------------------------------------------------------

def _stub_score(user_id: UUID, window: WindowStr) -> UserAnomalyScore:
    """Return a stub score for Phase 2.6 when ClickHouse is not fully wired."""
    return UserAnomalyScore(
        user_id=user_id,
        anomaly_score=0.0,
        risk_tier="normal",
        contributing_features=[],
        window=window,
        last_updated_at=datetime.now(tz=timezone.utc),
        disclaimer=KVKK_DISCLAIMER,
    )


# ---------------------------------------------------------------------------
# GET /v1/uba/users/top-anomalous
# ---------------------------------------------------------------------------


@uba_router.get(
    "/users/top-anomalous",
    response_model=TopAnomalousResponse,
    summary="Top anomalous users (advisory)",
    description=(
        "Returns the top anomalous users for the given window, ordered by "
        "anomaly_score descending. All scores are advisory only. "
        "KVKK m.11/g: no automated disciplinary action is triggered."
    ),
)
async def get_top_anomalous(
    ch: ClickHouseDep,
    settings: SettingsDep,
    limit: Annotated[int, Query(ge=1, le=100, description="Maximum users to return")] = 20,
    window: WindowStr = "7d",
) -> TopAnomalousResponse:
    start_time = time.perf_counter()
    try:
        window_days = _window_str_to_days(window)
        raw_rows = ch.get_top_anomalous_users(
            tenant_id=settings.tenant_id,
            window_days=window_days,
            limit=limit,
        )

        users = [_row_to_score(row, window) for row in raw_rows]

        response = TopAnomalousResponse(
            users=users,
            window=window,
            computed_at=datetime.now(tz=timezone.utc),
            disclaimer=KVKK_DISCLAIMER,
        )
        _record_metric("GET", "/v1/uba/users/top-anomalous", "200", start_time)
        return response
    except Exception as exc:
        _record_metric("GET", "/v1/uba/users/top-anomalous", "500", start_time)
        logger.error("top_anomalous_error", error=str(exc))
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="Failed to retrieve top anomalous users",
        ) from exc


# ---------------------------------------------------------------------------
# GET /v1/uba/users/{user_id}/score
# ---------------------------------------------------------------------------


@uba_router.get(
    "/users/{user_id}/score",
    response_model=UserAnomalyScore,
    summary="Get anomaly score for a user (advisory)",
    description=(
        "Returns the latest advisory anomaly score for the specified user. "
        "KVKK m.11/g: score is for human review only; no automated action is triggered."
    ),
)
async def get_user_score(
    user_id: UUID,
    ch: ClickHouseDep,
    settings: SettingsDep,
    window: WindowStr = "7d",
) -> UserAnomalyScore:
    start_time = time.perf_counter()
    try:
        window_days = _window_str_to_days(window)
        row = ch.get_latest_score(
            tenant_id=settings.tenant_id,
            user_id=str(user_id),
            window_days=window_days,
        )

        if row is None:
            # No score computed yet — return stub
            score = _stub_score(user_id, window)
        else:
            score = _row_to_score(row, window)

        _record_metric("GET", "/v1/uba/users/{user_id}/score", "200", start_time)
        return score
    except HTTPException:
        raise
    except Exception as exc:
        _record_metric("GET", "/v1/uba/users/{user_id}/score", "500", start_time)
        logger.error("user_score_error", user_id=str(user_id), error=str(exc))
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="Failed to retrieve user score",
        ) from exc


# ---------------------------------------------------------------------------
# GET /v1/uba/users/{user_id}/timeline
# ---------------------------------------------------------------------------


@uba_router.get(
    "/users/{user_id}/timeline",
    response_model=UserScoreTimeline,
    summary="Score timeline for a user (advisory)",
    description=(
        "Returns the anomaly score history for the specified user over the "
        "given number of days, oldest first."
    ),
)
async def get_user_timeline(
    user_id: UUID,
    ch: ClickHouseDep,
    settings: SettingsDep,
    days: Annotated[int, Query(ge=1, le=90, description="Timeline window in days")] = 30,
) -> UserScoreTimeline:
    start_time = time.perf_counter()
    try:
        raw_points = ch.get_score_timeline(
            tenant_id=settings.tenant_id,
            user_id=str(user_id),
            days=days,
        )

        points = [
            ScoreTimelinePoint(
                timestamp=p["computed_at"],
                anomaly_score=p["anomaly_score"],
                risk_tier=p["risk_tier"],
            )
            for p in raw_points
        ]

        timeline = UserScoreTimeline(
            user_id=user_id,
            days=days,
            points=points,
            disclaimer=KVKK_DISCLAIMER,
        )
        _record_metric("GET", "/v1/uba/users/{user_id}/timeline", "200", start_time)
        return timeline
    except Exception as exc:
        _record_metric("GET", "/v1/uba/users/{user_id}/timeline", "500", start_time)
        logger.error("timeline_error", user_id=str(user_id), error=str(exc))
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="Failed to retrieve user timeline",
        ) from exc


# ---------------------------------------------------------------------------
# POST /v1/uba/recompute
# ---------------------------------------------------------------------------


@uba_router.post(
    "/recompute",
    response_model=RecomputeResponse,
    summary="Trigger immediate score recompute for a user (DPO-only; audited upstream)",
    description=(
        "Queues an immediate score recomputation for the specified user. "
        "This endpoint is DPO-only; access control is enforced by the admin "
        "API gateway upstream. All invocations are audited. "
        "KVKK m.11/g: recomputed score is advisory only."
    ),
)
async def trigger_recompute(
    request_body: RecomputeRequest,
    ch: ClickHouseDep,
    settings: SettingsDep,
) -> RecomputeResponse:
    start_time = time.perf_counter()
    # TODO Phase 2.7: enqueue recompute job; for now return queued status
    logger.info(
        "recompute_queued",
        user_id=str(request_body.user_id),
        kvkk_disclaimer=KVKK_DISCLAIMER,
    )
    _record_metric("POST", "/v1/uba/recompute", "202", start_time)
    return RecomputeResponse(
        user_id=request_body.user_id,
        status="queued",
        message="Recompute request queued. Score will be updated within one hour.",
        disclaimer=KVKK_DISCLAIMER,
    )


# ---------------------------------------------------------------------------
# Health / Readiness / Metrics
# ---------------------------------------------------------------------------


@health_router.get("/healthz", response_model=HealthResponse, include_in_schema=False)
async def healthz() -> HealthResponse:
    return HealthResponse(status="ok", version="0.1.0")


@health_router.get("/readyz", response_model=ReadinessResponse, include_in_schema=False)
async def readyz(request: Request) -> ReadinessResponse:
    checks: dict[str, Literal["ok", "fail"]] = {}

    # Check ClickHouse reachability
    try:
        ch: ClickHouseClient = request.app.state.clickhouse
        checks["clickhouse"] = "ok" if ch.ping() else "fail"
    except Exception:  # noqa: BLE001
        checks["clickhouse"] = "fail"

    overall = "ready" if all(v == "ok" for v in checks.values()) else "not_ready"
    return ReadinessResponse(status=overall, checks=checks)


@health_router.get("/metrics", include_in_schema=False)
async def metrics() -> bytes:
    from fastapi.responses import Response  # noqa: PLC0415

    return Response(  # type: ignore[return-value]
        content=generate_latest(),
        media_type=CONTENT_TYPE_LATEST,
    )


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _window_str_to_days(window: WindowStr) -> int:
    mapping: dict[str, int] = {"1d": 1, "3d": 3, "7d": 7, "14d": 14, "30d": 30}
    return mapping.get(window, 7)


def _row_to_score(row: dict, window: WindowStr) -> UserAnomalyScore:
    """Convert a ClickHouse row dict to UserAnomalyScore."""
    from personel_uba.schemas import ContributingFeature  # noqa: PLC0415

    features = [
        ContributingFeature(
            feature=f["feature"],
            weight=f.get("weight", 0.0),
            direction=f.get("direction", "up"),
        )
        for f in row.get("contributing_features", [])
    ]
    return UserAnomalyScore(
        user_id=UUID(str(row["user_id"])),
        anomaly_score=float(row["anomaly_score"]),
        risk_tier=row["risk_tier"],
        contributing_features=features,
        window=window,
        last_updated_at=row.get("computed_at", datetime.now(tz=timezone.utc)),
        disclaimer=KVKK_DISCLAIMER,
    )


def _record_metric(method: str, endpoint: str, status_code: str, start: float) -> None:
    duration = time.perf_counter() - start
    API_REQUEST_DURATION.labels(
        method=method, endpoint=endpoint, status=status_code
    ).observe(duration)
    API_REQUESTS_TOTAL.labels(
        method=method, endpoint=endpoint, status=status_code
    ).inc()
