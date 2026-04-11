package heartbeat

import "time"

// SilenceLevel represents the severity of an endpoint silence gap.
// Used for DPO notification thresholds.
type SilenceLevel string

const (
	SilenceLevelNormal    SilenceLevel = "NORMAL"
	SilenceLevelSuspicious SilenceLevel = "SUSPICIOUS"
	SilenceLevelDisabled  SilenceLevel = "DISABLED"
)

// ClassifySilence maps a gap duration to a SilenceLevel.
//
// From the threat model (Flow 7):
//   - NORMAL:     gap < 1 hour
//   - SUSPICIOUS: 1h <= gap < 8h
//   - DISABLED:   gap >= 8h
func ClassifySilence(gap time.Duration) SilenceLevel {
	switch {
	case gap >= 8*time.Hour:
		return SilenceLevelDisabled
	case gap >= 1*time.Hour:
		return SilenceLevelSuspicious
	default:
		return SilenceLevelNormal
	}
}

// BusinessHoursThreshold returns the configured silence alert threshold
// depending on whether the current time is within business hours (UTC+3 for Turkey).
// Outside business hours the threshold is relaxed (24h) to avoid false positives
// from overnight shutdowns.
func BusinessHoursThreshold(now time.Time, businessHoursThreshold, overnightThreshold time.Duration) time.Duration {
	loc := time.FixedZone("TRT", 3*60*60) // UTC+3 Turkey
	local := now.In(loc)
	hour := local.Hour()
	weekday := local.Weekday()

	isBusinessHours := weekday >= time.Monday && weekday <= time.Friday &&
		hour >= 8 && hour < 18

	if isBusinessHours {
		return businessHoursThreshold
	}
	return overnightThreshold
}
