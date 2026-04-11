"""
Configuration — pydantic-settings based, reads from env vars.

All settings are required or have safe defaults. Secrets (passwords, DSNs
with credentials) must be provided via environment; never hardcoded.
"""

from __future__ import annotations

from pydantic import Field, SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="UBA_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    # --- Service ---
    port: int = Field(default=8080, description="FastAPI HTTP port")
    metrics_port: int = Field(default=9090, description="Prometheus metrics port")
    log_level: str = Field(default="info", description="Logging level")
    tenant_id: str = Field(default="", description="Tenant UUID for multi-tenant isolation")

    # --- ClickHouse (read-only) ---
    clickhouse_host: str = Field(default="clickhouse", description="ClickHouse host")
    clickhouse_port: int = Field(default=8123, description="ClickHouse HTTP port")
    clickhouse_database: str = Field(default="personel", description="ClickHouse database")
    clickhouse_user: str = Field(default="personel_uba_ro", description="ClickHouse read-only user")
    clickhouse_password: SecretStr = Field(
        default=SecretStr(""), description="ClickHouse password"
    )
    clickhouse_secure: bool = Field(default=False, description="Use TLS for ClickHouse")

    # --- ClickHouse (write — uba_scores table) ---
    # TODO Phase 2.7: DBA must provision personel_uba_writer role with INSERT
    # on uba_scores table. Until then, scoring results are held in memory only.
    clickhouse_write_user: str = Field(
        default="personel_uba_writer", description="ClickHouse write user for uba_scores"
    )
    clickhouse_write_password: SecretStr = Field(
        default=SecretStr(""), description="ClickHouse write password"
    )

    # --- Model ---
    model_dir: str = Field(
        default="/tmp/uba-models",  # noqa: S108
        description="Directory for persisted model files",
    )
    isolation_forest_n_estimators: int = Field(
        default=100, description="IsolationForest n_estimators"
    )
    isolation_forest_contamination: float = Field(
        default=0.05,
        description="Expected fraction of anomalous users (IsolationForest contamination)",
    )
    isolation_forest_random_state: int = Field(default=42, description="Random seed")

    # --- Scoring ---
    score_window_days: int = Field(default=7, description="Default scoring window in days")
    recompute_interval_minutes: int = Field(
        default=60, description="Background recompute interval in minutes"
    )
    high_score_notify_threshold: float = Field(
        default=0.9,
        description="Anomaly score above which DPO audit feed notification is pushed (advisory)",
    )

    # --- Risk tiers ---
    tier_watch_threshold: float = Field(
        default=0.3,
        description="Score >= this value is classified 'watch'",
    )
    tier_investigate_threshold: float = Field(
        default=0.7,
        description="Score >= this value is classified 'investigate'",
    )

    # --- Audit feed (advisory notifications only) ---
    # TODO Phase 2.7: backend team must expose /v1/internal/uba-alert endpoint
    # from the admin API. Until then, notifications are logged only.
    audit_feed_url: str = Field(
        default="",
        description="Admin API endpoint for advisory DPO notifications (optional)",
    )
    audit_feed_token: SecretStr = Field(
        default=SecretStr(""),
        description="Bearer token for audit feed endpoint",
    )


_settings: Settings | None = None


def get_settings() -> Settings:
    """Return cached settings singleton."""
    global _settings  # noqa: PLW0603
    if _settings is None:
        _settings = Settings()
    return _settings
