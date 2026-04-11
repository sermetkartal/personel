"""
Unit tests for feature extraction functions.

All tests operate on synthetic DataFrames — no ClickHouse required.

Tests cover:
  - off_hours_activity: business hours detection (UTC+3 aware)
  - app_diversity: distinct app counting
  - data_egress_volume: multi-column byte summing
  - screenshot_rate: per-hour rate computation
  - file_access_rate: per-hour rate computation
  - policy_violation_count: violation event counting
  - new_host_ratio: new host fraction
  - extract_features: integrated 7-feature extraction
  - zscore_normalise: normalisation correctness
"""

from __future__ import annotations

from datetime import datetime, timezone

import numpy as np
import pandas as pd
import pytest

from personel_uba.features import (
    BUSINESS_HOUR_END,
    BUSINESS_HOUR_START,
    TZ_ISTANBUL,
    compute_app_diversity,
    compute_data_egress_volume,
    compute_file_access_rate,
    compute_new_host_ratio,
    compute_off_hours_activity,
    compute_policy_violation_count,
    compute_screenshot_rate,
    extract_features,
    zscore_normalise,
)

TZ_UTC = timezone.utc


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def make_events(timestamps: list[str], **extra_cols: list) -> pd.DataFrame:
    """Create a minimal DataFrame with occurred_at and optional extra columns."""
    data: dict = {"occurred_at": pd.to_datetime(timestamps, utc=True)}
    data.update(extra_cols)
    return pd.DataFrame(data)


# ---------------------------------------------------------------------------
# off_hours_activity
# ---------------------------------------------------------------------------


class TestOffHoursActivity:
    def test_empty_returns_zero(self) -> None:
        df = pd.DataFrame({"occurred_at": pd.Series([], dtype="datetime64[ns, UTC]")})
        assert compute_off_hours_activity(df) == 0.0

    def test_all_business_hours_returns_zero(self) -> None:
        # Monday 09:00 UTC = 12:00 Istanbul (business hours)
        timestamps = [
            "2026-04-06 09:00:00+00:00",  # Mon 12:00 Istanbul
            "2026-04-07 10:00:00+00:00",  # Tue 13:00 Istanbul
            "2026-04-08 11:00:00+00:00",  # Wed 14:00 Istanbul
        ]
        df = make_events(timestamps)
        result = compute_off_hours_activity(df)
        assert result == pytest.approx(0.0, abs=1e-6)

    def test_all_off_hours_returns_one(self) -> None:
        # 02:00 UTC = 05:00 Istanbul (definitely off-hours)
        timestamps = [
            "2026-04-06 23:00:00+00:00",  # Mon 02:00+1 Istanbul — next day
            "2026-04-06 22:00:00+00:00",  # Mon 01:00+1 Istanbul
        ]
        df = make_events(timestamps)
        result = compute_off_hours_activity(df)
        assert result == pytest.approx(1.0, abs=1e-6)

    def test_weekend_is_off_hours(self) -> None:
        # Saturday 10:00 UTC = 13:00 Istanbul, but weekend
        timestamps = [
            "2026-04-11 10:00:00+00:00",  # Saturday
            "2026-04-12 10:00:00+00:00",  # Sunday
        ]
        df = make_events(timestamps)
        assert compute_off_hours_activity(df) == pytest.approx(1.0)

    def test_mixed_returns_correct_ratio(self) -> None:
        # 2 business hours, 2 off-hours -> 0.5
        timestamps = [
            "2026-04-06 09:00:00+00:00",  # Mon 12:00 Istanbul — business
            "2026-04-07 10:00:00+00:00",  # Tue 13:00 Istanbul — business
            "2026-04-06 23:00:00+00:00",  # Mon 02:00 Istanbul next day — off
            "2026-04-07 22:00:00+00:00",  # Tue 01:00 Istanbul next day — off
        ]
        df = make_events(timestamps)
        result = compute_off_hours_activity(df)
        assert result == pytest.approx(0.5, abs=1e-6)

    def test_boundary_8am_is_business(self) -> None:
        # 05:00 UTC = exactly 08:00 Istanbul (boundary, should be business)
        timestamps = ["2026-04-06 05:00:00+00:00"]
        df = make_events(timestamps)
        assert compute_off_hours_activity(df) == pytest.approx(0.0)

    def test_boundary_18pm_is_off_hours(self) -> None:
        # 15:00 UTC = exactly 18:00 Istanbul (exclusive, so off-hours)
        timestamps = ["2026-04-06 15:00:00+00:00"]
        df = make_events(timestamps)
        assert compute_off_hours_activity(df) == pytest.approx(1.0)


# ---------------------------------------------------------------------------
# app_diversity
# ---------------------------------------------------------------------------


