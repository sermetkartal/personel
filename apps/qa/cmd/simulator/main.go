// cmd/simulator/main.go — the multi-agent load test driver.
//
// Connects up to 10K synthetic agents to a Personel gateway and streams
// realistic event batches. Scenario parameters come from a JSON file.
//
// Usage:
//
//	simulator run --scenario test/load/scenarios/500_steady.json \
//	             --gateway localhost:9443 \
//	             --report ./reports
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/personel/qa/test/load"
)

func main() {
	root := &cobra.Command{
		Use:   "simulator",
		Short: "Personel synthetic agent load simulator",
		Long: `Simulator runs up to 10K synthetic agents against a Personel gateway.
Each agent performs mTLS handshake, key version negotiation, and streams
realistic events according to the event taxonomy distribution.`,
	}

	var (
		scenarioFile  string
		gatewayAddr   string
		reportDir     string
		thresholds    string
		showProgress  bool
		verbose       bool
	)

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a load scenario",
		RunE: func(cmd *cobra.Command, args []string) error {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			cfg := load.RunnerConfig{
				ScenarioFile:   scenarioFile,
				GatewayAddr:    gatewayAddr,
				ReportPath:     reportDir,
				ThresholdsFile: thresholds,
				ShowProgress:   showProgress,
			}

			runner := load.NewRunner(cfg)
			result, err := runner.Run(ctx)
			if err != nil {
				return fmt.Errorf("run failed: %w", err)
			}

			reporter := &load.Reporter{OutputDir: reportDir}
			reporter.PrintSummary(result)

			if !result.Passed {
				os.Exit(1)
			}
			return nil
		},
	}

	runCmd.Flags().StringVarP(&scenarioFile, "scenario", "s", "", "Path to scenario JSON file (required)")
	runCmd.Flags().StringVarP(&gatewayAddr, "gateway", "g", "", "Gateway address host:port (overrides scenario JSON)")
	runCmd.Flags().StringVarP(&reportDir, "report", "r", "./reports", "Directory for output reports")
	runCmd.Flags().StringVar(&thresholds, "thresholds", "ci/thresholds.yaml", "Path to thresholds YAML")
	runCmd.Flags().BoolVar(&showProgress, "progress", true, "Show ramp-up progress bar")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	_ = runCmd.MarkFlagRequired("scenario")

	listCmd := &cobra.Command{
		Use:   "list-scenarios",
		Short: "List available load scenarios",
		RunE: func(cmd *cobra.Command, args []string) error {
			scenarios := []string{
				"test/load/scenarios/500_steady.json     — 500 agents, 30-min steady (pilot validation)",
				"test/load/scenarios/10k_ramp.json       — 10K agents, 5-min ramp + 30-min steady (scale cliff)",
				"test/load/scenarios/10k_burst.json      — 10K agents, synchronized burst every 60s",
				"test/load/scenarios/chaos_mix.json      — 10K agents + gateway kill + NATS partition",
			}
			for _, s := range scenarios {
				fmt.Println(" ", s)
			}
			return nil
		},
	}

	root.AddCommand(runCmd, listCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
