"""
Prometheus metrics for the UBA Detector service.

Exposes on /metrics endpoint (port UBA_METRICS_PORT by default).
"""

from __future__ import annotations

from prometheus_client import Counter, Gauge, Histogram, Info

# --- Service info ---
SERVICE_INFO = Info("personel_uba_service", "UBA Detector service metadata")
SERVICE_INFO.info({"version": "0.1.0", "phase": "2.6"})

# --- Scoring pipeline ---
SCORES_COMPUTED = Counter(
    "personel_uba_scores_computed_total",
    "Total anomaly scores computed",
    labelnames=["tenant_id", "risk_tier"],
)

SCORE_COMPUTATION_DURATION = Histogram(
    "personel_uba_score_computation_seconds",
    "Time taken to compute a single user anomaly score",
    buckets=[0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0],
)

BATCH_COMPUTATION_DURATION = Histogram(
    "personel_uba_batch_computation_seconds",
    "Time taken to compute scores for an entire tenant batch",
    buckets=[1.0, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0],
)

ACTIVE_USERS_GAUGE = Gauge(
    "personel_uba_active_users",
    "Number of users with a score computed in the current window",
    labelnames=["tenant_id"],
)

INVESTIGATE_TIER_GAUGE = Gauge(
    "personel_uba_investigate_tier_users",
    "Number of users currently in the 'investigate' tier",
    labelnames=["tenant_id"],
)

HIGH_SCORE_NOTIFICATIONS = Counter(
    "personel_uba_high_score_notifications_total",
    "Total advisory DPO notifications sent for scores > 0.9",
    labelnames=["tenant_id"],
)

# --- Feature extraction ---
FEATURE_EXTRACTION_DURATION = Histogram(
    "personel_uba_feature_extraction_seconds",
    "Time taken to extract features for a single user",
    buckets=[0.005, 0.01, 0.05, 0.1, 0.5, 1.0],
)

CLICKHOUSE_QUERY_DURATION = Histogram(
    "personel_uba_clickhouse_query_seconds",
    "ClickHouse query duration",
    labelnames=["view"],
    buckets=[0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0],
)

CLICKHOUSE_QUERY_ERRORS = Counter(
    "personel_uba_clickhouse_query_errors_total",
    "ClickHouse query errors",
    labelnames=["view"],
)

# --- API ---
API_REQUEST_DURATION = Histogram(
    "personel_uba_api_request_seconds",
    "API endpoint request duration",
    labelnames=["method", "endpoint", "status"],
    buckets=[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0],
)

API_REQUESTS_TOTAL = Counter(
    "personel_uba_api_requests_total",
    "Total API requests",
    labelnames=["method", "endpoint", "status"],
)

# --- Model ---
MODEL_FIT_DURATION = Histogram(
    "personel_uba_model_fit_seconds",
    "Time to fit IsolationForest on training data",
    buckets=[0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0],
)

MODEL_LAST_FIT_TIMESTAMP = Gauge(
    "personel_uba_model_last_fit_timestamp_seconds",
    "Unix timestamp of the last model fit",
    labelnames=["tenant_id"],
)
