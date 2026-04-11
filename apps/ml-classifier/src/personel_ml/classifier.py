"""
Classifier implementations.

LlamaClassifier  — uses llama-cpp-python to load and run the GGUF model.
FallbackClassifier — regex/dict-based rule engine; zero dependencies beyond stdlib.

Design contract (ADR 0017):
  - Both classifiers expose classify(item: ClassifyItem) -> ClassifyResult
  - Both honour the confidence_threshold: below it, category is forced to 'unknown'
  - LlamaClassifier raises on model load failure; main.py catches and falls through
    to FallbackClassifier (degraded mode, HTTP 200 always)
"""

from __future__ import annotations

import re
import time
from abc import ABC, abstractmethod

import structlog

from personel_ml.schemas import BackendLiteral, ClassifyItem, ClassifyResult

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Abstract base
# ---------------------------------------------------------------------------


class BaseClassifier(ABC):
    """Common interface for all classifier implementations."""

    @abstractmethod
    def classify(self, item: ClassifyItem) -> ClassifyResult:
        ...

    @property
    @abstractmethod
    def backend(self) -> BackendLiteral:
        ...

    @property
    @abstractmethod
    def model_version(self) -> str:
        ...

    @property
    @abstractmethod
    def is_loaded(self) -> bool:
        ...


# ---------------------------------------------------------------------------
# Llama classifier (llama-cpp-python)
# ---------------------------------------------------------------------------


class LlamaClassifier(BaseClassifier):
    """Classifies items using a locally loaded GGUF model via llama-cpp-python.

    Raises RuntimeError on model load failure so the caller can fall back
    to FallbackClassifier without crashing the service.
    """

    def __init__(
        self,
        model_path: str,
        model_version: str,
        n_threads: int,
        n_ctx: int,
        n_batch: int,
        n_gpu_layers: int,
        max_tokens: int,
        temperature: float,
        confidence_threshold: float,
    ) -> None:
        self._model_path = model_path
        self._model_version = model_version
        self._n_threads = n_threads
        self._n_ctx = n_ctx
        self._n_batch = n_batch
        self._n_gpu_layers = n_gpu_layers
        self._max_tokens = max_tokens
        self._temperature = temperature
        self._confidence_threshold = confidence_threshold
        self._llm = None
        self._loaded = False

    def load(self) -> None:
        """Load the GGUF model.  Raises RuntimeError on any failure."""
        from personel_ml.metrics import MODEL_LOAD_TIME

        t0 = time.monotonic()
        logger.info(
            "llama.model.loading",
            model_path=self._model_path,
            n_threads=self._n_threads,
            n_ctx=self._n_ctx,
            n_gpu_layers=self._n_gpu_layers,
        )
        try:
            # Import is intentionally deferred so the module can be imported
            # in test environments where llama_cpp is not installed.
            from llama_cpp import Llama  # type: ignore[import]

            self._llm = Llama(
                model_path=self._model_path,
                n_threads=self._n_threads,
                n_ctx=self._n_ctx,
                n_batch=self._n_batch,
                n_gpu_layers=self._n_gpu_layers,
                verbose=False,
            )
            self._loaded = True
            elapsed = time.monotonic() - t0
            MODEL_LOAD_TIME.set(elapsed)
            logger.info(
                "llama.model.loaded",
                elapsed_s=round(elapsed, 2),
                model_version=self._model_version,
            )
        except Exception as exc:
            elapsed = time.monotonic() - t0
            logger.error(
                "llama.model.load_failed",
                error=str(exc),
                model_path=self._model_path,
                elapsed_s=round(elapsed, 2),
            )
            raise RuntimeError(f"Failed to load GGUF model from {self._model_path}: {exc}") from exc

    def classify(self, item: ClassifyItem) -> ClassifyResult:
        from personel_ml.metrics import CLASSIFY_ERRORS, CLASSIFY_LATENCY, CLASSIFY_TOTAL
        from personel_ml.prompt import build_prompt, extract_json_from_response

        if not self._loaded or self._llm is None:
            raise RuntimeError("Model not loaded; call load() first")

        t0 = time.monotonic()
        try:
            prompt = build_prompt(item)
            output = self._llm(
                prompt,
                max_tokens=self._max_tokens,
                temperature=self._temperature,
                stop=["<|eot_id|>", "\n\n"],
                echo=False,
            )
            raw_text = output["choices"][0]["text"]
            parsed = extract_json_from_response(raw_text)
            category, confidence = self._validate_parsed(parsed)
        except Exception as exc:
            elapsed = time.monotonic() - t0
            logger.warning(
                "llama.classify.error",
                app_name=item.app_name,
                error=str(exc),
                elapsed_s=round(elapsed, 3),
            )
            CLASSIFY_ERRORS.labels(backend="llama").inc()
            category, confidence = "unknown", 0.0

        elapsed = time.monotonic() - t0
        CLASSIFY_TOTAL.labels(backend="llama", category=category).inc()
        CLASSIFY_LATENCY.labels(backend="llama").observe(elapsed)

        return ClassifyResult(
            category=category,
            confidence=confidence,
            backend="llama",
            model_version=self._model_version,
        )

    def _validate_parsed(self, parsed: dict) -> tuple[str, float]:
        """Validate parsed JSON and apply confidence threshold."""
        valid_categories = {"work", "personal", "distraction", "unknown"}

        raw_cat = str(parsed.get("category", "")).lower().strip()
        raw_conf = parsed.get("confidence", 0.0)

        try:
            confidence = float(raw_conf)
            confidence = max(0.0, min(1.0, confidence))
        except (TypeError, ValueError):
            confidence = 0.0

        if raw_cat not in valid_categories:
            return "unknown", 0.0

        if confidence < self._confidence_threshold:
            return "unknown", confidence

        return raw_cat, confidence  # type: ignore[return-value]

    @property
    def backend(self) -> BackendLiteral:
        return "llama"

    @property
    def model_version(self) -> str:
        return self._model_version

    @property
    def is_loaded(self) -> bool:
        return self._loaded

    def unload(self) -> None:
        """Release the model from memory."""
        if self._llm is not None:
            try:
                del self._llm
            except Exception:
                pass
            self._llm = None
        self._loaded = False
        logger.info("llama.model.unloaded")


