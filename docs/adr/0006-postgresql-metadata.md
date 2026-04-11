# ADR 0006 — PostgreSQL for Relational Metadata

**Status**: Accepted

## Context

Beyond the high-volume telemetry stream, Personel needs strong-consistency storage for tenants, users, endpoints, policies, certificates (serial→status), live-view request state machines, and the hash-chained audit log.

## Decision

Use **PostgreSQL 16** as the single relational metadata store. One logical database per deployment (tenant-aware schemas or row-level multi-tenant keys). Connection pooling via `pgbouncer` in transaction mode. Backups via `pg_basebackup` + WAL archiving to MinIO.

Key tables:
- `tenants`, `users`, `roles`, `role_bindings`
- `endpoints`, `enrollment_tokens`, `agent_versions`, `endpoint_health`
- `policies`, `policy_assignments`, `policy_versions`
- `live_view_requests`, `live_view_sessions`
- `audit_records` (append-only, hash-chained; see live-view doc)
- `keystroke_keys` (wrapped DEK store; see key-hierarchy doc)
- `dlp_rules`, `dlp_matches`
- `cert_registry` (serial → endpoint, status)

## Consequences

- Rock-solid consistency and tooling; wide operator familiarity in Turkey.
- Row-level security can enforce multi-tenant isolation when that phase activates.
- JSONB columns handle semi-structured policy bundles without a schema migration each time.
- Not suitable for telemetry scale — that is why ClickHouse exists.
- Audit table requires deliberate role/grant setup (INSERT+SELECT only for the audit service role).

## Alternatives Considered

- **MySQL/MariaDB**: rejected — weaker JSONB, weaker row-level security, replication story less clean for on-prem.
- **CockroachDB**: overkill for single-node on-prem MVP; revisit if customers demand multi-region.
- **SQLite (server side)**: inadequate concurrency.
- **Store metadata in ClickHouse**: rejected — async mutations make strong-consistency workflows (live-view approvals, certificate revocation) impossible to implement correctly.
