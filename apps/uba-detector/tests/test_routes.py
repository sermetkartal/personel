"""
Route tests using FastAPI TestClient with mocked ClickHouse.

Tests cover:
  - Response shape matches schemas.py models
  - KVKK disclaimer is always present in responses
  - HTTP status codes
  - window/limit query parameter handling
  - /healthz, /readyz, /metrics
"""

from __future__ import annotations

from uuid import UUID

import pytest
from fastapi.testclient import TestClient

from personel_uba import KVKK_DISCLAIMER

BASELINE_UID = "00000000-0000-0000-0000-000000000001"
ANOMALOUS_UID = "00000000-0000-0000-0000-000000000002"


# ---------------------------------------------------------------------------
# /healthz
# ---------------------------------------------------------------------------


class TestHealthz:
    def test_returns_200(self, test_client: TestClient) -> None:
        resp = test_client.get("/healthz")
        assert resp.status_code == 200

    def test_status_ok(self, test_client: TestClient) -> None:
        data = test_client.get("/healthz").json()
        assert data["status"] == "ok"

    def test_version_present(self, test_client: TestClient) -> None:
        data = test_client.get("/healthz").json()
        assert "version" in data


# ---------------------------------------------------------------------------
# /readyz
# ---------------------------------------------------------------------------


class TestReadyz:
    def test_returns_200_or_503(self, test_client: TestClient) -> None:
        resp = test_client.get("/readyz")
        assert resp.status_code in (200, 503)

    def test_has_checks(self, test_client: TestClient) -> None:
        data = test_client.get("/readyz").json()
        assert "checks" in data
        assert "status" in data


# ---------------------------------------------------------------------------
# /metrics
# ---------------------------------------------------------------------------


class TestMetrics:
    def test_returns_200(self, test_client: TestClient) -> None:
        resp = test_client.get("/metrics")
        assert resp.status_code == 200

    def test_content_type_prometheus(self, test_client: TestClient) -> None:
        resp = test_client.get("/metrics")
        assert "text/plain" in resp.headers.get("content-type", "")


# ---------------------------------------------------------------------------
# GET /v1/uba/users/top-anomalous
# ---------------------------------------------------------------------------


