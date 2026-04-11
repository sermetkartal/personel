"""
Tests for the scoring pipeline.

Key assertions:
  - Known-bad (anomalous) user scores > 0.7
  - Baseline user scores < 0.3
  - Score is always in [0.0, 1.0]
  - Risk tier thresholds are correct
  - Contributing features are identified
  - KVKK disclaimer is always present
  - should_notify_dpo correctly thresholds at 0.9
"""

from __future__ import annotations

from datetime import datetime, timezone
from uuid import UUID, uuid4

import numpy as np
import pandas as pd
import pytest

from personel_uba import KVKK_DISCLAIMER
from personel_uba.model import IsolationForestDetector, LSTMDetector
from personel_uba.schemas import FeatureVector
from personel_uba.scoring import (
    _top_contributing_features,
    compute_feature_vector,
    score_user,
    score_users_batch,
    should_notify_dpo,
)
from personel_uba.tiers import classify_tier, tier_ordering, tier_to_display

TZ_UTC = timezone.utc
BASELINE_UID = UUID("00000000-0000-0000-0000-000000000001")
ANOMALOUS_UID = UUID("00000000-0000-0000-0000-000000000002")


# ---------------------------------------------------------------------------
# Model: IsolationForestDetector
# ---------------------------------------------------------------------------


