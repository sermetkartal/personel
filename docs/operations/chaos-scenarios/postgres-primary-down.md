# Chaos Drill — postgres-primary-down

**Faz 14 #151** — validates Postgres streaming replication failover
from vm3 primary to vm5 replica under an unplanned primary loss.

## Objective

Verify that when the primary Postgres container dies without a
clean shutdown, the replica on vm5 promotes to primary within the
agreed RTO of **30 seconds**, with zero committed transaction loss.

## Pre-requisites

- vm3 + vm5 both healthy, `pg_isready` returns 0 on both
- Replication state is `streaming` (not `catchup`)
- Monitoring dashboard is open: `last_wal_replay_lsn` widget
- Maintenance window with customer sign-off
- Agent fleet is idle (to bound data loss blast radius)

## Setup

```bash
# 1. Baseline — capture current LSN on both nodes
ssh kartal@192.168.5.44 'docker exec personel-postgres psql -U postgres -tc "SELECT pg_current_wal_lsn()"'
ssh kartal@192.168.5.32 'docker exec personel-postgres-replica psql -U postgres -tc "SELECT pg_last_wal_replay_lsn()"'

# 2. Write a canary row (used for data-loss accounting)
ssh kartal@192.168.5.44 'docker exec personel-postgres psql -U postgres -c \
  "INSERT INTO chaos_canary (marker, created_at) VALUES ('\''postgres-primary-down-'$(date +%s)'\'', now())"'
```

Record both LSNs and the canary marker in `chaos-report.md`.

## Inject

```bash
# Kill primary — no graceful shutdown
ssh kartal@192.168.5.44 'docker kill personel-postgres'
```

**Start timer.** Expected: HAProxy / consul health check fails
within 5s, pg_rewind fires on vm5, replica promotes.

## Monitor

For 2 minutes after inject:

1. Every second, attempt a write via the VIP:

```bash
while true; do
  ssh kartal@192.168.5.32 'docker exec personel-postgres-replica psql -U postgres \
    -c "INSERT INTO chaos_canary(marker) VALUES (now()::text)"' && break
  sleep 1
done
```

2. Record the first successful write — **RTO measured**.
3. SELECT the canary table and verify the Setup canary row survived.
4. Check `pg_stat_replication` on the new primary — zero replicas
   expected (until old primary is rejoined).

## Recover

```bash
# 1. Restart the old primary container
ssh kartal@192.168.5.44 'docker start personel-postgres'

# 2. Let it detect it's behind, then pg_rewind against the new primary
ssh kartal@192.168.5.44 'docker exec personel-postgres pg_rewind \
  --target-pgdata=/var/lib/postgresql/data \
  --source-server="host=192.168.5.32 port=5432 user=replicator dbname=postgres"'

# 3. Start in standby mode
ssh kartal@192.168.5.44 'docker exec personel-postgres pg_ctl start \
  -D /var/lib/postgresql/data -m fast -w'

# 4. Verify replication state
ssh kartal@192.168.5.32 'docker exec personel-postgres-replica psql -U postgres \
  -c "SELECT application_name, state, sync_state FROM pg_stat_replication"'
```

Expected: `state=streaming`, `sync_state=async` (or `sync` if
synchronous replication is enabled).

## Pass criteria

| Metric | Target | Measured |
|---|---|---|
| Recovery time (first successful write) | < 30s | fill in |
| Committed transaction loss | 0 | fill in |
| Split-brain writes | 0 | fill in |
| Chain integrity post-recovery | preserved | fill in |

## Post-drill

1. Run `audit.verify_hash_chain` on both nodes — audit chain must be intact.
2. File a chaos-report.md in `docs/operations/chaos-reports/YYYY-MM-DD-postgres-primary-down.md`.
3. If any pass criterion missed → open an incident, block further Faz 14 testing.

## Rollback-only path (if drill goes wrong)

```bash
# If pg_rewind fails, re-provision vm3 from a fresh base backup:
ssh kartal@192.168.5.44 'docker rm -f personel-postgres && rm -rf /var/lib/personel/postgres/data/*'
ssh kartal@192.168.5.44 'docker exec personel-postgres pg_basebackup \
  -h 192.168.5.32 -U replicator -D /var/lib/postgresql/data -Fp -Xs -P -R'
```
