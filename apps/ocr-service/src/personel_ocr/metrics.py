"""
Prometheus metrics for the OCR service.

Exported via GET /metrics (Prometheus text format).

Metrics:
  personel_ocr_extractions_total         counter   total extraction calls by engine, status
  personel_ocr_extraction_latency_seconds histogram  per-request latency by engine
  personel_ocr_redactions_total          counter   PII items redacted by kind
  personel_ocr_engine_available          gauge     1 if engine is available, 0 otherwise
  personel_ocr_word_count                histogram  words extracted per image
  personel_ocr_http_requests_total       counter   HTTP requests by method, path, status_code
  personel_ocr_http_request_latency_seconds histogram HTTP latency by method, path
  personel_ocr_batch_size                histogram  items per /v1/extract/batch request
"""

from __future__ import annotations

from prometheus_client import Counter, Gauge, Histogram

# ---------------------------------------------------------------------------
# Extraction metrics
# ---------------------------------------------------------------------------

EXTRACTIONS_TOTAL = Counter(
    "personel_ocr_extractions_total",
    "Total OCR extraction calls",
    labelnames=["engine", "status"],  # status: success | error
)

EXTRACTION_LATENCY = Histogram(
    "personel_ocr_extraction_latency_seconds",
    "End-to-end OCR pipeline latency in seconds",
    labelnames=["engine"],
    buckets=(0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1.0, 1.5, 2.0, 3.0, 5.0, 10.0),
)

WORD_COUNT = Histogram(
    "personel_ocr_word_count",
    "Number of words extracted per image",
    buckets=(0, 10, 25, 50, 100, 200, 500, 1000, 2000, 5000),
)

# ---------------------------------------------------------------------------
# Redaction metrics
# ---------------------------------------------------------------------------

REDACTIONS_TOTAL = Counter(
    "personel_ocr_redactions_total",
    "Total PII items redacted from OCR output",
    labelnames=["kind"],  # kind: tckn | iban | credit_card | phone | email
)

# ---------------------------------------------------------------------------
# Engine status
# ---------------------------------------------------------------------------

ENGINE_AVAILABLE = Gauge(
    "personel_ocr_engine_available",
    "1 if the named OCR engine is available, 0 otherwise",
    labelnames=["engine"],
)

# ---------------------------------------------------------------------------
# HTTP metrics
# ---------------------------------------------------------------------------

HTTP_REQUEST_TOTAL = Counter(
    "personel_ocr_http_requests_total",
    "Total HTTP requests by method, path, and status code",
    labelnames=["method", "path", "status_code"],
)

HTTP_REQUEST_LATENCY = Histogram(
    "personel_ocr_http_request_latency_seconds",
    "HTTP request latency by method and path",
    labelnames=["method", "path"],
    buckets=(0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.0, 2.5, 5.0),
)

BATCH_SIZE = Histogram(
    "personel_ocr_batch_size",
    "Number of items per /v1/extract/batch request",
    buckets=(1, 2, 4, 8, 16, 32, 64),
)