class TestIsolationForestDetector:
    def test_fit_requires_7_features(
        self, population_feature_matrix: np.ndarray
    ) -> None:
        detector = IsolationForestDetector(tenant_id="t1", n_estimators=10)
        # Wrong number of features
        bad_matrix = np.random.default_rng(0).random((10, 5))
        with pytest.raises(ValueError, match="7"):
            detector.fit(bad_matrix)

    def test_fit_requires_at_least_2_rows(self) -> None:
        detector = IsolationForestDetector(tenant_id="t1", n_estimators=10)
        with pytest.raises(ValueError, match="2"):
            detector.fit(np.zeros((1, 7)))

    def test_score_without_fit_raises(self) -> None:
        detector = IsolationForestDetector(tenant_id="t1")
        with pytest.raises(RuntimeError, match="fitted"):
            detector.score(np.zeros(7))

    def test_score_in_unit_interval(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        feats = extract_features(baseline_user_events, window_days=7)
        vec = np.array(list(feats.values()), dtype=float)
        score = fitted_detector.score(vec)
        assert 0.0 <= score <= 1.0

    def test_anomalous_user_scores_higher_than_baseline(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
        anomalous_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        baseline_feats = np.array(
            list(extract_features(baseline_user_events, window_days=7).values())
        )
        anomalous_feats = np.array(
            list(extract_features(anomalous_user_events, window_days=7).values())
        )
        score_baseline = fitted_detector.score(baseline_feats)
        score_anomalous = fitted_detector.score(anomalous_feats)

        assert score_anomalous > score_baseline, (
            f"Anomalous score ({score_anomalous:.4f}) should exceed "
            f"baseline score ({score_baseline:.4f})"
        )

    def test_known_bad_user_scores_above_threshold(
        self,
        fitted_detector: IsolationForestDetector,
        anomalous_user_events: pd.DataFrame,
    ) -> None:
        """Known-bad user (extreme off-hours, large egress, DLP hits) must score > 0.7."""
        from personel_uba.features import extract_features  # noqa: PLC0415

        feats = np.array(
            list(extract_features(anomalous_user_events, window_days=7).values())
        )
        score = fitted_detector.score(feats)
        assert score > 0.7, f"Expected score > 0.7, got {score:.4f}"

    def test_baseline_user_scores_below_threshold(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
    ) -> None:
        """Normal baseline user must score < 0.3."""
        from personel_uba.features import extract_features  # noqa: PLC0415

        feats = np.array(
            list(extract_features(baseline_user_events, window_days=7).values())
        )
        score = fitted_detector.score(feats)
        assert score < 0.3, f"Expected score < 0.3, got {score:.4f}"

    def test_score_batch_consistent_with_individual(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
        anomalous_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        baseline_feats = np.array(
            list(extract_features(baseline_user_events, window_days=7).values())
        )
        anomalous_feats = np.array(
            list(extract_features(anomalous_user_events, window_days=7).values())
        )
        matrix = np.vstack([baseline_feats, anomalous_feats])
        batch_scores = fitted_detector.score_batch(matrix)

        individual_baseline = fitted_detector.score(baseline_feats)
        individual_anomalous = fitted_detector.score(anomalous_feats)

        assert batch_scores[0] == pytest.approx(individual_baseline, rel=1e-6)
        assert batch_scores[1] == pytest.approx(individual_anomalous, rel=1e-6)

    def test_is_fitted_property(
        self, population_feature_matrix: np.ndarray
    ) -> None:
        detector = IsolationForestDetector(tenant_id="t1", n_estimators=10)
        assert not detector.is_fitted
        detector.fit(population_feature_matrix)
        assert detector.is_fitted

    def test_save_and_load(
        self,
        fitted_detector: IsolationForestDetector,
        tmp_path: object,
    ) -> None:
        import os  # noqa: PLC0415

        model_dir = str(tmp_path)
        fitted_detector.save(model_dir)

        loaded = IsolationForestDetector.load(model_dir, tenant_id="test-tenant")
        assert loaded.is_fitted

        # Scores should match
        vec = np.zeros(7)
        s1 = fitted_detector.score(vec)
        s2 = loaded.score(vec)
        assert s1 == pytest.approx(s2, rel=1e-6)


# ---------------------------------------------------------------------------
# LSTM placeholder
# ---------------------------------------------------------------------------


class TestLSTMDetector:
    def test_fit_raises_not_implemented(self) -> None:
        lstm = LSTMDetector()
        with pytest.raises(NotImplementedError, match="Phase 2.7"):
            lstm.fit(None)

    def test_score_raises_not_implemented(self) -> None:
        lstm = LSTMDetector()
        with pytest.raises(NotImplementedError, match="Phase 2.7"):
            lstm.score(None)


# ---------------------------------------------------------------------------
# Tiers
# ---------------------------------------------------------------------------


class TestClassifyTier:
    def test_normal_below_watch(self) -> None:
        assert classify_tier(0.0) == "normal"
        assert classify_tier(0.29) == "normal"

    def test_watch_at_threshold(self) -> None:
        assert classify_tier(0.3) == "watch"
        assert classify_tier(0.5) == "watch"
        assert classify_tier(0.699) == "watch"

    def test_investigate_at_threshold(self) -> None:
        assert classify_tier(0.7) == "investigate"
        assert classify_tier(0.85) == "investigate"
        assert classify_tier(1.0) == "investigate"

    def test_custom_thresholds(self) -> None:
        assert classify_tier(0.4, watch_threshold=0.4, investigate_threshold=0.8) == "watch"
        assert classify_tier(0.9, watch_threshold=0.4, investigate_threshold=0.8) == "investigate"
        assert classify_tier(0.1, watch_threshold=0.4, investigate_threshold=0.8) == "normal"

    def test_tier_ordering(self) -> None:
        assert tier_ordering("investigate") > tier_ordering("watch") > tier_ordering("normal")

    def test_tier_display_strings(self) -> None:
        assert tier_to_display("normal") == "Normal"
        assert tier_to_display("watch") == "İzlemede"
        assert tier_to_display("investigate") == "İnceleme Gerektirir"


# ---------------------------------------------------------------------------
# Contributing features
# ---------------------------------------------------------------------------


class TestTopContributingFeatures:
    def test_returns_n_features(self) -> None:
        raw = np.array([1.0, 5.0, 2.0, 0.1, 3.0, 4.0, 0.5])
        mean = np.zeros(7)
        std = np.ones(7)
        features = _top_contributing_features(raw, mean, std, n=2)
        assert len(features) == 2

    def test_highest_zscore_first(self) -> None:
        raw = np.array([10.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0])
        mean = np.zeros(7)
        std = np.ones(7)
        features = _top_contributing_features(raw, mean, std, n=2)
        assert features[0].feature == "off_hours_activity"

    def test_direction_up_for_elevated(self) -> None:
        raw = np.array([5.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0])
        mean = np.zeros(7)
        std = np.ones(7)
        features = _top_contributing_features(raw, mean, std, n=1)
        assert features[0].direction == "up"

    def test_direction_down_for_depressed(self) -> None:
        raw = np.array([-5.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0])
        mean = np.zeros(7)
        std = np.ones(7)
        features = _top_contributing_features(raw, mean, std, n=1)
        assert features[0].direction == "down"

    def test_weights_sum_to_one(self) -> None:
        raw = np.array([3.0, 2.0, 1.5, 0.5, 1.0, 2.5, 0.3])
        mean = np.zeros(7)
        std = np.ones(7)
        features = _top_contributing_features(raw, mean, std, n=3)
        total_weight = sum(f.weight for f in features)
        assert total_weight == pytest.approx(1.0, abs=1e-3)


# ---------------------------------------------------------------------------
# score_user (full pipeline)
# ---------------------------------------------------------------------------


class TestScoreUser:
    def _make_feature_vector(
        self, uid: UUID, feats: dict
    ) -> FeatureVector:
        return FeatureVector(
            user_id=uid,
            off_hours_activity=feats.get("off_hours_activity", 0.0),
            app_diversity=feats.get("app_diversity", 0.0),
            data_egress_volume=feats.get("data_egress_volume", 0.0),
            screenshot_rate=feats.get("screenshot_rate", 0.0),
            file_access_rate=feats.get("file_access_rate", 0.0),
            policy_violation_count=feats.get("policy_violation_count", 0.0),
            new_host_ratio=feats.get("new_host_ratio", 0.0),
            window_days=7,
            computed_at=datetime.now(tz=TZ_UTC),
        )

    def test_disclaimer_always_present(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        raw = extract_features(baseline_user_events, window_days=7)
        fv = self._make_feature_vector(BASELINE_UID, raw)
        pop_mean = np.zeros(7)
        pop_std = np.ones(7)

        result = score_user(
            user_id=BASELINE_UID,
            feature_vec=fv,
            detector=fitted_detector,
            population_mean=pop_mean,
            population_std=pop_std,
        )
        assert result.disclaimer == KVKK_DISCLAIMER

    def test_score_in_unit_interval(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        raw = extract_features(baseline_user_events, window_days=7)
        fv = self._make_feature_vector(BASELINE_UID, raw)

        result = score_user(
            user_id=BASELINE_UID,
            feature_vec=fv,
            detector=fitted_detector,
            population_mean=np.zeros(7),
            population_std=np.ones(7),
        )
        assert 0.0 <= result.anomaly_score <= 1.0

    def test_risk_tier_consistent_with_score(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        raw = extract_features(baseline_user_events, window_days=7)
        fv = self._make_feature_vector(BASELINE_UID, raw)

        result = score_user(
            user_id=BASELINE_UID,
            feature_vec=fv,
            detector=fitted_detector,
            population_mean=np.zeros(7),
            population_std=np.ones(7),
        )
        expected_tier = classify_tier(result.anomaly_score)
        assert result.risk_tier == expected_tier

    def test_contributing_features_max_2(
        self,
        fitted_detector: IsolationForestDetector,
        anomalous_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        raw = extract_features(anomalous_user_events, window_days=7)
        fv = self._make_feature_vector(ANOMALOUS_UID, raw)

        result = score_user(
            user_id=ANOMALOUS_UID,
            feature_vec=fv,
            detector=fitted_detector,
            population_mean=np.zeros(7),
            population_std=np.ones(7),
        )
        assert len(result.contributing_features) <= 2


# ---------------------------------------------------------------------------
# score_users_batch
# ---------------------------------------------------------------------------


class TestScoreUsersBatch:
    def test_empty_input_returns_empty(
        self, fitted_detector: IsolationForestDetector
    ) -> None:
        result = score_users_batch([], fitted_detector)
        assert result == []

    def test_same_count_as_input(
        self,
        fitted_detector: IsolationForestDetector,
        baseline_user_events: pd.DataFrame,
        anomalous_user_events: pd.DataFrame,
    ) -> None:
        from personel_uba.features import extract_features  # noqa: PLC0415

        def make_fv(uid: UUID, events: pd.DataFrame) -> FeatureVector:
            raw = extract_features(events, window_days=7)
            return FeatureVector(
                user_id=uid,
                off_hours_activity=raw["off_hours_activity"],
                app_diversity=raw["app_diversity"],
                data_egress_volume=raw["data_egress_volume"],
                screenshot_rate=raw["screenshot_rate"],
                file_access_rate=raw["file_access_rate"],
                policy_violation_count=raw["policy_violation_count"],
                new_host_ratio=raw["new_host_ratio"],
                window_days=7,
                computed_at=datetime.now(tz=TZ_UTC),
            )

        pairs = [
            (BASELINE_UID, make_fv(BASELINE_UID, baseline_user_events)),
            (ANOMALOUS_UID, make_fv(ANOMALOUS_UID, anomalous_user_events)),
        ]
        results = score_users_batch(pairs, fitted_detector)
        assert len(results) == 2
        assert all(r.disclaimer == KVKK_DISCLAIMER for r in results)


# ---------------------------------------------------------------------------
# should_notify_dpo
# ---------------------------------------------------------------------------


class TestShouldNotifyDpo:
    def test_below_threshold_false(self) -> None:
        assert not should_notify_dpo(0.89, threshold=0.9)

    def test_at_threshold_true(self) -> None:
        assert should_notify_dpo(0.9, threshold=0.9)

    def test_above_threshold_true(self) -> None:
        assert should_notify_dpo(0.99, threshold=0.9)

    def test_zero_score_false(self) -> None:
        assert not should_notify_dpo(0.0, threshold=0.9)
