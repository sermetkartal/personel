// runner.go reads a scenario JSON and launches the simulator with those params,
// collects Prometheus metrics, and writes a pass/fail report against thresholds.yaml.
package load

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/personel/qa/internal/simulator"
)

// Scenario defines a load test scenario. All scenario JSONs in scenarios/ must
// conform to this schema.
type Scenario struct {
	Name             string        `json:"name"`
	Description      string        `json:"description"`
	AgentCount       int           `json:"agent_count"`
	RampDurationSec  int           `json:"ramp_duration_sec"`
	SteadyDurationSec int          `json:"steady_duration_sec"`
	RampDownSec      int           `json:"ramp_down_sec"`
	GatewayAddr      string        `json:"gateway_addr"`
	TenantID         string        `json:"tenant_id"`
	Seed             uint64        `json:"seed"`
	// Phase1Criteria lists which exit criteria to evaluate.
	Phase1Criteria   []string      `json:"phase1_criteria"`
}

// RunResult is the output of a load test run.
type RunResult struct {
	Scenario    *Scenario
	StartedAt   time.Time
	FinishedAt  time.Time
	PoolStats   simulator.PoolStats
	Metrics     map[string]float64
	CriteriaResults []CriterionResult
	Passed      bool
}

// CriterionResult is the pass/fail result for one Phase 1 exit criterion.
type CriterionResult struct {
	ID          string
	Description string
	Threshold   float64
	Actual      float64
	Unit        string
	Passed      bool
}

// RunnerConfig holds config for a load test run.
type RunnerConfig struct {
	ScenarioFile  string
	ThresholdsFile string
	GatewayAddr   string // overrides scenario JSON if set
	ReportPath    string
	ShowProgress  bool
}

// Runner executes a load test scenario and produces a Phase 1 exit report.
type Runner struct {
	cfg    RunnerConfig
	log    *slog.Logger
	reg    *prometheus.Registry
}

// NewRunner creates a new Runner.
func NewRunner(cfg RunnerConfig) *Runner {
	return &Runner{
		cfg: cfg,
		log: slog.Default().With("component", "load_runner"),
		reg: prometheus.NewRegistry(),
	}
}

// Run loads the scenario, starts the pool, collects metrics, and writes the report.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	// Load scenario.
	scenario, err := loadScenario(r.cfg.ScenarioFile)
	if err != nil {
		return nil, fmt.Errorf("load scenario: %w", err)
	}

	// Override gateway addr if specified.
	if r.cfg.GatewayAddr != "" {
		scenario.GatewayAddr = r.cfg.GatewayAddr
	}

	if scenario.GatewayAddr == "" {
		return nil, fmt.Errorf("gateway_addr not set in scenario or config")
	}

	r.log.Info("starting load test",
		"scenario", scenario.Name,
		"agents", scenario.AgentCount,
		"steady_sec", scenario.SteadyDurationSec,
	)

	// Create test CA.
	tenantID := scenario.TenantID
	if tenantID == "" {
		tenantID = "load-test-00-0000-0000-000000000001"
	}

	ca, err := simulator.NewTestCA(tenantID)
	if err != nil {
		return nil, fmt.Errorf("create test CA: %w", err)
	}

	// Create metrics.
	metrics := simulator.NewSimulatorMetrics(r.reg)

	// Create pool.
	agentCfg := simulator.DefaultAgentConfig()
	agentCfg.BatchSize = 50
	agentCfg.HeartbeatEvery = 30 * time.Second
	agentCfg.UploadEvery = 5 * time.Second

	poolCfg := simulator.PoolConfig{
		AgentCount:       scenario.AgentCount,
		RampDuration:     time.Duration(scenario.RampDurationSec) * time.Second,
		SteadyDuration:   time.Duration(scenario.SteadyDurationSec) * time.Second,
		RampDownDuration: time.Duration(scenario.RampDownSec) * time.Second,
		GatewayAddr:      scenario.GatewayAddr,
		TenantID:         tenantID,
		CA:               ca,
		AgentCfgTemplate: agentCfg,
		Metrics:          metrics,
		Seed:             scenario.Seed,
		ShowProgress:     r.cfg.ShowProgress,
	}

	pool := simulator.NewAgentPool(poolCfg)
	startedAt := time.Now()

	if err := pool.Run(ctx); err != nil {
		return nil, fmt.Errorf("pool run: %w", err)
	}

	finishedAt := time.Now()
	stats := pool.Stats()

	// Collect Prometheus metrics.
	collectedMetrics := r.collectMetrics()

	// Load thresholds.
	thresholds, err := loadThresholds(r.cfg.ThresholdsFile)
	if err != nil {
		r.log.Warn("could not load thresholds; skipping pass/fail evaluation", "error", err)
		thresholds = defaultThresholds()
	}

	// Evaluate Phase 1 criteria.
	criteriaResults := evaluateCriteria(collectedMetrics, thresholds, scenario.Phase1Criteria)

	allPassed := true
	for _, cr := range criteriaResults {
		if !cr.Passed {
			allPassed = false
		}
	}

	result := &RunResult{
		Scenario:        scenario,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		PoolStats:       stats,
		Metrics:         collectedMetrics,
		CriteriaResults: criteriaResults,
		Passed:          allPassed,
	}

	// Write report.
	if r.cfg.ReportPath != "" {
		reporter := &Reporter{OutputDir: r.cfg.ReportPath}
		if err := reporter.Write(result); err != nil {
			r.log.Error("write report", "error", err)
		}
	}

	return result, nil
}

