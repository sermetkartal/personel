# Chaos Drill — nats-partition

**Faz 14 #151** — validates JetStream cluster (R=2) behaviour under
a network partition between vm3 and vm5.

## Objective

Confirm that when cluster communication between the two NATS nodes
is blocked, the cluster:

1. Keeps accepting writes on the majority side (whichever holds
   the leader for each stream)
2. Rejects writes on the minority side (no quorum)
3. Resyncs cleanly once the partition heals — **no duplicate
   events delivered, no gaps in the stream**

## Pre-requisites

- Cluster green: `nats server check connection -s nats://192.168.5.44:4222`
- Stream report shows `events_raw`, `events_sensitive`,
  `live_view_control`, `agent_health`, `pki_events` all replicated
  with `num_replicas=2`
- iptables rules currently empty on both nodes (`sudo iptables -L
  INPUT -n | grep 6222` returns empty)

## Setup

```bash
# 1. Snapshot stream state on both nodes
nats stream info events_raw --server nats://192.168.5.44:4222 > before-vm3.txt
nats stream info events_raw --server nats://192.168.5.32:4222 > before-vm5.txt

# 2. Seed a marker message so we can prove "no duplicates"
nats pub events.raw.marker.$(date +%s) "chaos-partition-start" \
  --server nats://192.168.5.44:4222
```

## Inject

```bash
# Block cluster port 6222 both directions
ssh kartal@192.168.5.44 'sudo iptables -I INPUT -p tcp -s 192.168.5.32 --dport 6222 -j DROP'
ssh kartal@192.168.5.32 'sudo iptables -I INPUT -p tcp -s 192.168.5.44 --dport 6222 -j DROP'
```

## Monitor

Duration: 3 minutes.

1. Observe cluster state on each node:
```bash
nats server list --server nats://192.168.5.44:4222
nats server list --server nats://192.168.5.32:4222
```
Expected: each sees the other as "unresponsive", cluster routes degraded.

2. Try publishing on both sides:
```bash
for i in $(seq 1 60); do
  nats pub events.raw.partition.$i "side-vm3-msg-$i" --server nats://192.168.5.44:4222
  sleep 0.5
done
for i in $(seq 1 60); do
  nats pub events.raw.partition.$i "side-vm5-msg-$i" --server nats://192.168.5.32:4222
  sleep 0.5
done
```

The side that holds the stream leader will accept; the other will
timeout or error. Note which side was which.

3. Record degradation metrics in Prometheus during partition:
   - `nats_jetstream_stream_messages{stream="events_raw"}` on each node
   - `nats_varz_leafnodes` should show 0 peers

## Recover

```bash
ssh kartal@192.168.5.44 'sudo iptables -F INPUT'
ssh kartal@192.168.5.32 'sudo iptables -F INPUT'
```

Start recovery timer.

## Pass criteria

| Metric | Target |
|---|---|
| Cluster re-converges | < 60s after heal |
| Minority-side writes during partition | all rejected or queued with clear error |
| Duplicate messages delivered to consumers | 0 |
| Stream gaps on events_raw | 0 |
| Leader re-election needed | at most 1 per stream |

## Post-drill

Run:
```bash
nats stream report --server nats://192.168.5.44:4222 > after-vm3.txt
nats stream report --server nats://192.168.5.32:4222 > after-vm5.txt
diff before-vm3.txt after-vm3.txt
```

Check enricher's DLQ hasn't grown. If any pass criterion missed,
file an incident and freeze deployments.
