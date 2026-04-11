//! Local event queue: enqueue, dequeue_batch, ack, size, vacuum, and eviction.
//!
//! All public methods are synchronous. Callers that need async should dispatch
//! to a `spawn_blocking` task. The connection pool (`r2d2`) provides
//! thread-safe concurrent access.

use std::sync::Arc;

use r2d2::Pool;
use r2d2_sqlite::SqliteConnectionManager;
use rusqlite::{params, OptionalExtension};
use tracing::{debug, error, warn};

use personel_core::error::{AgentError, Result};
use personel_core::event::Priority;

// Pool error conversion helper for r2d2 errors (which don't implement Into<rusqlite::Error>).
fn pool_err<E: std::fmt::Display>(e: E) -> AgentError {
    AgentError::Queue(format!("connection pool error: {e}"))
}


use crate::schema::{INIT_SQL, SCHEMA_VERSION};

// ──────────────────────────────────────────────────────────────────────────────
// Configuration
// ──────────────────────────────────────────────────────────────────────────────

/// Configuration for the local event queue.
#[derive(Debug, Clone)]
pub struct QueueConfig {
    /// Path to the SQLite database file.
    pub db_path: std::path::PathBuf,
    /// SQLCipher encryption key (32+ bytes; kept in memory only).
    pub key: zeroize::Zeroizing<Vec<u8>>,
    /// Maximum allowed database size in bytes before eviction.
    pub max_bytes: u64,
    /// Number of connections in the r2d2 pool.
    pub pool_size: u32,
}