// collectMetrics gathers current values from the Prometheus registry.
func (r *Runner) collectMetrics() map[string]float64 {
	metrics := make(map[string]float64)

	mfs, err := r.reg.Gather()
	if err != nil {
		r.log.Warn("gather metrics", "error", err)
		return metrics
	}

	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			name := mf.GetName()
			// Build label suffix.
			labels := ""
			for _, lp := range m.GetLabel() {
				labels += fmt.Sprintf("_%s_%s", lp.GetName(), lp.GetValue())
			}
			key := name + labels

			switch {
			case m.GetCounter() != nil:
				metrics[key] = m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				metrics[key] = m.GetGauge().GetValue()
			case m.GetHistogram() != nil:
				h := m.GetHistogram()
				if h.GetSampleCount() > 0 {
					metrics[key+"_p95"] = computeP95FromHistogram(h)
					metrics[key+"_avg"] = h.GetSampleSum() / float64(h.GetSampleCount())
				}
			}
		}
	}

	return metrics
}

// computeP95FromHistogram estimates p95 from a Prometheus histogram using
// linear interpolation between the bucket boundaries that straddle the 95th
// percentile. This matches the standard Prometheus approach used in recording
// rules (histogram_quantile).
func computeP95FromHistogram(h *dto.Histogram) float64 {
	if h == nil || h.GetSampleCount() == 0 {
		return 0
	}

	total := float64(h.GetSampleCount())
	target := 0.95 * total

	buckets := h.GetBucket()
	if len(buckets) == 0 {
		return 0
	}

	var prevBound float64
	var prevCount float64

	for _, b := range buckets {
		count := float64(b.GetCumulativeCount())
		bound := b.GetUpperBound()

		if count >= target {
			// Linearly interpolate within this bucket.
			if count == prevCount {
				return bound
			}
			fraction := (target - prevCount) / (count - prevCount)
			return prevBound + fraction*(bound-prevBound)
		}

		prevBound = bound
		prevCount = count
	}

	// Fell off the end — use the last bucket's upper bound.
	last := buckets[len(buckets)-1]
	return last.GetUpperBound()
}

// loadScenario reads a scenario JSON file.
func loadScenario(path string) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var s Scenario
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Threshold is one pass/fail gate from thresholds.yaml.
type Threshold struct {
	ID          string  `yaml:"id"`
	Description string  `yaml:"description"`
	MetricKey   string  `yaml:"metric_key"`
	Operator    string  `yaml:"operator"` // "lt" | "lte" | "gt" | "gte"
	Value       float64 `yaml:"value"`
	Unit        string  `yaml:"unit"`
}

func loadThresholds(path string) ([]Threshold, error) {
	if path == "" {
		return defaultThresholds(), nil
	}
	// In a full implementation this would use koanf/yaml to parse thresholds.yaml.
	return defaultThresholds(), nil
}

func defaultThresholds() []Threshold {
	return []Threshold{
		{ID: "EC-2", Description: "Agent CPU < 2%", MetricKey: "agent_cpu_avg", Operator: "lt", Value: 2.0, Unit: "%"},
		{ID: "EC-3", Description: "Agent RAM < 150MB", MetricKey: "agent_rss_peak_mb", Operator: "lt", Value: 150, Unit: "MB"},
		{ID: "EC-6", Description: "Event loss < 0.01%", MetricKey: "event_loss_pct", Operator: "lt", Value: 0.01, Unit: "%"},
		{ID: "EC-7", Description: "E2E latency p95 < 5s", MetricKey: "e2e_latency_p95_sec", Operator: "lt", Value: 5.0, Unit: "s"},
		{ID: "EC-5", Description: "Dashboard query p95 < 1s", MetricKey: "dashboard_query_p95_sec", Operator: "lt", Value: 1.0, Unit: "s"},
		{ID: "EC-8", Description: "Server uptime >= 99.5%", MetricKey: "server_uptime_pct", Operator: "gte", Value: 99.5, Unit: "%"},
	}
}

func evaluateCriteria(metrics map[string]float64, thresholds []Threshold, enabledIDs []string) []CriterionResult {
	// Build enabled set.
	enabled := make(map[string]bool)
	for _, id := range enabledIDs {
		enabled[id] = true
	}

	results := make([]CriterionResult, 0, len(thresholds))
	for _, t := range thresholds {
		if len(enabledIDs) > 0 && !enabled[t.ID] {
			continue
		}

		actual, ok := metrics[t.MetricKey]
		if !ok {
			// Metric not collected — cannot evaluate.
			results = append(results, CriterionResult{
				ID:          t.ID,
				Description: t.Description,
				Threshold:   t.Value,
				Actual:      -1,
				Unit:        t.Unit,
				Passed:      false,
			})
			continue
		}

		var passed bool
		switch t.Operator {
		case "lt":
			passed = actual < t.Value
		case "lte":
			passed = actual <= t.Value
		case "gt":
			passed = actual > t.Value
		case "gte":
			passed = actual >= t.Value
		}

		results = append(results, CriterionResult{
			ID:          t.ID,
			Description: t.Description,
			Threshold:   t.Value,
			Actual:      actual,
			Unit:        t.Unit,
			Passed:      passed,
		})
	}

	return results
}
