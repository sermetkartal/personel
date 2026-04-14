# Personel Admin API — Postgres Migrations

## Schema Ownership Contract

The Postgres schema used by the admin API has **two layers** with distinct owners:

### Layer 1 — Installer Baseline (`infra/compose/postgres/init.sql`)

- **Owner**: devops-engineer (infra/)
- **Lifecycle**: Runs exactly once, at customer install time, before any app container starts
- **Contents**: extensions (`pgcrypto`, `pg_cron`), role creation (`app_api`, `app_gateway`, `app_dlp_ro`, `audit_writer`, `migration_runner`), row-level security enablement, the hash-chain `audit.append_event` stored procedure, append-only trigger guards, and initial `audit_log` table
- **Invariant**: `init.sql` creates the foundations that the API migration system ASSUMES exist. It does NOT create business tables (endpoints, users, tenants, dsr_requests, etc.) — those belong to Layer 2.
- **Rollback**: not reversible; a fresh install requires DB wipe

### Layer 2 — API Migrations (`apps/api/internal/postgres/migrations/*.sql`)

- **Owner**: backend-developer (apps/api/)
- **Lifecycle**: Runs at every API container startup via `golang-migrate` (or equivalent)
- **Contents**: all business tables (tenants, users, endpoints, policies, dsr_requests, legal_holds, live_view_sessions, destruction_reports, keystroke_keys, dlp_state, first_login_acks)
- **Contract with Layer 1**:
  - MIGRATIONS ASSUME `init.sql` HAS ALREADY RUN. They do NOT re-create roles or extensions.
  - Migrations reference roles created in `init.sql` (e.g., `GRANT ... TO app_api`) without creating them.
  - Migrations do NOT duplicate `audit_log` or `audit.append_event` — those are exclusively Layer 1.
  - Migrations may grant additional privileges on new tables to existing roles.

### Why This Split

Two layers, not one:

- **Bootstrap complexity**: Role creation, RLS enablement, and stored procedures with SECURITY DEFINER require Postgres superuser. The migration runner (`app_migrator`) intentionally does NOT have superuser privileges — restricting the blast radius of a compromised migration.
- **Compliance**: The hash-chained `audit_log` table is the foundation of KVKK m.12 accountability. Its trigger-enforced append-only property must be set up BEFORE any data goes in; a migration could otherwise leave a window where audit entries can be mutated.
- **WORM sink (ADR 0014)**: The audit verifier cross-validates Postgres chain with MinIO Object Lock. The Object Lock bucket is created by `infra/compose/minio/worm-bucket-init.sh` during install — Layer 2 cannot establish this.

## Migration Numbering

| Prefix Style | Usage |
|---|---|
| `001_` … `007_` | Early foundational migrations (tenants, users, endpoints, base RBAC) |
| `008_` | Heaviest legacy migration (historical; do not split) |
| `0020_` onward | Polish round additions (4-digit padding so migrations stay sorted) |

`golang-migrate` parses the numeric prefix as a integer, so `008` < `0020` is interpreted correctly (8 < 20). Do not retroactively rename existing 3-digit prefixes — it breaks checksums.

## Adding a New Migration

1. Increment from the highest existing number
2. Create a **pair**: `NNNN_description.up.sql` and `NNNN_description.down.sql`
3. `up.sql` should be idempotent where possible (`CREATE TABLE IF NOT EXISTS`, `ALTER TABLE ... IF NOT EXISTS`)
4. `down.sql` must correctly reverse everything `up.sql` did (tested!)
5. Never modify a migration that has been merged — always append a new one
6. If a migration creates a table that holds personal data, update:
   - `docs/architecture/data-retention-matrix.md`
   - `docs/compliance/kvkk-framework.md` §5 matrix
   - Appropriate `ON DELETE CASCADE` rules for tenant/user lifecycle

## Current Migration List (as of 2026-04-14)

Ordered by `golang-migrate` numeric parse (`008 < 0020`). Columns: file,
phase, and the substantive schema change. `down.sql` pairs exist for every
entry.

