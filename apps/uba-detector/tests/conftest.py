"""
Shared pytest fixtures for UBA Detector tests.

All tests operate on synthetic DataFrames — no external services required.
"""

from __future__ import annotations

import os
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import UUID, uuid4

import numpy as np
import pandas as pd
import pytest
from fastapi.testclient import TestClient

# ---------------------------------------------------------------------------
# Synthetic data helpers
# ---------------------------------------------------------------------------

FIXTURE_DIR = Path(__file__).parent / "fixtures"
TZ_UTC = timezone.utc

BASELINE_USER_ID = UUID("00000000-0000-0000-0000-000000000001")
ANOMALOUS_USER_ID = UUID("00000000-0000-0000-0000-000000000002")


def _make_ts(day_offset: int, hour: int, tz: timezone = TZ_UTC) -> pd.Timestamp:
    base = datetime(2026, 4, 3, tzinfo=TZ_UTC)
    dt = base + timedelta(days=day_offset, hours=hour)
    return pd.Timestamp(dt)


@pytest.fixture(scope="session")
def synthetic_events_csv() -> pd.DataFrame:
    """Load the canonical synthetic events fixture CSV."""
    df = pd.read_csv(FIXTURE_DIR / "synthetic_events.csv", parse_dates=["occurred_at"])
    df["occurred_at"] = pd.to_datetime(df["occurred_at"], utc=True)
    return df


@pytest.fixture()
def baseline_user_events() -> pd.DataFrame:
    """
    Normal (baseline) user event DataFrame.
    All events during business hours, moderate activity, no violations.
    """
    rows = []
    for day in range(7):
        for hour in [9, 10, 11, 14, 15, 16]:
            rows.append(
                {
                    "occurred_at": _make_ts(day, hour),
                    "event_type": "file_read",
                    "app_name": "Microsoft Word",
                    "process_name": "WINWORD.EXE",
                    "bytes_written": 0,
                    "clipboard_bytes": 0,
                    "bytes_out": 0,
                    "remote_host": "192.168.1.10",
                }
            )
        # One screenshot per day during work hours
        rows.append(
            {
                "occurred_at": _make_ts(day, 10),
                "event_type": "screen_capture",
                "app_name": "Microsoft Word",
                "process_name": "WINWORD.EXE",
                "bytes_written": 0,
                "clipboard_bytes": 0,
                "bytes_out": 0,
                "remote_host": "",
            }
        )
    return pd.DataFrame(rows)


@pytest.fixture()
def anomalous_user_events() -> pd.DataFrame:
    """
    Anomalous (insider threat) user event DataFrame.
    - Off-hours activity (01:00-05:00)
    - Large data egress (hundreds of MB)
    - Multiple DLP matches and blocked events
    - Many screenshots
    - Diverse apps (WinRAR, 7-Zip, PowerShell, cmd)
    - New/unusual remote hosts
    """
    rows = []
    # Off-hours activity: multiple nights
    for day in range(7):
        for hour in [1, 2, 3, 4]:
            rows.append(
                {
                    "occurred_at": _make_ts(day, hour),
                    "event_type": "file_write",
                    "app_name": "WinRAR",
                    "process_name": "WinRAR.exe",
                    "bytes_written": 52_428_800,  # 50 MB per write
                    "clipboard_bytes": 1_048_576,
                    "bytes_out": 104_857_600,  # 100 MB network out
                    "remote_host": f"185.100.200.{50 + day}",
                }
            )
        # DLP matches
        for hour in [2, 3]:
            rows.append(
                {
                    "occurred_at": _make_ts(day, hour),
                    "event_type": "dlp_match",
                    "app_name": "PowerShell",
                    "process_name": "powershell.exe",
                    "bytes_written": 0,
                    "clipboard_bytes": 5_242_880,
                    "bytes_out": 209_715_200,  # 200 MB
                    "remote_host": f"103.50.200.{100 + day}",
                }
            )
        # Blocked apps/web
        rows.append(
            {
                "occurred_at": _make_ts(day, 3),
                "event_type": "app_blocked",
                "app_name": "cmd.exe",
                "process_name": "cmd.exe",
                "bytes_written": 0,
                "clipboard_bytes": 0,
                "bytes_out": 0,
                "remote_host": "",
            }
        )
        rows.append(
            {
                "occurred_at": _make_ts(day, 3),
                "event_type": "web_blocked",
                "app_name": "7-Zip",
                "process_name": "7zFM.exe",
                "bytes_written": 0,
                "clipboard_bytes": 0,
                "bytes_out": 0,
                "remote_host": "mega.nz",
            }
        )
        # Many screenshots at night
        for hour in [2, 3, 4, 5]:
            rows.append(
                {
                    "occurred_at": _make_ts(day, hour),
                    "event_type": "screen_capture",
                    "app_name": "WinRAR",
                    "process_name": "WinRAR.exe",
                    "bytes_written": 0,
                    "clipboard_bytes": 0,
                    "bytes_out": 0,
                    "remote_host": "",
                }
            )

    return pd.DataFrame(rows)


