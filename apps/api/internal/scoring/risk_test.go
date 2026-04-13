// risk_test.go — Faz 8 #86 unit tests for ComputeRisk.
//
// Coverage:
//   - All-zero inputs → score 0, tier "low", no factors
//   - Tier boundaries (low/medium/high/critical)
//   - Hard override: tamper ≥ 1 promotes to at least "high"
//   - Hard override: av_deactivation ≥ 1 forces "critical"
//   - Hard override precedence (av beats tamper)
//   - Defensive clamping of UBA + off-hours to [0,1]
//   - Negative ints clamped to zero
//   - AdvisoryOnly invariant — NEVER false
//   - Score cannot exceed 100
//   - TopFactors ordered descending, zero-weight trimmed
//   - KVKK disclaimer always present

package scoring

import (
	"strings"
	"testing"
)

func TestComputeRisk_AllZeroInputs(t *testing.T) {
	got := ComputeRisk(RiskInputs{})
	if got.Score != 0 {
		t.Errorf("all zero: want 0, got %d", got.Score)
	}
	if got.Tier != "low" {
		t.Errorf("all zero tier: want 'low', got %q", got.Tier)
	}
	if !got.AdvisoryOnly {
		t.Error("AdvisoryOnly invariant broken on zero input")
	}
	if got.Disclaimer == "" {
		t.Error("Disclaimer must always be populated")
	}
	if len(got.TopFactors) != 0 {
		t.Errorf("all-zero should produce empty TopFactors, got %d", len(got.TopFactors))
	}
}

func TestComputeRisk_LowTier(t *testing.T) {
	// Small UBA signal only: uba=0.3 → 9 points
	got := ComputeRisk(RiskInputs{UBAAnomalyScore: 0.3})
	if got.Tier != "low" {
		t.Errorf("uba=0.3 alone: want low tier, got %q (score %d)", got.Tier, got.Score)
	}
}

func TestComputeRisk_MediumTier(t *testing.T) {
	// uba=0.8 → 24 + dlp=3 → 15 + offhrs=0.5 → 10 = 49
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore:  0.8,
		DLPBlockedCount:  3,
		OffHoursActivity: 0.5,
	})
	if got.Score < 40 || got.Score > 55 {
		t.Errorf("medium tier sample: score out of expected range: %d", got.Score)
	}
	if got.Tier != "medium" {
		t.Errorf("medium tier sample: want medium, got %q", got.Tier)
	}
}

func TestComputeRisk_HighTier(t *testing.T) {
	// Heavy signals without tamper/av overrides
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore:      0.9,  // 27
		DLPBlockedCount:      6,    // 30 cap
		SensitiveFileAccess:  5,    // 15
		OffHoursActivity:     0.6,  // 12
	})
	// total ≈ 84 → clamped? 27+30+15+12 = 84 (no clamp needed)
	if got.Score < 70 || got.Score > 90 {
		t.Errorf("high tier sample: score unexpected: %d", got.Score)
	}
	if got.Tier != "critical" && got.Tier != "high" {
		t.Errorf("high tier sample: want high/critical, got %q", got.Tier)
	}
}

func TestComputeRisk_CriticalTier(t *testing.T) {
	// Saturated signals — score should clamp at 100
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore:      1.0,  // 30
		DLPBlockedCount:      20,   // 30 cap
		SensitiveFileAccess:  20,   // 20 cap
		USBExternalTransfers: 10,   // 20 cap
		OffHoursActivity:     1.0,  // 20
		TamperFindings:       5,    // 30 cap
		AVDeactivationCount:  1,    // 20 → but hard override → critical
	})
	if got.Score != 100 {
		t.Errorf("saturated inputs should clamp to 100, got %d", got.Score)
	}
	if got.Tier != "critical" {
		t.Errorf("saturated inputs tier: want critical, got %q", got.Tier)
	}
}

// --- Hard overrides --------------------------------------------------------

func TestComputeRisk_TamperElevatesToAtLeastHigh(t *testing.T) {
	// Low base signals + 1 tamper finding → must elevate to at least "high"
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore: 0.1, // 3
		TamperFindings:  1,   // 10 raw
	})
	// pre-override score = 13 → "low" tier
	// post-override → tier promoted to "high"
	if got.Tier != "high" && got.Tier != "critical" {
		t.Errorf("tamper override: want high/critical, got %q (score %d)", got.Tier, got.Score)
	}
}

func TestComputeRisk_TamperDoesNotDemoteCritical(t *testing.T) {
	// av deactivation forces critical, tamper only floors at high —
	// verify that critical is preserved, not demoted.
	got := ComputeRisk(RiskInputs{
		TamperFindings:      2,
		AVDeactivationCount: 1,
	})
	if got.Tier != "critical" {
		t.Errorf("tamper+av: want critical, got %q", got.Tier)
	}
}

