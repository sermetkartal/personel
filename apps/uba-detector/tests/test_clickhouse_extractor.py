"""
Unit tests for ClickHouseFeatureExtractor (Faz 8 #84).

These tests use a fake ClickHouse client that returns canned rows for each
query. No network or live ClickHouse instance required.

Coverage goals:
- Happy path: all 7 features computed from canned responses
- Empty result handling (zero rows → defaults)
- Missing-table tolerance (UNKNOWN_TABLE / code: 60)
- Parameterised query shape (no string interpolation of tenant_id)
- new_host_ratio logic (current vs baseline overlap)
- window_hours=0 raises ValueError
- KVKK: `to_schema_vector` preserves user_id + computed_at
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any
from uuid import UUID

import pytest

from personel_uba.clickhouse_extractor import (
    CHFeatureVector,
    ClickHouseFeatureExtractor,
    _is_missing_table,
)


# ---------------------------------------------------------------------------
# Fake ClickHouse client
# ---------------------------------------------------------------------------


class _FakeQueryResult:
    def __init__(self, rows: list[tuple]) -> None:
        self.result_rows = rows


class FakeClient:
    """
    Minimal fake matching the subset of clickhouse-connect.Client used by
    ClickHouseFeatureExtractor.

    Responses are keyed by a substring match against the SQL query text,
    so tests can set specific rows per feature.
    """

    def __init__(self) -> None:
        self.responses: dict[str, list[tuple]] = {}
        self.errors: dict[str, Exception] = {}
        self.calls: list[tuple[str, dict[str, Any]]] = []

    def set_response(self, marker: str, rows: list[tuple]) -> None:
        self.responses[marker] = rows

    def set_error(self, marker: str, exc: Exception) -> None:
        self.errors[marker] = exc

    def query(self, sql: str, parameters: dict[str, Any] | None = None) -> _FakeQueryResult:
        self.calls.append((sql, parameters or {}))

        for marker, err in self.errors.items():
            if marker in sql:
                raise err

        for marker, rows in self.responses.items():
            if marker in sql:
                return _FakeQueryResult(rows)
        return _FakeQueryResult([])

    def close(self) -> None:
        pass


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_extractor(client: FakeClient) -> ClickHouseFeatureExtractor:
    return ClickHouseFeatureExtractor(
        host="fake",
        port=9000,
        database="personel",
        username="u",
        password="p",
        client=client,
    )


TENANT_ID = "be459dac-1a79-4054-b6e1-fa934a927315"
USER_ID = "S-1-5-21-123"


# ---------------------------------------------------------------------------
# Happy path
# ---------------------------------------------------------------------------


class TestExtractHappyPath:
    def test_all_features_populated(self) -> None:
        client = FakeClient()
        # off_hours_ratio — 30% off hours
        client.set_response("off_hours_ratio", [(0.30,)])
        # app_diversity — 7 distinct apps
        client.set_response("uniqExact", [(7,)])
        # data_egress — 10 MB = 10 * 1024 * 1024
        client.set_response("total_bytes", [(10 * 1024 * 1024,)])
        # screenshot_count — 12 over 24h window → 12 / (24*60) = 0.00833 per min
        client.set_response("screenshot_count", [(12,)])
        # file_access_count — 240 events → 240 / 1440 ≈ 0.1667 per min
        client.set_response("file_events", [(240,)])
        # policy_violations — 3
        client.set_response("violations", [(3,)])
        # distinct hosts — current: h1, h2, h3; baseline: h1, h2
        # We share the same marker for both windows, so the extractor will
        # see three hosts twice. Override per-call by using a side-effect list.
        # Simpler: re-use the same rows; new_host_ratio will compute 0.0 since
        # current ⊆ baseline. Tested separately below.
        client.set_response(
            "DISTINCT JSONExtractString(payload, 'remote_host')",
            [("h1",), ("h2",), ("h3",)],
        )

        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)

        assert isinstance(fv, CHFeatureVector)
        assert fv.off_hours_ratio == pytest.approx(0.30)
        assert fv.app_diversity == 7.0
        assert fv.data_egress_mb == pytest.approx(10.0)
        assert fv.screenshot_rate == pytest.approx(12 / 1440, rel=1e-3)
        assert fv.file_access_rate == pytest.approx(240 / 1440, rel=1e-3)
        assert fv.policy_violation_count == 3.0
        # All hosts seen in both windows → new_host_ratio = 0
        assert fv.new_host_ratio == 0.0
        assert fv.tenant_id == TENANT_ID
        assert fv.user_id == USER_ID
        assert fv.window_hours == 24

    def test_to_list_order(self) -> None:
        fv = CHFeatureVector(
            off_hours_ratio=0.1,
            app_diversity=2.0,
            data_egress_mb=3.0,
            screenshot_rate=0.4,
            file_access_rate=0.5,
            policy_violation_count=6.0,
            new_host_ratio=0.7,
        )
        assert fv.to_list() == [0.1, 2.0, 3.0, 0.4, 0.5, 6.0, 0.7]

    def test_to_schema_vector_preserves_user_id(self) -> None:
        fv = CHFeatureVector(
            off_hours_ratio=0.1,
            app_diversity=2.0,
            data_egress_mb=3.0,
            screenshot_rate=0.4,
            file_access_rate=0.5,
            policy_violation_count=6.0,
            new_host_ratio=0.7,
            window_hours=48,
            computed_at=datetime(2026, 4, 13, 12, 0, 0, tzinfo=timezone.utc),
        )
        uid = UUID("11111111-2222-3333-4444-555555555555")
        schema_fv = fv.to_schema_vector(uid)
        assert schema_fv.user_id == uid
        assert schema_fv.off_hours_activity == 0.1
        assert schema_fv.data_egress_volume == 3.0
        # 48h → 2 days
        assert schema_fv.window_days == 2
        assert schema_fv.computed_at.tzinfo is not None


# ---------------------------------------------------------------------------
# Empty / missing table handling
# ---------------------------------------------------------------------------


class TestDefensiveHandling:
    def test_empty_results_produce_zero_features(self) -> None:
        client = FakeClient()  # no responses set → all queries return []
        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)

        assert fv.off_hours_ratio == 0.0
        assert fv.app_diversity == 0.0
        assert fv.data_egress_mb == 0.0
        assert fv.screenshot_rate == 0.0
        assert fv.file_access_rate == 0.0
        assert fv.policy_violation_count == 0.0
        assert fv.new_host_ratio == 0.0

    def test_missing_table_is_tolerated(self) -> None:
        client = FakeClient()
        client.set_error(
            "off_hours_ratio",
            RuntimeError("Code: 60. DB::Exception: UNKNOWN_TABLE: events_raw"),
        )
        client.set_response("uniqExact", [(5,)])

        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)
        # off_hours defaults to 0.0; other features still work
        assert fv.off_hours_ratio == 0.0
        assert fv.app_diversity == 5.0

    def test_is_missing_table_detects_variants(self) -> None:
        assert _is_missing_table(RuntimeError("Code: 60. Unknown_Table"))
        assert _is_missing_table(RuntimeError("code: 81. DB::Exception"))
        assert _is_missing_table(RuntimeError("UNKNOWN_TABLE"))
        assert not _is_missing_table(RuntimeError("connection refused"))

    def test_window_hours_zero_raises(self) -> None:
        extractor = _make_extractor(FakeClient())
        with pytest.raises(ValueError):
            extractor.extract(TENANT_ID, USER_ID, window_hours=0)

    def test_window_hours_negative_raises(self) -> None:
        extractor = _make_extractor(FakeClient())
        with pytest.raises(ValueError):
            extractor.extract(TENANT_ID, USER_ID, window_hours=-5)


# ---------------------------------------------------------------------------
# Parameter binding (SQL injection guard)
# ---------------------------------------------------------------------------


class TestParameterBinding:
    def test_tenant_id_is_never_interpolated(self) -> None:
        client = FakeClient()
        extractor = _make_extractor(client)
        # This tenant_id contains a SQL-injection-looking sequence. If we ever
        # interpolate it into the query text, the fake will see it in sql;
        # with parameter binding it should appear ONLY in the parameters dict.
        evil = "'; DROP TABLE events_raw; --"
        extractor.extract(evil, USER_ID, window_hours=24)

        assert client.calls, "no queries issued"
        for sql, params in client.calls:
            assert evil not in sql, f"SQL injection risk: {sql!r}"
            assert params.get("tenant_id") == evil

    def test_user_id_goes_through_parameters(self) -> None:
        client = FakeClient()
        extractor = _make_extractor(client)
        extractor.extract(TENANT_ID, "S-1-5-21-XYZ", window_hours=12)

        # Every call binds user_sid
        for _, params in client.calls:
            assert params.get("user_sid") == "S-1-5-21-XYZ"
            assert "tenant_id" in params
            assert "start_ts" in params
            assert "end_ts" in params


# ---------------------------------------------------------------------------
# new_host_ratio — current vs baseline comparison
# ---------------------------------------------------------------------------


class TestNewHostRatio:
    def test_all_new_hosts(self) -> None:
        """When baseline is empty, ratio = 1.0."""
        client = FakeClient()
        # Provide distinct hosts via two sequential responses: first call is
        # the current window, second is baseline. We use a counter.
        call_count = {"n": 0}
        original_query = client.query

        def staged_query(sql: str, parameters: dict[str, Any] | None = None) -> _FakeQueryResult:
            if "DISTINCT JSONExtractString(payload, 'remote_host')" in sql:
                call_count["n"] += 1
                if call_count["n"] == 1:
                    return _FakeQueryResult([("h1",), ("h2",), ("h3",)])
                else:
                    return _FakeQueryResult([])  # empty baseline
            return original_query(sql, parameters)

        client.query = staged_query  # type: ignore[method-assign]

        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)
        assert fv.new_host_ratio == 1.0

    def test_partial_overlap(self) -> None:
        """Current {h1,h2,h3}, baseline {h1} → ratio = 2/3."""
        client = FakeClient()
        call_count = {"n": 0}
        original_query = client.query

        def staged_query(sql: str, parameters: dict[str, Any] | None = None) -> _FakeQueryResult:
            if "DISTINCT JSONExtractString(payload, 'remote_host')" in sql:
                call_count["n"] += 1
                if call_count["n"] == 1:
                    return _FakeQueryResult([("h1",), ("h2",), ("h3",)])
                else:
                    return _FakeQueryResult([("h1",)])
            return original_query(sql, parameters)

        client.query = staged_query  # type: ignore[method-assign]

        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)
        assert fv.new_host_ratio == pytest.approx(2 / 3)

    def test_full_overlap(self) -> None:
        """Current ⊆ baseline → ratio = 0."""
        client = FakeClient()
        client.set_response(
            "DISTINCT JSONExtractString(payload, 'remote_host')",
            [("h1",), ("h2",)],
        )
        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)
        assert fv.new_host_ratio == 0.0

    def test_no_current_hosts(self) -> None:
        """Empty current window → ratio = 0 (no hosts to be anomalous)."""
        client = FakeClient()
        extractor = _make_extractor(client)
        fv = extractor.extract(TENANT_ID, USER_ID, window_hours=24)
        assert fv.new_host_ratio == 0.0
