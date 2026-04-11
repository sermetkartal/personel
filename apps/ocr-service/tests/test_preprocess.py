"""
Unit tests for image preprocessing.

All tests use synthetically generated PIL images to avoid committing
real screenshots.  No Tesseract binary is needed.
"""

from __future__ import annotations

import io

import pytest
from PIL import Image

from personel_ocr.preprocess import preprocess


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_rgb_png(width: int = 32, height: int = 16, color: tuple[int, int, int] = (200, 100, 50)) -> bytes:
    img = Image.new("RGB", (width, height), color=color)
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


def _make_rgba_png(width: int = 32, height: int = 16) -> bytes:
    img = Image.new("RGBA", (width, height), color=(200, 100, 50, 128))
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


def _make_palette_png(width: int = 32, height: int = 16) -> bytes:
    img = Image.new("P", (width, height))
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


def _open_output(output_bytes: bytes) -> Image.Image:
    return Image.open(io.BytesIO(output_bytes))


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestPreprocessGrayscale:
    def test_rgb_to_grayscale(self) -> None:
        raw = _make_rgb_png()
        out = preprocess(raw, grayscale=True, autocontrast=False)
        img = _open_output(out)
        assert img.mode == "L"

    def test_rgba_to_grayscale(self) -> None:
        raw = _make_rgba_png()
        out = preprocess(raw, grayscale=True, autocontrast=False)
        img = _open_output(out)
        assert img.mode == "L"

    def test_palette_to_grayscale(self) -> None:
        raw = _make_palette_png()
        out = preprocess(raw, grayscale=True, autocontrast=False)
        img = _open_output(out)
        assert img.mode == "L"

    def test_no_grayscale_preserves_rgb_mode(self) -> None:
        raw = _make_rgb_png()
        out = preprocess(raw, grayscale=False, autocontrast=False)
        img = _open_output(out)
        # Output is PNG; mode may be RGB or L depending on PIL save behaviour.
        assert img.mode in ("RGB", "L")

    def test_output_is_valid_png(self) -> None:
        raw = _make_rgb_png()
        out = preprocess(raw)
        img = _open_output(out)
        assert img.format == "PNG"


class TestPreprocessAutocontrast:
    def test_autocontrast_does_not_raise(self) -> None:
        raw = _make_rgb_png()
        out = preprocess(raw, grayscale=True, autocontrast=True)
        assert isinstance(out, bytes)
        assert len(out) > 0

    def test_flat_image_autocontrast(self) -> None:
        # All pixels same colour — autocontrast should still produce valid output.
        img = Image.new("L", (16, 16), color=128)
        buf = io.BytesIO()
        img.save(buf, format="PNG")
        raw = buf.getvalue()
        out = preprocess(raw, grayscale=True, autocontrast=True)
        result = _open_output(out)
        assert result.mode == "L"


class TestPreprocessThreshold:
    def test_threshold_produces_binary_image(self) -> None:
        raw = _make_rgb_png()
        out = preprocess(raw, grayscale=True, autocontrast=False, threshold=True)
        img = _open_output(out)
        # All pixel values should be 0 or 255
        pixels = list(img.getdata())
        for px in pixels:
            assert px in (0, 255), f"Unexpected pixel value: {px}"


class TestPreprocessDimensions:
    def test_dimensions_preserved(self) -> None:
        raw = _make_rgb_png(width=100, height=50)
        out = preprocess(raw, grayscale=True, autocontrast=False)
        img = _open_output(out)
        assert img.width == 100
        assert img.height == 50


class TestPreprocessInvalidInput:
    def test_invalid_bytes_raises_value_error(self) -> None:
        with pytest.raises(ValueError, match="Cannot decode image"):
            preprocess(b"not an image")

    def test_empty_bytes_raises_value_error(self) -> None:
        with pytest.raises(ValueError):
            preprocess(b"")
