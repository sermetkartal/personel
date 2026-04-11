-- UBA Materialized View: user_off_hours_ratio
-- Pre-computes off-hours vs total event counts per user per day.
-- Turkish business hours: UTC+3 (Istanbul), Mon-Fri 08:00-18:00.
--
-- ClickHouse toHour() uses UTC. UTC+3 offset is applied here:
--   Istanbul hour = (toHour(occurred_at) + 3) % 24
--   Weekend: toDayOfWeek(toTimeZone(occurred_at, 'Europe/Istanbul')) IN (6, 7)
--
-- Used by features.py for off_hours_activity computation.
--
-- Reads from: events_raw
-- KVKK: only timestamp and user_sid accessed — no payload content.

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
    -- is_off_hours = 1 if outside Mon-Fri 08:00-18:00 Istanbul time
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

-- Query pattern for features.py:
-- SELECT
--     sum(if(is_off_hours = 1, event_count, 0)) AS off_hours_count,
--     sum(event_count)                           AS total_count,
--     off_hours_count / total_count              AS off_hours_ratio
-- FROM uba_user_off_hours_ratio
-- WHERE tenant_id = {tenant_id}
--   AND user_sid = {user_sid}
--   AND day_bucket BETWEEN {start_date} AND {end_date}