@pytest.fixture()
def population_feature_matrix(
    baseline_user_events: pd.DataFrame,
    anomalous_user_events: pd.DataFrame,
) -> np.ndarray:
    """
    Build a synthetic population feature matrix (10 users) for fitting the model.

    The matrix contains 9 baseline-like users + 1 clearly anomalous user.
    This ensures IsolationForest can distinguish the anomalous pattern.
    """
    from personel_uba.features import extract_features  # noqa: PLC0415

    rows = []
    # 9 normal users (slight variations)
    for i in range(9):
        events = baseline_user_events.copy()
        # Add small noise
        events = events.sample(frac=0.9 + i * 0.01, replace=False, random_state=i)
        feats = extract_features(events, window_days=7)
        rows.append(list(feats.values()))

    # 1 anomalous user
    feats_anomalous = extract_features(anomalous_user_events, window_days=7)
    rows.append(list(feats_anomalous.values()))

    return np.array(rows, dtype=float)


@pytest.fixture()
def fitted_detector(population_feature_matrix: np.ndarray) -> object:
    """Return a fitted IsolationForestDetector trained on synthetic population."""
    from personel_uba.model import IsolationForestDetector  # noqa: PLC0415

    detector = IsolationForestDetector(
        tenant_id="test-tenant",
        n_estimators=50,  # smaller for test speed
        contamination=0.1,
        random_state=42,
    )
    detector.fit(population_feature_matrix)
    return detector


# ---------------------------------------------------------------------------
# FastAPI test client
# ---------------------------------------------------------------------------


class MockClickHouseClient:
    """Minimal ClickHouseClient mock for route tests."""

    def ping(self) -> bool:
        return True

    def get_latest_score(self, **kwargs: object) -> dict | None:
        return {
            "user_id": "00000000-0000-0000-0000-000000000001",
            "anomaly_score": 0.15,
            "risk_tier": "normal",
            "contributing_features": [
                {"feature": "off_hours_activity", "weight": 0.6, "direction": "up"},
                {"feature": "app_diversity", "weight": 0.4, "direction": "down"},
            ],
            "computed_at": datetime(2026, 4, 10, 12, 0, 0, tzinfo=TZ_UTC),
        }

    def get_score_timeline(self, **kwargs: object) -> list[dict]:
        return [
            {
                "computed_at": datetime(2026, 4, 8, 12, 0, 0, tzinfo=TZ_UTC),
                "anomaly_score": 0.10,
                "risk_tier": "normal",
            },
            {
                "computed_at": datetime(2026, 4, 9, 12, 0, 0, tzinfo=TZ_UTC),
                "anomaly_score": 0.12,
                "risk_tier": "normal",
            },
            {
                "computed_at": datetime(2026, 4, 10, 12, 0, 0, tzinfo=TZ_UTC),
                "anomaly_score": 0.15,
                "risk_tier": "normal",
            },
        ]

    def get_top_anomalous_users(self, **kwargs: object) -> list[dict]:
        return [
            {
                "user_id": "00000000-0000-0000-0000-000000000002",
                "anomaly_score": 0.85,
                "risk_tier": "investigate",
                "contributing_features": [
                    {"feature": "data_egress_volume", "weight": 0.55, "direction": "up"},
                    {"feature": "policy_violation_count", "weight": 0.45, "direction": "up"},
                ],
                "computed_at": datetime(2026, 4, 10, 12, 0, 0, tzinfo=TZ_UTC),
            }
        ]

    def write_score(self, score_row: dict) -> None:
        pass


@pytest.fixture()
def test_client() -> TestClient:
    """Return a FastAPI TestClient with mocked ClickHouse."""
    # Must set env vars before importing config
    os.environ.setdefault("UBA_CLICKHOUSE_HOST", "localhost")
    os.environ.setdefault("UBA_TENANT_ID", "test-tenant")

    from personel_uba.main import create_app  # noqa: PLC0415
    from personel_uba.routes import get_clickhouse  # noqa: PLC0415

    app = create_app()

    mock_ch = MockClickHouseClient()
    app.dependency_overrides[get_clickhouse] = lambda: mock_ch
    app.state.clickhouse = mock_ch

    return TestClient(app, raise_server_exceptions=True)
