"""
Adapter that bridges ``OCRPipeline`` (legacy shape) and the canonical Faz 8
response model.

Design:
  - We delegate extraction to the existing pipeline path — redaction stays
    inside the pipeline, exactly as the KVKK invariant demands (redaction
    is the LAST transformation before the response leaves the service).
  - The adapter then reformats the legacy output into the canonical shape,
    and — if requested — copies the per-word text through the same redaction
    function that already sanitises the aggregate text. Both are re-run so a
    caller filtering by confidence cannot reassemble a pre-redaction string.
  - Batch execution uses a ``ThreadPoolExecutor`` because the tesseract path
    releases the GIL via the C binding. PaddleOCR and preprocess also spend
    their time in C / native extensions. Threads are the right primitive here.

Threading note:
  The pipeline object itself is stateless — engines are the only shared state,
  and tesseract/paddle engines are safe to call concurrently (each ``extract``
  call spawns its own subprocess / runs within its own GIL window).
"""

from __future__ import annotations

import concurrent.futures as cf
import time
import uuid
from base64 import b64decode

import structlog

from personel_ocr.canonical import (
    KIND_TO_RULE,
    BackendLiteral,
    CanonicalBatchRequest,
    CanonicalBatchResponse,
    CanonicalExtractRequest,
    CanonicalExtractResponse,
    CanonicalRedactionHit,
    CanonicalWord,
)
from personel_ocr.metrics import (
    BATCH_SIZE,
    EXTRACTIONS_TOTAL,
    EXTRACTION_LATENCY,
    REDACTIONS_TOTAL,
    WORD_COUNT,
)
from personel_ocr.pipeline import OCRPipeline
from personel_ocr.redaction import redact
from personel_ocr.schemas import ExtractRequest as LegacyExtractRequest

logger = structlog.get_logger(__name__)

# Max total payload for a batch call, in bytes. 10 MB is the task contract.
MAX_BATCH_PAYLOAD_BYTES = 10 * 1024 * 1024


def _language_to_iso(req_language: str) -> list[str]:
    """Canonical request language -> legacy ISO-639-1 list used by engines."""
    if req_language == "tur":
        return ["tr", "en"]  # include en as safety net; tesseract-tur can mix
    if req_language == "eng":
        return ["en"]
    return ["tr", "en"]  # auto


def _iso_to_canonical(detected: str) -> str:
    """Engine-reported code -> canonical language identifier."""
    if not detected:
        return "auto"
    d = detected.lower()
    if d in ("tr", "tur", "tr-tr"):
        return "tur"
    if d in ("en", "eng", "en-us", "en-gb"):
        return "eng"
    return d