# ---------------------------------------------------------------------------
# Fallback classifier — regex/dict rule engine
# ---------------------------------------------------------------------------

# Each entry: (pattern, category, confidence)
# Patterns are matched case-insensitively against "app_name window_title url".
# First match wins.

_RULES: list[tuple[str, str, float]] = [
    # Turkish business applications (highest confidence)
    (r"\blogo\s+tiger\b", "work", 0.99),
    (r"\blogo\b.*\b(fatura|muhasebe|bordro|stok|crm|hr)\b", "work", 0.98),
    (r"\bmikro\s+(gold|erp|business)\b", "work", 0.99),
    (r"\bnetsis\b", "work", 0.99),
    (r"\bparaşüt\b", "work", 0.98),
    (r"\bbordroplus\b", "work", 0.99),
    (r"\be-fatura\b", "work", 0.97),
    (r"\be-arşiv\b", "work", 0.97),
    (r"\beba\b.*\b(portal|login)\b", "work", 0.95),
    (r"\bidefix\b", "personal", 0.80),
    (r"\btrendyol\b", "personal", 0.85),
    (r"\bhepsiburada\b", "personal", 0.85),
    (r"\bn11\b", "personal", 0.80),
    # International productivity / work tools
    (r"\bmicrosoft\s+(excel|word|powerpoint|teams|outlook|sharepoint|onenote|visio|project)\b", "work", 0.97),
    (r"\bexcel\.exe\b", "work", 0.97),
    (r"\bwinword\.exe\b", "work", 0.97),
    (r"\bpowerpnt\.exe\b", "work", 0.97),
    (r"\boutlook\.exe\b", "work", 0.96),
    (r"\bteams\.exe\b", "work", 0.94),
    (r"\bslack\b", "work", 0.90),
    (r"\bzoom\b", "work", 0.88),
    (r"\bgoogle\s+meet\b", "work", 0.90),
    (r"\bwebex\b", "work", 0.90),
    (r"\bvscode\b|visual\s+studio\s+code", "work", 0.97),
    (r"\bjetbrains\b|\bintelij\b|\bpycharm\b|\bgoland\b|\bwebstorm\b|\bridr\b", "work", 0.97),
    (r"\bgithub\b|github\.com", "work", 0.93),
    (r"\bgitlab\b|gitlab\.com", "work", 0.93),
    (r"\bjira\b|atlassian\.net", "work", 0.95),
    (r"\bconfluence\b", "work", 0.95),
    (r"\bnotion\b", "work", 0.85),
    (r"\bfigma\b", "work", 0.93),
    (r"\blinear\b", "work", 0.88),
    (r"\basana\b", "work", 0.90),
    (r"\btrello\b", "work", 0.88),
    (r"\bpostman\b", "work", 0.96),
    (r"\binsomnia\b", "work", 0.95),
    (r"\bdatagrip\b|\bdbaver\b|\btableplus\b", "work", 0.96),
    (r"\bterminal\b|\bcmd\.exe\b|\bpowershell\b|\bwsl\b|\biterm\b|\bkitty\b", "work", 0.88),
    (r"\bdocker\s+desktop\b", "work", 0.93),
    (r"\bkubernetes\b|\bkubectl\b|\bk9s\b|\blens\b", "work", 0.95),
    (r"\baws\s+console\b|console\.aws\.amazon\.com", "work", 0.96),
    (r"\bazure\s+portal\b|portal\.azure\.com", "work", 0.96),
    (r"\bgcp\b|console\.cloud\.google\.com", "work", 0.95),
    (r"\bstackoverflow\b|stackoverflow\.com", "work", 0.92),
    (r"\bmdnweb\b|developer\.mozilla\.org", "work", 0.93),
    (r"\bgraafana\b|\bprometheus\b", "work", 0.92),
    # Distractions — social media, video streaming, gaming
    (r"\byoutube\b|youtube\.com", "distraction", 0.90),
    (r"\bnetflix\b|netflix\.com", "distraction", 0.98),
    (r"\btwitch\b|twitch\.tv", "distraction", 0.97),
    (r"\binstagram\b|instagram\.com", "distraction", 0.95),
    (r"\bfacebook\b|facebook\.com", "distraction", 0.93),
    (r"\btwitter\b|\bx\.com\b", "distraction", 0.90),
    (r"\btiktok\b|tiktok\.com", "distraction", 0.97),
    (r"\breddit\b|reddit\.com", "distraction", 0.87),
    (r"\bdiscord\b", "distraction", 0.82),  # can be work context too
    (r"\bspotify\b", "distraction", 0.80),  # can be background music
    (r"\bsteam\b|\bepic\s+games\b|\borigin\b|\buplay\b|\bgog\b", "distraction", 0.97),
    (r"\bminecraft\b|\bfortnite\b|\bvalheim\b|\bcsgo\b|\bcs2\b|\bdota\b", "distraction", 0.99),
    # Personal — banking, e-commerce, private comms
    (r"\bwhatsapp\b", "personal", 0.75),   # can be used for work
    (r"\btelegram\b", "personal", 0.70),   # can be used for work
    (r"\byahoo\s+mail\b|mail\.yahoo\.com", "personal", 0.85),
    (r"\bgmail\.com\b", "personal", 0.65),  # could be work gmail
    (r"\bamazon\b|amazon\.com|amazon\.com\.tr", "personal", 0.83),
    (r"\bebay\b|ebay\.com", "personal", 0.87),
    (r"\baliexpress\b|aliexpress\.com", "personal", 0.88),
    (r"\bing\s+bank\b|ing\.com\.tr", "personal", 0.92),
    (r"\bgaran\b|garanti\b|garantibbva\b", "personal", 0.92),
    (r"\bisbank\b|isbank\.com\.tr", "personal", 0.92),
    (r"\byapikredisi\b|yapikredisi\.com\.tr", "personal", 0.92),
    (r"\bziraatbank\b|ziraatbank\.com\.tr", "personal", 0.92),
    (r"\bakbank\b|akbank\.com", "personal", 0.92),
    (r"\bqnb\b|qnbfinansbank\.com", "personal", 0.92),
]

