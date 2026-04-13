"""
Pydantic v2 schemas for all API request/response models.

Strict mode is enabled on every response model.
Every user-facing response includes the KVKK m.11/g disclaimer in Turkish.
"""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator

from personel_uba import KVKK_DISCLAIMER

# ---------------------------------------------------------------------------
# Type aliases
# ---------------------------------------------------------------------------
AnomalyScore = Annotated[float, Field(ge=0.0, le=1.0)]
RiskTier = Literal["normal", "watch", "investigate"]
FeatureDirection = Literal["up", "down"]
WindowStr = Literal["1d", "3d", "7d", "14d", "30d"]

# ---------------------------------------------------------------------------
# Feature contribution
# ---------------------------------------------------------------------------


class ContributingFeature(BaseModel):
    model_config = ConfigDict(strict=True)

    feature: str = Field(
        description="Feature name (e.g. 'off_hours_activity', 'data_egress_volume')"
    )
    weight: Annotated[float, Field(ge=0.0, le=1.0)] = Field(
        description="Normalised contribution weight (0–1)"
    )
    direction: FeatureDirection = Field(
        description="'up' = feature value is elevated vs baseline, 'down' = depressed"
    )


# ---------------------------------------------------------------------------
# Anomaly score (single user)
# ---------------------------------------------------------------------------


class UserAnomalyScore(BaseModel):
    """
    Per-user anomaly score — advisory only.

    KVKK m.11/g: this output NEVER triggers automated disciplinary action
    or access restriction. It is a prioritisation signal for human review.
    """

    model_config = ConfigDict(strict=True)

    user_id: UUID = Field(description="User UUID")
    anomaly_score: AnomalyScore = Field(description="Normalised anomaly score [0.0, 1.0]")
    risk_tier: RiskTier = Field(
        description="Risk classification: normal (<0.3) | watch (0.3-0.7) | investigate (>0.7)"
    )
    contributing_features: list[ContributingFeature] = Field(
        default_factory=list,
        max_length=7,
        description="Top contributing features (max 2 returned by default)",
    )
    window: WindowStr = Field(description="Scoring window used to compute this score")
    last_updated_at: datetime = Field(description="ISO-8601 timestamp of last score computation")
    disclaimer: str = Field(
        default=KVKK_DISCLAIMER,
        description="KVKK m.11/g advisory disclaimer (Turkish)",
    )

    @field_validator("disclaimer")
    @classmethod
    def disclaimer_must_be_present(cls, v: str) -> str:
        if not v:
            return KVKK_DISCLAIMER
        return v


# ---------------------------------------------------------------------------
# Timeline point
# ---------------------------------------------------------------------------


class ScoreTimelinePoint(BaseModel):
    model_config = ConfigDict(strict=True)

    timestamp: datetime = Field(description="Score computation timestamp")
    anomaly_score: AnomalyScore = Field(description="Anomaly score at this point in time")
    risk_tier: RiskTier = Field(description="Risk tier at this point in time")


class UserScoreTimeline(BaseModel):
    model_config = ConfigDict(strict=True)

    user_id: UUID = Field(description="User UUID")
    days: int = Field(ge=1, le=90, description="Timeline window in days")
    points: list[ScoreTimelinePoint] = Field(
        default_factory=list, description="Score timeline points, oldest first"
    )
    disclaimer: str = Field(default=KVKK_DISCLAIMER)


# ---------------------------------------------------------------------------
# Top anomalous users list
# ---------------------------------------------------------------------------


class TopAnomalousResponse(BaseModel):
    model_config = ConfigDict(strict=True)

    users: list[UserAnomalyScore] = Field(
        description="Users ordered by anomaly_score descending"
    )
    window: WindowStr = Field(description="Scoring window used")
    computed_at: datetime = Field(description="Timestamp of this response")
    disclaimer: str = Field(default=KVKK_DISCLAIMER)


# ---------------------------------------------------------------------------
# Recompute trigger
# ---------------------------------------------------------------------------


class RecomputeRequest(BaseModel):
    model_config = ConfigDict(strict=True)

    user_id: UUID = Field(description="User to recompute score for")


class RecomputeResponse(BaseModel):
    model_config = ConfigDict(strict=True)

    user_id: UUID
    status: Literal["queued", "completed", "error"]
    message: str = Field(default="")
    disclaimer: str = Field(default=KVKK_DISCLAIMER)


# ---------------------------------------------------------------------------
# Live score request (Faz 8 #84) — real ClickHouse feature extraction path
# ---------------------------------------------------------------------------


class LiveScoreRequest(BaseModel):
    """Request body for POST /v1/uba/score.

    Unlike the GET endpoints which read pre-computed rows from uba_scores,
    this endpoint runs the feature extraction + isolation forest synchronously
    against live ClickHouse data for a single (tenant_id, user_id) pair.
    """

    model_config = ConfigDict(strict=True)

    tenant_id: str = Field(min_length=1, max_length=64)
    user_id: str = Field(min_length=1, max_length=256)
    window_hours: int = Field(default=24, ge=1, le=720)


class LiveScoreResponse(BaseModel):
    """Response body for POST /v1/uba/score.

    Every response carries ``advisory_only: true`` + the KVKK m.11/g
    Turkish disclaimer. There is no way to disable the advisory flag.
    """

    model_config = ConfigDict(strict=True)

    tenant_id: str
    user_id: str
    anomaly_score: float = Field(ge=0.0, le=1.0)
    risk_tier: Literal["normal", "watch", "investigate"]
    features: dict[str, float]
    contributing_features: list[ContributingFeature] = Field(default_factory=list)
    window_hours: int
    computed_at: datetime
    advisory_only: Literal[True] = True
    notice: str = Field(
        default=(
            "Bu skor karar destek amaçlıdır. KVKK m.11/g uyarınca otomatik "
            "karar verme yasaktır; insan incelemesi olmadan aleyhine işlem "
            "yapılamaz."
        )
    )
    disclaimer: str = Field(default=KVKK_DISCLAIMER)


# ---------------------------------------------------------------------------
# Health / readiness
# ---------------------------------------------------------------------------


class HealthResponse(BaseModel):
    model_config = ConfigDict(strict=True)

    status: Literal["ok", "degraded", "down"]
    version: str


class ReadinessResponse(BaseModel):
    model_config = ConfigDict(strict=True)

    status: Literal["ready", "not_ready"]
    checks: dict[str, Literal["ok", "fail"]] = Field(default_factory=dict)


# ---------------------------------------------------------------------------
# Internal: feature vector (not exposed in API)
# ---------------------------------------------------------------------------


class FeatureVector(BaseModel):
    """Internal representation of the 7-dimensional feature vector for one user."""

    model_config = ConfigDict(strict=True)

    user_id: UUID
    off_hours_activity: float
    app_diversity: float
    data_egress_volume: float
    screenshot_rate: float
    file_access_rate: float
    policy_violation_count: float
    new_host_ratio: float
    window_days: int = Field(default=7)
    computed_at: datetime

    def to_array(self) -> list[float]:
        """Return feature values in canonical order for model input."""
        return [
            self.off_hours_activity,
            self.app_diversity,
            self.data_egress_volume,
            self.screenshot_rate,
            self.file_access_rate,
            self.policy_violation_count,
            self.new_host_ratio,
        ]

    @classmethod
    def feature_names(cls) -> list[str]:
        return [
            "off_hours_activity",
            "app_diversity",
            "data_egress_volume",
            "screenshot_rate",
            "file_access_rate",
            "policy_violation_count",
            "new_host_ratio",
        ]
