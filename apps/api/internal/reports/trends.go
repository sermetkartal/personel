// trends.go — Faz 8 #87 week-over-week / month-over-month trend analysis.
//
// Computes a trend report for a named metric across two consecutive windows
// (current + previous) plus a z-score vs a 30-day sliding baseline. The
// source of truth is Postgres `employee_daily_stats` — Phase 1 still uses
// the Postgres roll-up path because the ClickHouse event pipeline has not
// yet landed for this metric set. When the CH path lands, only the Store
// interface needs a new implementation; handler + service are unchanged.
//
// KVKK proportionality note: trend windows are capped at 90 days (matches
// the CH handler cap) and the endpoint is DPO/admin/hr/manager-gated. The
// report deliberately rolls up per-user metrics — returning trend tables
// per-employee would weaponise the endpoint for covert monitoring.
//
// Error taxonomy:
//   - unknown metric name        → ErrUnknownMetric (400)
//   - window < 2 days            → ErrInvalidWindow (400)
//   - not enough history in DB   → ErrInsufficientData (409) — not 500
//   - pool / scan failures       → propagated (500)
package reports

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// MetricName is the enum of supported trend metrics.
type MetricName string

const (
	MetricActiveMinutes     MetricName = "active_minutes"
	MetricProductivityScore MetricName = "productivity_score"
	MetricScreenshotCount   MetricName = "screenshot_count"
	MetricKeystrokeCount    MetricName = "keystroke_count"
	MetricDLPRedactions     MetricName = "dlp_redactions"
	MetricPolicyViolations  MetricName = "policy_violations"
)

// allMetrics is the canonical enum set. The handler rejects any name not
// in this map. The value stores the SQL column reference used by the store
// implementation; rich_signals-backed metrics dereference a JSONB path.
var allMetrics = map[MetricName]metricSource{
	MetricActiveMinutes:     {kind: sourceColumn, column: "active_minutes"},
	MetricProductivityScore: {kind: sourceColumn, column: "productivity_score"},
	MetricScreenshotCount:   {kind: sourceColumn, column: "screenshot_count"},
	MetricKeystrokeCount:    {kind: sourceColumn, column: "keystroke_count"},
	MetricDLPRedactions:     {kind: sourceJSON, jsonPath: "dlp_redactions_total"},
	MetricPolicyViolations:  {kind: sourceJSON, jsonPath: "policy_violations_total"},
}

type metricSource struct {
	kind     sourceKind
	column   string
	jsonPath string
}

type sourceKind int

const (
	sourceColumn sourceKind = iota
	sourceJSON
)

// MetricSnapshot summarises a metric over a window.
type MetricSnapshot struct {
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	Sum    int64   `json:"sum"`
	Count  int64   `json:"count"`
}