_COMPILED_RULES: list[tuple[re.Pattern, str, float]] | None = None


def _get_compiled_rules() -> list[tuple[re.Pattern, str, float]]:
    global _COMPILED_RULES
    if _COMPILED_RULES is None:
        _COMPILED_RULES = [
            (re.compile(pattern, re.IGNORECASE), category, confidence)
            for pattern, category, confidence in _RULES
        ]
    return _COMPILED_RULES


class FallbackClassifier(BaseClassifier):
    """Rule-based classifier used when the LLM model is unavailable.

    Always returns results with backend='fallback'.  Confidence is set to
    0.5 (no-match) or the rule's pre-assigned value.  The confidence
    threshold still applies: below threshold → 'unknown'.
    """

    def __init__(self, confidence_threshold: float = 0.70, model_version: str = "fallback") -> None:
        self._confidence_threshold = confidence_threshold
        self._model_version = model_version

    def classify(self, item: ClassifyItem) -> ClassifyResult:
        from personel_ml.metrics import CLASSIFY_LATENCY, CLASSIFY_TOTAL

        t0 = time.monotonic()
        category, confidence = self._match(item)
        elapsed = time.monotonic() - t0

        CLASSIFY_TOTAL.labels(backend="fallback", category=category).inc()
        CLASSIFY_LATENCY.labels(backend="fallback").observe(elapsed)

        return ClassifyResult(
            category=category,
            confidence=confidence,
            backend="fallback",
            model_version=self._model_version,
        )

    def _match(self, item: ClassifyItem) -> tuple[str, float]:
        """Run regex rules against the concatenated item fields."""
        haystack = " ".join(
            filter(None, [item.app_name, item.window_title, item.url or ""])
        )
        for pattern, category, confidence in _get_compiled_rules():
            if pattern.search(haystack):
                if confidence < self._confidence_threshold:
                    return "unknown", confidence
                return category, confidence  # type: ignore[return-value]
        # No match — return unknown with neutral confidence
        return "unknown", 0.5

    @property
    def backend(self) -> BackendLiteral:
        return "fallback"

    @property
    def model_version(self) -> str:
        return self._model_version

    @property
    def is_loaded(self) -> bool:
        # Fallback is always "loaded" — no model file required
        return True