class CanonicalAdapter:
    """Canonical-shape orchestrator sitting in front of the legacy pipeline."""

    def __init__(self, pipeline: OCRPipeline) -> None:
        self._pipeline = pipeline

    # ------------------------------------------------------------------
    # Single
    # ------------------------------------------------------------------

    def extract(self, req: CanonicalExtractRequest) -> CanonicalExtractResponse:
        t0 = time.monotonic()
        request_id = _new_request_id()

        legacy_req = LegacyExtractRequest(
            image_bytes=req.image_bytes,
            tenant_id=req.tenant_id,
            endpoint_id=req.endpoint_id,
            screenshot_id=req.screenshot_id,
            engine_hint=req.backend_hint,
            languages=_language_to_iso(req.language),
        )

        legacy_resp = self._pipeline.extract(legacy_req)

        # Legacy path already returned a redacted payload + summary. Shape-
        # convert the summary to the canonical [{rule, count}] form.
        hits: list[CanonicalRedactionHit] = []
        for entry in legacy_resp.redactions:
            if entry.count > 0:
                rule = KIND_TO_RULE.get(entry.kind)
                if rule is not None:
                    hits.append(CanonicalRedactionHit(rule=rule, count=entry.count))  # type: ignore[arg-type]

        # Optional per-word output — only computed when the caller opts in.
        words_out: list[CanonicalWord] | None = None
        if req.confidence_per_word:
            words_out = self._build_redacted_words(legacy_resp.text, legacy_resp.confidence)

        # Best-effort engine-detected -> canonical language mapping.
        language_canonical = _iso_to_canonical(legacy_resp.language_detected) or (
            "tur" if req.language == "auto" else req.language
        )

        elapsed_ms = int((time.monotonic() - t0) * 1000)

        # Observability: single-extract counters + histograms. We double-emit
        # some metrics here so the canonical endpoint shows up independently
        # in the Prometheus series.
        EXTRACTIONS_TOTAL.labels(engine=legacy_resp.engine, status="success").inc()
        EXTRACTION_LATENCY.labels(engine=legacy_resp.engine).observe(
            max(elapsed_ms, 1) / 1000.0
        )
        WORD_COUNT.observe(legacy_resp.word_count)
        for hit in hits:
            REDACTIONS_TOTAL.labels(kind=hit.rule.lower()).inc(hit.count)

        return CanonicalExtractResponse(
            request_id=request_id,
            backend=legacy_resp.engine,  # type: ignore[arg-type]
            language=language_canonical,
            text_redacted=legacy_resp.text,
            confidence_overall=legacy_resp.confidence,
            word_count=legacy_resp.word_count,
            redaction_hits=hits,
            processing_ms=elapsed_ms,
            words=words_out,
        )

    # ------------------------------------------------------------------
    # Batch
    # ------------------------------------------------------------------

    def extract_batch(
        self,
        batch: CanonicalBatchRequest,
        *,
        max_workers: int = 4,
    ) -> CanonicalBatchResponse:
        t0 = time.monotonic()
        request_id = _new_request_id()

        # Enforce the 10 MB total-payload cap. We decode lazily per-item for
        # extraction but sum base64 lengths (approx 1.33x raw) for the guard
        # to avoid touching every byte twice.
        total_b64 = sum(len(i.image_bytes) for i in batch.items)
        total_raw_estimate = int(total_b64 * 0.75)
        if total_raw_estimate > MAX_BATCH_PAYLOAD_BYTES:
            raise ValueError(
                f"batch payload too large: {total_raw_estimate} bytes > {MAX_BATCH_PAYLOAD_BYTES}"
            )

        BATCH_SIZE.observe(len(batch.items))

        results: list[CanonicalExtractResponse | None] = [None] * len(batch.items)

        def _run(i: int, item: CanonicalExtractRequest) -> None:
            try:
                results[i] = self.extract(item)
            except Exception as exc:  # noqa: BLE001
                # KVKK invariant: never echo pre-redaction text in errors.
                logger.warning(
                    "canonical_batch.item_error",
                    screenshot_id=item.screenshot_id,
                    error_class=type(exc).__name__,
                )
                results[i] = CanonicalExtractResponse(
                    request_id=_new_request_id(),
                    backend="tesseract",
                    language="auto",
                    text_redacted="",
                    confidence_overall=0.0,
                    word_count=0,
                    redaction_hits=[],
                    processing_ms=0,
                    words=None,
                )

        # Clamp worker count to batch size — no point spawning 4 threads for 1 item.
        workers = min(max_workers, max(1, len(batch.items)))
        with cf.ThreadPoolExecutor(max_workers=workers) as pool:
            futs = [pool.submit(_run, i, item) for i, item in enumerate(batch.items)]
            for _f in cf.as_completed(futs):
                pass  # exceptions captured inside _run

        elapsed_ms = int((time.monotonic() - t0) * 1000)
        return CanonicalBatchResponse(
            request_id=request_id,
            total=len(results),
            processing_ms=elapsed_ms,
            results=[r for r in results if r is not None],
        )

    # ------------------------------------------------------------------
    # Word shape helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _build_redacted_words(redacted_text: str, overall_conf: float) -> list[CanonicalWord]:
        """Tokenise redacted text on whitespace and re-run redaction per-word.

        Whitespace tokenisation is pragmatic — Tesseract gives us aggregate
        text at the `extract` stage, so per-word confidence is approximated
        from the mean. Re-running redaction is defence in depth: even if the
        aggregate pass missed a pattern that only becomes visible once the
        text is tokenised (unlikely), the per-word output stays KVKK-safe.
        """
        if not redacted_text:
            return []
        out: list[CanonicalWord] = []
        for raw_word in redacted_text.split():
            word_safe = redact(raw_word).text
            out.append(CanonicalWord(text=word_safe, confidence=overall_conf))
        return out


def _new_request_id() -> str:
    """Short, URL-safe request id (UUID7-ish via uuid4).

    We use a plain ``uuid4`` to avoid a dep on a ULID library. The goal is
    correlation in logs, not monotonicity.
    """
    return uuid.uuid4().hex
