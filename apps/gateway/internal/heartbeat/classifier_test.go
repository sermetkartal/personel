package heartbeat

import (
	"testing"
	"time"
)

func TestClassifySilence(t *testing.T) {
	cases := []struct {
		name      string
		gap       time.Duration
		wantLevel SilenceLevel
	}{
		{"30min is normal", 30 * time.Minute, SilenceLevelNormal},
		{"1h boundary is suspicious", 1 * time.Hour, SilenceLevelSuspicious},
		{"3h is suspicious", 3 * time.Hour, SilenceLevelSuspicious},
		{"8h boundary is disabled", 8 * time.Hour, SilenceLevelDisabled},
		{"24h is disabled", 24 * time.Hour, SilenceLevelDisabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySilence(tc.gap)
			if got != tc.wantLevel {
				t.Errorf("ClassifySilence(%v) = %v, want %v", tc.gap, got, tc.wantLevel)
			}
		})
	}
}

func TestBusinessHoursThreshold(t *testing.T) {
	business := 4 * time.Hour
	overnight := 24 * time.Hour

	cases := []struct {
		name string
		// utcTime is expressed in UTC; the function converts to TRT (UTC+3)
		utcTime   time.Time
		wantLevel time.Duration
	}{
		{
			"tuesday morning TRT",
			time.Date(2026, 4, 7, 7, 0, 0, 0, time.UTC), // 10:00 TRT
			business,
		},
		{
			"tuesday midnight TRT",
			time.Date(2026, 4, 7, 21, 0, 0, 0, time.UTC), // 00:00 TRT next day
			overnight,
		},
		{
			"saturday morning TRT",
			time.Date(2026, 4, 11, 7, 0, 0, 0, time.UTC), // 10:00 TRT Saturday
			overnight,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BusinessHoursThreshold(tc.utcTime, business, overnight)
			if got != tc.wantLevel {
				t.Errorf("BusinessHoursThreshold(%v) = %v, want %v",
					tc.utcTime, got, tc.wantLevel)
			}
		})
	}
}
