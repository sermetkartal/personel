"""
Pytest configuration and shared fixtures for personel-ocr tests.

Tests are designed to run WITHOUT a Tesseract binary or PaddleOCR model.
All engine calls are mocked.  The OCR pipeline is tested end-to-end by
injecting a fake engine that returns synthetic EngineResult objects.
"""

from __future__ import annotations

import base64
import io
from pathlib import Path
from typing import Generator
from unittest.mock import MagicMock

import pytest
from fastapi.testclient import TestClient
from PIL import Image

from personel_ocr.config import Settings, get_settings
from personel_ocr.engines.base import EngineResult, OCREngine, WordResult
from personel_ocr.pipeline import OCRPipeline

FIXTURES_DIR = Path(__file__).parent / "fixtures"
SYNTH_DIR = FIXTURES_DIR / "synthetic_screenshots"


# ---------------------------------------------------------------------------
# Settings override
# ---------------------------------------------------------------------------


@pytest.fixture(autouse=True)
def reset_settings_cache() -> Generator[None, None, None]:
    get_settings.cache_clear()
    yield
    get_settings.cache_clear()


@pytest.fixture
def test_settings(monkeypatch: pytest.MonkeyPatch) -> Settings:
    monkeypatch.setenv("PERSONEL_OCR_LOG_LEVEL", "debug")
    monkeypatch.setenv("PERSONEL_OCR_CONFIDENCE_THRESHOLD", "0.30")
    get_settings.cache_clear()
    return get_settings()


# ---------------------------------------------------------------------------
# Synthetic image helpers
# ---------------------------------------------------------------------------


def make_blank_png(width: int = 64, height: int = 32) -> bytes:
    """Create a small blank white PNG in memory."""
    img = Image.new("RGB", (width, height), color=(255, 255, 255))
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


def make_blank_png_b64(width: int = 64, height: int = 32) -> str:
    return base64.b64encode(make_blank_png(width, height)).decode()


# ---------------------------------------------------------------------------
# Mock engine
# ---------------------------------------------------------------------------


class FakeOCREngine(OCREngine):
    """A fully controllable mock OCR engine for testing."""

    def __init__(
        self,
        name_str: str = "tesseract",
        available: bool = True,
        result: EngineResult | None = None,
        raise_on_extract: Exception | None = None,
    ) -> None:
        self._name = name_str
        self._available = available
        self._result = result or EngineResult(
            words=[WordResult(text="hello", confidence=0.9)],
            engine=name_str,
            language_detected="tr",
            raw_text="hello",
        )
        self._raise = raise_on_extract

    @property
    def name(self) -> str:
        return self._name

    @property
    def is_available(self) -> bool:
        return self._available

    def extract(self, image_bytes: bytes, languages: list[str]) -> EngineResult:  # noqa: ARG002
        if self._raise is not None:
            raise self._raise
        return self._result


@pytest.fixture
def fake_tesseract() -> FakeOCREngine:
    return FakeOCREngine(name_str="tesseract", available=True)


@pytest.fixture
def unavailable_tesseract() -> FakeOCREngine:
    return FakeOCREngine(name_str="tesseract", available=False)


@pytest.fixture
def fake_paddle() -> FakeOCREngine:
    return FakeOCREngine(name_str="paddle", available=False)


# ---------------------------------------------------------------------------
# Pipeline fixture
# ---------------------------------------------------------------------------


@pytest.fixture
def ocr_pipeline(
    fake_tesseract: FakeOCREngine,
    fake_paddle: FakeOCREngine,
    test_settings: Settings,
) -> OCRPipeline:
    return OCRPipeline(
        tesseract_engine=fake_tesseract,
        paddle_engine=fake_paddle,
        settings=test_settings,
    )


# ---------------------------------------------------------------------------
# FastAPI TestClient with mocked pipeline
# ---------------------------------------------------------------------------


@pytest.fixture
def app_client(
    ocr_pipeline: OCRPipeline,
    test_settings: Settings,
) -> Generator[TestClient, None, None]:
    from personel_ocr.main import create_app

    application = create_app()
    application.state.pipeline = ocr_pipeline

    with TestClient(application, raise_server_exceptions=True, lifespan="off") as client:
        yield client
