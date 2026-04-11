"""
OCREngine abstract base class.

All concrete engine implementations must subclass OCREngine and implement
the `extract` method.  The pipeline (pipeline.py) calls only this interface,
so engines are interchangeable without touching routing logic.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field


@dataclass
class WordResult:
    """A single recognised word with its confidence score."""

    text: str
    confidence: float  # [0.0, 1.0]


@dataclass
class EngineResult:
    """Raw output produced by an OCR engine before postprocessing.

    Attributes:
        words:           All recognised words with per-word confidence.
        engine:          Engine name that produced this result ("tesseract" | "paddle").
        language_detected: BCP-47 / ISO-639-1 language code detected by the engine,
                           or empty string if detection is not supported.
        raw_text:        Full concatenated text as returned by the engine
                         (before postprocessing / redaction).
    """

    words: list[WordResult] = field(default_factory=list)
    engine: str = ""
    language_detected: str = ""
    raw_text: str = ""


class OCREngine(ABC):
    """Abstract interface for OCR backends.

    Engines are expected to be stateful singletons loaded once at startup.
    The `is_available` property must return False if the underlying binary
    or model cannot be found — the service degrades gracefully in that case.
    """

    @property
    @abstractmethod
    def name(self) -> str:
        """Short engine identifier, e.g. "tesseract" or "paddle"."""

    @property
    @abstractmethod
    def is_available(self) -> bool:
        """True if the engine binary / model is present and usable."""

    @abstractmethod
    def extract(self, image_bytes: bytes, languages: list[str]) -> EngineResult:
        """Run OCR on the provided image bytes.

        Args:
            image_bytes: Raw image bytes (JPEG, PNG, WebP, BMP, TIFF).
            languages:   List of ISO-639-1 language codes, e.g. ["tr", "en"].
                         The engine maps these to its own language pack names.

        Returns:
            EngineResult with recognised words and metadata.

        Raises:
            RuntimeError: If the engine is not available or encounters a fatal error.
        """