| # | File | Phase | Change |
|---|---|---|---|
| 1 | `001_tenants_users.up.sql` | Faz 1 | `tenants`, `users`, `tenant_settings`; enables `pgcrypto`; RLS baseline |
| 2 | `002_endpoints.up.sql` | Faz 1 | `endpoints` (enrolled agents) + `enrollment_tokens` |
| 3 | `003_policies.up.sql` | Faz 1 | `policies` + `endpoint_policies` with signing key refs |
| 4 | `004_audit_log.up.sql` | Faz 1 | `audit` schema + hash-chained append-only `audit_log` |
| 5 | `005_dsr.up.sql` | Faz 1 | KVKK m.11 `dsr_requests` + SLA timer |
| 6 | `006_legal_holds.up.sql` | Faz 1 | `legal_holds` (DPO-only, 2-year max) |
| 7 | `007_live_view.up.sql` | Faz 1 | `live_view_sessions` state machine persistence |
| 8 | `008_destruction_reports.up.sql` | Faz 1 | 6-month periodic `destruction_reports` + audit grants |
| 9 | `0020_dlp_state.up.sql` | Polish | ADR 0013 single-row `dlp_state` read-by-API only |
| 10 | `0021_first_login_acknowledgement.up.sql` | Polish | KVKK m.10 first-login ack (Aşama 5) |
| 11 | `0022_keystroke_keys.up.sql` | Polish | Per-endpoint wrapped PE-DEK store (ADR 0013 A2) |
| 12 | `0023_users_hris_fields.up.sql` | Faz 2 | Nullable HRIS columns on `users` (ADR 0018) |
| 13 | `0024_mobile_push_tokens.up.sql` | Faz 2 | `mobile_push_tokens` for mobile admin app (ADR 0019) |
| 14 | `0025_evidence_items.up.sql` | Faz 3.0 | SOC 2 evidence locker table (append-only, RLS) |
| 15 | `0026_employee_daily_stats.up.sql` | Faz 2 | Daily rolled-up activity per user per day |
| 16 | `0027_employee_hourly_stats.up.sql` | Faz 2 | Per-hour active/idle bar chart signals |
| 17 | `0028_users_role_it_hierarchy.up.sql` | Polish | Add `it_operator` + `it_manager` to `users.role` check |
| 18 | `0029_audit_append_event_legacy_overload.up.sql` | Polish | Postgres overload bridging init.sql ↔ Go recorder signatures |
| 19 | `0030_employee_daily_stats_rich_signals.up.sql` | Faz 2 | `rich_signals` JSONB pass-through for non-core collector signals |
| 20 | `0031_endpoint_refresh_fields.up.sql` | Faz 6 | `last_refresh_at` + `refresh_count` for endpoint token refresh (#63) |
| 21 | `0032_endpoint_commands.up.sql` | Faz 6 | Remote command audit + state (deactivate/wipe/revoke, #64/#65) |
| 22 | `0033_dsr_fulfillment_and_apikeys.up.sql` | Faz 6 | DSR fulfillment workflow (#69) + service API key auth (#72) |

**Count**: 22 migrations. `init.sql` (Layer 1) is the baseline and is not
counted here. Any new migration in Faz 7+ starts at `0034_*`.

## Testing

Migrations are tested in three ways:

1. **Migration runner at startup** — `api` container runs `migrate up` before serving traffic
2. **CI `compose-validate` job** — spins up a disposable Postgres + runs all migrations (Phase 1 exit criterion #17)
3. **Integration tests** — `apps/api/test/integration/helpers_test.go::testDB` uses testcontainers-go to provide a migrated Postgres for each test suite

## Rollback Policy

`migrate down` is allowed in development but **prohibited in production** — rolling back destroys data. Production rollbacks must go through:

1. Full DB snapshot first
2. DPO sign-off (hash-chain audit integrity risk)
3. Post-rollback audit chain verification
4. WORM sink cross-validation

See `docs/security/runbooks/incident-response-playbook.md` for the full rollback procedure.
