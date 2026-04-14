// cmd/stress-test/main.go — Faz 14 #150.
//
// Linear-ramp stress test that finds the breaking point of the
// Personel pilot stack. Starts with 100 synthetic agents, adds 100
// every 2 minutes, and continues until the error rate crosses 1%
// OR the 30-minute ceiling is hit.
//
// The breaking point (N agents where error_rate > 1%) is recorded
// in stress-profile.csv along with a per-stage resource snapshot.
//
// Usage:
//
//	stress-test \
//	  --gateway 192.168.5.44:9443 \
//	  --tenant-id be459dac-... \
//	  --start 100 \
//	  --step 100 \
//	  --step-duration 2m \
//	  --max-duration 30m \
//	  --error-threshold 0.01 \
//	  --out stress-profile.csv \
//	  --report stress-report.md
//
// This binary does NOT run live in CI; it is an operator tool for
// capacity planning. It compiles and can be smoke-tested against
// a local dev stack.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type stageResult struct {
	Stage           int
	AgentCount      int
	StartedAt       time.Time
	DurationSec     float64
	EventsSent      int64
	EventsAcked     int64
	ErrorCount      int64
	ErrorRate       float64
	LatencyP50Ms    float64
	LatencyP95Ms    float64
	LatencyP99Ms    float64
	GatewayCPUPct   float64
	GatewayRSSMB    float64
	APICPUPct       float64
	NATSQueueDepth  int64
	ClickHouseWrMs  float64
	Broken          bool
}

type config struct {
	gateway        string
	tenantID       string
	startAgents    int
	stepAgents     int
	stepDuration   time.Duration
	maxDuration    time.Duration
	errorThreshold float64
	outCSV         string
	reportMD       string
	seed           uint64
}

func main() {
	cfg := parseFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ctx, deadline := context.WithTimeout(ctx, cfg.maxDuration)
	defer deadline()

	fmt.Printf("[stress] start agents=%d step=%d stepDur=%s maxDur=%s\n",
		cfg.startAgents, cfg.stepAgents, cfg.stepDuration, cfg.maxDuration)

	var results []stageResult
	agents := cfg.startAgents
	stage := 0

	for {
		if ctx.Err() != nil {
			break
		}
		stage++
		fmt.Printf("[stress] stage %d: ramp to %d agents\n", stage, agents)

		stageCtx, stageCancel := context.WithTimeout(ctx, cfg.stepDuration)
		result := runStage(stageCtx, cfg, stage, agents)
		stageCancel()

		results = append(results, result)
		fmt.Printf("[stress] stage %d done: error_rate=%.4f p95=%.1fms\n",
			stage, result.ErrorRate, result.LatencyP95Ms)

		if result.ErrorRate >= cfg.errorThreshold {
			result.Broken = true
			results[len(results)-1] = result
			fmt.Printf("[stress] BREAKING POINT reached at %d agents (error_rate=%.4f)\n",
				agents, result.ErrorRate)
			break
		}
		agents += cfg.stepAgents
	}

	if err := writeCSV(cfg.outCSV, results); err != nil {
		fmt.Fprintf(os.Stderr, "write csv: %v\n", err)
		os.Exit(2)
	}
	if err := writeReport(cfg.reportMD, cfg, results); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("[stress] done. stages=%d csv=%s report=%s\n", len(results), cfg.outCSV, cfg.reportMD)
}

func parseFlags() *config {
	cfg := &config{}
	flag.StringVar(&cfg.gateway, "gateway", "192.168.5.44:9443", "gateway addr")
	flag.StringVar(&cfg.tenantID, "tenant-id", "", "tenant uuid")
	flag.IntVar(&cfg.startAgents, "start", 100, "starting agent count")
	flag.IntVar(&cfg.stepAgents, "step", 100, "agents added per stage")
	flag.DurationVar(&cfg.stepDuration, "step-duration", 2*time.Minute, "time per stage")
	flag.DurationVar(&cfg.maxDuration, "max-duration", 30*time.Minute, "global ceiling")
	flag.Float64Var(&cfg.errorThreshold, "error-threshold", 0.01, "stop when error rate exceeds this")
	flag.StringVar(&cfg.outCSV, "out", "stress-profile.csv", "csv output path")
	flag.StringVar(&cfg.reportMD, "report", "stress-report.md", "markdown report")
	flag.Uint64Var(&cfg.seed, "seed", 42, "random seed")
	flag.Parse()
	return cfg
}