// TrendResult is the output of a single metric-window computation.
type TrendResult struct {
	Metric         MetricName     `json:"metric"`
	WindowDays     int            `json:"window_days"`
	CurrentPeriod  MetricSnapshot `json:"current_period"`
	PreviousPeriod MetricSnapshot `json:"previous_period"`
	Delta          float64        `json:"delta"`     // (curr - prev) / prev, 0..N scale
	Direction      string         `json:"direction"` // "up" | "down" | "flat"
	ZScore         float64        `json:"z_score"`
	Anomaly        bool           `json:"anomaly"`
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// ErrUnknownMetric is returned when the caller supplies an unsupported metric.
var ErrUnknownMetric = errors.New("trends: unknown metric")

// ErrInvalidWindow is returned for window sizes < 2 days or > 90 days.
var ErrInvalidWindow = errors.New("trends: window must be between 2 and 90 days")

// ErrInsufficientData is returned when the DB does not have enough history
// to compute a meaningful trend (fewer than `window` days in current OR
// previous window OR baseline). Handler translates to 409 + problem+json.
var ErrInsufficientData = errors.New("trends: insufficient history")

// ---------------------------------------------------------------------------
// Store abstraction — trivially faked in tests
// ---------------------------------------------------------------------------

// DailyValue is the per-day datapoint pulled from employee_daily_stats.
// One row per (user_id, day) summed/averaged server-side.
type DailyValue struct {
	Day   time.Time
	Value float64
}

// TrendStore is the minimum interface the service needs. A Postgres
// implementation lives in the same package (see below); tests can pass a
// fake in-memory store.
type TrendStore interface {
	// FetchDailySeries returns one aggregated value per day in [from, to] for
	// the given tenant and metric. Days with no data are NOT filled — the
	// caller is responsible for treating gaps as "insufficient data".
	FetchDailySeries(ctx context.Context, tenantID string, metric MetricName, from, to time.Time) ([]DailyValue, error)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// TrendService computes trend reports against a TrendStore.
type TrendService struct {
	store TrendStore
	// now is the clock used for "today" — injectable for deterministic tests.
	now func() time.Time
}

// NewTrendService constructs the trend service.
func NewTrendService(store TrendStore) *TrendService {
	return &TrendService{store: store, now: time.Now}
}

// TrendReport computes a single trend result for the given metric + window.
// It pulls a baseline window of at least 30 days ending today, splits it into
// (current, previous) windows of `windowDays` each, and computes statistics.
//
// Minimum history required:
//   - current window: at least windowDays values
//   - previous window: at least windowDays values
//   - baseline (30 days): at least 14 values (enough to approximate stddev)
//
// If any of the above is not satisfied → ErrInsufficientData.
func (s *TrendService) TrendReport(
	ctx context.Context,
	tenantID string,
	metric MetricName,
	windowDays int,
) (*TrendResult, error) {
	if _, ok := allMetrics[metric]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownMetric, metric)
	}
	if windowDays < 2 || windowDays > 90 {
		return nil, ErrInvalidWindow
	}

	// Pull a superset window large enough to cover the baseline and both
	// halves. Baseline is max(30, 2*window) so we always have at least as
	// much history as a single split comparison needs.
	baseline := 30
	if 2*windowDays > baseline {
		baseline = 2 * windowDays
	}

	now := s.now().UTC()
	// Drop the hours/minutes part so "today" always begins at 00:00 UTC.
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -baseline)

	values, err := s.store.FetchDailySeries(ctx, tenantID, metric, start, end)
	if err != nil {
		return nil, fmt.Errorf("trends: fetch series: %w", err)
	}

	// Map by day for O(1) lookups and gap detection.
	byDay := make(map[time.Time]float64, len(values))
	for _, v := range values {
		d := time.Date(v.Day.Year(), v.Day.Month(), v.Day.Day(), 0, 0, 0, 0, time.UTC)
		byDay[d] = v.Value
	}

	currentVals := collectWindow(byDay, end.AddDate(0, 0, -windowDays), end)
	previousVals := collectWindow(byDay, end.AddDate(0, 0, -2*windowDays), end.AddDate(0, 0, -windowDays))
	baselineVals := collectWindow(byDay, start, end)

	// We deliberately require `windowDays` full values in each half. A
	// seeding job that left gaps in the previous window cannot fake a trend.
	if len(currentVals) < windowDays || len(previousVals) < windowDays {
		return nil, ErrInsufficientData
	}
	if len(baselineVals) < 14 {
		return nil, ErrInsufficientData
	}

	curr := snapshotOf(currentVals)
	prev := snapshotOf(previousVals)

	var delta float64
	switch {
	case prev.Mean == 0 && curr.Mean == 0:
		delta = 0
	case prev.Mean == 0:
		// Previous period was zero but current isn't — treat as +inf-ish, but
		// cap at 10x for UI sanity. "Up" direction.
		delta = 10
	default:
		delta = (curr.Mean - prev.Mean) / prev.Mean
	}

	direction := "flat"
	if delta >= 0.05 {
		direction = "up"
	} else if delta <= -0.05 {
		direction = "down"
	}

	mean, stddev := meanStddev(baselineVals)
	var z float64
	if stddev > 0 {
		z = (curr.Mean - mean) / stddev
	}
	anomaly := math.Abs(z) > 2.0

	return &TrendResult{
		Metric:         metric,
		WindowDays:     windowDays,
		CurrentPeriod:  curr,
		PreviousPeriod: prev,
		Delta:          delta,
		Direction:      direction,
		ZScore:         z,
		Anomaly:        anomaly,
	}, nil
}

// ---------------------------------------------------------------------------
// Math helpers
// ---------------------------------------------------------------------------

// collectWindow extracts values for days in [from, to) sorted by day ascending.
// Missing days are skipped (not zero-filled) so the caller can detect gaps.
func collectWindow(byDay map[time.Time]float64, from, to time.Time) []float64 {
	out := make([]float64, 0)
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	for day.Before(cutoff) {
		if v, ok := byDay[day]; ok {
			out = append(out, v)
		}
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// snapshotOf computes mean/median/p95/sum/count for a slice.
func snapshotOf(values []float64) MetricSnapshot {
	if len(values) == 0 {
		return MetricSnapshot{}
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	median := percentile(sorted, 50)
	p95 := percentile(sorted, 95)

	return MetricSnapshot{
		Mean:   round2(mean),
		Median: round2(median),
		P95:    round2(p95),
		Sum:    int64(math.Round(sum)),
		Count:  int64(len(sorted)),
	}
}

// percentile returns the value at the given percentile from a sorted slice.
// Uses nearest-rank, which is stable for small datasets.
func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := int(math.Ceil(pct / 100.0 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// meanStddev computes the sample mean and standard deviation.
func meanStddev(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var sq float64
	for _, v := range values {
		d := v - mean
		sq += d * d
	}
	// Sample stddev (N-1) for robustness with small windows.
	if len(values) < 2 {
		return mean, 0
	}
	variance := sq / float64(len(values)-1)
	return mean, math.Sqrt(variance)
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
