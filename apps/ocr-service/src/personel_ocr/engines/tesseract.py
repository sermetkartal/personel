"""
Tesseract OCR engine backend.

Uses pytesseract as the Python binding to Tesseract 5.x.  The import of
pytesseract is deferred so that the service starts even if the Tesseract
binary is not installed — in that case `is_available` returns False and
the /readyz endpoint returns 'degraded'.

Language mapping:
  ISO-639-1 "tr" -> Tesseract lang code "tur"
  ISO-639-1 "en" -> Tesseract lang code "eng"
  Unknown codes are passed through unchanged (Tesseract rejects unknown
  lang packs at runtime, which is caught and surfaced as RuntimeError).
"""

from __future__ import annotations

import io
import logging

from personel_ocr.engines.base import EngineResult, OCREngine, WordResult

logger = logging.getLogger(__name__)

# Tesseract ISO-639-1 to Tesseract language pack name mapping.
_LANG_MAP: dict[str, str] = {
    "tr": "tur",
    "en": "eng",
    "de": "deu",
    "fr": "fra",
    "es": "spa",
    "ar": "ara",
    "zh": "chi_sim",
}


def _map_languages(languages: list[str]) -> str:
    """Convert ISO-639-1 codes to Tesseract '+'-joined lang string."""
    mapped = [_LANG_MAP.get(lang.lower(), lang.lower()) for lang in languages]
    return "+".join(mapped) if mapped else "tur+eng"


class TesseractEngine(OCREngine):
    """Tesseract 5.x OCR engine.

    pytesseract is imported lazily inside __init__ to allow the rest of the
    service to import this module even when tesseract is not installed.
    """

    def __init__(self, tesseract_cmd: str = "tesseract", extra_config: str = "--oem 3 --psm 3") -> None:
        self._tesseract_cmd = tesseract_cmd
        self._extra_config = extra_config
        self._pytesseract: object | None = None
        self._available: bool | None = None  # lazily evaluated

    @property
    def name(self) -> str:
        return "tesseract"

    @property
    def is_available(self) -> bool:
        if self._available is not None:
            return self._available
        self._available = self._check_availability()
        return self._available

    def _check_availability(self) -> bool:
        """Try to import pytesseract and locate the tesseract binary."""
        try:
            import pytesseract  # noqa: PLC0415

            pytesseract.pytesseract.tesseract_cmd = self._tesseract_cmd
            # get_tesseract_version raises EnvironmentError if binary not found
            pytesseract.get_tesseract_version()
            self._pytesseract = pytesseract
            logger.info("tesseract_engine.available", extra={"cmd": self._tesseract_cmd})
            return True
        except Exception as exc:
            logger.warning(
                "tesseract_engine.unavailable",
                extra={"reason": str(exc)},
            )
            return False

    def extract(self, image_bytes: bytes, languages: list[str]) -> EngineResult:
        if not self.is_available:
            raise RuntimeError(
                "Tesseract is not available. "
                "Install tesseract-ocr and the required language packs (tur, eng)."
            )

        import pytesseract  # noqa: PLC0415
        from PIL import Image  # noqa: PLC0415

        pytesseract.pytesseract.tesseract_cmd = self._tesseract_cmd
        lang_str = _map_languages(languages)

        try:
            image = Image.open(io.BytesIO(image_bytes))
        except Exception as exc:
            raise RuntimeError(f"Failed to decode image: {exc}") from exc

        try:
            # image_to_data returns a TSV-formatted string with per-word details.
            tsv_data = pytesseract.image_to_data(
                image,
                lang=lang_str,
                config=self._extra_config,
                output_type=pytesseract.Output.DICT,
            )
        except Exception as exc:
            raise RuntimeError(f"Tesseract extraction failed: {exc}") from exc

        words: list[WordResult] = []
        texts: list[str] = []

        n_boxes = len(tsv_data.get("text", []))
        for i in range(n_boxes):
            word_text = str(tsv_data["text"][i]).strip()
            if not word_text:
                continue
            raw_conf = tsv_data["conf"][i]
            # Tesseract confidence is 0-100; -1 means block-level (not word-level).
            if isinstance(raw_conf, (int, float)) and raw_conf >= 0:
                confidence = float(raw_conf) / 100.0
            else:
                confidence = 0.0
            words.append(WordResult(text=word_text, confidence=confidence))
            texts.append(word_text)

        raw_text = " ".join(texts)

        # Attempt language detection from Tesseract's own output (best effort).
        # pytesseract does not provide language detection per-image; we report
        # the first requested language as the detected language.
        language_detected = languages[0] if languages else ""

        return EngineResult(
            words=words,
            engine=self.name,
            language_detected=language_detected,
            raw_text=raw_text,
        )
