# ClickHouse Scaling Plan

> Language: English. Status: Authoritative. Closes Gap 9 from Phase 0 revision round. Downstream owner: postgres-pro (despite the name, same data specialist owns ClickHouse ops) + devops-engineer.

## Problem

Phase 0 ADR 0004 committed to ClickHouse for telemetry and noted "single-node for MVP; replicated cluster path documented for 10k scale." The documentation of that path was deferred. Security-engineer review surfaced this as a scale cliff: a 500-endpoint pilot is comfortable on one node, but any customer beyond ~5 000 endpoints will hit availability and write-throughput walls with no rehearsed upgrade path. This plan closes that gap.

## Stage 1 — Pilot (Phase 1, 500 endpoints)

**Topology**: single ClickHouse node on the same host as the rest of the application plane (or a small dedicated VM if host resources permit).

**Why this is acceptable**: 500 endpoints × ~4.6 MB/day metadata = ~2.3 GB/day ingest. With 10–30× compression (ClickHouse norms for our schema) this is ~100–230 MB/day on disk. A single 8-core, 32 GB RAM, 2 TB NVMe node handles this with significant headroom. Ingest latency p95 is <1 s.

**Configuration**:
- `MergeTree` (not `ReplicatedMergeTree`) engine for Phase 1 pilot tables.
- Zookeeper / ClickHouse Keeper **not required** at this stage.
- TTL clauses enforced per the retention matrix.
- Daily `clickhouse-backup` to MinIO (separate bucket `backups/clickhouse/`).
- Full backup retained 7 days; weekly full retained 30 days.
- Restore drill executed monthly.

**Failure mode**: single-node failure = ingest outage until restore. Mitigation = local SQLite queue on agents with 48-hour offline tolerance; acceptable for pilot.

**Monitoring alarms**:
- Disk usage > 60%
- Insert queue depth > 1 000 parts
- Merge backlog > 30 min
- Query p95 > 1 s (exit-criterion threshold)

## Stage 2 — Phase 1 Exit (pre-paying-customer, up to ~2 000 endpoints)

**Must be validated in staging before Personel takes on any customer beyond the pilot.** This is Phase 1 exit criterion #17.

**Topology**: **2 replicas** using `ReplicatedMergeTree` + **ClickHouse Keeper** (embedded, 3-instance quorum). Replicas live on separate physical or virtual hosts.

Hosts:
```
ch-01  (ReplicatedMergeTree replica 1, Keeper instance 1)
ch-02  (ReplicatedMergeTree replica 2, Keeper instance 2)
ch-03  (Keeper-only witness)
```

**Why 2 replicas, not 3**: on-prem hardware budget for mid-market Turkish customers is tight. 2 replicas give us an HA pair, and the 3-instance Keeper ensures quorum without a third full ClickHouse node. Reads can be served from either replica; writes go to whichever is leader for that shard (one shard at this stage).

**Schema migration**: `MergeTree` → `ReplicatedMergeTree` using `ATTACH PARTITION` trick, zero-downtime, rehearsed twice in staging.

**Config changes**:
- `macros.xml` per host
- `remote_servers.xml` with one cluster definition, one shard, two replicas
- `keeper_server.xml` on the three hosts
- `users.xml` per-role grants unchanged
- Load balancer (HAProxy or Caddy stream) in front for client connections
- Writers (NATS consumers) use `Distributed` table over the cluster

**Backup**: same `clickhouse-backup`, now executed from one replica (`--tables ...` and `--remote`). Restore procedure rewritten to rebuild both replicas from the backup.

**Validation criteria** (staging before production):
- Kill replica 1 during ingest; ingest continues on replica 2 with < 500 ms write pause.
- Restart replica 1; it catches up within 5 minutes of the ingest rate.
- Read traffic failover is transparent to the Admin API (connection retry at the client).
- TTL drops work correctly under replication.
- Backup restore rebuilds a dead replica from MinIO in < 1 hour for 100 GB of data.

## Stage 3 — Phase 2 Scale-Out (beyond ~20 000 endpoints)

**Topology**: **sharded + replicated cluster**. Sharding key = `tenant_id` for multi-tenant deployments, or `cityHash64(endpoint_id) % N` for single-tenant-high-scale.

Initial shape:
```
shard_1: ch-11, ch-12 (replicas)
shard_2: ch-21, ch-22 (replicas)
shard_3: ch-31, ch-32 (replicas)
keeper quorum: 3-instance, co-located or separate
```

**Reasoning**:
- 20 000 endpoints × ~4.6 MB/day = ~92 GB/day metadata uncompressed, ~5–10 GB/day on disk.
- A single shard can absorb this, but query parallelism benefits greatly from sharding.
- Three shards provide linear write scaling headroom to ~60 000 endpoints without reshuffling.

**Write path**: NATS consumers write to the `Distributed` table; ClickHouse internal routing handles shard placement. Dead-letter on write errors goes back to NATS with exponential retry.

**Read path**: Admin API queries hit the `Distributed` table; fan-out and merge is handled by ClickHouse.

**Data balancing**: reshuffle is expensive; we plan shard count conservatively up-front to avoid it. If a customer grows past 60 000 endpoints, a fresh shard is added and `tenant_id`-based sharding isolates the rebalancing cost.

**Phase 2 exit criterion (future)**: sustained 10 000 endpoints at p95 < 2 s dashboard query under the sharded cluster.

## Phase 3 Considerations (informative only)

- **Columnar tiering**: cold-tier partitions offloaded to S3 via `MergeTree` S3 disk, keeping only hot+warm on local NVMe.
- **Query caching layer**: Redis or ClickHouse query cache in front of the common dashboard queries (top apps, active time).
- **Async Materialized Views**: for heavy aggregations (per-tenant daily rollups) refreshed hourly.
- **Geographic distribution**: only relevant after SaaS phase; not applicable on-prem.

## Phase 1 Exit Criterion (added to `mvp-scope.md`)

> **#17** — Stage 2 topology (2-replica `ReplicatedMergeTree` + Keeper) validated end-to-end in staging with failover, catch-up, backup restore, and TTL correctness, before Personel takes on any customer beyond the pilot.

This exit criterion is non-waivable: it is the gate between "pilot" and "first real production customer."

## Operational Runbook Hooks

- Daily backup verification: `clickhouse-backup list` + random restore sample.
- Weekly: TTL enforcement verification (count of rows older than their category's TTL should be 0 modulo in-flight merges).
- Monthly: disaster-recovery drill (Stage 2 onward).
- On Keeper quorum loss: documented procedure to restart Keeper cluster and re-elect leaders.

## Related

- `docs/adr/0004-clickhouse-for-timeseries.md`
- `docs/architecture/data-retention-matrix.md`
- `docs/architecture/mvp-scope.md` (exit criterion #17)
- Future: `docs/runbooks/clickhouse-operations.md` (to be authored by devops-engineer)
