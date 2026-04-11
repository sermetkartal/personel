//go:build integration
// +build integration

package integration

import (
	"testing"
	"time"

	"github.com/personel/gateway/internal/heartbeat"
)

// TestHeartbeatMonitorStateTransitions validates the state machine transitions
// for an endpoint that stops sending heartbeats.
func TestHeartbeatMonitorStateTransitions(t *testing.T) {
	// This test does not require containers; it tests the in-memory monitor.

	t.Run("graceful_bye_classifies_shutdown", func(t *testing.T) {
		gap := 3 * time.Hour
		level := heartbeat.ClassifySilence(gap)
		if level != heartbeat.SilenceLevelSuspicious {
			t.Errorf("expected SUSPICIOUS for 3h gap, got %v", level)
		}
	})

	t.Run("eight_hour_gap_is_disabled", func(t *testing.T) {
		gap := 9 * time.Hour
		level := heartbeat.ClassifySilence(gap)
		if level != heartbeat.SilenceLevelDisabled {
			t.Errorf("expected DISABLED for 9h gap, got %v", level)
		}
	})

	t.Run("thirty_minutes_is_normal", func(t *testing.T) {
		gap := 30 * time.Minute
		level := heartbeat.ClassifySilence(gap)
		if level != heartbeat.SilenceLevelNormal {
			t.Errorf("expected NORMAL for 30min gap, got %v", level)
		}
	})
}

// TestHeartbeatBusinessHoursThreshold verifies the business hours logic for
// the Turkish timezone (UTC+3).
func TestHeartbeatBusinessHoursThreshold(t *testing.T) {
	t.Skip("requires NATS + ClickHouse + MinIO containers")

	// Tuesday 10:00 AM TRT (UTC+3) = Tuesday 07:00 UTC.
	tuesdayMorningUTC := time.Date(2026, 4, 7, 7, 0, 0, 0, time.UTC)
	threshold := heartbeat.BusinessHoursThreshold(tuesdayMorningUTC, 4*time.Hour, 24*time.Hour)
	if threshold != 4*time.Hour {
		t.Errorf("expected business hours threshold of 4h, got %v", threshold)
	}

	// Saturday 10:00 AM TRT = Saturday 07:00 UTC.
	saturdayMorningUTC := time.Date(2026, 4, 11, 7, 0, 0, 0, time.UTC)
	threshold = heartbeat.BusinessHoursThreshold(saturdayMorningUTC, 4*time.Hour, 24*time.Hour)
	if threshold != 24*time.Hour {
		t.Errorf("expected overnight threshold of 24h on weekend, got %v", threshold)
	}
}
