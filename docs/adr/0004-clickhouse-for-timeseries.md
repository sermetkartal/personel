# ADR 0004 — ClickHouse for Telemetry Storage

**Status**: Accepted

## Context

Personel ingests ~1B events/day at 10k endpoints. Typical queries: "top apps last 7 days per user," "time-in-app histograms," "search window titles," "DLP match rollups." Access pattern is write-heavy, append-only, with aggregation-dominated reads. Storage must be operable on-prem by customer IT teams with modest expertise.

## Decision

Use **ClickHouse** as the primary telemetry store. Single-node for MVP; replicated cluster path documented for 10k scale. PostgreSQL is reserved for metadata (see ADR 0006). Full-text over window titles/URLs goes to OpenSearch, not ClickHouse.

Table design principles:
- One table per event family (process events, window events, file events, network events, etc.).
- MergeTree with `(tenant_id, endpoint_id, occurred_at)` order key and `PARTITION BY toYYYYMM(occurred_at)`.
- `TTL` clauses enforce the retention matrix automatically.
- Materialized views precompute dashboard aggregates (hourly rollups per user, per app).
- String dictionaries for exe names to keep cardinality under control.

## Consequences

- Excellent compression (10–30×) keeps on-prem disk budgets realistic.
- Sub-second aggregate queries on hundreds of millions of rows on commodity hardware.
- Operational footprint is simpler than Cassandra/Druid for on-prem IT teams.
- `UPDATE/DELETE` are asynchronous mutations — our retention strategy uses `TTL`, not explicit deletes, which aligns well.
- Schema migrations require care (no cheap `ALTER` on compressed columns) — migrations are additive.
- The DLP pipeline runs separately; ClickHouse holds metadata rows for keystroke events only, never plaintext.

## Alternatives Considered

- **TimescaleDB**: rejected — simpler ops but orders of magnitude higher cost per billion rows at our compression profile.
- **Elasticsearch/OpenSearch as primary**: rejected — unsuitable storage economics for time-series analytics at this scale; retained as search-only.
- **InfluxDB**: rejected — v3 reset and licensing flux; operational risk.
- **Druid**: rejected — operational complexity beyond customer IT comfort.
- **Parquet on MinIO + DuckDB**: interesting for cold tier, revisit Phase 2 for archive.
