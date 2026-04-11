"""
Postprocessing of raw OCR engine output.

Steps applied in order:
  1. Filter words below the confidence threshold.
  2. Strip control characters (NUL, BEL, BS, etc.).
  3. Collapse whitespace (multiple spaces/newlines -> single space).
  4. Compute mean confidence across retained words.
"""

from __future__ import annotations

import re
import unicodedata
from dataclasses import dataclass

from personel_ocr.engines.base import EngineResult, WordResult

# Control characters to strip: C0 and C1 ranges except tab (\x09), LF (\x0A),
# CR (\x0D) which are legitimate whitespace in multi-line OCR output.
_CONTROL_CHAR_RE = re.compile(
    r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]"
)

# Collapse runs of whitespace (including tabs and newlines) to a single space.
_WHITESPACE_RE = re.compile(r"\s+")


@dataclass
class PostprocessResult:
    """Output of the postprocessing step."""

    text: str
    confidence: float  # mean confidence over retained words; 0.0 if none
    word_count: int
    language_detected: str
    engine: str


def postprocess(
    engine_result: EngineResult,
    confidence_threshold: float = 0.30,
) -> PostprocessResult:
    """Postprocess a raw EngineResult.

    Args:
        engine_result:        Raw output from an OCREngine.extract() call.
        confidence_threshold: Words with confidence below this value are dropped.
                              Default 0.30 per PERSONEL_OCR_CONFIDENCE_THRESHOLD.

    Returns:
        PostprocessResult with cleaned text and summary metrics.
    """
    retained: list[WordResult] = [
        w for w in engine_result.words if w.confidence >= confidence_threshold
    ]

    if retained:
        raw_text = " ".join(w.text for w in retained)
        mean_confidence = sum(w.confidence for w in retained) / len(retained)
    else:
        # Fall back to the full raw_text if no words survive threshold filtering.
        # This can happen when the engine doesn't produce per-word confidences
        # (e.g., the entire output has confidence 0.0 — still better than empty).
        raw_text = engine_result.raw_text
        mean_confidence = 0.0

    # Step 1: Normalise Unicode (NFC)
    text = unicodedata.normalize("NFC", raw_text)

    # Step 2: Strip control characters
    text = _CONTROL_CHAR_RE.sub("", text)

    # Step 3: Collapse whitespace
    text = _WHITESPACE_RE.sub(" ", text).strip()

    word_count = len(text.split()) if text else 0

    return PostprocessResult(
        text=text,
        confidence=round(mean_confidence, 4),
        word_count=word_count,
        language_detected=engine_result.language_detected,
        engine=engine_result.engine,
    )
