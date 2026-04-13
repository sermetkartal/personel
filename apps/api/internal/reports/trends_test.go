package reports

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// fakeStore is an in-memory TrendStore used by tests.
type fakeStore struct {
	series map[MetricName][]DailyValue
	err    error
}

func (f *fakeStore) FetchDailySeries(_ context.Context, _ string, metric MetricName, from, to time.Time) ([]DailyValue, error) {
	if f.err != nil {
		return nil, f.err
	}
	all := f.series[metric]
	out := make([]DailyValue, 0, len(all))
	for _, v := range all {
		d := time.Date(v.Day.Year(), v.Day.Month(), v.Day.Day(), 0, 0, 0, 0, time.UTC)
		if (d.After(from) || d.Equal(from)) && d.Before(to) {
			out = append(out, v)
		}
	}
	return out, nil
}

// makeFlatSeries produces N days of identical values ending at `end`.
func makeFlatSeries(end time.Time, days int, value float64) []DailyValue {
	out := make([]DailyValue, 0, days)
	for i := days; i >= 1; i-- {
		d := end.AddDate(0, 0, -i).UTC()
		out = append(out, DailyValue{Day: d, Value: value})
	}
	return out
}

// makeSeriesWithChange produces N days where the last `halfDays` days carry
// a different value than the preceding half.
func makeSeriesWithChange(end time.Time, totalDays, halfDays int, oldValue, newValue float64) []DailyValue {
	out := make([]DailyValue, 0, totalDays)
	for i := totalDays; i >= 1; i-- {
		d := end.AddDate(0, 0, -i).UTC()
		v := oldValue
		if i <= halfDays {
			v = newValue
		}
		out = append(out, DailyValue{Day: d, Value: v})
	}
	return out
}

func fixedNow() time.Time {
	return time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Service tests
// ---------------------------------------------------------------------------

func TestTrendReport_FlatSeries_DirectionFlat(t *testing.T) {
	now := fixedNow()
	// 40 days of flat value 60
	store := &fakeStore{series: map[MetricName][]DailyValue{
		MetricActiveMinutes: makeFlatSeries(now, 40, 60),
	}}
	svc := &TrendService{store: store, now: func() time.Time { return now }}

	res, err := svc.TrendReport(context.Background(), "tenant-1", MetricActiveMinutes, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Direction != "flat" {
		t.Errorf("direction = %q, want flat", res.Direction)
	}
	if math.Abs(res.Delta) > 0.01 {
		t.Errorf("delta = %f, want ~0", res.Delta)
	}
	if math.Abs(res.ZScore) > 0.001 {
		t.Errorf("z_score = %f, want ~0 (stddev=0 path)", res.ZScore)
	}
	if res.Anomaly {
		t.Errorf("flat series should not be anomaly")
	}
	if res.CurrentPeriod.Count != 7 || res.PreviousPeriod.Count != 7 {
		t.Errorf("counts = curr %d prev %d; want 7/7", res.CurrentPeriod.Count, res.PreviousPeriod.Count)
	}
}

func TestTrendReport_SharpDrop_DownAnomaly(t *testing.T) {
	now := fixedNow()
	// 40 days: first 33 days at 80, last 7 days at 20 → sharp drop.
	store := &fakeStore{series: map[MetricName][]DailyValue{
		MetricProductivityScore: makeSeriesWithChange(now, 40, 7, 80, 20),
	}}
	svc := &TrendService{store: store, now: func() time.Time { return now }}

	res, err := svc.TrendReport(context.Background(), "tenant-1", MetricProductivityScore, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Direction != "down" {
		t.Errorf("direction = %q, want down", res.Direction)
	}
	if res.Delta >= -0.5 {
		t.Errorf("delta = %f, want <= -0.5 for 80→20 drop", res.Delta)
	}
	if !res.Anomaly {
		t.Errorf("sharp drop should be flagged anomaly (z=%f)", res.ZScore)
	}
	if res.ZScore >= -1.5 {
		t.Errorf("z_score = %f, want strongly negative", res.ZScore)
	}
}

func TestTrendReport_SharpRise_UpAnomaly(t *testing.T) {
	now := fixedNow()
	store := &fakeStore{series: map[MetricName][]DailyValue{
		MetricScreenshotCount: makeSeriesWithChange(now, 40, 7, 10, 100),
	}}
	svc := &TrendService{store: store, now: func() time.Time { return now }}

	res, err := svc.TrendReport(context.Background(), "tenant-1", MetricScreenshotCount, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Direction != "up" {
		t.Errorf("direction = %q, want up", res.Direction)
	}
	if res.Delta <= 0.5 {
		t.Errorf("delta = %f, want > 0.5 for 10→100 rise", res.Delta)
	}
	if !res.Anomaly {
		t.Errorf("sharp rise should be flagged anomaly (z=%f)", res.ZScore)
	}
}

func TestTrendReport_InsufficientHistory_ReturnsError(t *testing.T) {
	now := fixedNow()
	// Only 10 days of data — not enough for a 7-day window comparison
	// (need 14 days + 14 baseline days).
	store := &fakeStore{series: map[MetricName][]DailyValue{
		MetricActiveMinutes: makeFlatSeries(now, 10, 50),
	}}
	svc := &TrendService{store: store, now: func() time.Time { return now }}

	_, err := svc.TrendReport(context.Background(), "tenant-1", MetricActiveMinutes, 7)
	if !errors.Is(err, ErrInsufficientData) {
		t.Errorf("err = %v, want ErrInsufficientData", err)
	}
}

func TestTrendReport_UnknownMetric(t *testing.T) {
	svc := &TrendService{store: &fakeStore{}, now: fixedNow}
	_, err := svc.TrendReport(context.Background(), "t", MetricName("bogus"), 7)
	if !errors.Is(err, ErrUnknownMetric) {
		t.Errorf("err = %v, want ErrUnknownMetric", err)
	}
}

func TestTrendReport_InvalidWindow(t *testing.T) {
	svc := &TrendService{store: &fakeStore{}, now: fixedNow}
	for _, w := range []int{0, 1, 91, -5} {
		_, err := svc.TrendReport(context.Background(), "t", MetricActiveMinutes, w)
		if !errors.Is(err, ErrInvalidWindow) {
			t.Errorf("window=%d: err = %v, want ErrInvalidWindow", w, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Snapshot / math helper tests
// ---------------------------------------------------------------------------

func TestSnapshotOf(t *testing.T) {
	snap := snapshotOf([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100})
	if snap.Count != 10 {
		t.Errorf("count = %d, want 10", snap.Count)
	}
	if snap.Mean != 55 {
		t.Errorf("mean = %f, want 55", snap.Mean)
	}
	if snap.Sum != 550 {
		t.Errorf("sum = %d, want 550", snap.Sum)
	}
	if snap.P95 != 100 {
		t.Errorf("p95 = %f, want 100", snap.P95)
	}
	if snap.Median != 50 && snap.Median != 60 {
		t.Errorf("median = %f, want 50 or 60", snap.Median)
	}
}

func TestSnapshotOf_Empty(t *testing.T) {
	snap := snapshotOf(nil)
	if snap.Count != 0 || snap.Mean != 0 {
		t.Errorf("empty snapshot should be zero-value, got %+v", snap)
	}
}

func TestMeanStddev(t *testing.T) {
	mean, stddev := meanStddev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if math.Abs(mean-5) > 0.001 {
		t.Errorf("mean = %f, want 5", mean)
	}
	// Sample stddev of the classic example ~= 2.138
	if math.Abs(stddev-2.138) > 0.1 {
		t.Errorf("stddev = %f, want ~2.138", stddev)
	}
}
