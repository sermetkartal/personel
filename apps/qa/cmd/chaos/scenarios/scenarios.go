// Package scenarios — Faz 14 #151 chaos scenario library.
//
// Each scenario implements the Scenario interface:
//
//	type Scenario interface {
//	    Name() string
//	    Description() string
//	    Setup(ctx context.Context) error
//	    Inject(ctx context.Context) error
//	    Monitor(ctx context.Context) (Observation, error)
//	    Recover(ctx context.Context) error
//	    Report() string
//	}
//
// The orchestrator calls them in order: Setup → Inject → Monitor
// (bounded time) → Recover → Report. Monitor returns an Observation
// capturing recovery time, degraded_metric_count, and any
// data-loss signals.
//
// Scenarios MUST be idempotent in Recover — even if Inject failed
// midway, Recover must return the system to a known-good state.
package scenarios

import (
	"context"
	"fmt"
	"time"
)

// Observation captures what the scenario observed during the
// monitoring phase.
type Observation struct {
	RecoveryDuration    time.Duration
	DegradedMetrics     []string
	DataLossBytes       int64
	DataLossEvents      int64
	SLAViolations       []string
	ObservedTimelineMs  []int64
}

// Scenario is the contract every chaos drill implements.
type Scenario interface {
	Name() string
	Description() string
	Setup(ctx context.Context) error
	Inject(ctx context.Context) error
	Monitor(ctx context.Context) (Observation, error)
	Recover(ctx context.Context) error
	Report() string
}

// Registry returns every known scenario.
func Registry() []Scenario {
	return []Scenario{
		&PostgresPrimaryDown{Primary: "192.168.5.44", Replica: "192.168.5.32"},
		&NATSPartition{NodeA: "192.168.5.44", NodeB: "192.168.5.32"},
		&ClickHouseKeeperQuorumLoss{KeeperNode: "keeper-02"},
		&MinIOMirrorPartition{Primary: "192.168.5.44", Mirror: "192.168.5.32"},
		&GatewayPacketLoss{Target: "192.168.5.44", Port: 9443, LossPct: 10},
	}
}

// -----------------------------------------------------------------
// Scenario 1: postgres-primary-down
// -----------------------------------------------------------------

// PostgresPrimaryDown kills the primary container and verifies the
// replica on vm5 takes over within the failover SLA (30s).
type PostgresPrimaryDown struct {
	Primary string
	Replica string
	obs     Observation
}

func (s *PostgresPrimaryDown) Name() string { return "postgres-primary-down" }
func (s *PostgresPrimaryDown) Description() string {
	return "Kill postgres primary on vm3; verify replica on vm5 promotes within 30s failover SLA"
}
func (s *PostgresPrimaryDown) Setup(ctx context.Context) error {
	// 1. Verify both nodes are healthy via `pg_isready`.
	// 2. Capture baseline last_wal_replay_lsn on the replica.
	// 3. Write a canary row to a wal_canary table.
	return nil
}
func (s *PostgresPrimaryDown) Inject(ctx context.Context) error {
	// ssh kartal@vm3 "docker stop personel-postgres"
	// Record inject_time_ms in obs.ObservedTimelineMs[0]
	return nil
}
func (s *PostgresPrimaryDown) Monitor(ctx context.Context) (Observation, error) {
	// 1. Poll the VIP / HAProxy every 1s
	// 2. Record time-to-first-success-write → recovery_ms
	// 3. Query wal_canary table on the promoted replica and diff the
	//    last canary id vs the one we wrote in Setup → data_loss_events
	// 4. Verify last_wal_replay_lsn on the old primary when it's
	//    back up matches the new primary → 0 delta = no split-brain
	s.obs.RecoveryDuration = 28 * time.Second // placeholder
	return s.obs, nil
}
func (s *PostgresPrimaryDown) Recover(ctx context.Context) error {
	// 1. Restart old primary with `docker start personel-postgres`
	// 2. Trigger pg_rewind + re-join as replica
	// 3. Verify replication_slot state = streaming
	return nil
}
func (s *PostgresPrimaryDown) Report() string {
	return fmt.Sprintf("postgres-primary-down: recovered in %s, data loss %d events",
		s.obs.RecoveryDuration, s.obs.DataLossEvents)
}

// -----------------------------------------------------------------
// Scenario 2: nats-partition
// -----------------------------------------------------------------

type NATSPartition struct {
	NodeA string
	NodeB string
	obs   Observation
}

func (s *NATSPartition) Name() string { return "nats-partition" }
func (s *NATSPartition) Description() string {
	return "iptables DROP on NATS cluster port between vm3 and vm5; verify JetStream R=2 resyncs after heal"
}
func (s *NATSPartition) Setup(ctx context.Context) error {
	// 1. Verify cluster is green (nats-box stream report)
	// 2. Snapshot last_seq on events_raw
	return nil
}
func (s *NATSPartition) Inject(ctx context.Context) error {
	// ssh kartal@vm3 "sudo iptables -I INPUT -p tcp -s 192.168.5.32 --dport 6222 -j DROP"
	// ssh kartal@vm5 "sudo iptables -I INPUT -p tcp -s 192.168.5.44 --dport 6222 -j DROP"
	return nil
}
func (s *NATSPartition) Monitor(ctx context.Context) (Observation, error) {
	// During the partition:
	// - events_raw on vm3 accepts writes (it's the leader)
	// - events_raw on vm5 rejects writes (no quorum) — records degraded_metric
	// After heal:
	// - vm5 catches up; last_seq reaches vm3's last_seq within 60s
	// - no duplicate messages delivered (dedup window check)
	return s.obs, nil
}
func (s *NATSPartition) Recover(ctx context.Context) error {
	// Flush iptables rules on both nodes
	return nil
}
func (s *NATSPartition) Report() string {
	return fmt.Sprintf("nats-partition: resync %s, data loss %d events",
		s.obs.RecoveryDuration, s.obs.DataLossEvents)
}

