"""
Image preprocessing pipeline.

Normalisations applied before OCR to improve recognition accuracy:
  1. Grayscale conversion  — reduces colour noise.
  2. Auto-contrast         — stretches histogram to full 0-255 range.
  3. Threshold (optional)  — binarises image; improves scanned documents,
                             can hurt anti-aliased screenshots.
  4. Deskew (stub)         — Phase 2 polish; not yet implemented.

All operations use PIL.Image only.  numpy is deliberately not imported here
so that the preprocessing path works even if numpy is not installed (numpy
is only required by PaddleOCR).
"""

from __future__ import annotations

import io

from PIL import Image, ImageFilter, ImageOps


def preprocess(
    image_bytes: bytes,
    *,
    grayscale: bool = True,
    autocontrast: bool = True,
    threshold: bool = False,
    deskew: bool = False,
) -> bytes:
    """Preprocess image bytes and return normalised image bytes (PNG).

    Args:
        image_bytes:   Raw image bytes from the request.
        grayscale:     Convert to single-channel grayscale.
        autocontrast:  Apply PIL ImageOps.autocontrast.
        threshold:     Apply binary threshold (Otsu approximation via PIL).
        deskew:        Deskew the image (stub — not implemented in Phase 2).

    Returns:
        PNG-encoded bytes of the processed image.

    Raises:
        ValueError: If the image cannot be decoded.
    """
    try:
        image = Image.open(io.BytesIO(image_bytes))
    except Exception as exc:
        raise ValueError(f"Cannot decode image: {exc}") from exc

    # Ensure we work in a consistent mode
    if image.mode in ("RGBA", "P", "LA"):
        image = image.convert("RGB")

    if grayscale:
        image = image.convert("L")

    if autocontrast:
        image = ImageOps.autocontrast(image)

    if threshold:
        image = _threshold_otsu_approx(image)

    if deskew:
        image = _deskew_stub(image)

    buf = io.BytesIO()
    image.save(buf, format="PNG")
    return buf.getvalue()


def _threshold_otsu_approx(image: Image.Image) -> Image.Image:
    """Apply a simple global threshold approximating Otsu's method via PIL.

    PIL does not implement Otsu natively.  We use a fixed midpoint threshold
    (128) which is a reasonable approximation for high-contrast screenshots.
    For true Otsu, numpy/scipy would be needed — deferred to Phase 2 polish.
    """
    if image.mode != "L":
        image = image.convert("L")
    # PIL point() applies a lookup table: pixel > 128 -> 255, else -> 0.
    return image.point(lambda px: 255 if px >= 128 else 0, mode="L")


def _deskew_stub(image: Image.Image) -> Image.Image:
    """Deskew stub — Phase 2 polish.

    A production implementation would:
      1. Detect skew angle via Hough transform (opencv or scikit-image).
      2. Rotate by -angle to correct.

    For now, returns the image unchanged.
    """
    return image
