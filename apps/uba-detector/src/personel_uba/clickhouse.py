"""
ClickHouse client — read-only stub for Phase 2.6.

The client reads from 5 materialized views for feature extraction and from
the uba_scores table for API responses.

Phase 2.6 status:
  - Connection setup: IMPLEMENTED
  - Read from materialized views: TODO (real SQL in materialized_views/)
  - Write to uba_scores: TODO (DBA must provision write role)

All methods that would hit ClickHouse are clearly marked TODO so that
test code can inject synthetic DataFrames via the dependency-injection
pattern used in routes.py.
"""

from __future__ import annotations

import contextlib
from datetime import datetime, timezone
from typing import TYPE_CHECKING
from uuid import UUID

import pandas as pd

if TYPE_CHECKING:
    import clickhouse_connect


class ClickHouseClient:
    """
    Read-only ClickHouse client for UBA materialized view queries.

    Dependency-injected into route handlers so tests can substitute a mock.
    """

    def __init__(
        self,
        host: str,
        port: int,
        database: str,
        username: str,
        password: str,
        secure: bool = False,
    ) -> None:
        self._host = host
        self._port = port
        self._database = database
        self._username = username
        self._password = password
        self._secure = secure
        self._client: "clickhouse_connect.driver.Client | None" = None

    def connect(self) -> None:
        """Open the ClickHouse connection."""
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

    def close(self) -> None:
        if self._client is not None:
            with contextlib.suppress(Exception):
                self._client.close()
            self._client = None

    def ping(self) -> bool:
        """Return True if ClickHouse is reachable."""
        if self._client is None:
            return False
        try:
            self._client.ping()
            return True
        except Exception:  # noqa: BLE001
            return False

    # -----------------------------------------------------------------------
    # Materialized view reads (TODO: implement real SQL)
    # -----------------------------------------------------------------------

    def get_user_events(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Query events_raw for a user in the given window.

        Returns columns: occurred_at, event_type, app_name, process_name,
        bytes_written, clipboard_bytes, bytes_out, remote_host.

        Returns empty DataFrame until implemented.
        """
        # TODO: implement real ClickHouse query
        # Example SQL:
        # SELECT occurred_at, event_type,
        #        JSONExtractString(payload, 'app_name') AS app_name,
        #        JSONExtractString(payload, 'process_name') AS process_name,
        #        JSONExtractUInt(payload, 'bytes_written') AS bytes_written,
        #        JSONExtractUInt(payload, 'clipboard_bytes') AS clipboard_bytes,
        #        JSONExtractUInt(payload, 'bytes_out') AS bytes_out,
        #        JSONExtractString(payload, 'remote_host') AS remote_host
        # FROM events_raw
        # WHERE tenant_id = {tenant_id}
        #   AND user_sid = {user_sid}
        #   AND occurred_at BETWEEN {start} AND {end}
        return pd.DataFrame()

    def get_hourly_activity(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Read from user_hourly_activity materialized view.

        Returns columns: hour_bucket, event_count, active_apps.
        """
        return pd.DataFrame()

    def get_app_diversity(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Read from user_app_diversity materialized view.

        Returns columns: app_name, event_count.
        """
        return pd.DataFrame()

    def get_off_hours_ratio(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Read from user_off_hours_ratio materialized view.

        Returns columns: off_hours_count, total_count, ratio.
        """
        return pd.DataFrame()

    def get_data_egress(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Read from user_data_egress materialized view.

        Returns columns: total_bytes_written, total_clipboard_bytes, total_bytes_out.
        """
        return pd.DataFrame()

    def get_policy_violations(
        self,
        tenant_id: str,
        user_sid: str,
        start: datetime,
        end: datetime,
    ) -> pd.DataFrame:
        """
        TODO Phase 2.7: Read from user_policy_violations materialized view.

        Returns columns: violation_type, count.
        """
        return pd.DataFrame()

    # -----------------------------------------------------------------------
    # uba_scores table
    # -----------------------------------------------------------------------

    def get_latest_score(
        self,
        tenant_id: str,
        user_id: str,
        window_days: int = 7,
    ) -> dict | None:
        """
        TODO Phase 2.7: Read latest uba_scores row for user.

        Requires DBA to provision personel_uba_writer role and create uba_scores table.
        Returns None until implemented.
        """
        return None

    def get_score_timeline(
        self,
        tenant_id: str,
        user_id: str,
        days: int = 30,
    ) -> list[dict]:
        """
        TODO Phase 2.7: Read score history from uba_scores for user.

        Returns empty list until implemented.
        """
        return []

    def get_top_anomalous_users(
        self,
        tenant_id: str,
        window_days: int = 7,
        limit: int = 20,
    ) -> list[dict]:
        """
        TODO Phase 2.7: Query uba_scores for top anomalous users.

        Returns empty list until implemented.
        """
        return []

    def write_score(self, score_row: dict) -> None:
        """
        TODO Phase 2.7: Insert a score row into uba_scores table.

        Blocked on DBA provisioning personel_uba_writer role with INSERT
        on uba_scores. Until then, scores are held in memory only.
        """
        # TODO: implement ClickHouse INSERT
        # Required columns: tenant_id, user_id, anomaly_score, risk_tier,
        # contributing_features (JSON), window_days, computed_at
        pass  # noqa: PIE790


def get_clickhouse_client(
    host: str,
    port: int,
    database: str,
    username: str,
    password: str,
    secure: bool = False,
) -> ClickHouseClient:
    """Factory function for creating a connected ClickHouse client."""
    client = ClickHouseClient(
        host=host,
        port=port,
        database=database,
        username=username,
        password=password,
        secure=secure,
    )
    client.connect()
    return client
