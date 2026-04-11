-- UBA Materialized View: user_hourly_activity
-- Aggregates event counts per user per hour bucket.
-- Used by features.py for screenshot_rate and file_access_rate computation.
--
-- Reads from: events_raw (partitioned by toYYYYMM(occurred_at))
-- Output: per (tenant_id, user_sid, hour_bucket) aggregate counts by event type.
--
-- KVKK: reads only event metadata (event_type, user_sid, timestamps).
-- No payload content is aggregated here.

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

-- Back-fill query (run once for existing data):
-- INSERT INTO uba_user_hourly_activity
-- SELECT
--     tenant_id,
--     user_sid,
--     toStartOfHour(occurred_at) AS hour_bucket,
--     event_type,
--     count() AS event_count
-- FROM events_raw
-- WHERE sensitive = FALSE
--   AND legal_hold = FALSE
-- GROUP BY tenant_id, user_sid, hour_bucket, event_type;
