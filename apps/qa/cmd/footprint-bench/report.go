//go:build windows

package main

import (
	"fmt"
	"io"
	"math"
	"time"
)

// Report is the aggregate structure the harness produces. It is
// serialised to JSON (for CI artifacts) and printed as a human-readable
// block (for console logs). Field names are stable — CI parsers depend
// on them.
type Report struct {
	// Run metadata.
	AgentExe   string              `json:"agent_exe"`
	AgentPID   uint32              `json:"agent_pid"`
	Duration   time.Duration       `json:"configured_duration"`
	Interval   time.Duration       `json:"sample_interval"`
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt time.Time           `json:"finished_at"`
	Thresholds FootprintThresholds `json:"thresholds"`

	// Raw + aggregated measurements.
	Samples []Sample `json:"samples"`

	CPUAvg float64 `json:"cpu_avg_percent"`
	CPUP50 float64 `json:"cpu_p50_percent"`
	CPUP95 float64 `json:"cpu_p95_percent"`
	CPUP99 float64 `json:"cpu_p99_percent"`
	CPUMax float64 `json:"cpu_max_percent"`

	RSSAvgMB float64 `json:"rss_avg_mb"`
	RSSMaxMB float64 `json:"rss_max_mb"`

	// Threshold evaluation.
	CPUAvgPass bool `json:"cpu_avg_pass"`
	CPUP95Pass bool `json:"cpu_p95_pass"`
	CPUMaxPass bool `json:"cpu_max_pass"`
	RSSAvgPass bool `json:"rss_avg_pass"`
	RSSMaxPass bool `json:"rss_max_pass"`
	Pass       bool `json:"pass"`

	// Error is populated when the run failed (agent crashed, ctx
	// cancelled, etc). CI parsers treat a non-empty Error as failure
	// regardless of Pass.
	Error string `json:"error,omitempty"`
}

// NewReport seeds a Report with the run configuration. Samples and
// aggregates are filled in incrementally by the main loop.
func NewReport(agentExe string, duration, interval time.Duration, thresholds FootprintThresholds) *Report {
	return &Report{
		AgentExe:   agentExe,
		Duration:   duration,
		Interval:   interval,
		Thresholds: thresholds,
		Samples:    make([]Sample, 0, 256),
	}
}

// Aggregate walks the samples slice once and fills in the summary
// statistics + threshold pass/fail booleans. Safe to call on an empty
// Samples slice (produces zero values + failing booleans).
func (r *Report) Aggregate() {
	if len(r.Samples) == 0 {
		r.Pass = false
		return
	}

	cpuSeries := make([]float64, 0, len(r.Samples))
	var cpuSum, rssSum float64
	var rssMax float64

	for _, s := range r.Samples {
		cpuSeries = append(cpuSeries, s.CPUPercent)
		cpuSum += s.CPUPercent
		rssMB := float64(s.RSSBytes) / 1024 / 1024
		rssSum += rssMB
		if rssMB > rssMax {
			rssMax = rssMB
		}
	}
	n := float64(len(r.Samples))
	r.CPUAvg = cpuSum / n
	r.RSSAvgMB = rssSum / n
	r.RSSMaxMB = rssMax

	sortFloat(cpuSeries)
	r.CPUP50 = percentile(cpuSeries, 0.50)
	r.CPUP95 = percentile(cpuSeries, 0.95)
	r.CPUP99 = percentile(cpuSeries, 0.99)
	r.CPUMax = cpuSeries[len(cpuSeries)-1]

	// Threshold evaluation — strict less-than for all caps.
	r.CPUAvgPass = r.CPUAvg < r.Thresholds.CPUAvgMaxPercent
	r.CPUP95Pass = r.CPUP95 < r.Thresholds.CPUP95MaxPercent
	r.CPUMaxPass = r.CPUMax < r.Thresholds.CPUMaxPercent
	r.RSSAvgPass = r.RSSAvgMB < r.Thresholds.RSSAvgMaxMB
	r.RSSMaxPass = r.RSSMaxMB < r.Thresholds.RSSMaxMB
	r.Pass = r.CPUAvgPass && r.CPUP95Pass && r.CPUMaxPass && r.RSSAvgPass && r.RSSMaxPass
}

// percentile returns the p-th percentile of a PRE-SORTED slice using
// the nearest-rank method. p is in [0.0, 1.0].
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	// Nearest-rank: ceil(p * N) - 1, clamped.
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Print writes a compact, human-readable summary to w. The format is
// stable for screen-scraping by CI log viewers but not a contract.
func (r *Report) Print(w io.Writer) {
	fmt.Fprintf(w, "\n== Personel Agent Footprint Bench ==\n")
	fmt.Fprintf(w, "Agent   : %s\n", r.AgentExe)
	if r.AgentPID != 0 {
		fmt.Fprintf(w, "PID     : %d\n", r.AgentPID)
	}
	fmt.Fprintf(w, "Duration: %s (configured %s)\n",
		r.FinishedAt.Sub(r.StartedAt).Round(time.Second), r.Duration)
	fmt.Fprintf(w, "Samples : %d (interval %s)\n", len(r.Samples), r.Interval)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "CPU%%   avg=%5.2f  p50=%5.2f  p95=%5.2f  p99=%5.2f  max=%5.2f\n",
		r.CPUAvg, r.CPUP50, r.CPUP95, r.CPUP99, r.CPUMax)
	fmt.Fprintf(w, "RSS    avg=%.1f MB  max=%.1f MB\n", r.RSSAvgMB, r.RSSMaxMB)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Thresholds:\n")
	fmt.Fprintf(w, "  cpu_avg < %.2f%%   %s  (got %.2f%%)\n",
		r.Thresholds.CPUAvgMaxPercent, mark(r.CPUAvgPass), r.CPUAvg)
	fmt.Fprintf(w, "  cpu_p95 < %.2f%%   %s  (got %.2f%%)\n",
		r.Thresholds.CPUP95MaxPercent, mark(r.CPUP95Pass), r.CPUP95)
	fmt.Fprintf(w, "  cpu_max < %.2f%%   %s  (got %.2f%%)\n",
		r.Thresholds.CPUMaxPercent, mark(r.CPUMaxPass), r.CPUMax)
	fmt.Fprintf(w, "  rss_avg < %.0f MB    %s  (got %.1f MB)\n",
		r.Thresholds.RSSAvgMaxMB, mark(r.RSSAvgPass), r.RSSAvgMB)
	fmt.Fprintf(w, "  rss_max < %.0f MB    %s  (got %.1f MB)\n",
		r.Thresholds.RSSMaxMB, mark(r.RSSMaxPass), r.RSSMaxMB)
	fmt.Fprintln(w)
	if r.Error != "" {
		fmt.Fprintf(w, "ERROR: %s\n", r.Error)
	}
	if r.Pass && r.Error == "" {
		fmt.Fprintln(w, "PASS")
	} else {
		fmt.Fprintln(w, "FAIL")
	}
}

func mark(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
