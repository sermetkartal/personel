# Chaos Drill — minio-mirror-partition

**Faz 14 #151** — validates MinIO site replication (mirror) under
a network partition between the primary on vm3 and the mirror on vm5.

## Objective

Verify that when `mc admin replicate` traffic is blocked between
the two MinIO deployments:

1. Writes on primary continue uninterrupted
2. Reads from mirror serve stale but consistent data
3. Replication backlog grows but does not corrupt objects
4. Backlog drains to zero within 5 minutes of heal

## Pre-requisites

- Site replication configured: `mc admin replicate status primary mirror`
  returns `Mode: active-active` or `one-way`
- Both deployments are green: `mc admin info primary` / `mc admin info mirror`
- Baseline bucket counts snapshotted
- No in-flight large uploads (> 100 MB)

## Setup

```bash
# Snapshot state
mc admin replicate status primary mirror > /tmp/replicate-before.txt

# Count objects per bucket
for bucket in events-raw events-sensitive screenshots audit-worm; do
  mc ls --recursive primary/$bucket | wc -l
  mc ls --recursive mirror/$bucket | wc -l
done > /tmp/counts-before.txt
```

## Inject

```bash
# Block the replication port (9000) between vm3 and vm5 in both directions
ssh kartal@192.168.5.44 'sudo iptables -I OUTPUT -d 192.168.5.32 -p tcp --dport 9000 -j DROP'
ssh kartal@192.168.5.32 'sudo iptables -I OUTPUT -d 192.168.5.44 -p tcp --dport 9000 -j DROP'
```

## Monitor

Duration: 3 minutes.

1. Write canary objects to primary every 15s:
```bash
for i in $(seq 1 12); do
  echo "chaos-mirror-$i-$(date +%s)" | \
    mc pipe primary/events-raw/chaos/canary-$i.txt
  sleep 15
done
```

2. Check mirror backlog growing:
```bash
mc admin replicate status primary mirror | grep -i "pending"
```
Expected: pending count rises from 0 to ~12 over 3 minutes.

3. Verify the mirror serves stale reads (should not see the
   canaries):
```bash
mc ls mirror/events-raw/chaos/
```

4. **Negative check**: confirm no object corruption on either
   side. Pick a random existing object on both sides and
   compare ETags:
```bash
mc stat primary/events-raw/<existing-key>
mc stat mirror/events-raw/<existing-key>
```
ETags must match.

## Recover

```bash
ssh kartal@192.168.5.44 'sudo iptables -D OUTPUT -d 192.168.5.32 -p tcp --dport 9000 -j DROP'
ssh kartal@192.168.5.32 'sudo iptables -D OUTPUT -d 192.168.5.44 -p tcp --dport 9000 -j DROP'
```

Start backlog-drain timer.

## Pass criteria

| Metric | Target |
|---|---|
| Primary write success during partition | 100% |
| Mirror read success (stale) during partition | 100% |
| Object corruption (either side) | 0 |
| Backlog drain time after heal | < 5 minutes |
| Canary visibility on mirror post-heal | all 12 |

## Post-drill

```bash
# Verify canaries now exist on mirror
for i in $(seq 1 12); do
  mc stat mirror/events-raw/chaos/canary-$i.txt || echo "MISSING $i"
done

# Clean up canaries
mc rm --recursive --force primary/events-raw/chaos/

# Snapshot replication status
mc admin replicate status primary mirror > /tmp/replicate-after.txt
diff /tmp/replicate-before.txt /tmp/replicate-after.txt
```

If any canary is missing on mirror post-heal → open incident,
escalate to MinIO support, do NOT trust the mirror for restore
until re-validated.
