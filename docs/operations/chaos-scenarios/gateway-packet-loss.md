# Chaos Drill — gateway-packet-loss

**Faz 14 #151** — validates that agent retry + offline queue
absorb a 10% packet loss spike on the gateway ingress without
data loss or user-visible alerts.

## Objective

Use Linux `tc netem` to inject 10% packet loss on the gateway
port (9443) and verify:

1. Agents' retry-with-exponential-backoff wins — at most a few
   batches are delayed, none are lost
2. Agent offline queue absorbs any spillover (< 100 MB growth)
3. No entries hit the DLQ
4. p95 ingest latency rises but stays under 3× baseline
5. Customer-visible dashboard alerts do NOT fire (the 10% loss is
   within the expected degradation envelope)

## Pre-requisites

- At least 100 agents connected (or simulator running)
- Baseline p95 latency < 100 ms
- DLQ empty (`nats stream info events_raw_dlq` → `Messages: 0`)
- `tc` available on vm3: `sudo tc qdisc show dev eth0`

## Setup

```bash
# 1. Note the active network interface on vm3
ssh kartal@192.168.5.44 'ip -brief addr show | grep 192.168.5.44'
# Expected output: eth0 (or ens... on cloud instances)

# 2. Snapshot baseline metrics
curl -s 192.168.5.44:9464/metrics | grep -E 'personel_gateway_.*_p95|personel_dlq_'
```

## Inject

```bash
# 10% packet loss, all outbound traffic on the gateway interface
ssh kartal@192.168.5.44 'sudo tc qdisc add dev eth0 root netem loss 10%'

# Confirm active
ssh kartal@192.168.5.44 'sudo tc qdisc show dev eth0'
```

## Monitor

Duration: 5 minutes.

1. Every 30s, record:
```bash
curl -s 192.168.5.44:9464/metrics | \
  awk '/personel_gateway_stream_recv_latency_bucket/ { print }' | tail -5
```

2. Watch agent offline queue (on a test agent):
```powershell
Get-Item 'C:\ProgramData\Personel\agent\queue.db' | Select Length
```
Should grow, but by < 100 MB.

3. Watch DLQ:
```bash
watch -n 5 'nats stream info events_raw_dlq --server nats://192.168.5.44:4222 | grep Messages'
```
Must stay at 0.

4. Check agent logs for retry storms:
```powershell
Get-Content C:\ProgramData\Personel\agent\agent.log -Tail 100 | Select-String 'retry'
```
Expected: retries at ~10% of attempts, backoff visible.

## Recover

```bash
ssh kartal@192.168.5.44 'sudo tc qdisc del dev eth0 root'
ssh kartal@192.168.5.44 'sudo tc qdisc show dev eth0'
# Expected: empty / default pfifo_fast
```

## Pass criteria

| Metric | Target |
|---|---|
| Agent-side batches lost (never delivered) | 0 |
| DLQ growth during inject | 0 messages |
| p95 ingest latency during inject | < 3× baseline |
| Offline queue growth on any agent | < 100 MB |
| Customer dashboards: critical alerts fired | 0 |

## Post-drill

1. Wait 2 minutes for agents to drain their offline queues.
2. Run a ClickHouse count vs NATS delivered count:
```bash
clickhouse-client -q "SELECT count(*) FROM events WHERE timestamp > now() - INTERVAL 10 MINUTE"
```
Must match (within 1%) the total number of batches the agents
believe they sent.

3. If any metric missed → open incident. The most likely root
   cause is insufficient offline queue capacity; bump the SQLite
   max size in agent config.

## Notes

- `tc netem` persists until the interface is reconfigured — do
  NOT forget the `tc qdisc del` in Recover.
- On cloud-hosted runners, confirm the hypervisor doesn't
  rate-limit below the induced loss (breaks the experiment).
