"""
OCR extraction pipeline.

Full pipeline:
  1. Decode base64 image bytes from the request.
  2. Preprocess (grayscale, autocontrast, optional threshold/deskew).
  3. Select OCR engine (tesseract / paddle / auto).
  4. Run engine extraction.
  5. Postprocess (confidence filter, whitespace collapse, unicode normalise).
  6. Redact PII (TCKN, IBAN, credit card, phone, email).
  7. Return ExtractResponse.

KVKK invariant: redaction is the LAST step before response assembly.
If redaction raises, the caller returns 500 with a generic error message —
the raw text is never echoed back.
"""

from __future__ import annotations

import base64
import binascii
import time

import structlog

from personel_ocr.config import Settings
from personel_ocr.engines.base import OCREngine
from personel_ocr.postprocess import postprocess
from personel_ocr.preprocess import preprocess
from personel_ocr.redaction import RedactionEntry, RedactionResult, redact
from personel_ocr.schemas import (
    EngineLiteral,
    ExtractRequest,
    ExtractResponse,
    RedactionEntry as SchemaRedactionEntry,
)

logger = structlog.get_logger(__name__)


class OCRPipeline:
    """Stateless pipeline coordinator.

    Engines are injected at construction time (from app.state), so the
    pipeline itself is side-effect-free and testable without I/O.
    """

    def __init__(
        self,
        tesseract_engine: OCREngine,
        paddle_engine: OCREngine,
        settings: Settings,
    ) -> None:
        self._tesseract = tesseract_engine
        self._paddle = paddle_engine
        self._settings = settings

    # ------------------------------------------------------------------
    # Engine selection
    # ------------------------------------------------------------------

    def _select_engine(self, hint: EngineLiteral) -> OCREngine:
        """Return the appropriate engine based on the hint and availability.

        Priority for "auto":
          1. tesseract (preferred for tr+en)
          2. paddle
        """
        if hint == "tesseract":
            if not self._tesseract.is_available:
                raise RuntimeError(
                    "Tesseract engine requested but not available. "
                    "Check /readyz for engine status."
                )
            return self._tesseract
        if hint == "paddle":
            if not self._paddle.is_available:
                raise RuntimeError(
                    "PaddleOCR engine requested but not available. "
                    "Check /readyz for engine status."
                )
            return self._paddle
        # auto
        if self._tesseract.is_available:
            return self._tesseract
        if self._paddle.is_available:
            return self._paddle
        raise RuntimeError(
            "No OCR engine is available. "
            "Install tesseract-ocr (tur+eng packs) or paddleocr."
        )

    # ------------------------------------------------------------------
    # Main extraction entry point
    # ------------------------------------------------------------------

    def extract(self, request: ExtractRequest) -> ExtractResponse:
        """Run the full OCR pipeline for a single extraction request.

        Args:
            request: Validated ExtractRequest from the HTTP layer.

        Returns:
            ExtractResponse with redacted text and metadata.

        Raises:
            ValueError:   If base64 decoding fails.
            RuntimeError: If no engine is available or engine extraction fails.
            Exception:    Propagated as-is — routes.py wraps in HTTP 500.
        """
        t0 = time.monotonic()

        log = logger.bind(
            screenshot_id=request.screenshot_id,
            tenant_id=request.tenant_id,
            endpoint_id=request.endpoint_id,
            engine_hint=request.engine_hint,
        )

        # Step 1: Decode base64
        try:
            raw_image_bytes = base64.b64decode(request.image_bytes, validate=True)
        except (binascii.Error, ValueError) as exc:
            raise ValueError(f"Invalid base64 image data: {exc}") from exc

        # Step 2: Preprocess
        try:
            processed_bytes = preprocess(
                raw_image_bytes,
                grayscale=self._settings.preprocess_grayscale,
                autocontrast=self._settings.preprocess_autocontrast,
                threshold=self._settings.preprocess_threshold,
                deskew=False,  # Phase 2 stub
            )
        except Exception as exc:
            raise RuntimeError(f"Image preprocessing failed: {exc}") from exc

        # Step 3: Select engine
        engine = self._select_engine(request.engine_hint)
        log = log.bind(engine=engine.name)

        # Step 4: Extract
        try:
            engine_result = engine.extract(processed_bytes, request.languages)
        except Exception as exc:
            raise RuntimeError(f"OCR extraction failed: {exc}") from exc

        # Step 5: Postprocess
        pp = postprocess(
            engine_result,
            confidence_threshold=self._settings.confidence_threshold,
        )

        # Step 6: Redact — KVKK invariant: must be the last transformation.
        # If this raises, propagate — routes.py returns 500 without text.
        redaction_result: RedactionResult = redact(pp.text)

        latency_ms = round((time.monotonic() - t0) * 1000, 2)

        log.info(
            "ocr.extracted",
            word_count=pp.word_count,
            confidence=pp.confidence,
            latency_ms=latency_ms,
            redactions_tckn=redaction_result.summary.tckn,
            redactions_iban=redaction_result.summary.iban,
            redactions_cc=redaction_result.summary.credit_card,
        )

        redaction_entries = [
            SchemaRedactionEntry(kind=entry["kind"], count=entry["count"])  # type: ignore[arg-type]
            for entry in redaction_result.summary.as_list()
        ]

        return ExtractResponse(
            text=redaction_result.text,
            confidence=pp.confidence,
            engine=engine.name,  # type: ignore[arg-type]
            language_detected=pp.language_detected,
            word_count=pp.word_count,
            redactions=redaction_entries,
            latency_ms=latency_ms,
        )
