// Package clickhouse provides the ClickHouse client, async batcher, and
// embedded DDL for all event tables.
package clickhouse

// Schema decision: unified events table with a JSON payload column plus a
// typed `event_type` string column and selected top-level indexed columns.
//
// Rationale vs. per-event tables:
//   - 36 event types at Phase 1, expanding in Phase 2: per-table DDL would
//     require schema migrations per new event type.
//   - ClickHouse JSON column (Semi-structured JSON support, v22.6+) retains
//     type information and is compressed efficiently.
//   - We add materialised columns for the most-queried fields (endpoint_id,
//     tenant_id, occurred_at, event_type, pii_class) so common dashboard
//     queries avoid JSON extraction per row.
//   - Sensitive-flagged events go to separate tables (events_sensitive_*)
//     per the retention matrix §Sensitive-Flagged, with shorter TTLs.
//
// The normal events table uses MergeTree for Phase 1 pilot. Phase 1 exit
// criterion #17 requires migration to ReplicatedMergeTree before any customer
// beyond the pilot (see clickhouse-scaling-plan.md).

// DDLStatements contains all CREATE TABLE statements, run once at startup.
// They are idempotent (IF NOT EXISTS).
var DDLStatements = []string{
	createEventsRaw,
	createEventsSensitiveWindow,
	createEventsSensitiveClipboardMeta,
	createEventsSensitiveKeystrokeMeta,
	createEventsSensitiveFile,
	createHeartbeats,
}

// createEventsRaw is the main events table: all 36 event types, normal TTL.
const createEventsRaw = `
CREATE TABLE IF NOT EXISTS events_raw (
    -- Partition and ordering key
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    -- Event identity
    event_id        String,   -- ULID (16 bytes, stored as hex string)
    event_type      LowCardinality(String),
    schema_version  UInt8 DEFAULT 1,
    -- User identity
    user_sid        String,   -- Windows SID
    -- Agent metadata
    agent_version_major UInt8,
    agent_version_minor UInt8,
    agent_version_patch UInt8,
    seq             UInt64,
    -- Classification
    pii_class       LowCardinality(String),
    retention_class LowCardinality(String),
    -- Server ingestion timestamp
    received_at     DateTime64(9, 'UTC'),
    -- Payload: serialised as JSON; ClickHouse JSON type requires 22.6+.
    -- Fallback to String if the ClickHouse version does not support JSON type.
    payload         String,   -- JSON-encoded payload specific to event_type
    -- Sensitivity flag (set by enricher; NOT set here; updated via ALTER UPDATE)
    sensitive       Bool DEFAULT false,
    -- Legal hold flag: TTL clause only runs when legal_hold = FALSE
    legal_hold      Bool DEFAULT false,
    -- Batch tracking
    batch_id        UInt64,
    batch_hmac      String    -- hex, optional
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at, event_type)
TTL occurred_at + INTERVAL 90 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`

// createEventsSensitiveWindow holds window title events flagged by
// SensitivityGuard.window_title_sensitive_regex (KVKK m.6 shortened TTL).
const createEventsSensitiveWindow = `
CREATE TABLE IF NOT EXISTS events_sensitive_window (
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    event_id        String,
    user_sid        String,
    seq             UInt64,
    received_at     DateTime64(9, 'UTC'),
    payload         String,
    legal_hold      Bool DEFAULT false
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL occurred_at + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`

// createEventsSensitiveClipboardMeta holds clipboard metadata for sensitive sessions.
const createEventsSensitiveClipboardMeta = `
CREATE TABLE IF NOT EXISTS events_sensitive_clipboard_meta (
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    event_id        String,
    user_sid        String,
    seq             UInt64,
    received_at     DateTime64(9, 'UTC'),
    payload         String,
    legal_hold      Bool DEFAULT false
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL occurred_at + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`

// createEventsSensitiveKeystrokeMeta holds keystroke stats for sensitive sessions.
const createEventsSensitiveKeystrokeMeta = `
CREATE TABLE IF NOT EXISTS events_sensitive_keystroke_meta (
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    event_id        String,
    user_sid        String,
    seq             UInt64,
    received_at     DateTime64(9, 'UTC'),
    payload         String,
    legal_hold      Bool DEFAULT false
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL occurred_at + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`

// createEventsSensitiveFile holds file events in sensitive directories
// (e.g., health/union directories per KVKK m.6).
const createEventsSensitiveFile = `
CREATE TABLE IF NOT EXISTS events_sensitive_file (
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    event_id        String,
    user_sid        String,
    seq             UInt64,
    received_at     DateTime64(9, 'UTC'),
    payload         String,
    legal_hold      Bool DEFAULT false
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL occurred_at + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`

// createHeartbeats is a separate compact table for agent health heartbeats.
// Short 30-day TTL per retention matrix.
const createHeartbeats = `
CREATE TABLE IF NOT EXISTS agent_heartbeats (
    tenant_id       UUID,
    endpoint_id     UUID,
    occurred_at     DateTime64(9, 'UTC'),
    received_at     DateTime64(9, 'UTC'),
    cpu_percent     Float32,
    rss_bytes       UInt64,
    queue_depth     UInt64,
    blob_queue_depth UInt64,
    drops_since_last UInt64,
    policy_version  String,
    legal_hold      Bool DEFAULT false
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL occurred_at + INTERVAL 30 DAY DELETE WHERE legal_hold = FALSE
SETTINGS index_granularity = 8192;
`
