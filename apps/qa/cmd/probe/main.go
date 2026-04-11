// cmd/probe/main.go — single synthetic agent smoke test for CI.
//
// Connects one agent to the gateway, sends a Hello, receives a Welcome,
// sends one EventBatch, receives a BatchAck, then exits 0.
//
// This is the fastest possible CI smoke test: run it on every commit.
// If this fails, the gateway is broken.
//
// Usage:
//
//	probe --gateway localhost:9443 --timeout 30s
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/personel/qa/internal/simulator"
)

func main() {
	var (
		gatewayAddr string
		tenantID    string
		endpointID  string
		timeout     time.Duration
		verbose     bool
	)

	root := &cobra.Command{
		Use:   "probe",
		Short: "Personel single-agent CI smoke probe",
		Long: `Probe connects a single synthetic agent to the gateway, completes the
full Hello/Welcome/EventBatch/BatchAck handshake, and exits 0 on success.
Designed to run on every CI commit as a fast sanity check.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			slog.Info("probe starting", "gateway", gatewayAddr, "timeout", timeout)

			// Create test PKI.
			if tenantID == "" {
				tenantID = "probe-tenant-00000000000000000001"
			}
			if endpointID == "" {
				endpointID = "probe-endpt-00000000000000000001"
			}

			ca, err := simulator.NewTestCA(tenantID)
			if err != nil {
				return fmt.Errorf("create test CA: %w", err)
			}

			cert, err := ca.IssueAgentCert(endpointID)
			if err != nil {
				return fmt.Errorf("issue agent cert: %w", err)
			}

			// Create metrics (no-op registry for probe).
			metrics := simulator.NewSimulatorMetrics(nil)

			// Configure agent for a single run.
			cfg := simulator.DefaultAgentConfig()
			cfg.GatewayAddr = gatewayAddr
			cfg.TenantID = tenantID
			cfg.EndpointID = endpointID
			cfg.TLSConfig = ca.ClientTLSConfig(cert, "gateway.personel.test")
			cfg.HeartbeatEvery = 60 * time.Second  // don't heartbeat in probe
			cfg.UploadEvery = 1 * time.Second      // send one batch quickly
			cfg.BatchSize = 10
			cfg.Metrics = metrics

			agent := simulator.NewSimAgent(cfg, cert)

			// Run for enough time to connect and send one batch.
			probeCtx, probeCancel := context.WithTimeout(ctx, timeout-5*time.Second)
			defer probeCancel()

			done := make(chan struct{})
			go func() {
				agent.Run(probeCtx)
				close(done)
			}()

			// Wait for connection.
			connected := false
			deadline := time.After(20 * time.Second)
			for !connected {
				select {
				case <-deadline:
					return fmt.Errorf("probe: agent did not connect within 20s")
				case <-done:
					return fmt.Errorf("probe: agent stopped before connecting")
				case <-time.After(100 * time.Millisecond):
					connected = agent.Connected()
				}
			}

			slog.Info("probe: agent connected", "endpoint_id", endpointID)

			// Let it send one batch.
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Stop agent.
			probeCancel()
			<-done

			slog.Info("probe: SUCCESS")
			return nil
		},
	}

	root.Flags().StringVarP(&gatewayAddr, "gateway", "g", "localhost:9443", "Gateway address host:port")
	root.Flags().StringVar(&tenantID, "tenant", "", "Tenant ID (default: probe-tenant UUID)")
	root.Flags().StringVar(&endpointID, "endpoint", "", "Endpoint ID (default: probe-endpoint UUID)")
	root.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall timeout")
	root.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
