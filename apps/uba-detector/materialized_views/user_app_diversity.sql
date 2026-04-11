-- UBA Materialized View: user_app_diversity
-- Counts distinct app names per user per day.
-- Used by features.py for app_diversity computation.
--
-- Note: ClickHouse MATERIALIZED VIEW cannot use uniq() with SummingMergeTree
-- for exact distinct counts across merges. We store per-day app-level rows
-- and compute distinct count in the query layer (SELECT count(DISTINCT app_name)).
--
-- Alternative: use AggregatingMergeTree with uniqState() for approximate
-- distinct counts — see commented block below.
--
-- Reads from: events_raw
-- KVKK: uses JSONExtractString on payload for app_name. App name is already
-- a non-sensitive field captured under KVKK m.5/2-f legitimate interest.

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

-- Query pattern for features.py:
-- SELECT count(DISTINCT app_name) AS distinct_apps
-- FROM uba_user_app_diversity
-- WHERE tenant_id = {tenant_id}
--   AND user_sid = {user_sid}
--   AND day_bucket BETWEEN {start_date} AND {end_date}
--   AND app_name != ''
