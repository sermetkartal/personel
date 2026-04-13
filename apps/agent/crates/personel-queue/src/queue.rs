//! Local event queue: enqueue, dequeue_batch, ack, size, vacuum, and eviction.
//!
//! All public methods are synchronous. Callers that need async should dispatch
//! to a `spawn_blocking` task. The connection pool (`r2d2`) provides
//! thread-safe concurrent access.

use std::sync::atomic::{AtomicU64, Ordering};
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
    /// High-water mark in bytes. When pending bytes exceed this, eviction
    /// is triggered (Low → Normal → High order; Critical never evicted).
    /// Default: 500 MB.
    pub high_water_bytes: u64,
    /// Hard limit in bytes. Beyond this, non-Critical events are rejected
    /// with `QueueBackpressure`. Critical events trigger aggressive eviction
    /// of High-priority events to make room. Default: 1 GB.
    pub hard_limit_bytes: u64,
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
            max_bytes: 200 * 1024 * 1024, // 200 MB default cap (legacy field)
            high_water_bytes: 500 * 1024 * 1024,  // 500 MB
            hard_limit_bytes: 1024 * 1024 * 1024, // 1 GB
            pool_size: 4,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// EvictionReport
// ──────────────────────────────────────────────────────────────────────────────

/// Summary of one eviction pass.
///
/// Returned by [`EventQueue::evict_until_under`] and useful for telemetry +
/// tests. The `hit_critical_floor` flag is set to `true` when eviction has
/// drained every Low/Normal/High event and the queue is still over the
/// requested target (i.e. the only remaining pending events are Critical
/// audit-essential rows that MUST NOT be evicted under any circumstance).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct EvictionReport {
    /// Number of Low priority rows deleted in this pass.
    pub low_count: u64,
    /// Number of Normal priority rows deleted in this pass.
    pub normal_count: u64,
    /// Number of High priority rows deleted in this pass (only happens
    /// during hard-limit pressure; Critical-events-only state).
    pub high_count: u64,
    /// Total bytes freed across all priorities.
    pub bytes_freed: u64,
    /// `true` if eviction could not reach the target because all remaining
    /// pending rows are Critical and the policy refuses to evict them.
    pub hit_critical_floor: bool,
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
    /// Lifetime count of Low-priority events evicted under capacity pressure.
    pub evictions_low_count: u64,
    /// Lifetime count of Normal-priority events evicted under capacity pressure.
    pub evictions_normal_count: u64,
    /// Lifetime count of High-priority events evicted under hard-limit pressure.
    pub evictions_high_count: u64,
    /// Lifetime total bytes freed by eviction across all priorities.
    pub evictions_total_bytes: u64,
    /// Lifetime count of `enqueue_with_pressure` calls rejected because the
    /// queue contained only Critical events and could not free space.
    pub enqueue_rejected_critical_only: u64,
}

// ──────────────────────────────────────────────────────────────────────────────
// EventQueue
// ──────────────────────────────────────────────────────────────────────────────

/// Thread-safe local event queue backed by SQLCipher.
///
/// Cloning an `EventQueue` is cheap — the internal pool is `Arc`-wrapped
/// and the eviction counters live behind an `Arc` so all clones share the
/// same lifetime tally.
#[derive(Clone)]
pub struct EventQueue {
    pool: Pool<SqliteConnectionManager>,
    max_bytes: u64,
    high_water_bytes: u64,
    hard_limit_bytes: u64,
    counters: Arc<EvictionCounters>,
}

#[derive(Debug, Default)]
struct EvictionCounters {
    low: AtomicU64,
    normal: AtomicU64,
    high: AtomicU64,
    bytes: AtomicU64,
    rejected_critical_only: AtomicU64,
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
        let max_bytes = config.max_bytes;
        let high_water_bytes = config.high_water_bytes;
        let hard_limit_bytes = config.hard_limit_bytes;

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