func TestComputeRisk_AVDeactivationForcesCritical(t *testing.T) {
	// Lowest possible base signals + 1 AV deactivation → must be critical
	got := ComputeRisk(RiskInputs{
		AVDeactivationCount: 1, // score contribution = 20
	})
	// Without override, score 20 → "low". Override → "critical".
	if got.Tier != "critical" {
		t.Errorf("av override: want critical, got %q (score %d)", got.Tier, got.Score)
	}
}

func TestComputeRisk_AVDeactivationOnZeroBaseStillCritical(t *testing.T) {
	// Edge case: verify override fires even when base score is 0 — the
	// AV deactivation itself still contributes 20 points, so score=20,
	// and the override kicks tier to critical.
	got := ComputeRisk(RiskInputs{AVDeactivationCount: 1})
	if got.Tier != "critical" {
		t.Errorf("av override on zero base: got tier %q", got.Tier)
	}
	if got.Score < 20 {
		t.Errorf("av contributes ≥20 to score, got %d", got.Score)
	}
}

// --- Defensive clamping ----------------------------------------------------

func TestComputeRisk_UBAOutOfRangeClamped(t *testing.T) {
	// UBA 5.0 → clamp to 1.0 → 30 contribution
	got := ComputeRisk(RiskInputs{UBAAnomalyScore: 5.0})
	if got.Score < 28 || got.Score > 32 {
		t.Errorf("UBA clamp: expected ~30, got %d", got.Score)
	}
}

func TestComputeRisk_UBANegativeClamped(t *testing.T) {
	got := ComputeRisk(RiskInputs{UBAAnomalyScore: -1.0})
	if got.Score != 0 {
		t.Errorf("UBA clamp negative: expected 0, got %d", got.Score)
	}
}

func TestComputeRisk_NegativeCountsClampedToZero(t *testing.T) {
	got := ComputeRisk(RiskInputs{
		DLPBlockedCount:      -10,
		SensitiveFileAccess:  -5,
		USBExternalTransfers: -3,
		TamperFindings:       -1,
		AVDeactivationCount:  -1,
	})
	if got.Score != 0 {
		t.Errorf("negative counts defensive: expected 0, got %d", got.Score)
	}
	if got.Tier != "low" {
		t.Errorf("negative counts: want low tier, got %q", got.Tier)
	}
}

// --- Invariants -----------------------------------------------------------

func TestComputeRisk_AdvisoryOnlyAlwaysTrue(t *testing.T) {
	// Cover the full input space cross-product for a handful of cases.
	cases := []RiskInputs{
		{},
		{UBAAnomalyScore: 0.5},
		{TamperFindings: 1},
		{AVDeactivationCount: 1},
		{UBAAnomalyScore: 1.0, TamperFindings: 3, AVDeactivationCount: 2, DLPBlockedCount: 10},
	}
	for i, in := range cases {
		r := ComputeRisk(in)
		if !r.AdvisoryOnly {
			t.Errorf("case %d: AdvisoryOnly must be true (%+v)", i, in)
		}
		if !strings.Contains(r.Disclaimer, "KVKK") {
			t.Errorf("case %d: disclaimer must contain KVKK text", i)
		}
	}
}

func TestComputeRisk_ScoreNeverExceeds100(t *testing.T) {
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore:      1.0,
		DLPBlockedCount:      1000,
		SensitiveFileAccess:  1000,
		USBExternalTransfers: 1000,
		OffHoursActivity:     1.0,
		TamperFindings:       1000,
		AVDeactivationCount:  1000,
	})
	if got.Score > 100 {
		t.Errorf("score must clamp to 100, got %d", got.Score)
	}
}

func TestComputeRisk_TopFactorsOrderedDescending(t *testing.T) {
	got := ComputeRisk(RiskInputs{
		UBAAnomalyScore:     0.9,
		DLPBlockedCount:     1,
		SensitiveFileAccess: 7,
	})
	prev := 999.0
	for _, f := range got.TopFactors {
		if f.Weight > prev {
			t.Errorf("TopFactors not descending: %v", got.TopFactors)
		}
		prev = f.Weight
	}
}

func TestComputeRisk_ZeroWeightFactorsTrimmed(t *testing.T) {
	got := ComputeRisk(RiskInputs{UBAAnomalyScore: 0.5})
	for _, f := range got.TopFactors {
		if f.Weight <= 0 {
			t.Errorf("zero-weight factor should be trimmed: %+v", f)
		}
	}
}

// --- Tier boundary invariants ---------------------------------------------

func TestClassifyTier_Boundaries(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{0, "low"},
		{25, "low"},
		{26, "medium"},
		{50, "medium"},
		{51, "high"},
		{75, "high"},
		{76, "critical"},
		{100, "critical"},
	}
	for _, c := range cases {
		if got := classifyTier(c.score); got != c.want {
			t.Errorf("classifyTier(%d): want %q, got %q", c.score, c.want, got)
		}
	}
}