impl QueueConfig {
    /// Creates a new configuration with sensible defaults.
    #[must_use]
    pub fn new(
        db_path: impl Into<std::path::PathBuf>,
        key: zeroize::Zeroizing<Vec<u8>>,
    ) -> Self {
        Self {
            db_path: db_path.into(),
            key,
            max_bytes: 200 * 1024 * 1024, // 200 MB default cap
            pool_size: 4,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// QueuedEvent (dequeued row)
// ──────────────────────────────────────────────────────────────────────────────

/// A row dequeued from the event queue for upload.
#[derive(Debug, Clone)]
pub struct QueuedEvent {
    /// Auto-increment row ID.
    pub id: i64,
    /// UUIDv7 bytes of the event.
    pub event_id: Vec<u8>,
    /// Dotted event type name.
    pub event_type: String,
    /// Queue priority.
    pub priority: Priority,
    /// Unix nanos when the event occurred.
    pub occurred_at: i64,
    /// Prost-encoded proto bytes.
    pub payload_pb: Vec<u8>,
    /// Number of previous upload attempts.
    pub attempts: i32,
}

// ──────────────────────────────────────────────────────────────────────────────
// QueueStats
// ──────────────────────────────────────────────────────────────────────────────

/// Summary statistics about the current queue state.
#[derive(Debug, Clone, Default)]
pub struct QueueStats {
    /// Total number of pending events.
    pub pending_count: u64,
    /// Total number of in-flight events.
    pub in_flight_count: u64,
    /// Total payload bytes of pending events.
    pub pending_bytes: u64,
    /// Approximate database file size in bytes.
    pub db_bytes: u64,
}

// ──────────────────────────────────────────────────────────────────────────────
// EventQueue
// ──────────────────────────────────────────────────────────────────────────────

/// Thread-safe local event queue backed by SQLCipher.
///
/// Cloning an `EventQueue` is cheap — the internal pool is `Arc`-wrapped.
#[derive(Clone)]
pub struct EventQueue {
    pool: Pool<SqliteConnectionManager>,
    max_bytes: u64,
}

impl EventQueue {
    /// Opens (or creates) the queue database at the given path with
    /// SQLCipher encryption.
    ///
    /// # Errors
    ///
    /// Returns an error if the database cannot be opened, the key is wrong,
    /// or the schema migration fails.
    pub fn open(config: QueueConfig) -> Result<Self> {
        let key = Arc::new(config.key);

        // The connection manager opens SQLite with a custom init hook that
        // sets the PRAGMA key before any other statement.
        let manager = SqliteConnectionManager::file(&config.db_path)
            .with_init(move |conn| {
                // SQLCipher requires PRAGMA key as the very first call.
                // We use a raw execute here because rusqlite's `execute_batch`
                // would go through the normal statement parser, which requires
                // the DB to already be open — but SQLCipher intercepts the
                // `key` pragma before the page is decrypted.
                conn.execute_batch(&format!(
                    "PRAGMA key = \"x'{}'\";",
                    hex::encode(key.as_ref())
                ))?;
                // Apply schema.
                conn.execute_batch(INIT_SQL)?;
                Ok(())
            });

        let pool = Pool::builder()
            .max_size(config.pool_size)
            .build(manager)
            .map_err(pool_err)?;

        // Verify schema version.
        let conn = pool.get().map_err(pool_err)?;
        Self::ensure_schema_version(&conn)?;

        Ok(Self { pool, max_bytes: config.max_bytes })
    }

    /// Opens an in-memory queue for unit tests (no encryption).
    ///
    /// # Errors
    ///
    /// Returns an error if the schema cannot be applied.
    #[cfg(test)]
    pub fn open_in_memory() -> Result<Self> {
        let manager = SqliteConnectionManager::memory().with_init(|conn| {
            conn.execute_batch(INIT_SQL)?;
            Ok(())
        });
        let pool = Pool::builder().max_size(1).build(manager).map_err(pool_err)?;
        Ok(Self { pool, max_bytes: 200 * 1024 * 1024 })
    }

    fn ensure_schema_version(conn: &rusqlite::Connection) -> Result<()> {
        // Read or initialise the stored schema version.
        let stored: Option<u32> = conn
            .query_row(
                "SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'schema_version'",
                [],
                |row| row.get(0),
            )
            .optional()?;

        match stored {
            None => {
                conn.execute(
                    "INSERT INTO meta (key, value) VALUES ('schema_version', ?1)",
                    params![SCHEMA_VERSION],
                )?;
            }
            Some(v) if v == SCHEMA_VERSION => {}
            Some(v) => {
                return Err(AgentError::Config(format!(
                    "queue schema version mismatch: stored={v}, expected={SCHEMA_VERSION}"
                )));
            }
        }
        Ok(())
    }

    // ── Write path ─────────────────────────────────────────────────────────

    /// Enqueues a single event.
    ///
    /// If the database is over capacity, [`Self::evict`] is called first.
    /// Tamper (priority=0) events are never evicted.
    ///
    /// # Errors
    ///
    /// Returns an error if the SQLite insert fails.
    pub fn enqueue(
        &self,
        event_id: &[u8],       // 16 bytes, UUIDv7
        event_type: &str,
        priority: Priority,
        occurred_at: i64,
        enqueued_at: i64,
        payload_pb: &[u8],
    ) -> Result<i64> {
        let size = payload_pb.len() as i64;
        let conn = self.get_conn()?;

        // Evict if over budget (best-effort; never blocks critical events).
        if priority != Priority::Critical {
            if let Ok(stats) = self.stats_inner(&conn) {
                if stats.db_bytes + size as u64 > self.max_bytes {
                    self.evict_inner(&conn, size as u64)?;
                }
            }
        }

        let row_id = conn.execute(
            r#"
            INSERT INTO event_queue
                (event_id, event_type, priority, occurred_at, enqueued_at,
                 payload_pb, size_bytes, status)
            VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, 0)
            "#,
            params![
                event_id,
                event_type,
                priority.as_i32(),
                occurred_at,
                enqueued_at,
                payload_pb,
                size,
            ],
        )?;

        debug!(event_type, priority = priority.as_i32(), size, "event enqueued");
        Ok(row_id as i64)
    }

    // ── Read path ──────────────────────────────────────────────────────────

    /// Dequeues up to `limit` pending events, ordered by priority then id.
    ///
    /// Marks fetched rows as `in_flight` and assigns `batch_id` so the caller
    /// can atomically ack or retry the batch.
    ///
    /// # Errors
    ///
    /// Returns an error if the SQLite query fails.
    pub fn dequeue_batch(&self, limit: usize, batch_id: u64) -> Result<Vec<QueuedEvent>> {
        let conn = self.get_conn()?;

        // Select pending events ordered by priority (lower = more urgent) then id.
        let ids: Vec<i64> = {
            let mut stmt = conn.prepare(
                r#"
                SELECT id FROM event_queue
                WHERE status = 0
                ORDER BY priority ASC, id ASC
                LIMIT ?1
                "#,
            )?;
            let rows: Vec<i64> = stmt
                .query_map(params![limit as i64], |row| row.get(0))?
                .collect::<std::result::Result<_, _>>()?;
            rows
        };

        if ids.is_empty() {
            return Ok(vec![]);
        }

        // Mark as in-flight in a single UPDATE.
        // SQLite doesn't support UPDATE ... WHERE id IN (?...) with rusqlite
        // directly for variable-length lists, so we use a transaction.
        conn.execute_batch("BEGIN IMMEDIATE;")?;
        for &id in &ids {
            conn.execute(
                "UPDATE event_queue SET status = 1, batch_id = ?1, attempts = attempts + 1 WHERE id = ?2",
                params![batch_id as i64, id],
            )?;
        }
        conn.execute_batch("COMMIT;")?;

        // Fetch the rows.
        let mut events = Vec::with_capacity(ids.len());
        for id in ids {
            let evt = conn.query_row(
                r#"
                SELECT id, event_id, event_type, priority, occurred_at, payload_pb, attempts
                FROM event_queue WHERE id = ?1
                "#,
                params![id],
                |row| {
                    Ok(QueuedEvent {
                        id: row.get(0)?,
                        event_id: row.get(1)?,
                        event_type: row.get(2)?,
                        priority: Priority::from_i32(row.get(3)?),
                        occurred_at: row.get(4)?,
                        payload_pb: row.get(5)?,
                        attempts: row.get(6)?,
                    })
                },
            )?;
            events.push(evt);
        }

        debug!(count = events.len(), batch_id, "batch dequeued");
        Ok(events)
    }

    /// Acknowledges a successfully uploaded batch, removing the rows.
    ///
    /// # Errors
    ///
    /// Returns an error if the DELETE fails.
    pub fn ack_batch(&self, batch_id: u64) -> Result<u64> {
        let conn = self.get_conn()?;
        let deleted = conn.execute(
            "DELETE FROM event_queue WHERE batch_id = ?1 AND status = 1",
            params![batch_id as i64],
        )?;
        debug!(batch_id, deleted, "batch acked");
        Ok(deleted as u64)
    }

    /// Marks an in-flight batch as pending again (for retry on transport error).
    ///
    /// Records `error_msg` for diagnostics.
    ///
    /// # Errors
    ///
    /// Returns an error if the UPDATE fails.
    pub fn nack_batch(&self, batch_id: u64, error_msg: &str) -> Result<u64> {
        let conn = self.get_conn()?;
        let updated = conn.execute(
            r#"
            UPDATE event_queue
            SET status = 0, batch_id = NULL, last_error = ?2
            WHERE batch_id = ?1 AND status = 1
            "#,
            params![batch_id as i64, error_msg],
        )?;
        warn!(batch_id, updated, error_msg, "batch nacked");
        Ok(updated as u64)
    }

    // ── Stats & maintenance ────────────────────────────────────────────────

    /// Returns current queue statistics.
    ///
    /// # Errors
    ///
    /// Returns an error if the SQLite query fails.
    pub fn stats(&self) -> Result<QueueStats> {
        let conn = self.get_conn()?;
        self.stats_inner(&conn)
    }

    fn stats_inner(&self, conn: &rusqlite::Connection) -> Result<QueueStats> {
        let (pending_count, pending_bytes, in_flight_count): (u64, u64, u64) = conn.query_row(
            r#"
            SELECT
                SUM(CASE WHEN status = 0 THEN 1 ELSE 0 END),
                SUM(CASE WHEN status = 0 THEN size_bytes ELSE 0 END),
                SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END)
            FROM event_queue
            "#,
            [],
            |row| Ok((row.get(0).unwrap_or(0), row.get(1).unwrap_or(0), row.get(2).unwrap_or(0))),
        )?;

        // Approximate DB file size via page_count * page_size.
        let db_bytes: u64 = conn
            .query_row("SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()", [], |r| r.get(0))
            .unwrap_or(0);

        Ok(QueueStats { pending_count, in_flight_count, pending_bytes, db_bytes })
    }

    /// Evicts low-priority events to recover `needed_bytes` of space.
    ///
    /// Eviction order (per architecture spec):
    /// 1. Pending low-priority (priority=3) events, oldest first.
    /// 2. Pending normal-priority (priority=2) events, oldest first.
    /// 3. Never evicts critical (priority=0) or high-priority (priority=1).
    ///
    /// # Errors
    ///
    /// Returns an error if the DELETE fails.
    pub fn evict(&self, needed_bytes: u64) -> Result<u64> {
        let conn = self.get_conn()?;
        self.evict_inner(&conn, needed_bytes)
    }

    fn evict_inner(&self, conn: &rusqlite::Connection, needed_bytes: u64) -> Result<u64> {
        let mut freed = 0u64;

        for priority in [3i32, 2i32] {
            if freed >= needed_bytes {
                break;
            }
            // Evict oldest events at this priority level until we've freed enough.
            let rows: Vec<(i64, i64)> = {
                let mut stmt = conn.prepare(
                    r#"
                    SELECT id, size_bytes FROM event_queue
                    WHERE status = 0 AND priority = ?1
                    ORDER BY id ASC
                    LIMIT 200
                    "#,
                )?;
                let rows: Vec<(i64, i64)> = stmt
                    .query_map(params![priority], |row| Ok((row.get(0)?, row.get(1)?)))?
                    .collect::<std::result::Result<_, _>>()?;
                rows
            };

            for (id, size) in rows {
                if freed >= needed_bytes {
                    break;
                }
                conn.execute("DELETE FROM event_queue WHERE id = ?1", params![id])?;
                freed += size as u64;
                warn!(id, size, priority, "event evicted from queue");
            }
        }

        if freed > 0 {
            // Emit an internal eviction watermark to meta.
            conn.execute(
                "INSERT OR REPLACE INTO meta (key, value) VALUES ('last_eviction_bytes', ?1)",
                params![freed as i64],
            )?;
        }

        Ok(freed)
    }

    /// Runs `VACUUM` to reclaim disk space after bulk deletes.
    ///
    /// This is a heavy operation; only call it during idle periods.
    ///
    /// # Errors
    ///
    /// Returns an error if VACUUM fails.
    pub fn vacuum(&self) -> Result<()> {
        let conn = self.get_conn()?;
        conn.execute_batch("VACUUM;")?;
        Ok(())
    }

    /// Stores a key-value pair in the `meta` table.
    ///
    /// # Errors
    ///
    /// Returns an error if the upsert fails.
    pub fn set_meta(&self, key: &str, value: &[u8]) -> Result<()> {
        let conn = self.get_conn()?;
        conn.execute(
            "INSERT OR REPLACE INTO meta (key, value) VALUES (?1, ?2)",
            params![key, value],
        )?;
        Ok(())
    }

    /// Reads a value from the `meta` table.
    ///
    /// # Errors
    ///
    /// Returns an error if the query fails.
    pub fn get_meta(&self, key: &str) -> Result<Option<Vec<u8>>> {
        let conn = self.get_conn()?;
        let value = conn
            .query_row("SELECT value FROM meta WHERE key = ?1", params![key], |r| r.get(0))
            .optional()?;
        Ok(value)
    }

    // ── Internal helpers ───────────────────────────────────────────────────

    fn get_conn(&self) -> Result<r2d2::PooledConnection<SqliteConnectionManager>> {
        self.pool.get().map_err(|e| {
            error!("failed to get queue DB connection: {e}");
            pool_err(e)
        })
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn fake_event_id(n: u8) -> Vec<u8> {
        vec![n; 16]
    }

    fn make_queue() -> EventQueue {
        EventQueue::open_in_memory().unwrap()
    }

    #[test]
    fn enqueue_dequeue_ack() {
        let q = make_queue();

        q.enqueue(
            &fake_event_id(1),
            "process.start",
            Priority::Normal,
            1_000_000,
            1_000_001,
            b"payload_bytes",
        )
        .unwrap();

        let batch = q.dequeue_batch(10, 42).unwrap();
        assert_eq!(batch.len(), 1);
        assert_eq!(batch[0].event_type, "process.start");

        let deleted = q.ack_batch(42).unwrap();
        assert_eq!(deleted, 1);

        let batch2 = q.dequeue_batch(10, 43).unwrap();
        assert!(batch2.is_empty());
    }

    #[test]
    fn priority_ordering() {
        let q = make_queue();

        q.enqueue(&fake_event_id(1), "process.start", Priority::Normal, 1, 1, b"n").unwrap();
        q.enqueue(&fake_event_id(2), "agent.tamper_detected", Priority::Critical, 2, 2, b"c")
            .unwrap();
        q.enqueue(&fake_event_id(3), "file.read", Priority::Low, 3, 3, b"l").unwrap();

        let batch = q.dequeue_batch(3, 1).unwrap();
        // Critical should come first.
        assert_eq!(batch[0].event_type, "agent.tamper_detected");
        assert_eq!(batch[1].event_type, "process.start");
        assert_eq!(batch[2].event_type, "file.read");
    }

    #[test]
    fn nack_returns_to_pending() {
        let q = make_queue();
        q.enqueue(&fake_event_id(1), "window.title_changed", Priority::Normal, 1, 1, b"x")
            .unwrap();

        q.dequeue_batch(1, 99).unwrap();
        q.nack_batch(99, "transport error").unwrap();

        // Should be available for another dequeue.
        let batch = q.dequeue_batch(1, 100).unwrap();
        assert_eq!(batch.len(), 1);
        assert_eq!(batch[0].attempts, 2);
    }

    #[test]
    fn eviction_skips_critical() {
        let q = make_queue();

        for i in 0u8..5 {
            q.enqueue(
                &fake_event_id(i),
                "file.read",
                Priority::Low,
                i as i64,
                i as i64,
                b"aaaa",
            )
            .unwrap();
        }
        q.enqueue(
            &fake_event_id(10),
            "agent.tamper_detected",
            Priority::Critical,
            100,
            100,
            b"tamper",
        )
        .unwrap();

        // Evict at least 20 bytes — should only remove low-priority rows.
        q.evict(20).unwrap();

        let batch = q.dequeue_batch(100, 1).unwrap();
        // The tamper event must survive.
        assert!(batch.iter().any(|e| e.event_type == "agent.tamper_detected"));
    }

    #[test]
    fn meta_roundtrip() {
        let q = make_queue();
        q.set_meta("last_seq", &42u64.to_le_bytes()).unwrap();
        let v = q.get_meta("last_seq").unwrap().unwrap();
        let seq = u64::from_le_bytes(v.try_into().unwrap());
        assert_eq!(seq, 42);
    }
}
