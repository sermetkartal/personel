# UBA Materialized Views

ClickHouse DDL for the pre-aggregated views that `features.py` reads.

These views read from the `events_raw` table (schema in
`apps/gateway/internal/clickhouse/schemas.go`). They aggregate per-user,
per-day statistics to make hourly feature extraction sub-second.

## Views

| File | View name | Purpose |
|------|-----------|---------|
| `user_hourly_activity.sql` | `uba_user_hourly_activity` | Event count per user per hour bucket |
| `user_app_diversity.sql` | `uba_user_app_diversity` | Distinct app names per user per day |
| `user_off_hours_ratio.sql` | `uba_user_off_hours_ratio` | Off-hours vs total event counts |
| `user_data_egress.sql` | `uba_user_data_egress` | Aggregated egress bytes (file + clipboard + network) |
| `user_policy_violations.sql` | `uba_user_policy_violations` | Count of violation events by type |

## Running migrations

```bash
# One-time setup (idempotent — uses IF NOT EXISTS)
clickhouse-client \
  --host localhost \
  --port 9000 \
  --database personel \
  --queries-file ../migrations/0001_uba_materialized_views.sql
```

## TTL

Views inherit no TTL of their own — they read from `events_raw` which has a
90-day TTL. The UBA scoring window is capped at 30 days, so this is safe.
The `uba_scores` output table (created in the migration) has a 90-day TTL per
the Phase 2.5 DPIA amendment.

## Access control

All UBA reads use the `personel_uba_ro` ClickHouse user which has `SELECT`
only on:
- `events_raw`
- `uba_user_*` views
- `uba_scores` (for API reads)

The `uba_scores` write path uses `personel_uba_writer` which has `INSERT` only
on `uba_scores`. This user is not provisioned yet — TODO for Phase 2.7.
