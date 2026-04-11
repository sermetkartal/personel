"""
Anomaly scoring pipeline.

Composes feature extraction + model inference into the output schema defined
in schemas.py. Produces UserAnomalyScore objects with all required fields
including the KVKK m.11/g disclaimer.

Pipeline (per user):
  1. Extract raw 7-dimensional feature vector
  2. Z-score normalise using tenant-level statistics
  3. Run IsolationForestDetector.score()
  4. Map score to risk tier via tiers.classify_tier()
  5. Identify top-2 contributing features by z-score magnitude
  6. Assemble UserAnomalyScore with disclaimer

Advisory notification (score > 0.9): caller is responsible for sending
the DPO feed notification (see routes.py / background job).
"""

from __future__ import annotations

import time
from datetime import datetime, timezone
from uuid import UUID

import numpy as np
import pandas as pd

from personel_uba import KVKK_DISCLAIMER
from personel_uba.features import extract_features, zscore_normalise
from personel_uba.logging import get_logger, log_score
from personel_uba.metrics import SCORE_COMPUTATION_DURATION, SCORES_COMPUTED
from personel_uba.model import IsolationForestDetector
from personel_uba.schemas import (
    ContributingFeature,
    FeatureVector,
    UserAnomalyScore,
)
from personel_uba.tiers import classify_tier

logger = get_logger(__name__)

# Canonical feature name ordering (must match FeatureVector.feature_names())
FEATURE_NAMES: list[str] = [
    "off_hours_activity",
    "app_diversity",
    "data_egress_volume",
    "screenshot_rate",
    "file_access_rate",
    "policy_violation_count",
    "new_host_ratio",
]

N_TOP_FEATURES = 2  # Number of contributing features to include in output


def compute_feature_vector(
    user_id: UUID,
    user_events: pd.DataFrame,
    baseline_events: pd.DataFrame | None = None,
    window_days: int = 7,
) -> FeatureVector:
    """
    Extract raw features for a single user and wrap in FeatureVector.

    Parameters
    ----------
    user_id:
        Target user UUID.
    user_events:
        Events DataFrame for the scoring window.
    baseline_events:
        Events DataFrame for the prior 30-day baseline (for new_host_ratio).
    window_days:
        Scoring window in days.

    Returns
    -------
    FeatureVector
        Pydantic model with all 7 raw feature values.
    """
    raw = extract_features(
        user_events=user_events,
        baseline_events=baseline_events,
        window_days=window_days,
    )
    return FeatureVector(
        user_id=user_id,
        off_hours_activity=raw["off_hours_activity"],
        app_diversity=raw["app_diversity"],
        data_egress_volume=raw["data_egress_volume"],
        screenshot_rate=raw["screenshot_rate"],
        file_access_rate=raw["file_access_rate"],
        policy_violation_count=raw["policy_violation_count"],
        new_host_ratio=raw["new_host_ratio"],
        window_days=window_days,
        computed_at=datetime.now(tz=timezone.utc),
    )


def _top_contributing_features(
    raw_vector: np.ndarray,
    mean_vector: np.ndarray,
    std_vector: np.ndarray,
    n: int = N_TOP_FEATURES,
) -> list[ContributingFeature]:
    """
    Identify the top-n features by their absolute z-score contribution.

    A feature with a positive z-score (above population mean) gets direction='up'.
    A feature with a negative z-score (below mean) gets direction='down'.

    Weight is the normalised contribution: |z_i| / sum(|z_j|).
    """
    safe_std = np.where(std_vector == 0, 1.0, std_vector)
    z_scores = (raw_vector - mean_vector) / safe_std
    abs_z = np.abs(z_scores)

    # Select top-n by absolute z-score
    top_indices = np.argsort(abs_z)[::-1][:n]
    total_abs_z = float(abs_z[top_indices].sum())

    contributions: list[ContributingFeature] = []
    for idx in top_indices:
        weight = float(abs_z[idx] / total_abs_z) if total_abs_z > 0 else 0.0
        direction = "up" if z_scores[idx] >= 0 else "down"
        contributions.append(
            ContributingFeature(
                feature=FEATURE_NAMES[int(idx)],
                weight=round(weight, 4),
                direction=direction,
            )
        )
    return contributions


