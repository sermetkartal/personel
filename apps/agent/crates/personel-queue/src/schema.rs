//! SQLite schema DDL and migrations for the local event queue.
//!
//! Schema mirrors the design from `docs/architecture/agent-module-architecture.md`
//! §"Local SQLite Queue Schema". WAL mode is always enabled for write
//! performance and crash safety.

/// Current schema version. Increment when making backward-incompatible changes.
/// Stored in `meta` table under key `schema_version`.
pub const SCHEMA_VERSION: u32 = 1;

/// DDL executed on first open (or upgrade). All statements are idempotent.
pub const INIT_SQL: &str = r#"
-- Enable WAL mode for better concurrency and crash safety.
PRAGMA journal_mode = WAL;
PRAGMA synchronous  = NORMAL;
-- Foreign keys are off by default in SQLite; enable explicitly.
PRAGMA foreign_keys = ON;
-- Reasonable page size for event blobs.
PRAGMA page_size = 4096;

-- ── Event queue ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS event_queue (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id     BLOB    NOT NULL UNIQUE,   -- UUIDv7, 16 bytes
    event_type   TEXT    NOT NULL,
    priority     INTEGER NOT NULL,          -- 0=critical, 1=high, 2=normal, 3=low
    occurred_at  INTEGER NOT NULL,          -- unix nanos
    enqueued_at  INTEGER NOT NULL,          -- unix nanos
    payload_pb   BLOB    NOT NULL,          -- prost-encoded events.v1.Event
    size_bytes   INTEGER NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    batch_id     INTEGER,                   -- set when picked up by uploader
    status       INTEGER NOT NULL DEFAULT 0 -- 0=pending, 1=in_flight, 2=acked
);

CREATE INDEX IF NOT EXISTS idx_queue_status_priority
    ON event_queue(status, priority, id);
CREATE INDEX IF NOT EXISTS idx_queue_batch
    ON event_queue(batch_id);
CREATE INDEX IF NOT EXISTS idx_queue_event_id
    ON event_queue(event_id);

-- ── Blob queue ────────────────────────────────────────────────────────────────
-- Tracks large blobs (screenshots, keystroke content, clipboard content)
-- that are stored as sealed files on disk and uploaded separately.
CREATE TABLE IF NOT EXISTS blob_queue (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT    NOT NULL,       -- screenshot | screenclip | keystroke_content | clipboard_content
    local_path      TEXT    NOT NULL,       -- sealed file on disk
    size_bytes      INTEGER NOT NULL,
    sha256          BLOB    NOT NULL,       -- 32 bytes
    linked_event_id INTEGER,               -- FK to event_queue.id
    status          INTEGER NOT NULL DEFAULT 0,
    attempts        INTEGER NOT NULL DEFAULT 0,
    enqueued_at     INTEGER NOT NULL,
    FOREIGN KEY (linked_event_id) REFERENCES event_queue(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_blob_status
    ON blob_queue(status, id);

-- ── Metadata / watermarks ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value BLOB
);
"#;

/// SQL to set the SQLCipher encryption key. Must be the first statement
/// executed on a new connection before any other operations.
///
/// The `?` placeholder is filled by the connection open hook.
pub const PRAGMA_KEY: &str = "PRAGMA key = ?;";
