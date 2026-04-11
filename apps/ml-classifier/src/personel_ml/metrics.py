"""
Prometheus metrics for the ml-classifier service.

Exported via GET /metrics (Prometheus text format, handled by routes.py).

Metrics:
  personel_ml_classify_total          counter   classifications by backend + category
  personel_ml_classify_latency_seconds histogram  per-request latency by backend
  personel_ml_classify_errors_total   counter   error count by backend
  personel_ml_model_load_time_seconds gauge     time taken to load the GGUF model
  personel_ml_batch_size              histogram  items per batch request
"""

from __future__ import annotations

from prometheus_client import Counter, Gauge, Histogram

# ---------------------------------------------------------------------------
# Labels
# ---------------------------------------------------------------------------
# backend: "llama" | "fallback"
# category: "work" | "personal" | "distraction" | "unknown"
# status: "success" | "error"

CLASSIFY_TOTAL = Counter(
    "personel_ml_classify_total",
    "Total number of individual items classified",
    labelnames=["backend", "category"],
)

CLASSIFY_LATENCY = Histogram(
    "personel_ml_classify_latency_seconds",
    "Per-item classification wall-clock time in seconds",
    labelnames=["backend"],
    buckets=(0.005, 0.010, 0.025, 0.050, 0.075, 0.100, 0.150, 0.250, 0.500, 1.0, 2.5, 5.0),
)

CLASSIFY_ERRORS = Counter(
    "personel_ml_classify_errors_total",
    "Total classification errors (model inference failures, parse errors)",
    labelnames=["backend"],
)

MODEL_LOAD_TIME = Gauge(
    "personel_ml_model_load_time_seconds",
    "Time in seconds taken to load the GGUF model at startup",
)

BATCH_SIZE = Histogram(
    "personel_ml_batch_size",
    "Number of items per /v1/classify/batch request",
    buckets=(1, 2, 4, 8, 16, 32, 64, 128),
)

HTTP_REQUEST_TOTAL = Counter(
    "personel_ml_http_requests_total",
    "Total HTTP requests by method, path, and status code",
    labelnames=["method", "path", "status_code"],
)

HTTP_REQUEST_LATENCY = Histogram(
    "personel_ml_http_request_latency_seconds",
    "HTTP request latency by method and path",
    labelnames=["method", "path"],
    buckets=(0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.0, 2.5),
)