def score_user(
    user_id: UUID,
    feature_vec: FeatureVector,
    detector: IsolationForestDetector,
    population_mean: np.ndarray,
    population_std: np.ndarray,
    window_days: int = 7,
    watch_threshold: float = 0.3,
    investigate_threshold: float = 0.7,
    tenant_id: str = "",
) -> UserAnomalyScore:
    """
    Compute the full advisory anomaly score for one user.

    Parameters
    ----------
    user_id:
        User UUID.
    feature_vec:
        Pre-computed FeatureVector for the user.
    detector:
        Fitted IsolationForestDetector instance.
    population_mean:
        Per-feature mean vector for z-score normalisation (shape: (7,)).
    population_std:
        Per-feature std vector for z-score normalisation (shape: (7,)).
    window_days:
        Scoring window in days.
    watch_threshold / investigate_threshold:
        Risk tier thresholds.
    tenant_id:
        For metrics labelling.

    Returns
    -------
    UserAnomalyScore
        Complete advisory score object with disclaimer.
    """
    raw_array = np.array(feature_vec.to_array(), dtype=float)

    with SCORE_COMPUTATION_DURATION.time():
        anomaly_score = detector.score(raw_array)

    tier = classify_tier(
        anomaly_score,
        watch_threshold=watch_threshold,
        investigate_threshold=investigate_threshold,
    )
    contributing = _top_contributing_features(raw_array, population_mean, population_std)

    window_str = _days_to_window_str(window_days)

    log_score(logger, str(user_id), anomaly_score, tier)

    SCORES_COMPUTED.labels(tenant_id=tenant_id, risk_tier=tier).inc()

    return UserAnomalyScore(
        user_id=user_id,
        anomaly_score=round(anomaly_score, 4),
        risk_tier=tier,
        contributing_features=contributing,
        window=window_str,
        last_updated_at=datetime.now(tz=timezone.utc),
        disclaimer=KVKK_DISCLAIMER,
    )


def score_users_batch(
    user_feature_vectors: list[tuple[UUID, FeatureVector]],
    detector: IsolationForestDetector,
    window_days: int = 7,
    watch_threshold: float = 0.3,
    investigate_threshold: float = 0.7,
    tenant_id: str = "",
) -> list[UserAnomalyScore]:
    """
    Score a batch of users.

    Computes population-level mean/std from the batch itself for z-score
    normalisation of contributing feature weights.

    In Phase 2.7, population statistics will be maintained across batches
    using a rolling window stored in ClickHouse.

    Parameters
    ----------
    user_feature_vectors:
        List of (user_id, FeatureVector) tuples.
    detector:
        Fitted IsolationForestDetector instance.
    window_days:
        Scoring window in days.
    watch_threshold / investigate_threshold:
        Risk tier thresholds.
    tenant_id:
        For metrics labelling.

    Returns
    -------
    list[UserAnomalyScore]
        One score per user, ordered same as input.
    """
    if not user_feature_vectors:
        return []

    # Build feature matrix
    raw_matrix = np.array(
        [fv.to_array() for _, fv in user_feature_vectors],
        dtype=float,
    )

    # Compute population statistics for contributing feature weights
    pop_mean = raw_matrix.mean(axis=0)
    pop_std = raw_matrix.std(axis=0)

    results: list[UserAnomalyScore] = []
    for user_id, fv in user_feature_vectors:
        result = score_user(
            user_id=user_id,
            feature_vec=fv,
            detector=detector,
            population_mean=pop_mean,
            population_std=pop_std,
            window_days=window_days,
            watch_threshold=watch_threshold,
            investigate_threshold=investigate_threshold,
            tenant_id=tenant_id,
        )
        results.append(result)

    return results


def _days_to_window_str(days: int) -> str:
    """Convert integer days to WindowStr literal."""
    mapping = {1: "1d", 3: "3d", 7: "7d", 14: "14d", 30: "30d"}
    return mapping.get(days, "7d")


def should_notify_dpo(score: float, threshold: float = 0.9) -> bool:
    """
    Return True if score exceeds the advisory DPO notification threshold.

    KVKK m.11/g: notification is advisory only — it does NOT trigger
    automated disciplinary action or access restriction.
    """
    return score >= threshold