        Ok(Self {
            pool,
            max_bytes,
            high_water_bytes,
            hard_limit_bytes,
            counters: Arc::new(EvictionCounters::default()),
        })
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
        Ok(Self {
            pool,
            max_bytes: 200 * 1024 * 1024,
            high_water_bytes: 500 * 1024 * 1024,
            hard_limit_bytes: 1024 * 1024 * 1024,
            counters: Arc::new(EvictionCounters::default()),
        })
    }

    /// Test-only constructor allowing custom byte limits.
    #[cfg(test)]
    pub fn open_in_memory_with_limits(high_water: u64, hard_limit: u64) -> Result<Self> {
        let mut q = Self::open_in_memory()?;
        q.high_water_bytes = high_water;
        q.hard_limit_bytes = hard_limit;
        Ok(q)
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

        Ok(QueueStats {
            pending_count,
            in_flight_count,
            pending_bytes,
            db_bytes,
            evictions_low_count: self.counters.low.load(Ordering::Relaxed),
            evictions_normal_count: self.counters.normal.load(Ordering::Relaxed),
            evictions_high_count: self.counters.high.load(Ordering::Relaxed),
            evictions_total_bytes: self.counters.bytes.load(Ordering::Relaxed),
            enqueue_rejected_critical_only: self
                .counters
                .rejected_critical_only
                .load(Ordering::Relaxed),
        })
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

    // ── Pressure-aware eviction (Faz 4 #35) ────────────────────────────────
    //
    // KVKK rationale: Critical priority events carry audit-essential meaning
    // (tamper detection, agent self-protection alarms, KVKK m.12 incident
    // signals). Losing these events would compromise the hash-chained audit
    // invariant the platform relies on for inspector readiness, so the
    // eviction policy treats them as immortal: even when the queue is at
    // its hard limit and on fire, Critical events are NEVER deleted to
    // reclaim space. In the worst case the agent rejects new enqueues
    // entirely (`QueueCriticalOnlyOverflow`) until the uploader drains
    // them upstream — a hard-failure mode is preferable to silently
    // dropping audit data.

    /// Evicts pending events until the total `pending_bytes` falls under the
    /// `target_bytes` threshold.
    ///
    /// Eviction order:
    /// 1. **Low** priority oldest first.
    /// 2. **Normal** priority oldest first (only if Low exhausted).
    /// 3. **High** priority oldest first (only if Low + Normal exhausted —
    ///    this is the hard-limit escape valve).
    /// 4. **Critical** is never evicted. If only Critical events remain
    ///    and the target is still not met, the report's
    ///    `hit_critical_floor` flag is set to `true`.
    ///
    /// Runs in a single `IMMEDIATE` SQLite transaction so the queue is
    /// never observed in a partially-evicted state.
    ///
    /// # Errors
    ///
    /// Returns an error if the SQLite transaction fails.
    pub fn evict_until_under(&self, target_bytes: u64) -> Result<EvictionReport> {
        let mut conn = self.get_conn()?;
        let mut report = EvictionReport::default();

        let tx = conn.transaction()?;

        // Compute current pending_bytes inside the transaction so the snapshot
        // is consistent with the rows we'll be deleting.
        let mut current: u64 = tx
            .query_row(
                "SELECT COALESCE(SUM(size_bytes), 0) FROM event_queue WHERE status = 0",
                [],
                |r| r.get::<_, i64>(0).map(|v| v as u64),
            )
            .unwrap_or(0);

        if current <= target_bytes {
            tx.commit()?;
            return Ok(report);
        }

        // Evict in priority order: Low (3) → Normal (2) → High (1).
        // Critical (0) is intentionally absent.
        for priority in [3i32, 2i32, 1i32] {
            if current <= target_bytes {
                break;
            }

            // Pull oldest pending rows at this priority. We process in chunks
            // of 500 to avoid loading the entire backlog into memory in one
            // shot for very large queues.
            loop {
                if current <= target_bytes {
                    break;
                }

                let rows: Vec<(i64, i64)> = {
                    let mut stmt = tx.prepare(
                        r#"
                        SELECT id, size_bytes FROM event_queue
                        WHERE status = 0 AND priority = ?1
                        ORDER BY id ASC
                        LIMIT 500
                        "#,
                    )?;
                    let mapped = stmt
                        .query_map(params![priority], |row| {
                            Ok((row.get(0)?, row.get(1)?))
                        })?
                        .collect::<std::result::Result<Vec<(i64, i64)>, _>>()?;
                    mapped
                };

                if rows.is_empty() {
                    break;
                }

                for (id, size) in rows {
                    if current <= target_bytes {
                        break;
                    }
                    tx.execute("DELETE FROM event_queue WHERE id = ?1", params![id])?;
                    let size_u = size as u64;
                    current = current.saturating_sub(size_u);
                    report.bytes_freed += size_u;
                    match priority {
                        3 => report.low_count += 1,
                        2 => report.normal_count += 1,
                        1 => report.high_count += 1,
                        _ => {}
                    }
                }
            }
        }

        // After Low+Normal+High exhaustion, anything left is Critical.
        if current > target_bytes {
            report.hit_critical_floor = true;
        }

        // Persist the eviction watermark for diagnostics.
        if report.bytes_freed > 0 {
            tx.execute(
                "INSERT OR REPLACE INTO meta (key, value) VALUES ('last_eviction_bytes', ?1)",
                params![report.bytes_freed as i64],
            )?;
        }

        tx.commit()?;

        // Fold counters atomically OUTSIDE the SQLite transaction (the tx
        // already committed; counters are best-effort observability).
        if report.low_count > 0 {
            self.counters.low.fetch_add(report.low_count, Ordering::Relaxed);
        }
        if report.normal_count > 0 {
            self.counters.normal.fetch_add(report.normal_count, Ordering::Relaxed);
        }
        if report.high_count > 0 {
            self.counters.high.fetch_add(report.high_count, Ordering::Relaxed);
            warn!(
                high_count = report.high_count,
                bytes_freed = report.bytes_freed,
                "evicted HIGH priority events under hard-limit pressure"
            );
        }
        if report.bytes_freed > 0 {
            self.counters
                .bytes
                .fetch_add(report.bytes_freed, Ordering::Relaxed);
            debug!(
                low = report.low_count,
                normal = report.normal_count,
                high = report.high_count,
                bytes = report.bytes_freed,
                hit_critical_floor = report.hit_critical_floor,
                "eviction pass complete"
            );
        }
        if report.hit_critical_floor {
            warn!(
                target_bytes,
                pending_bytes = current,
                "eviction hit critical floor — only Critical events remain, queue still over target"
            );
        }

        Ok(report)
    }

    /// Pressure-aware enqueue.
    ///
    /// This is the new admission path that respects the high-water and
    /// hard-limit thresholds. Behaviour:
    ///
    /// 1. If the new event would push pending bytes above `high_water_bytes`,
    ///    call [`Self::evict_until_under`] to reclaim space first.
    /// 2. If after eviction the new event would still exceed
    ///    `hard_limit_bytes` AND it is NOT [`Priority::Critical`], reject
    ///    with [`AgentError::QueueBackpressure`]. This is the standard
    ///    admission control path for overloaded agents.
    /// 3. If the new event IS [`Priority::Critical`] and the queue is at
    ///    the hard limit, log an error, run an aggressive eviction targeted
    ///    at `hard_limit_bytes - new_size` (which is allowed to drain
    ///    High priority — see [`Self::evict_until_under`]), then accept.
    /// 4. If even the aggressive eviction can't make room (i.e. all remaining
    ///    pending rows are themselves Critical), return
    ///    [`AgentError::QueueCriticalOnlyOverflow`] and increment the
    ///    `enqueue_rejected_critical_only` counter. The caller MUST drain
    ///    the queue (i.e. retry the upload) before the next enqueue can
    ///    succeed.
    ///
    /// Existing [`Self::enqueue`] remains unchanged for backward compatibility;
    /// callers migrate to this method opt-in.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::QueueBackpressure`] when a non-Critical event
    /// cannot be admitted, [`AgentError::QueueCriticalOnlyOverflow`] when
    /// a Critical event cannot be admitted because the queue is already
    /// full of Critical events, or any underlying SQLite error.
    pub fn enqueue_with_pressure(
        &self,
        event_id: &[u8],
        event_type: &str,
        priority: Priority,
        occurred_at: i64,
        enqueued_at: i64,
        payload_pb: &[u8],
    ) -> Result<i64> {
        let new_size = payload_pb.len() as u64;

        let stats = self.stats()?;
        let would_be = stats.pending_bytes.saturating_add(new_size);

        // Step 1: high-water proactive eviction (Low + Normal only — High is
        // protected at this stage because we're nowhere near the hard limit).
        if would_be > self.high_water_bytes {
            // Compute a target that leaves room for the incoming event.
            let target = self.high_water_bytes.saturating_sub(new_size);
            // Use a Low/Normal-only pass first — we don't want to evict High
            // until we're truly over the hard limit.
            self.evict_low_and_normal_until_under(target)?;
        }

        // Re-check the post-eviction state.
        let stats = self.stats()?;
        let would_be = stats.pending_bytes.saturating_add(new_size);

        if would_be > self.hard_limit_bytes {
            if priority == Priority::Critical {
                // Step 3: aggressive eviction allowed to drain High.
                error!(
                    pending_bytes = stats.pending_bytes,
                    hard_limit = self.hard_limit_bytes,
                    new_size,
                    "queue at hard limit, evicting High to make room for Critical event"
                );
                let target = self.hard_limit_bytes.saturating_sub(new_size);
                let report = self.evict_until_under(target)?;
                if report.hit_critical_floor {
                    // The queue is full of Critical events; we cannot admit
                    // even another Critical event. Caller MUST drain.
                    self.counters
                        .rejected_critical_only
                        .fetch_add(1, Ordering::Relaxed);
                    error!(
                        pending_bytes = self.stats()?.pending_bytes,
                        hard_limit = self.hard_limit_bytes,
                        "queue critical-only overflow — cannot evict; rejecting Critical event"
                    );
                    return Err(AgentError::QueueCriticalOnlyOverflow);
                }
                // Re-check after aggressive eviction.
                let stats2 = self.stats()?;
                if stats2.pending_bytes.saturating_add(new_size) > self.hard_limit_bytes {
                    self.counters
                        .rejected_critical_only
                        .fetch_add(1, Ordering::Relaxed);
                    return Err(AgentError::QueueCriticalOnlyOverflow);
                }
            } else {
                // Step 2: backpressure for non-Critical events.
                warn!(
                    pending_bytes = stats.pending_bytes,
                    hard_limit = self.hard_limit_bytes,
                    new_size,
                    priority = priority.as_i32(),
                    "queue backpressure: rejecting non-critical event"
                );
                return Err(AgentError::QueueBackpressure {
                    hard_limit: self.hard_limit_bytes,
                });
            }
        }

        // Admission granted — perform the actual insert.
        let conn = self.get_conn()?;
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
                new_size as i64,
            ],
        )?;

        debug!(
            event_type,
            priority = priority.as_i32(),
            size = new_size,
            "event enqueued (pressure-aware)"
        );
        Ok(row_id as i64)
    }

    /// Internal helper: evict only Low + Normal priority rows until under
    /// `target_bytes`. Used by the high-water path so we never delete High
    /// priority events unless we're at the hard limit.
    fn evict_low_and_normal_until_under(&self, target_bytes: u64) -> Result<EvictionReport> {
        let mut conn = self.get_conn()?;
        let mut report = EvictionReport::default();

        let tx = conn.transaction()?;

        let mut current: u64 = tx
            .query_row(
                "SELECT COALESCE(SUM(size_bytes), 0) FROM event_queue WHERE status = 0",
                [],
                |r| r.get::<_, i64>(0).map(|v| v as u64),
            )
            .unwrap_or(0);

        if current <= target_bytes {
            tx.commit()?;
            return Ok(report);
        }

        for priority in [3i32, 2i32] {
            if current <= target_bytes {
                break;
            }
            loop {
                if current <= target_bytes {
                    break;
                }
                let rows: Vec<(i64, i64)> = {
                    let mut stmt = tx.prepare(
                        r#"
                        SELECT id, size_bytes FROM event_queue
                        WHERE status = 0 AND priority = ?1
                        ORDER BY id ASC
                        LIMIT 500
                        "#,
                    )?;
                    let mapped = stmt
                        .query_map(params![priority], |row| {
                            Ok((row.get(0)?, row.get(1)?))
                        })?
                        .collect::<std::result::Result<Vec<(i64, i64)>, _>>()?;
                    mapped
                };
                if rows.is_empty() {
                    break;
                }
                for (id, size) in rows {
                    if current <= target_bytes {
                        break;
                    }
                    tx.execute("DELETE FROM event_queue WHERE id = ?1", params![id])?;
                    let size_u = size as u64;
                    current = current.saturating_sub(size_u);
                    report.bytes_freed += size_u;
                    if priority == 3 {
                        report.low_count += 1;
                    } else {
                        report.normal_count += 1;
                    }
                }
            }
        }

        // Note: hit_critical_floor is intentionally NOT set here even if
        // current > target_bytes — this method explicitly refuses to touch
        // High/Critical events, so "stuck" is the expected outcome and the
        // caller will fall through to enqueue_with_pressure's hard-limit
        // branch which calls the full evict_until_under.

        if report.bytes_freed > 0 {
            tx.execute(
                "INSERT OR REPLACE INTO meta (key, value) VALUES ('last_eviction_bytes', ?1)",
                params![report.bytes_freed as i64],
            )?;
        }
        tx.commit()?;

        if report.low_count > 0 {
            self.counters.low.fetch_add(report.low_count, Ordering::Relaxed);
        }
        if report.normal_count > 0 {
            self.counters
                .normal
                .fetch_add(report.normal_count, Ordering::Relaxed);
        }
        if report.bytes_freed > 0 {
            self.counters
                .bytes
                .fetch_add(report.bytes_freed, Ordering::Relaxed);
            debug!(
                low = report.low_count,
                normal = report.normal_count,
                bytes = report.bytes_freed,
                "high-water eviction pass complete"
            );
        }

        Ok(report)
    }

    /// Returns the configured high-water mark in bytes.
    #[must_use]
    pub fn high_water_bytes(&self) -> u64 {
        self.high_water_bytes
    }

    /// Returns the configured hard limit in bytes.
    #[must_use]
    pub fn hard_limit_bytes(&self) -> u64 {
        self.hard_limit_bytes
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

    // ──────────────────────────────────────────────────────────────────────
    // Faz 4 #35 — pressure-aware eviction tests
    // ──────────────────────────────────────────────────────────────────────

    /// Helper: enqueue N events of a given priority with payloads of the
    /// given size, returning the total bytes written.
    fn fill(q: &EventQueue, priority: Priority, count: u8, size: usize) -> u64 {
        let payload = vec![0xAB; size];
        for i in 0..count {
            q.enqueue(
                &fake_event_id(i),
                "test.event",
                priority,
                i as i64,
                i as i64,
                &payload,
            )
            .unwrap();
        }
        (count as u64) * (size as u64)
    }

    /// Helper: enqueue with explicit id seed so we can mix multiple priorities.
    fn fill_with_seed(
        q: &EventQueue,
        seed: u8,
        priority: Priority,
        count: u8,
        size: usize,
    ) -> u64 {
        let payload = vec![0xCD; size];
        for i in 0..count {
            q.enqueue(
                &fake_event_id(seed.wrapping_add(i)),
                "test.event",
                priority,
                i as i64,
                i as i64,
                &payload,
            )
            .unwrap();
        }
        (count as u64) * (size as u64)
    }

    #[test]
    fn evict_until_under_low_only() {
        let q = make_queue();
        // 10 Low events, 100 bytes each => 1000 bytes pending.
        fill(&q, Priority::Low, 10, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);

        // Target: 500 bytes — should free at least 500 bytes from Low.
        let report = q.evict_until_under(500).unwrap();
        assert!(report.bytes_freed >= 500);
        assert_eq!(report.normal_count, 0);
        assert_eq!(report.high_count, 0);
        assert!(!report.hit_critical_floor);
        assert!(q.stats().unwrap().pending_bytes <= 500);
    }

    #[test]
    fn evict_escalates_low_then_normal() {
        let q = make_queue();
        // 5 Low (500 bytes) + 5 Normal (500 bytes) => 1000 bytes pending.
        fill_with_seed(&q, 0, Priority::Low, 5, 100);
        fill_with_seed(&q, 50, Priority::Normal, 5, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);

        // Target 200 bytes — must drain ALL 5 Low (500 bytes) plus
        // some Normal to get under 200.
        let report = q.evict_until_under(200).unwrap();
        assert_eq!(report.low_count, 5);
        assert!(report.normal_count >= 3); // at least 3 normal to get under 200
        assert_eq!(report.high_count, 0);
        assert!(!report.hit_critical_floor);
    }

    #[test]
    fn evict_escalates_to_high_when_needed() {
        let q = make_queue();
        // Only High + Critical present (no Low/Normal).
        fill_with_seed(&q, 0, Priority::High, 5, 100);
        fill_with_seed(&q, 50, Priority::Critical, 2, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 700);

        // Target 250 bytes. Critical is 200 of those; we need to drain
        // High to get under 250, leaving 200 (just Critical) which is OK.
        let report = q.evict_until_under(250).unwrap();
        assert_eq!(report.low_count, 0);
        assert_eq!(report.normal_count, 0);
        assert!(report.high_count >= 4);
        // We DID get under 250 (200 < 250) so critical floor not hit.
        assert!(!report.hit_critical_floor);
        assert_eq!(q.stats().unwrap().pending_bytes, 200);
    }

    #[test]
    fn evict_critical_only_hits_floor() {
        let q = make_queue();
        // Pure Critical buffer.
        fill(&q, Priority::Critical, 10, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);

        // Target 100 bytes — eviction MUST refuse to touch Critical and
        // signal hit_critical_floor.
        let report = q.evict_until_under(100).unwrap();
        assert_eq!(report.low_count, 0);
        assert_eq!(report.normal_count, 0);
        assert_eq!(report.high_count, 0);
        assert_eq!(report.bytes_freed, 0);
        assert!(report.hit_critical_floor);
        // Pending bytes unchanged.
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);
    }

    #[test]
    fn evict_below_target_is_noop() {
        let q = make_queue();
        fill(&q, Priority::Low, 3, 100); // 300 bytes
        let report = q.evict_until_under(1000).unwrap();
        assert_eq!(report.bytes_freed, 0);
        assert_eq!(report.low_count, 0);
        assert_eq!(report.normal_count, 0);
        assert_eq!(report.high_count, 0);
        assert!(!report.hit_critical_floor);
    }

    #[test]
    fn evict_oldest_first_within_priority() {
        let q = make_queue();
        // Insert 3 Low events with distinct payloads to verify FIFO-by-id.
        for i in 0u8..3 {
            q.enqueue(
                &fake_event_id(i),
                "test.event",
                Priority::Low,
                i as i64,
                i as i64,
                &[i; 100],
            )
            .unwrap();
        }
        // Evict ~one event worth.
        q.evict_until_under(250).unwrap();

        // The remaining batch should NOT contain the oldest (id=1, payload[0]==0).
        let remaining = q.dequeue_batch(10, 1).unwrap();
        assert!(!remaining.iter().any(|e| e.payload_pb[0] == 0));
    }

    #[test]
    fn enqueue_with_pressure_happy_path() {
        let q = EventQueue::open_in_memory_with_limits(1000, 2000).unwrap();
        // Plenty of room.
        let result = q.enqueue_with_pressure(
            &fake_event_id(1),
            "process.start",
            Priority::Normal,
            1,
            1,
            &[0u8; 100],
        );
        assert!(result.is_ok());
        assert_eq!(q.stats().unwrap().pending_count, 1);
    }

    #[test]
    fn enqueue_with_pressure_triggers_high_water_eviction() {
        let q = EventQueue::open_in_memory_with_limits(500, 2000).unwrap();
        // Fill above high-water with Low priority (600 bytes).
        fill(&q, Priority::Low, 6, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 600);

        // Enqueue a 100-byte Normal event — must trigger eviction first.
        q.enqueue_with_pressure(
            &fake_event_id(99),
            "test.event",
            Priority::Normal,
            999,
            999,
            &[0u8; 100],
        )
        .unwrap();

        // After eviction + new insert, we should be under high_water (500).
        let stats = q.stats().unwrap();
        assert!(stats.pending_bytes <= 500, "got {}", stats.pending_bytes);
        assert!(stats.evictions_low_count > 0);
    }

    #[test]
    fn enqueue_with_pressure_rejects_non_critical_at_hard_limit() {
        let q = EventQueue::open_in_memory_with_limits(500, 1000).unwrap();
        // Fill to hard limit with HIGH priority (high-water eviction won't
        // touch them, so we'll be parked at 1000 bytes pending).
        fill(&q, Priority::High, 10, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);

        // A Normal-priority new event should be rejected with
        // QueueBackpressure since high-water eviction can't free High events.
        let err = q
            .enqueue_with_pressure(
                &fake_event_id(99),
                "test.event",
                Priority::Normal,
                999,
                999,
                &[0u8; 100],
            )
            .unwrap_err();
        match err {
            AgentError::QueueBackpressure { hard_limit } => assert_eq!(hard_limit, 1000),
            other => panic!("expected QueueBackpressure, got {:?}", other),
        }
        // No eviction of the High events should have happened.
        assert_eq!(q.stats().unwrap().evictions_high_count, 0);
    }

    #[test]
    fn enqueue_with_pressure_critical_evicts_high_at_hard_limit() {
        let q = EventQueue::open_in_memory_with_limits(500, 1000).unwrap();
        // Fill to hard limit with HIGH priority.
        fill(&q, Priority::High, 10, 100);

        // A Critical event MUST evict High to make room.
        q.enqueue_with_pressure(
            &fake_event_id(99),
            "agent.tamper_detected",
            Priority::Critical,
            999,
            999,
            &[0u8; 100],
        )
        .unwrap();

        let stats = q.stats().unwrap();
        // Critical event is in.
        assert!(stats.pending_bytes <= 1000);
        // Some High events were evicted (counter should be > 0).
        assert!(stats.evictions_high_count > 0);
    }

    #[test]
    fn enqueue_with_pressure_critical_only_overflow() {
        let q = EventQueue::open_in_memory_with_limits(500, 1000).unwrap();
        // Fill to hard limit with CRITICAL priority — eviction can't touch.
        fill(&q, Priority::Critical, 10, 100);
        assert_eq!(q.stats().unwrap().pending_bytes, 1000);

        // Another Critical event must be rejected with CriticalOnlyOverflow.
        let err = q
            .enqueue_with_pressure(
                &fake_event_id(99),
                "agent.tamper_detected",
                Priority::Critical,
                999,
                999,
                &[0u8; 100],
            )
            .unwrap_err();
        assert!(matches!(err, AgentError::QueueCriticalOnlyOverflow));
        let stats = q.stats().unwrap();
        assert_eq!(stats.enqueue_rejected_critical_only, 1);
        // Critical events are intact.
        assert_eq!(stats.pending_count, 10);
        assert_eq!(stats.evictions_high_count, 0);
    }

    #[test]
    fn counters_accumulate_across_passes() {
        let q = make_queue();
        fill(&q, Priority::Low, 10, 100); // 1000 bytes
        q.evict_until_under(700).unwrap();
        let s1 = q.stats().unwrap();
        let first_low = s1.evictions_low_count;
        let first_bytes = s1.evictions_total_bytes;
        assert!(first_low > 0);

        // Add more Low and evict again.
        fill_with_seed(&q, 100, Priority::Low, 10, 100);
        q.evict_until_under(200).unwrap();
        let s2 = q.stats().unwrap();
        assert!(s2.evictions_low_count > first_low);
        assert!(s2.evictions_total_bytes > first_bytes);
    }

    #[test]
    fn critical_events_survive_aggressive_eviction() {
        let q = EventQueue::open_in_memory_with_limits(500, 1000).unwrap();
        // 5 Critical (500 bytes) + 5 High (500 bytes) = at hard limit.
        fill_with_seed(&q, 0, Priority::Critical, 5, 100);
        fill_with_seed(&q, 50, Priority::High, 5, 100);

        // Force aggressive eviction by submitting a Critical event.
        q.enqueue_with_pressure(
            &fake_event_id(99),
            "agent.tamper_detected",
            Priority::Critical,
            999,
            999,
            &[0u8; 100],
        )
        .unwrap();

        // All 5 original Critical events must still be present + the new one = 6.
        let batch = q.dequeue_batch(100, 1).unwrap();
        let critical_count = batch
            .iter()
            .filter(|e| e.priority == Priority::Critical)
            .count();
        assert_eq!(critical_count, 6);
    }
}
