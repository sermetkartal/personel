-- UBA Materialized View: user_data_egress
-- Aggregates data egress volume: file writes + clipboard content + network outbound.
-- Used by features.py for data_egress_volume computation.
--
-- Source event types:
--   file_write     -> payload.bytes_written
--   clipboard_copy -> payload.clipboard_bytes  (sensitive path excluded)
--   network_flow   -> payload.bytes_out
--
-- KVKK note: clipboard_copy events on the SENSITIVE path (events_sensitive_clipboard_meta)
-- are NOT included here per KVKK m.6 — we read only from events_raw where sensitive=FALSE.
-- This means clipboard-based egress in sensitive sessions is excluded from UBA scoring,
-- which is the conservative / privacy-protective choice.
--
-- Reads from: events_raw (sensitive=FALSE only)

CREATE MATERIALIZED VIEW IF NOT EXISTS uba_user_data_egress
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day_bucket)
ORDER BY (tenant_id, user_sid, day_bucket, egress_type)
TTL day_bucket + INTERVAL 90 DAY DELETE
AS
SELECT
    tenant_id,
    user_sid,
    toDate(occurred_at)                                        AS day_bucket,
    event_type                                                 AS egress_type,
    -- Extract bytes from payload JSON based on event type
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

-- Query pattern for features.py:
-- SELECT
--     sum(file_bytes_written) + sum(clipboard_bytes) + sum(network_bytes_out)
--         AS total_egress_bytes
-- FROM uba_user_data_egress
-- WHERE tenant_id = {tenant_id}
--   AND user_sid = {user_sid}
--   AND day_bucket BETWEEN {start_date} AND {end_date}
