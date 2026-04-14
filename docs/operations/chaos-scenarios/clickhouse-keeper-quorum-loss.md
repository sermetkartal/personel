# Chaos Drill — clickhouse-keeper-quorum-loss

**Faz 14 #151** — validates that ClickHouse keeps serving reads
when one ClickHouse Keeper node is lost, and degrades writes
gracefully rather than corrupting tables.

## Objective

The pilot runs 2 ClickHouse shards + 2 Keeper nodes. A keeper
quorum loss (one keeper down) is the most likely real-world
failure. This drill verifies:

1. SELECT continues to succeed
2. INSERT either commits to both replicas or fails fast (no
   hung sessions, no half-committed parts)
3. No table / part / metadata corruption

## Pre-requisites

- Both CH nodes green: `clickhouse-client -q "SELECT 1"`
- Both keepers green: `echo ruok | nc <keeper-host> 9181` returns `imok`
- `system.clusters` shows both shards and both replicas reachable
- Snapshot: `SELECT count(*) FROM events` on both nodes

## Setup

```bash
ssh kartal@192.168.5.44 'docker exec personel-clickhouse-01 clickhouse-client \
  -q "SELECT count(*) FROM events FORMAT TSV" > /tmp/count-before.txt'
ssh kartal@192.168.5.32 'docker exec personel-clickhouse-02 clickhouse-client \
  -q "SELECT count(*) FROM events FORMAT TSV" > /tmp/count-before.txt'
```

## Inject

```bash
# Kill keeper-02 (the second of the two keepers). keeper-01 remains.
ssh kartal@192.168.5.32 'docker stop personel-keeper-02'
```

## Monitor

For 5 minutes:

1. Read path — run every 10s:
```bash
clickhouse-client -h 192.168.5.44 -q "SELECT count(*) FROM events"
clickhouse-client -h 192.168.5.32 -q "SELECT count(*) FROM events"
```
Both must continue returning a sensible count.

2. Write path — run every 10s (from the enricher, observed
   via its logs):
```bash
docker logs personel-enricher --tail 20 | grep -i "clickhouse"
```
Expected: some writes may return `Code: 242, DB::Exception: too
many parts` or `KEEPER_EXCEPTION`. The enricher MUST NOT retry
forever — it should DLQ after N retries.

3. Check DLQ growth:
```bash
nats stream info events_raw_dlq --server nats://192.168.5.44:4222
```

## Recover

```bash
ssh kartal@192.168.5.32 'docker start personel-keeper-02'
# Wait for keeper to rejoin quorum
for i in $(seq 1 30); do
  echo ruok | nc 192.168.5.32 9181 && break
  sleep 2
done
```

## Pass criteria

| Metric | Target |
|---|---|
| SELECT success rate during inject | 100% |
| INSERT error rate during inject | < 20% (depending on keeper coordination) |
| Parts corruption post-recovery | 0 (run `CHECK TABLE events`) |
| DLQ replay success rate | 100% |
| Keeper quorum restore | < 60s post-recover |

## Post-drill

```bash
ssh kartal@192.168.5.44 'docker exec personel-clickhouse-01 clickhouse-client \
  -q "CHECK TABLE events"'
```
Expected: `1` (OK) for every part. If any part is `0`, the part is
corrupted → restore from replica via `SYSTEM SYNC REPLICA events`.

Replay the DLQ:
```bash
go run apps/qa/cmd/chaos/ replay-dlq --stream events_raw_dlq
```
