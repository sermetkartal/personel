"""
PaddleOCR engine backend (scaffolded — Phase 2 supplementary).

PaddleOCR is used as a fallback for handwriting and CJK script.  Turkish
support via Tesseract is the primary path; this engine is supplementary and
loaded lazily.

The paddleocr package is an optional heavy dependency (~1 GB of model data).
The import is deferred so the service starts even if paddleocr is not
installed.  When unavailable, `is_available` returns False and the engine
is skipped in the auto-selection logic.

Phase 2 status: interface complete; real inference is wired in but model
files are not bundled.  The engine will degrade gracefully at runtime when
models are absent.
"""

from __future__ import annotations

import io
import logging

from personel_ocr.engines.base import EngineResult, OCREngine, WordResult

logger = logging.getLogger(__name__)


class PaddleEngine(OCREngine):
    """PaddleOCR engine — Latin/CJK fallback."""

    def __init__(self, lang: str = "en", use_gpu: bool = False) -> None:
        self._lang = lang
        self._use_gpu = use_gpu
        self._ocr: object | None = None
        self._available: bool | None = None

    @property
    def name(self) -> str:
        return "paddle"

    @property
    def is_available(self) -> bool:
        if self._available is not None:
            return self._available
        self._available = self._check_availability()
        return self._available

    def _check_availability(self) -> bool:
        try:
            from paddleocr import PaddleOCR  # noqa: PLC0415

            self._ocr = PaddleOCR(
                use_angle_cls=True,
                lang=self._lang,
                use_gpu=self._use_gpu,
                show_log=False,
            )
            logger.info("paddle_engine.available", extra={"lang": self._lang})
            return True
        except Exception as exc:
            logger.warning("paddle_engine.unavailable", extra={"reason": str(exc)})
            return False

    def extract(self, image_bytes: bytes, languages: list[str]) -> EngineResult:  # noqa: ARG002
        """Run PaddleOCR inference on the provided image bytes.

        Args:
            image_bytes: Raw image bytes.
            languages:   Ignored — PaddleOCR uses the lang model set at init time.

        Returns:
            EngineResult with recognised words.

        Raises:
            RuntimeError: If paddleocr is not installed or inference fails.
        """
        if not self.is_available or self._ocr is None:
            raise RuntimeError(
                "PaddleOCR is not available. "
                "Install paddleocr and its model dependencies."
            )

        import numpy as np  # noqa: PLC0415
        from PIL import Image  # noqa: PLC0415

        try:
            image = Image.open(io.BytesIO(image_bytes)).convert("RGB")
            img_array = np.array(image)
        except Exception as exc:
            raise RuntimeError(f"Failed to decode image for PaddleOCR: {exc}") from exc

        try:
            # PaddleOCR.ocr returns: list of pages, each page is a list of
            # [bounding_box, (text, confidence)] entries.
            result = self._ocr.ocr(img_array, cls=True)  # type: ignore[union-attr]
        except Exception as exc:
            raise RuntimeError(f"PaddleOCR inference failed: {exc}") from exc

        words: list[WordResult] = []
        texts: list[str] = []

        if result:
            for page in result:
                if not page:
                    continue
                for line in page:
                    if not line or len(line) < 2:
                        continue
                    text_conf = line[1]
                    if not text_conf or len(text_conf) < 2:
                        continue
                    text = str(text_conf[0]).strip()
                    conf = float(text_conf[1]) if text_conf[1] is not None else 0.0
                    if text:
                        words.append(WordResult(text=text, confidence=conf))
                        texts.append(text)

        raw_text = " ".join(texts)
        return EngineResult(
            words=words,
            engine=self.name,
            language_detected="",
            raw_text=raw_text,
        )
