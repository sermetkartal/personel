"""
Real ClickHouse feature extraction — Faz 8 #84.

This module wires the pandas feature computations in ``features.py`` to live
ClickHouse queries over the ``events_raw`` table. It is deliberately a
separate file so the pure-pandas helpers remain unit-testable without any
database dependency.

KVKK m.11/g reminder: features computed here feed the isolation forest which
produces advisory-only anomaly scores. No query in this module is permitted
to make a human-impact decision on its own.

All queries are **parameterized** via clickhouse-connect's ``parameters``
kwarg (positional or named) — no string interpolation of tenant_id, user_id
or timestamps anywhere. SQL injection is structurally impossible.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from typing import TYPE_CHECKING, Any

from personel_uba.features import BUSINESS_HOUR_END, BUSINESS_HOUR_START

if TYPE_CHECKING:
    import clickhouse_connect

logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# CHFeatureVector dataclass — what the extractor returns
# ---------------------------------------------------------------------------


@dataclass
class CHFeatureVector:
    """Raw (un-normalised) UBA feature vector for a single user + window.

    The isolation forest consumes these 7 fields in the order declared on
    ``to_list()`` — do NOT reorder without retraining.
    """

    off_hours_ratio: float = 0.0
    app_diversity: float = 0.0
    data_egress_mb: float = 0.0
    screenshot_rate: float = 0.0
    file_access_rate: float = 0.0
    policy_violation_count: float = 0.0
    new_host_ratio: float = 0.0

    # Metadata (not a feature; for explainability only)
    tenant_id: str = ""
    user_id: str = ""
    window_hours: int = 24
    computed_at: datetime = field(default_factory=lambda: datetime.now(tz=timezone.utc))

    def to_list(self) -> list[float]:
        """Return the 7 features in the canonical order for the model."""
        return [
            self.off_hours_ratio,
            self.app_diversity,
            self.data_egress_mb,
            self.screenshot_rate,
            self.file_access_rate,
            self.policy_violation_count,
            self.new_host_ratio,
        ]

    def to_dict(self) -> dict[str, float]:
        return {
            "off_hours_ratio": self.off_hours_ratio,
            "app_diversity": self.app_diversity,
            "data_egress_mb": self.data_egress_mb,
            "screenshot_rate": self.screenshot_rate,
            "file_access_rate": self.file_access_rate,
            "policy_violation_count": self.policy_violation_count,
            "new_host_ratio": self.new_host_ratio,
        }

    def to_schema_vector(self, user_uuid: Any) -> Any:
        """Convert to the pydantic ``schemas.FeatureVector`` used by scoring.py.

        The canonical ``schemas.FeatureVector`` retains the original Phase 2.6
        field names (off_hours_activity, data_egress_volume). This adapter
        lets the new CH-backed extract path feed the existing scoring
        pipeline without touching ``scoring.py``.
        """
        from personel_uba.schemas import FeatureVector as SchemaFV  # noqa: PLC0415

        return SchemaFV(
            user_id=user_uuid,
            off_hours_activity=self.off_hours_ratio,
            app_diversity=self.app_diversity,
            data_egress_volume=self.data_egress_mb,
            screenshot_rate=self.screenshot_rate,
            file_access_rate=self.file_access_rate,
            policy_violation_count=self.policy_violation_count,
            new_host_ratio=self.new_host_ratio,
            window_days=max(1, self.window_hours // 24),
            computed_at=self.computed_at,
        )


# ---------------------------------------------------------------------------
# ClickHouseFeatureExtractor
# ---------------------------------------------------------------------------


class ClickHouseFeatureExtractor:
    """Run parameterised queries against events_raw to build a CHFeatureVector.

    The client is lazily instantiated on first ``extract()`` call so that
    unit tests can construct the extractor without a live CH connection.
    All queries accept **named parameters**; the clickhouse-connect driver
    quotes/escapes them on the wire.

    Expected schema (``events_raw``):
        tenant_id   UUID
        endpoint_id UUID
        user_sid    String        -- user identifier (SID or HRIS employee_id)
        event_type  LowCardinality(String)
        occurred_at DateTime64(3)
        payload     String         -- JSON blob varying per event_type
    """

    # Turkish business hours (UTC+3). We compare against UTC hours for the
    # off-hours SQL filter, so the SQL window is [05:00 UTC, 15:00 UTC) —
    # equivalent to [08:00, 18:00) Istanbul local time, Mon–Fri.
    _BUSINESS_HOUR_UTC_START = BUSINESS_HOUR_START - 3   # 08 - 3 = 5 UTC
    _BUSINESS_HOUR_UTC_END = BUSINESS_HOUR_END - 3       # 18 - 3 = 15 UTC

    def __init__(
        self,
        host: str,
        port: int,
        database: str,
        username: str,
        password: str,
        secure: bool = False,
        client: Any | None = None,  # for dependency injection in tests
    ) -> None:
        self._host = host
        self._port = port
        self._database = database
        self._username = username
        self._password = password
        self._secure = secure
        self._client = client

    def _get_client(self) -> Any:
        if self._client is not None:
            return self._client
        import clickhouse_connect  # noqa: PLC0415

        self._client = clickhouse_connect.get_client(
            host=self._host,
            port=self._port,
            database=self._database,
            username=self._username,
            password=self._password,
            secure=self._secure,
            connect_timeout=10,
            send_receive_timeout=60,
        )
        return self._client

    def close(self) -> None:
        if self._client is not None:
            try:
                self._client.close()
            except Exception:  # noqa: BLE001
                pass
            self._client = None

    # -----------------------------------------------------------------------
    # Public entry point
    # -----------------------------------------------------------------------

    def extract(
        self,
        tenant_id: str,
        user_id: str,
        window_hours: int = 24,
    ) -> CHFeatureVector:
        """Compute all 7 features for a single user over the given window.

        Parameters are bound positionally via clickhouse-connect; none of
        ``tenant_id``, ``user_id`` or the timestamps are ever interpolated
        into the SQL string.
        """
        if window_hours <= 0:
            raise ValueError("window_hours must be > 0")

        end = datetime.now(tz=timezone.utc)
        start = end - timedelta(hours=window_hours)

        # new_host_ratio baseline window: prior 7 days before `start`
        baseline_start = start - timedelta(days=7)

        client = self._get_client()

        params_base: dict[str, Any] = {
            "tenant_id": tenant_id,
            "user_sid": user_id,
            "start_ts": start,
            "end_ts": end,
            "biz_start": self._BUSINESS_HOUR_UTC_START,
            "biz_end": self._BUSINESS_HOUR_UTC_END,
        }

        off_hours_ratio = self._run_scalar(
            client, _Q_OFF_HOURS, params_base, default=0.0
        )
        app_diversity = self._run_scalar(
            client, _Q_APP_DIVERSITY, params_base, default=0.0
        )
        data_egress_bytes = self._run_scalar(
            client, _Q_DATA_EGRESS, params_base, default=0.0
        )
        screenshot_count = self._run_scalar(
            client, _Q_SCREENSHOT_COUNT, params_base, default=0.0
        )
        file_access_count = self._run_scalar(
            client, _Q_FILE_ACCESS_COUNT, params_base, default=0.0
        )
        policy_violation_count = self._run_scalar(
            client, _Q_POLICY_VIOLATIONS, params_base, default=0.0
        )

        # new_host_ratio needs TWO queries: current window + baseline
        current_hosts = self._run_set(client, _Q_DISTINCT_HOSTS, params_base)
        baseline_params = dict(params_base)
        baseline_params["start_ts"] = baseline_start
        baseline_params["end_ts"] = start
        baseline_hosts = self._run_set(client, _Q_DISTINCT_HOSTS, baseline_params)

        if current_hosts:
            new_hosts = current_hosts - baseline_hosts
            new_host_ratio = len(new_hosts) / len(current_hosts)
        else:
            new_host_ratio = 0.0

        # Convert raw byte egress to MB for consistency with the model's
        # training feature scale.
        data_egress_mb = float(data_egress_bytes) / (1024.0 * 1024.0)

        # Rates are per-minute (for screenshot + file_access)
        window_minutes = max(1.0, window_hours * 60.0)
        screenshot_rate = float(screenshot_count) / window_minutes
        file_access_rate = float(file_access_count) / window_minutes

        return CHFeatureVector(
            off_hours_ratio=float(off_hours_ratio),
            app_diversity=float(app_diversity),
            data_egress_mb=data_egress_mb,
            screenshot_rate=screenshot_rate,
            file_access_rate=file_access_rate,
            policy_violation_count=float(policy_violation_count),
            new_host_ratio=float(new_host_ratio),
            tenant_id=tenant_id,
            user_id=user_id,
            window_hours=window_hours,
            computed_at=end,
        )

    # -----------------------------------------------------------------------
    # Query helpers
    # -----------------------------------------------------------------------

    def _run_scalar(
        self,
        client: Any,
        query: str,
        params: dict[str, Any],
        default: float,
    ) -> float:
        """Run a scalar-returning query; tolerate missing tables."""
        try:
            result = client.query(query, parameters=params)
            rows = getattr(result, "result_rows", None) or []
            if not rows:
                return default
            first = rows[0]
            if not first:
                return default
            value = first[0]
            if value is None:
                return default
            return float(value)
        except Exception as exc:  # noqa: BLE001
            if _is_missing_table(exc):
                logger.info(
                    "uba.clickhouse.table_missing",
                    extra={"error": str(exc)},
                )
                return default
            logger.warning(
                "uba.clickhouse.query_failed",
                extra={"error": str(exc), "query": query[:80]},
            )
            return default

    def _run_set(
        self,
        client: Any,
        query: str,
        params: dict[str, Any],
    ) -> set[str]:
        """Run a multi-row query returning a set of string values."""
        try:
            result = client.query(query, parameters=params)
            rows = getattr(result, "result_rows", None) or []
            out: set[str] = set()
            for row in rows:
                if row and row[0] is not None:
                    out.add(str(row[0]))
            return out
        except Exception as exc:  # noqa: BLE001
            if _is_missing_table(exc):
                return set()
            logger.warning(
                "uba.clickhouse.query_failed",
                extra={"error": str(exc), "query": query[:80]},
            )
            return set()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _is_missing_table(exc: Exception) -> bool:
    msg = str(exc).lower()
    return "unknown_table" in msg or "code: 60" in msg or "code: 81" in msg


# ---------------------------------------------------------------------------
# Parameterised SQL templates
# ---------------------------------------------------------------------------
#
# NOTE: All ``{param_name:Type}`` placeholders are clickhouse-connect
# server-side parameter bindings — NOT Python f-string interpolation.
# The ``parameters=`` kwarg on ``client.query`` supplies the values.

# 1. off_hours_ratio — fraction of events outside business hours
_Q_OFF_HOURS = """
SELECT
    countIf(
        toDayOfWeek(occurred_at) IN (6, 7)
        OR toHour(occurred_at) < {biz_start:UInt8}
        OR toHour(occurred_at) >= {biz_end:UInt8}
    ) / nullIf(count(), 0) AS off_hours_ratio
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
"""

# 2. app_diversity — distinct app_name count
_Q_APP_DIVERSITY = """
SELECT uniqExact(JSONExtractString(payload, 'app_name')) AS app_count
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND JSONExtractString(payload, 'app_name') != ''
"""

# 3. data_egress — bytes via USB + cloud upload + outbound net
_Q_DATA_EGRESS = """
SELECT sum(
    toUInt64OrZero(JSONExtractString(payload, 'bytes_written'))
    + toUInt64OrZero(JSONExtractString(payload, 'clipboard_bytes'))
    + toUInt64OrZero(JSONExtractString(payload, 'bytes_out'))
) AS total_bytes
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND event_type IN (
    'usb_write', 'file_write', 'cloud_storage_upload', 'clipboard_copy',
    'network_flow_out', 'network_flow'
  )
"""

# 4. screenshot_count — raw count; rate computed in Python
_Q_SCREENSHOT_COUNT = """
SELECT count() AS screenshot_count
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND event_type = 'screen_capture'
"""

# 5. file_access_count — filesystem events
_Q_FILE_ACCESS_COUNT = """
SELECT count() AS file_events
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND event_type IN ('file_read', 'file_open', 'file_write', 'file_rename')
"""

# 6. policy violations — blocked_* + dlp_match events
_Q_POLICY_VIOLATIONS = """
SELECT count() AS violations
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND event_type IN (
    'app_blocked', 'web_blocked', 'dlp_match', 'blocked_upload',
    'blocked_transfer', 'sensitive_file_blocked'
  )
"""

# Distinct remote hosts for new_host_ratio — run twice with different windows
_Q_DISTINCT_HOSTS = """
SELECT DISTINCT JSONExtractString(payload, 'remote_host') AS host
FROM events_raw
WHERE tenant_id = {tenant_id:String}
  AND user_sid = {user_sid:String}
  AND occurred_at >= {start_ts:DateTime64(3)}
  AND occurred_at <  {end_ts:DateTime64(3)}
  AND JSONExtractString(payload, 'remote_host') != ''
"""
