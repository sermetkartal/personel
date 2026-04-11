-- UBA Materialized View: user_policy_violations
-- Counts policy violation events: app_blocked, web_blocked, dlp_match.
-- Also pre-aggregates network_flow remote_host for new_host_ratio computation.
-- Used by features.py for policy_violation_count and new_host_ratio.
--
-- Reads from: events_raw
-- KVKK: violation events are already captured under KVKK m.5/2-f legitimate
-- interest (security monitoring). DLP matches are counted but DLP content
-- is NOT stored here — count only, no payload extraction.

-- Part 1: Policy violation counts
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

-- Part 2: Network host observations (for new_host_ratio)
-- Separate view to avoid wide row shape.
CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_network_hosts
ENGINE = ReplacingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, remote_host)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)                                AS day_bucket,
    JSONExtractString(payload, 'remote_host')           AS remote_host,
    count()                                             AS connection_count,
    max(occurred_at)                                    AS last_seen_at
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

-- Query pattern for new_host_ratio (features.py):
-- Current window hosts:
-- SELECT DISTINCT remote_host FROM uba_user_network_hosts
-- WHERE tenant_id = {tenant_id} AND user_sid = {user_sid}
--   AND day_bucket BETWEEN {window_start} AND {window_end}
--
-- Baseline hosts (prior 30 days):
-- SELECT DISTINCT remote_host FROM uba_user_network_hosts
-- WHERE tenant_id = {tenant_id} AND user_sid = {user_sid}
--   AND day_bucket BETWEEN {baseline_start} AND {window_start}
