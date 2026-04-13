-- =============================================================================
-- Personel — ClickHouse ZSTD Tuning Migration
-- Faz 7 / Roadmap item #77
-- =============================================================================
--
-- This migration applies CODEC(ZSTD(3)) to the high-entropy text columns
-- across all events tables. Expected gains:
--
--   * ~25% reduction in on-disk bytes vs default LZ4
--   * ~2x CPU on the write path (batcher)
--   * negligible read impact (ZSTD decompression cost ≈ LZ4)
--
-- Level 3 is chosen as the sweet spot per ClickHouse docs — above level 5
-- the CPU cost grows non-linearly without meaningful gains on
-- JSON-heavy payloads.
--
-- IDEMPOTENCY: MODIFY COLUMN with identical CODEC is a no-op in ClickHouse
-- 24+, so these statements are safe to rerun. They do NOT rewrite existing
-- parts — new parts will use ZSTD, and older parts will migrate via the
-- background mutation that OPTIMIZE TABLE ... FINAL triggers. Operators
-- may run OPTIMIZE after the migration to rewrite old data under ZSTD.
--
-- Reference: infra/scripts/clickhouse-compression-audit.sh (generates
-- the pre/post audit report).
-- =============================================================================

-- ---------------------------------------------------------------------------
-- events_raw — main events table
-- ---------------------------------------------------------------------------
ALTER TABLE events_raw MODIFY COLUMN payload    String CODEC(ZSTD(3));
ALTER TABLE events_raw MODIFY COLUMN user_sid   String CODEC(ZSTD(3));
ALTER TABLE events_raw MODIFY COLUMN batch_hmac String CODEC(ZSTD(3));
ALTER TABLE events_raw MODIFY COLUMN event_id   String CODEC(ZSTD(3));

-- ---------------------------------------------------------------------------
-- events_sensitive_* — shorter retention, same text shape
-- ---------------------------------------------------------------------------
ALTER TABLE events_sensitive_window            MODIFY COLUMN payload  String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_window            MODIFY COLUMN user_sid String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_window            MODIFY COLUMN event_id String CODEC(ZSTD(3));

ALTER TABLE events_sensitive_clipboard_meta    MODIFY COLUMN payload  String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_clipboard_meta    MODIFY COLUMN user_sid String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_clipboard_meta    MODIFY COLUMN event_id String CODEC(ZSTD(3));

ALTER TABLE events_sensitive_keystroke_meta    MODIFY COLUMN payload  String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_keystroke_meta    MODIFY COLUMN user_sid String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_keystroke_meta    MODIFY COLUMN event_id String CODEC(ZSTD(3));

ALTER TABLE events_sensitive_file              MODIFY COLUMN payload  String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_file              MODIFY COLUMN user_sid String CODEC(ZSTD(3));
ALTER TABLE events_sensitive_file              MODIFY COLUMN event_id String CODEC(ZSTD(3));

-- ---------------------------------------------------------------------------
-- agent_heartbeats — smallish numeric columns, only policy_version is text
-- ---------------------------------------------------------------------------
ALTER TABLE agent_heartbeats MODIFY COLUMN policy_version String CODEC(ZSTD(3));

-- ---------------------------------------------------------------------------
-- OPTIONAL: rewrite existing parts under ZSTD so the gain is realised
-- immediately instead of waiting for natural merges. This is a heavy
-- operation — run during a low-traffic window and monitor merges.
--
-- OPTIMIZE TABLE events_raw FINAL;
-- OPTIMIZE TABLE events_sensitive_window FINAL;
-- OPTIMIZE TABLE events_sensitive_clipboard_meta FINAL;
-- OPTIMIZE TABLE events_sensitive_keystroke_meta FINAL;
-- OPTIMIZE TABLE events_sensitive_file FINAL;
-- OPTIMIZE TABLE agent_heartbeats FINAL;
