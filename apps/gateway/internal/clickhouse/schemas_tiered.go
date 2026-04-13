// Package clickhouse — tiered storage migration statements.
//
// Roadmap item #76 (Faz 7). Applied as a one-shot migration AFTER the
// storage policy `tiered` is declared in ClickHouse config
// (infra/compose/clickhouse/storage-config.xml). Running these ALTERs
// against a server that does not have the `tiered` policy will fail, so
// they are kept in a separate slice which the operator runs explicitly
// during the tiering rollout rather than at every startup.
//
// Order of operator actions:
//  1. Copy infra/compose/clickhouse/storage-config.xml into CH config.d/
//  2. Restart clickhouse-server; verify `SELECT policy_name FROM
//     system.storage_policies` contains `tiered`.
//  3. Run TieredStorageMigration statements (docker exec clickhouse
//     clickhouse-client --multiquery < migration.sql).
//  4. Observe system.parts to confirm warm-volume placement as parts age.
//
// The TTL MOVE clause complements the existing DELETE clause:
//   - hot  ( 0 – 7 d): stays on `hot` volume
//   - warm ( 8 – 90 d): moved to `warm` volume by TTL MOVE
//   - cold (≥91 d): cold-export job writes Parquet to MinIO then DROP PARTITION
//
// See infra/runbooks/storage-tiering.md for the operator runbook.
package clickhouse

// TieredStorageMigration contains ALTER statements that attach the
// `tiered` storage policy and the TTL MOVE/DELETE rules to the
// production event tables.
//
// These statements are idempotent at the ClickHouse level — repeated
// ALTERs with the same value are no-ops.
var TieredStorageMigration = []string{
	// events_raw: primary hot→warm→delete ladder.
	`ALTER TABLE events_raw
	   MODIFY SETTING storage_policy = 'tiered'`,

	`ALTER TABLE events_raw
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 90 DAY DELETE WHERE legal_hold = FALSE`,

	// Sensitive tables have 15-day DELETE TTL per KVKK m.6 — no cold tier,
	// but they still benefit from warm-volume placement after 7 days so
	// the hot NVMe is reserved for the newest data.
	`ALTER TABLE events_sensitive_window
	   MODIFY SETTING storage_policy = 'tiered'`,
	`ALTER TABLE events_sensitive_window
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE`,

	`ALTER TABLE events_sensitive_clipboard_meta
	   MODIFY SETTING storage_policy = 'tiered'`,
	`ALTER TABLE events_sensitive_clipboard_meta
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE`,

	`ALTER TABLE events_sensitive_keystroke_meta
	   MODIFY SETTING storage_policy = 'tiered'`,
	`ALTER TABLE events_sensitive_keystroke_meta
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE`,

	`ALTER TABLE events_sensitive_file
	   MODIFY SETTING storage_policy = 'tiered'`,
	`ALTER TABLE events_sensitive_file
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 15 DAY DELETE WHERE legal_hold = FALSE`,

	// Heartbeats: short 30-day retention, tiered so very-old heartbeats
	// move off NVMe.
	`ALTER TABLE agent_heartbeats
	   MODIFY SETTING storage_policy = 'tiered'`,
	`ALTER TABLE agent_heartbeats
	   MODIFY TTL
	     toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
	     toDateTime(occurred_at) + INTERVAL 30 DAY DELETE WHERE legal_hold = FALSE`,
}