class TestTopAnomalous:
    def test_returns_200(self, test_client: TestClient) -> None:
        resp = test_client.get("/v1/uba/users/top-anomalous")
        assert resp.status_code == 200

    def test_response_has_disclaimer(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        assert data["disclaimer"] == KVKK_DISCLAIMER

    def test_response_has_users_list(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        assert "users" in data
        assert isinstance(data["users"], list)

    def test_users_have_required_fields(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        for user in data["users"]:
            assert "user_id" in user
            assert "anomaly_score" in user
            assert "risk_tier" in user
            assert "contributing_features" in user
            assert "window" in user
            assert "last_updated_at" in user
            assert "disclaimer" in user

    def test_anomaly_score_in_range(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        for user in data["users"]:
            assert 0.0 <= user["anomaly_score"] <= 1.0

    def test_risk_tier_valid_value(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        valid_tiers = {"normal", "watch", "investigate"}
        for user in data["users"]:
            assert user["risk_tier"] in valid_tiers

    def test_window_defaults_to_7d(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        assert data["window"] == "7d"

    def test_window_param_respected(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous?window=30d").json()
        assert data["window"] == "30d"

    def test_limit_param(self, test_client: TestClient) -> None:
        resp = test_client.get("/v1/uba/users/top-anomalous?limit=5")
        assert resp.status_code == 200

    def test_invalid_limit_returns_422(self, test_client: TestClient) -> None:
        resp = test_client.get("/v1/uba/users/top-anomalous?limit=0")
        assert resp.status_code == 422

    def test_computed_at_present(self, test_client: TestClient) -> None:
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        assert "computed_at" in data

    def test_every_user_has_disclaimer(self, test_client: TestClient) -> None:
        """Each nested user object must also carry the KVKK disclaimer."""
        data = test_client.get("/v1/uba/users/top-anomalous").json()
        for user in data["users"]:
            assert user["disclaimer"] == KVKK_DISCLAIMER


# ---------------------------------------------------------------------------
# GET /v1/uba/users/{user_id}/score
# ---------------------------------------------------------------------------


class TestUserScore:
    def test_returns_200_for_known_user(self, test_client: TestClient) -> None:
        resp = test_client.get(f"/v1/uba/users/{BASELINE_UID}/score")
        assert resp.status_code == 200

    def test_response_has_disclaimer(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/score").json()
        assert data["disclaimer"] == KVKK_DISCLAIMER

    def test_response_schema(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/score").json()
        required_fields = {
            "user_id", "anomaly_score", "risk_tier",
            "contributing_features", "window", "last_updated_at", "disclaimer",
        }
        assert required_fields.issubset(set(data.keys()))

    def test_score_in_range(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/score").json()
        assert 0.0 <= data["anomaly_score"] <= 1.0

    def test_invalid_uuid_returns_422(self, test_client: TestClient) -> None:
        resp = test_client.get("/v1/uba/users/not-a-uuid/score")
        assert resp.status_code == 422

    def test_window_param_accepted(self, test_client: TestClient) -> None:
        resp = test_client.get(f"/v1/uba/users/{BASELINE_UID}/score?window=14d")
        assert resp.status_code == 200


# ---------------------------------------------------------------------------
# GET /v1/uba/users/{user_id}/timeline
# ---------------------------------------------------------------------------


class TestUserTimeline:
    def test_returns_200(self, test_client: TestClient) -> None:
        resp = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline")
        assert resp.status_code == 200

    def test_response_has_disclaimer(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline").json()
        assert data["disclaimer"] == KVKK_DISCLAIMER

    def test_response_schema(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline").json()
        assert "user_id" in data
        assert "days" in data
        assert "points" in data

    def test_points_have_required_fields(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline").json()
        for point in data["points"]:
            assert "timestamp" in point
            assert "anomaly_score" in point
            assert "risk_tier" in point

    def test_days_default_is_30(self, test_client: TestClient) -> None:
        data = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline").json()
        assert data["days"] == 30

    def test_days_param_respected(self, test_client: TestClient) -> None:
        data = test_client.get(
            f"/v1/uba/users/{BASELINE_UID}/timeline?days=7"
        ).json()
        assert data["days"] == 7

    def test_invalid_days_returns_422(self, test_client: TestClient) -> None:
        resp = test_client.get(f"/v1/uba/users/{BASELINE_UID}/timeline?days=0")
        assert resp.status_code == 422


# ---------------------------------------------------------------------------
# POST /v1/uba/recompute
# ---------------------------------------------------------------------------


class TestRecompute:
    def test_returns_200(self, test_client: TestClient) -> None:
        resp = test_client.post(
            "/v1/uba/recompute",
            json={"user_id": BASELINE_UID},
        )
        assert resp.status_code == 200

    def test_response_has_disclaimer(self, test_client: TestClient) -> None:
        data = test_client.post(
            "/v1/uba/recompute",
            json={"user_id": BASELINE_UID},
        ).json()
        assert data["disclaimer"] == KVKK_DISCLAIMER

    def test_status_queued(self, test_client: TestClient) -> None:
        data = test_client.post(
            "/v1/uba/recompute",
            json={"user_id": BASELINE_UID},
        ).json()
        assert data["status"] in ("queued", "completed", "error")

    def test_invalid_user_id_returns_422(self, test_client: TestClient) -> None:
        resp = test_client.post(
            "/v1/uba/recompute",
            json={"user_id": "not-a-uuid"},
        )
        assert resp.status_code == 422

    def test_missing_body_returns_422(self, test_client: TestClient) -> None:
        resp = test_client.post("/v1/uba/recompute")
        assert resp.status_code == 422