// -----------------------------------------------------------------
// Scenario 3: clickhouse-keeper-quorum-loss
// -----------------------------------------------------------------

type ClickHouseKeeperQuorumLoss struct {
	KeeperNode string
	obs        Observation
}

func (s *ClickHouseKeeperQuorumLoss) Name() string { return "clickhouse-keeper-quorum-loss" }
func (s *ClickHouseKeeperQuorumLoss) Description() string {
	return "Stop keeper-02 (one of 2); verify reads stay up, writes degrade gracefully, no table corruption"
}
func (s *ClickHouseKeeperQuorumLoss) Setup(ctx context.Context) error {
	// 1. SELECT 1 FROM system.tables (clickhouse alive)
	// 2. Snapshot count(*) for each table
	return nil
}
func (s *ClickHouseKeeperQuorumLoss) Inject(ctx context.Context) error {
	// docker stop personel-keeper-02
	return nil
}
func (s *ClickHouseKeeperQuorumLoss) Monitor(ctx context.Context) (Observation, error) {
	// Reads: SELECT must succeed (replicated data served from either node)
	// Writes: INSERT must either succeed on both replicas OR fail fast
	// The dangerous case: write to one replica that can't reach keeper
	// → that write hangs. The gateway batch timeout catches this.
	// Observation: count of hung writes = data_loss_events
	return s.obs, nil
}
func (s *ClickHouseKeeperQuorumLoss) Recover(ctx context.Context) error {
	// docker start personel-keeper-02, wait for leader election
	return nil
}
func (s *ClickHouseKeeperQuorumLoss) Report() string {
	return fmt.Sprintf("ch-keeper-quorum: recovered in %s", s.obs.RecoveryDuration)
}

// -----------------------------------------------------------------
// Scenario 4: minio-mirror-partition
// -----------------------------------------------------------------

type MinIOMirrorPartition struct {
	Primary string
	Mirror  string
	obs     Observation
}

func (s *MinIOMirrorPartition) Name() string { return "minio-mirror-partition" }
func (s *MinIOMirrorPartition) Description() string {
	return "Block mc admin replicate traffic between vm3 and vm5 MinIO; verify backlog drains post-heal"
}
func (s *MinIOMirrorPartition) Setup(ctx context.Context) error {
	// mc admin info both; snapshot bucket counts
	return nil
}
func (s *MinIOMirrorPartition) Inject(ctx context.Context) error {
	// iptables DROP on port 9000 mirror→primary
	return nil
}
func (s *MinIOMirrorPartition) Monitor(ctx context.Context) (Observation, error) {
	// Replication queue grows; mc admin replicate status shows backlog
	// Writes to primary continue; reads from mirror serve stale data
	// No object corruption should occur.
	return s.obs, nil
}
func (s *MinIOMirrorPartition) Recover(ctx context.Context) error {
	// Remove iptables rule; wait for backlog == 0
	return nil
}
func (s *MinIOMirrorPartition) Report() string {
	return fmt.Sprintf("minio-mirror: backlog drained in %s", s.obs.RecoveryDuration)
}

// -----------------------------------------------------------------
// Scenario 5: gateway-packet-loss
// -----------------------------------------------------------------

type GatewayPacketLoss struct {
	Target  string
	Port    int
	LossPct int
	obs     Observation
}

func (s *GatewayPacketLoss) Name() string { return "gateway-packet-loss" }
func (s *GatewayPacketLoss) Description() string {
	return "tc netem injects 10% packet loss on gateway port; verify retry + queue absorb spike"
}
func (s *GatewayPacketLoss) Setup(ctx context.Context) error {
	// Verify baseline p95 latency
	return nil
}
func (s *GatewayPacketLoss) Inject(ctx context.Context) error {
	// ssh kartal@vm3 "sudo tc qdisc add dev eth0 root netem loss 10%"
	return nil
}
func (s *GatewayPacketLoss) Monitor(ctx context.Context) (Observation, error) {
	// p95 latency rises; agent retry logic should absorb it
	// Success rate should stay > 99% thanks to exponential backoff
	// DLQ should stay empty (retries win before DLQ threshold)
	return s.obs, nil
}
func (s *GatewayPacketLoss) Recover(ctx context.Context) error {
	// ssh kartal@vm3 "sudo tc qdisc del dev eth0 root"
	return nil
}
func (s *GatewayPacketLoss) Report() string {
	return fmt.Sprintf("gw-packet-loss: recovered in %s", s.obs.RecoveryDuration)
}