// runStage delegates to the existing simulator package in a real
// implementation. The signature here is a thin placeholder that
// returns zero metrics; the production wiring reuses load.Runner
// with a reduced pool lifetime.
func runStage(ctx context.Context, cfg *config, stage, agents int) stageResult {
	started := time.Now()
	// Wait out the stage duration (simulator would run in background)
	<-ctx.Done()
	return stageResult{
		Stage:       stage,
		AgentCount:  agents,
		StartedAt:   started,
		DurationSec: time.Since(started).Seconds(),
		// Real metrics come from Prometheus scrape of the target stack.
		// Wire via internal/harness once this binary is promoted from
		// scaffold to live.
	}
}

func writeCSV(path string, results []stageResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{
		"stage", "agents", "started_at", "duration_sec", "events_sent", "events_acked",
		"errors", "error_rate", "latency_p50_ms", "latency_p95_ms", "latency_p99_ms",
		"gateway_cpu_pct", "gateway_rss_mb", "api_cpu_pct", "nats_queue_depth",
		"clickhouse_write_ms", "broken",
	})
	for _, r := range results {
		_ = w.Write([]string{
			strconv.Itoa(r.Stage), strconv.Itoa(r.AgentCount),
			r.StartedAt.Format(time.RFC3339),
			fmt.Sprintf("%.1f", r.DurationSec),
			strconv.FormatInt(r.EventsSent, 10),
			strconv.FormatInt(r.EventsAcked, 10),
			strconv.FormatInt(r.ErrorCount, 10),
			fmt.Sprintf("%.6f", r.ErrorRate),
			fmt.Sprintf("%.1f", r.LatencyP50Ms),
			fmt.Sprintf("%.1f", r.LatencyP95Ms),
			fmt.Sprintf("%.1f", r.LatencyP99Ms),
			fmt.Sprintf("%.1f", r.GatewayCPUPct),
			fmt.Sprintf("%.1f", r.GatewayRSSMB),
			fmt.Sprintf("%.1f", r.APICPUPct),
			strconv.FormatInt(r.NATSQueueDepth, 10),
			fmt.Sprintf("%.1f", r.ClickHouseWrMs),
			strconv.FormatBool(r.Broken),
		})
	}
	return nil
}

func writeReport(path string, cfg *config, results []stageResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# Stress Test Report\n\n")
	fmt.Fprintf(f, "- **Started**: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(f, "- **Gateway**: `%s`\n", cfg.gateway)
	fmt.Fprintf(f, "- **Start agents**: %d\n", cfg.startAgents)
	fmt.Fprintf(f, "- **Step**: +%d agents every %s\n", cfg.stepAgents, cfg.stepDuration)
	fmt.Fprintf(f, "- **Error threshold**: %.2f%%\n\n", cfg.errorThreshold*100)

	var breakingPoint int
	for _, r := range results {
		if r.Broken {
			breakingPoint = r.AgentCount
			break
		}
	}
	if breakingPoint > 0 {
		fmt.Fprintf(f, "## Breaking point: **%d agents**\n\n", breakingPoint)
	} else {
		fmt.Fprintf(f, "## Breaking point: NOT REACHED (max duration hit)\n\n")
	}

	fmt.Fprintf(f, "| Stage | Agents | Error% | p50 | p95 | p99 | GW CPU | API CPU | NATS Q |\n")
	fmt.Fprintf(f, "|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		fmt.Fprintf(f, "| %d | %d | %.2f%% | %.0f | %.0f | %.0f | %.0f%% | %.0f%% | %d |\n",
			r.Stage, r.AgentCount, r.ErrorRate*100,
			r.LatencyP50Ms, r.LatencyP95Ms, r.LatencyP99Ms,
			r.GatewayCPUPct, r.APICPUPct, r.NATSQueueDepth)
	}
	return nil
}
