-- =============================================================================
-- Migration 0001: UBA Materialized Views + uba_scores output table
-- Run once against ClickHouse (idempotent — uses IF NOT EXISTS).
--
-- Prerequisites:
--   - events_raw table must exist (created by gateway at startup)
--   - Database: personel (or set via --database flag)
--   - User running this migration: must have CREATE TABLE + SELECT on events_raw
--
-- After running:
--   - uba_user_hourly_activity MV populates on new events_raw inserts
--   - uba_user_app_diversity MV populates on new events_raw inserts
--   - uba_user_off_hours_ratio MV populates on new events_raw inserts
--   - uba_user_data_egress MV populates on new events_raw inserts
--   - uba_user_policy_violations MV populates on new events_raw inserts
--   - uba_user_network_hosts MV populates on new events_raw inserts
--   - uba_scores table ready for UBA Detector writes (Phase 2.7)
--
-- KVKK: UBA scores are personal data. The uba_scores table has a 90-day TTL
-- per Phase 2.5 DPIA amendment. Employees can request their score history via
-- KVKK m.11 DSR (request_type=access) and dispute it (request_type=object).
-- =============================================================================

-- ---------------------------------------------------------------------------
-- 1. user_hourly_activity
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_hourly_activity
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour_bucket)
ORDER BY (tenant_id, user_sid, hour_bucket, event_type)
TTL hour_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toStartOfHour(occurred_at)  AS hour_bucket,
    event_type,
    count()                     AS event_count
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
GROUP BY
    tenant_id,
    user_sid,
    hour_bucket,
    event_type;

-- ---------------------------------------------------------------------------
-- 2. user_app_diversity
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_app_diversity
ENGINE = ReplacingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, app_name)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)                                    AS day_bucket,
    JSONExtractString(payload, 'app_name')                 AS app_name,
    JSONExtractString(payload, 'process_name')             AS process_name,
    count()                                                AS event_count,
    max(occurred_at)                                       AS last_seen_at
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
  AND (
      event_type IN ('window_focus', 'process_start', 'screen_capture', 'file_read', 'file_write')
      OR JSONExtractString(payload, 'app_name') != ''
  )
GROUP BY
    tenant_id,
    user_sid,
    day_bucket,
    app_name,
    process_name;

-- ---------------------------------------------------------------------------
-- 3. user_off_hours_ratio
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_off_hours_ratio
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, is_off_hours)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(toTimeZone(occurred_at, 'Europe/Istanbul'))     AS day_bucket,
    toUInt8(
        toDayOfWeek(toTimeZone(occurred_at, 'Europe/Istanbul')) IN (6, 7)
        OR toHour(toTimeZone(occurred_at, 'Europe/Istanbul')) < 8
        OR toHour(toTimeZone(occurred_at, 'Europe/Istanbul')) >= 18
    )                                                       AS is_off_hours,
    count()                                                 AS event_count
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
GROUP BY
    tenant_id,
    user_sid,
    day_bucket,
    is_off_hours;

-- ---------------------------------------------------------------------------
-- 4. user_data_egress
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_data_egress
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, egress_type)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)                                         AS day_bucket,
    event_type                                                  AS egress_type,
    sumIf(
        toUInt64OrZero(JSONExtractString(payload, 'bytes_written')),
        event_type = 'file_write'
    )                                                           AS file_bytes_written,
    sumIf(
        toUInt64OrZero(JSONExtractString(payload, 'clipboard_bytes')),
        event_type = 'clipboard_copy'
    )                                                           AS clipboard_bytes,
    sumIf(
        toUInt64OrZero(JSONExtractString(payload, 'bytes_out')),
        event_type = 'network_flow'
    )                                                           AS network_bytes_out,
    count()                                                     AS event_count
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
  AND event_type IN ('file_write', 'clipboard_copy', 'network_flow')
GROUP BY
    tenant_id,
    user_sid,
    day_bucket,
    egress_type;

-- ---------------------------------------------------------------------------
-- 5. user_policy_violations
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_policy_violations
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, violation_type)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)     AS day_bucket,
    event_type              AS violation_type,
    count()                 AS violation_count
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
  AND event_type IN ('app_blocked', 'web_blocked', 'dlp_match')
GROUP BY
    tenant_id,
    user_sid,
    day_bucket,
    violation_type;

-- ---------------------------------------------------------------------------
-- 6. user_network_hosts (for new_host_ratio)
-- ---------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_network_hosts
ENGINE = ReplacingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, remote_host)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)                                 AS day_bucket,
    JSONExtractString(payload, 'remote_host')            AS remote_host,
    count()                                              AS connection_count,
    max(occurred_at)                                     AS last_seen_at
FROM events_raw
WHERE sensitive = FALSE
  AND legal_hold = FALSE
  AND event_type = 'network_flow'
  AND JSONExtractString(payload, 'remote_host') != ''
GROUP BY
    tenant_id,
    user_sid,
    day_bucket,
    remote_host;

-- ---------------------------------------------------------------------------
-- 7. uba_scores — UBA Detector output table (Phase 2.7 write path)
--
-- KVKK: personal data. TTL 90 days. DSR-exportable. No automated actions
-- triggered by rows in this table (advisory only — KVKK m.11/g).
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS uba_scores (
    tenant_id               UUID,
    user_id                 UUID,
    computed_at             DateTime64(9, 'UTC'),
    window_days             UInt8,
    anomaly_score           Float32,
    risk_tier               LowCardinality(String),
    -- JSON array: [{feature, weight, direction}, ...]
    contributing_features   String,
    -- Model metadata
    model_version           String  DEFAULT '0.1.0',
    -- KVKK: advisory flag — this score NEVER triggers automated action
    is_advisory             Bool    DEFAULT true,
    legal_hold              Bool    DEFAULT false
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(computed_at)
ORDER BY (tenant_id, user_id, computed_at)
TTL computed_at + INTERVAL 90 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;

-- Grant (run as DBA, not in this migration — see ops runbook):
-- GRANT SELECT ON personel.uba_user_* TO personel_uba_ro;
-- GRANT SELECT ON personel.uba_scores TO personel_uba_ro;
-- GRANT INSERT ON personel.uba_scores TO personel_uba_writer;
