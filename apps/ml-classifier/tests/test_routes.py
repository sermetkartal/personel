"""
FastAPI route tests using TestClient.

LlamaClassifier is mocked (no model file required).
Separate fixture tests verify the FallbackClassifier path.
"""

from __future__ import annotations

import json

import pytest
from fastapi.testclient import TestClient

from personel_ml.schemas import ClassifyResult


# ---------------------------------------------------------------------------
# GET /healthz
# ---------------------------------------------------------------------------


class TestHealthz:
    def test_returns_200(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/healthz")
        assert resp.status_code == 200

    def test_body_status_ok(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/healthz")
        assert resp.json() == {"status": "ok"}

    def test_content_type_json(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/healthz")
        assert "application/json" in resp.headers["content-type"]


# ---------------------------------------------------------------------------
# GET /readyz — fallback mode
# ---------------------------------------------------------------------------


class TestReadyzFallback:
    def test_returns_200(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/readyz")
        assert resp.status_code == 200

    def test_status_is_degraded(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/readyz")
        data = resp.json()
        assert data["status"] == "degraded"
        assert data["backend"] == "fallback"
        assert data["model_loaded"] is True  # fallback is always "loaded"

    def test_model_version_present(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/readyz")
        assert "model_version" in resp.json()


# ---------------------------------------------------------------------------
# GET /readyz — llama mode (mocked)
# ---------------------------------------------------------------------------


class TestReadyzLlama:
    def test_status_is_ready(self, app_with_mock_llama: TestClient) -> None:
        resp = app_with_mock_llama.get("/readyz")
        data = resp.json()
        assert data["status"] == "ready"
        assert data["backend"] == "llama"
        assert data["model_loaded"] is True


# ---------------------------------------------------------------------------
# POST /v1/classify — fallback classifier
# ---------------------------------------------------------------------------


class TestClassifySingleFallback:
    def test_classify_excel_returns_work(self, app_with_fallback: TestClient) -> None:
        payload = {
            "app_name": "Microsoft Excel",
            "window_title": "Q4-Budget.xlsx",
            "url": None,
        }
        resp = app_with_fallback.post("/v1/classify", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        assert data["category"] == "work"
        assert data["backend"] == "fallback"
        assert 0.0 <= data["confidence"] <= 1.0
        assert "model_version" in data

    def test_classify_youtube_returns_distraction(self, app_with_fallback: TestClient) -> None:
        payload = {
            "app_name": "chrome.exe",
            "window_title": "YouTube - Trending",
            "url": "youtube.com",
        }
        resp = app_with_fallback.post("/v1/classify", json=payload)
        assert resp.status_code == 200
        assert resp.json()["category"] == "distraction"

    def test_classify_logo_tiger_returns_work(self, app_with_fallback: TestClient) -> None:
        payload = {
            "app_name": "Logo Tiger 3",
            "window_title": "Logo Tiger - Fatura Modulu",
            "url": None,
        }
        resp = app_with_fallback.post("/v1/classify", json=payload)
        assert resp.status_code == 200
        assert resp.json()["category"] == "work"

    def test_classify_missing_app_name_returns_422(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post("/v1/classify", json={"window_title": "test"})
        assert resp.status_code == 422

    def test_classify_empty_app_name_returns_422(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post("/v1/classify", json={"app_name": ""})
        assert resp.status_code == 422

    def test_classify_url_optional(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post(
            "/v1/classify",
            json={"app_name": "Slack", "window_title": "# engineering"},
        )
        assert resp.status_code == 200

    def test_classify_response_has_request_id_header(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post(
            "/v1/classify",
            json={"app_name": "Excel"},
        )
        assert "x-request-id" in resp.headers

    def test_classify_honours_caller_request_id(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post(
            "/v1/classify",
            json={"app_name": "Excel"},
            headers={"X-Request-Id": "test-correlation-id-123"},
        )
        assert resp.headers.get("x-request-id") == "test-correlation-id-123"


# ---------------------------------------------------------------------------
# POST /v1/classify — mocked llama classifier
# ---------------------------------------------------------------------------


class TestClassifySingleLlama:
    def test_classify_returns_llama_backend(self, app_with_mock_llama: TestClient) -> None:
        payload = {"app_name": "Excel", "window_title": "Budget.xlsx"}
        resp = app_with_mock_llama.post("/v1/classify", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        assert data["backend"] == "llama"
        assert data["category"] == "work"
        assert data["confidence"] == 0.92

    def test_classify_model_version_echoed(self, app_with_mock_llama: TestClient) -> None:
        resp = app_with_mock_llama.post(
            "/v1/classify",
            json={"app_name": "Slack"},
        )
        assert resp.json()["model_version"] == "llama-3.2-3b-q4_k_m-test"


# ---------------------------------------------------------------------------
# POST /v1/classify/batch
# ---------------------------------------------------------------------------


class TestClassifyBatch:
    def test_batch_two_items(self, app_with_fallback: TestClient) -> None:
        payload = {
            "items": [
                {"app_name": "Microsoft Excel", "window_title": "Budget.xlsx"},
                {"app_name": "YouTube", "window_title": "Trending", "url": "youtube.com"},
            ]
        }
        resp = app_with_fallback.post("/v1/classify/batch", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        assert data["total"] == 2
        assert len(data["results"]) == 2
        assert data["latency_ms"] >= 0.0

    def test_batch_result_order_preserved(self, app_with_fallback: TestClient) -> None:
        payload = {
            "items": [
                {"app_name": "Microsoft Excel"},
                {"app_name": "YouTube", "url": "youtube.com"},
                {"app_name": "Logo Tiger 3"},
            ]
        }
        resp = app_with_fallback.post("/v1/classify/batch", json=payload)
        data = resp.json()
        # Excel → work, YouTube → distraction, Logo Tiger → work
        assert data["results"][0]["category"] == "work"
        assert data["results"][1]["category"] == "distraction"
        assert data["results"][2]["category"] == "work"

    def test_batch_empty_items_returns_422(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post("/v1/classify/batch", json={"items": []})
        assert resp.status_code == 422

    def test_batch_missing_items_field_returns_422(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.post("/v1/classify/batch", json={})
        assert resp.status_code == 422

    def test_batch_single_item(self, app_with_fallback: TestClient) -> None:
        payload = {"items": [{"app_name": "Slack"}]}
        resp = app_with_fallback.post("/v1/classify/batch", json=payload)
        assert resp.status_code == 200
        assert resp.json()["total"] == 1


# ---------------------------------------------------------------------------
# GET /metrics
# ---------------------------------------------------------------------------


class TestMetrics:
    def test_metrics_endpoint_returns_200(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/metrics")
        assert resp.status_code == 200

    def test_metrics_content_type_is_prometheus(self, app_with_fallback: TestClient) -> None:
        resp = app_with_fallback.get("/metrics")
        assert "text/plain" in resp.headers["content-type"]

    def test_metrics_contains_classify_counter(self, app_with_fallback: TestClient) -> None:
        # Trigger a classify first so the counter exists
        app_with_fallback.post("/v1/classify", json={"app_name": "Slack"})
        resp = app_with_fallback.get("/metrics")
        assert "personel_ml_classify_total" in resp.text
