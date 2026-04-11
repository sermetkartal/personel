"""
Pytest configuration and shared fixtures for personel-ml tests.

Tests are designed to run without a GGUF model file or GPU.
All LlamaClassifier tests are skipped in CI unless PERSONEL_ML_MODEL_PATH
is explicitly set and the file exists.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Generator
from unittest.mock import MagicMock, patch

import pytest
from fastapi.testclient import TestClient

from personel_ml.classifier import FallbackClassifier, LlamaClassifier
from personel_ml.config import Settings, get_settings
from personel_ml.schemas import ClassifyItem, ClassifyResult

# ---------------------------------------------------------------------------
# Fixtures directory
# ---------------------------------------------------------------------------

FIXTURES_DIR = Path(__file__).parent / "fixtures"


# ---------------------------------------------------------------------------
# Settings override — use test defaults, avoid reading .env
# ---------------------------------------------------------------------------


@pytest.fixture(autouse=True)
def reset_settings_cache() -> Generator[None, None, None]:
    """Clear the lru_cache on Settings so each test gets a fresh config."""
    get_settings.cache_clear()
    yield
    get_settings.cache_clear()


@pytest.fixture
def test_settings(monkeypatch: pytest.MonkeyPatch) -> Settings:
    """Settings with safe test defaults — no real model path."""
    monkeypatch.setenv("PERSONEL_ML_MODEL_PATH", "/nonexistent/model.gguf")
    monkeypatch.setenv("PERSONEL_ML_LOG_LEVEL", "debug")
    monkeypatch.setenv("PERSONEL_ML_N_THREADS", "2")
    get_settings.cache_clear()
    return get_settings()


# ---------------------------------------------------------------------------
# Classifier fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def fallback_classifier() -> FallbackClassifier:
    return FallbackClassifier(confidence_threshold=0.70, model_version="fallback-test")


@pytest.fixture
def classify_item_factory():
    """Factory function for creating ClassifyItem instances."""

    def _factory(
        app_name: str = "chrome.exe",
        window_title: str = "",
        url: str | None = None,
    ) -> ClassifyItem:
        return ClassifyItem(app_name=app_name, window_title=window_title, url=url)

    return _factory


# ---------------------------------------------------------------------------
# Test examples from fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def classify_examples() -> list[dict]:
    """Load the 30+ classify examples from fixtures/classify_examples.json."""
    examples_file = FIXTURES_DIR / "classify_examples.json"
    if not examples_file.exists():
        return []
    with open(examples_file, encoding="utf-8") as f:
        data = json.load(f)
    return data.get("examples", [])


# ---------------------------------------------------------------------------
# FastAPI TestClient with mocked LlamaClassifier
# ---------------------------------------------------------------------------


@pytest.fixture
def mock_llama_result() -> ClassifyResult:
    return ClassifyResult(
        category="work",
        confidence=0.92,
        backend="llama",
        model_version="llama-3.2-3b-q4_k_m-test",
    )


@pytest.fixture
def app_with_fallback(test_settings: Settings) -> Generator:
    """FastAPI app with FallbackClassifier injected (no model needed).

    Uses TestClient with lifespan=False to skip the startup event that would
    attempt to load the GGUF model. The classifier is injected directly into
    app.state before the client is constructed.
    """
    from personel_ml.main import create_app

    application = create_app()

    fallback = FallbackClassifier(
        confidence_threshold=test_settings.confidence_threshold,
        model_version="fallback-test",
    )
    application.state.classifier = fallback

    # lifespan=False prevents the startup/shutdown events from running,
    # so no model loading is attempted.
    with TestClient(application, raise_server_exceptions=True, lifespan="off") as client:
        yield client


@pytest.fixture
def app_with_mock_llama(mock_llama_result: ClassifyResult) -> Generator:
    """FastAPI app with a mocked LlamaClassifier that always returns mock_llama_result."""
    from personel_ml.main import create_app

    application = create_app()

    mock_classifier = MagicMock(spec=LlamaClassifier)
    mock_classifier.classify.return_value = mock_llama_result
    mock_classifier.backend = "llama"
    mock_classifier.model_version = "llama-3.2-3b-q4_k_m-test"
    mock_classifier.is_loaded = True

    application.state.classifier = mock_classifier

    with TestClient(application, raise_server_exceptions=True, lifespan="off") as client:
        yield client