class TestAppDiversity:
    def test_empty_returns_zero(self) -> None:
        df = pd.DataFrame({"app_name": pd.Series([], dtype=str)})
        assert compute_app_diversity(df) == 0.0

    def test_no_app_column_returns_zero(self) -> None:
        df = pd.DataFrame({"event_type": ["file_read", "file_write"]})
        assert compute_app_diversity(df) == 0.0

    def test_distinct_apps_counted(self) -> None:
        df = pd.DataFrame(
            {"app_name": ["Word", "Excel", "Word", "PowerPoint", "Excel", "Word"]}
        )
        assert compute_app_diversity(df) == pytest.approx(3.0)

    def test_single_app_returns_one(self) -> None:
        df = pd.DataFrame({"app_name": ["Word"] * 10})
        assert compute_app_diversity(df) == pytest.approx(1.0)

    def test_process_name_fallback(self) -> None:
        df = pd.DataFrame(
            {"process_name": ["WINWORD.EXE", "EXCEL.EXE", "WINWORD.EXE"]}
        )
        assert compute_app_diversity(df) == pytest.approx(2.0)

    def test_null_values_excluded(self) -> None:
        df = pd.DataFrame({"app_name": ["Word", None, "Excel", None]})
        assert compute_app_diversity(df) == pytest.approx(2.0)


# ---------------------------------------------------------------------------
# data_egress_volume
# ---------------------------------------------------------------------------


class TestDataEgressVolume:
    def test_empty_returns_zero(self) -> None:
        assert compute_data_egress_volume(pd.DataFrame()) == 0.0

    def test_no_relevant_columns_returns_zero(self) -> None:
        df = pd.DataFrame({"event_type": ["file_read"] * 5})
        assert compute_data_egress_volume(df) == 0.0

    def test_sums_all_three_columns(self) -> None:
        df = pd.DataFrame(
            {
                "bytes_written": [1000, 2000],
                "clipboard_bytes": [500, 500],
                "bytes_out": [10000, 20000],
            }
        )
        expected = 1000 + 2000 + 500 + 500 + 10000 + 20000
        assert compute_data_egress_volume(df) == pytest.approx(expected)

    def test_partial_columns(self) -> None:
        df = pd.DataFrame({"bytes_written": [1000, 2000]})
        assert compute_data_egress_volume(df) == pytest.approx(3000.0)

    def test_nans_treated_as_zero(self) -> None:
        df = pd.DataFrame(
            {
                "bytes_written": [1000, None, 2000],
                "bytes_out": [None, 5000, None],
            }
        )
        assert compute_data_egress_volume(df) == pytest.approx(8000.0)

    def test_negative_values_clipped_to_zero(self) -> None:
        df = pd.DataFrame({"bytes_written": [-100, 1000]})
        assert compute_data_egress_volume(df) == pytest.approx(1000.0)


# ---------------------------------------------------------------------------
# screenshot_rate
# ---------------------------------------------------------------------------


class TestScreenshotRate:
    def test_empty_returns_zero(self) -> None:
        assert compute_screenshot_rate(pd.DataFrame(), window_hours=168.0) == 0.0

    def test_zero_window_returns_zero(self) -> None:
        df = pd.DataFrame({"event_type": ["screen_capture"] * 10})
        assert compute_screenshot_rate(df, window_hours=0) == 0.0

    def test_correct_rate(self) -> None:
        # 168 captures in 168 hours = 1.0 per hour
        df = pd.DataFrame({"event_type": ["screen_capture"] * 168})
        assert compute_screenshot_rate(df, window_hours=168.0) == pytest.approx(1.0)

    def test_ignores_other_events(self) -> None:
        df = pd.DataFrame(
            {"event_type": ["screen_capture"] * 10 + ["file_read"] * 50}
        )
        rate = compute_screenshot_rate(df, window_hours=10.0)
        assert rate == pytest.approx(1.0)


# ---------------------------------------------------------------------------
# file_access_rate
# ---------------------------------------------------------------------------


class TestFileAccessRate:
    def test_empty_returns_zero(self) -> None:
        assert compute_file_access_rate(pd.DataFrame(), window_hours=168.0) == 0.0

    def test_counts_file_read_and_open(self) -> None:
        df = pd.DataFrame(
            {"event_type": ["file_read"] * 7 + ["file_open"] * 7 + ["screen_capture"] * 14}
        )
        rate = compute_file_access_rate(df, window_hours=7.0)
        assert rate == pytest.approx(2.0)  # 14 / 7.0


# ---------------------------------------------------------------------------
# policy_violation_count
# ---------------------------------------------------------------------------


class TestPolicyViolationCount:
    def test_empty_returns_zero(self) -> None:
        assert compute_policy_violation_count(pd.DataFrame()) == 0.0

    def test_counts_all_violation_types(self) -> None:
        df = pd.DataFrame(
            {
                "event_type": [
                    "app_blocked",
                    "web_blocked",
                    "dlp_match",
                    "file_read",
                    "screen_capture",
                ]
            }
        )
        assert compute_policy_violation_count(df) == pytest.approx(3.0)

    def test_zero_violations(self) -> None:
        df = pd.DataFrame({"event_type": ["file_read", "screen_capture", "file_write"]})
        assert compute_policy_violation_count(df) == pytest.approx(0.0)


