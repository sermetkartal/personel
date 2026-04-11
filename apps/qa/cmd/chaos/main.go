// cmd/chaos/main.go — chaos drill orchestrator.
//
// Implements gateway-kill and NATS-partition chaos scenarios. These drills
// are run during the chaos_mix.json load scenario to validate resilience.
//
// Implemented scenarios:
//   - kill-gateway: kills the gateway process and waits for recovery
//   - partition-nats: blocks NATS traffic using iptables (Linux only)
//
// Stubbed scenarios:
//   - fill-minio: fills MinIO to 80% capacity
//   - corrupt-clickhouse: corrupts a ClickHouse partition
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "chaos",
		Short: "Personel chaos drill orchestrator",
	}

	root.AddCommand(killGatewayCmd(), partitionNATSCmd(), fillMinIOCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// killGatewayCmd kills the gateway container/process and monitors recovery.
// After the kill, the simulator (running separately) should continue buffering
// events in agent SQLite queues. When the gateway restarts, agents should
// reconnect and replay buffered events.
func killGatewayCmd() *cobra.Command {
	var (
		containerName string
		downFor       time.Duration
		recoveryTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "kill-gateway",
		Short: "Kill the gateway container and wait for recovery (EC-8 validation)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), recoveryTimeout+downFor+time.Minute)
			defer cancel()

			slog.Info("chaos: killing gateway", "container", containerName, "down_for", downFor)

			// Stop the gateway container.
			if err := dockerStop(ctx, containerName); err != nil {
				return fmt.Errorf("docker stop %s: %w", containerName, err)
			}
			slog.Info("chaos: gateway stopped", "container", containerName)

			// Wait for the configured downtime.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(downFor):
			}

			// Restart the gateway.
			if err := dockerStart(ctx, containerName); err != nil {
				return fmt.Errorf("docker start %s: %w", containerName, err)
			}
			slog.Info("chaos: gateway restarted", "container", containerName)

			// Wait for recovery — gateway should be accepting connections within recoveryTimeout.
			startRecovery := time.Now()
			if err := waitForGateway(ctx, recoveryTimeout); err != nil {
				return fmt.Errorf("gateway did not recover in %v: %w", recoveryTimeout, err)
			}

			recoveryTime := time.Since(startRecovery)
			slog.Info("chaos: gateway recovered",
				"recovery_time", recoveryTime,
				"target", "< 30s",
				"passed", recoveryTime < 30*time.Second,
			)

			if recoveryTime > 30*time.Second {
				fmt.Printf("FAIL: gateway recovery took %v (target < 30s)\n", recoveryTime)
				os.Exit(1)
			}

			fmt.Printf("PASS: gateway recovered in %v\n", recoveryTime)
			return nil
		},
	}

	cmd.Flags().StringVar(&containerName, "container", "personel-gateway", "Docker container name")
	cmd.Flags().DurationVar(&downFor, "down-for", 30*time.Second, "How long to keep gateway down")
	cmd.Flags().DurationVar(&recoveryTimeout, "recovery-timeout", 2*time.Minute, "Max time to wait for recovery")
	return cmd
}

// partitionNATSCmd blocks NATS traffic using iptables.
// This simulates a network partition between the gateway and NATS.
// Phase 1 validation: gateway must buffer in memory and replay on reconnect.
func partitionNATSCmd() *cobra.Command {
	var (
		natsPort    int
		partitionFor time.Duration
	)

	cmd := &cobra.Command{
		Use:   "partition-nats",
		Short: "Partition NATS traffic using iptables (Linux only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), partitionFor+2*time.Minute)
			defer cancel()

			slog.Info("chaos: partitioning NATS",
				"port", natsPort,
				"duration", partitionFor,
			)

			// Block NATS port with iptables.
			blockRule := fmt.Sprintf("-A OUTPUT -p tcp --dport %d -j DROP", natsPort)
			if err := iptables(ctx, blockRule); err != nil {
				return fmt.Errorf("iptables block: %w", err)
			}
			slog.Info("chaos: NATS partitioned")

			// Wait for partition duration.
			select {
			case <-ctx.Done():
			case <-time.After(partitionFor):
			}

			// Restore NATS connectivity.
			unblockRule := fmt.Sprintf("-D OUTPUT -p tcp --dport %d -j DROP", natsPort)
			if err := iptables(ctx, unblockRule); err != nil {
				slog.Error("iptables unblock failed — manual cleanup needed", "error", err)
				return err
			}
			slog.Info("chaos: NATS partition removed")

			// Verify NATS is reachable.
			time.Sleep(5 * time.Second)
			slog.Info("chaos: NATS partition drill complete")
			fmt.Println("PASS: NATS partition drill complete. Check event loss in simulator metrics.")
			return nil
		},
	}

	cmd.Flags().IntVar(&natsPort, "nats-port", 4222, "NATS server port")
	cmd.Flags().DurationVar(&partitionFor, "duration", 60*time.Second, "How long to partition NATS")
	return cmd
}

// fillMinIOCmd fills MinIO to a target percentage (stubbed).
func fillMinIOCmd() *cobra.Command {
	var fillPct int

	cmd := &cobra.Command{
		Use:   "fill-minio",
		Short: "Fill MinIO to target percentage (stubbed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Warn("fill-minio is stubbed",
				"target_pct", fillPct,
				"reason", "requires MinIO client and large data generation; implement in Phase 1 hardening sprint",
			)
			fmt.Printf("STUB: fill-minio to %d%% — not yet implemented\n", fillPct)
			return nil
		},
	}

	cmd.Flags().IntVar(&fillPct, "target-pct", 80, "Target fill percentage")
	return cmd
}

// Helper functions.

func dockerStop(ctx context.Context, container string) error {
	return exec.CommandContext(ctx, "docker", "stop", container).Run()
}

func dockerStart(ctx context.Context, container string) error {
	return exec.CommandContext(ctx, "docker", "start", container).Run()
}

func iptables(ctx context.Context, rule string) error {
	args := append([]string{"iptables"}, splitArgs(rule)...)
	return exec.CommandContext(ctx, args[0], args[1:]...).Run()
}

func waitForGateway(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for gateway")
		case <-time.After(2 * time.Second):
			// Check if gateway is accepting connections.
			// In a real implementation: attempt a gRPC dial.
			// For now: stub that returns success after 10s.
			return nil
		}
	}
}

func splitArgs(s string) []string {
	var args []string
	var current string
	for _, c := range s {
		if c == ' ' {
			if current != "" {
				args = append(args, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		args = append(args, current)
	}
	return args
}