# ---------------------------------------------------------------------------
# new_host_ratio
# ---------------------------------------------------------------------------


class TestNewHostRatio:
    def test_empty_current_returns_zero(self) -> None:
        assert compute_new_host_ratio(pd.DataFrame(), pd.DataFrame()) == 0.0

    def test_no_baseline_all_new(self) -> None:
        current = pd.DataFrame({"remote_host": ["1.2.3.4", "5.6.7.8"]})
        assert compute_new_host_ratio(current, pd.DataFrame()) == pytest.approx(1.0)

    def test_all_known_hosts_returns_zero(self) -> None:
        current = pd.DataFrame({"remote_host": ["1.2.3.4", "5.6.7.8"]})
        baseline = pd.DataFrame({"remote_host": ["1.2.3.4", "5.6.7.8", "9.10.11.12"]})
        assert compute_new_host_ratio(current, baseline) == pytest.approx(0.0)

    def test_half_new_returns_half(self) -> None:
        current = pd.DataFrame({"remote_host": ["1.2.3.4", "5.6.7.8"]})
        baseline = pd.DataFrame({"remote_host": ["1.2.3.4"]})
        assert compute_new_host_ratio(current, baseline) == pytest.approx(0.5)

    def test_missing_column_returns_zero(self) -> None:
        current = pd.DataFrame({"event_type": ["file_read"]})
        baseline = pd.DataFrame({"event_type": ["file_read"]})
        assert compute_new_host_ratio(current, baseline) == 0.0


# ---------------------------------------------------------------------------
# extract_features (integration)
# ---------------------------------------------------------------------------


class TestExtractFeatures:
    def test_returns_all_seven_keys(self, baseline_user_events: pd.DataFrame) -> None:
        result = extract_features(baseline_user_events, window_days=7)
        expected_keys = {
            "off_hours_activity",
            "app_diversity",
            "data_egress_volume",
            "screenshot_rate",
            "file_access_rate",
            "policy_violation_count",
            "new_host_ratio",
        }
        assert set(result.keys()) == expected_keys

    def test_all_values_are_float(self, baseline_user_events: pd.DataFrame) -> None:
        result = extract_features(baseline_user_events, window_days=7)
        for key, value in result.items():
            assert isinstance(value, float), f"{key} should be float, got {type(value)}"

    def test_baseline_user_low_off_hours(self, baseline_user_events: pd.DataFrame) -> None:
        """Baseline user should have very low off-hours activity."""
        result = extract_features(baseline_user_events, window_days=7)
        assert result["off_hours_activity"] == pytest.approx(0.0, abs=0.1)

    def test_anomalous_user_high_off_hours(self, anomalous_user_events: pd.DataFrame) -> None:
        """Anomalous user should have high off-hours activity."""
        result = extract_features(anomalous_user_events, window_days=7)
        assert result["off_hours_activity"] > 0.7

    def test_anomalous_user_high_egress(self, anomalous_user_events: pd.DataFrame) -> None:
        """Anomalous user should have very high data egress."""
        result = extract_features(anomalous_user_events, window_days=7)
        assert result["data_egress_volume"] > 1_000_000  # > 1 MB

    def test_anomalous_user_policy_violations(self, anomalous_user_events: pd.DataFrame) -> None:
        """Anomalous user should have policy violations."""
        result = extract_features(anomalous_user_events, window_days=7)
        assert result["policy_violation_count"] > 0

    def test_empty_events_all_zeros(self) -> None:
        result = extract_features(pd.DataFrame(), window_days=7)
        for v in result.values():
            assert v == pytest.approx(0.0)


# ---------------------------------------------------------------------------
# zscore_normalise
# ---------------------------------------------------------------------------


class TestZscoreNormalise:
    def test_zero_mean_unit_std_after_normalisation(self) -> None:
        matrix = np.array([[1.0, 2.0], [3.0, 4.0], [5.0, 6.0]])
        normalised, mean, std = zscore_normalise(matrix)
        assert normalised.mean(axis=0) == pytest.approx([0.0, 0.0], abs=1e-9)
        assert normalised.std(axis=0) == pytest.approx([1.0, 1.0], abs=1e-6)

    def test_constant_column_not_nan(self) -> None:
        matrix = np.array([[5.0, 1.0], [5.0, 2.0], [5.0, 3.0]])
        normalised, _, _ = zscore_normalise(matrix)
        assert not np.isnan(normalised).any()
        assert (normalised[:, 0] == 0.0).all()

    def test_precomputed_mean_std_used(self) -> None:
        matrix = np.array([[1.0], [2.0], [3.0]])
        mean = np.array([2.0])
        std = np.array([1.0])
        normalised, out_mean, out_std = zscore_normalise(matrix, mean=mean, std=std)
        assert out_mean[0] == pytest.approx(2.0)
        assert normalised[0, 0] == pytest.approx(-1.0)
        assert normalised[2, 0] == pytest.approx(1.0)
