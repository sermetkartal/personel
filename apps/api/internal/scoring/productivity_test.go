// productivity_test.go — Faz 8 #85 unit tests for ComputeProductivity.
//
// Coverage goals:
//   - Pure productive user → 100
//   - Pure distracting user → 0 (with violations compounded)
//   - Balanced mix → ~55 range
//   - Violations cap the score at ceiling-violations
//   - Idle-heavy day → idle penalty applied
//   - Low keystroke detection only kicks in on long active windows
//   - Negative/zero inputs are handled defensively (no panics, no NaN)
//   - PenaltyReason aggregation (none/single/mixed)

package scoring

import (
	"testing"
)

func TestComputeProductivity_PurelyProductive(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  480,
		NeutralAppMinutes:     0,
		DistractingAppMinutes: 0,
		KeystrokeCount:        50000, // ~1.7 kps over 480min — clearly human typing
	})
	if got.Score != 100 {
		t.Errorf("pure productive user: want 100, got %d", got.Score)
	}
	if got.PenaltyReason != "" {
		t.Errorf("pure productive should have no penalty reason, got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_PurelyDistracting(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  0,
		NeutralAppMinutes:     0,
		DistractingAppMinutes: 480,
		KeystrokeCount:        20000,
		PolicyViolations:      0,
	})
	// baseline = (0 + 0) / (0 + 0 + 480 + 0) = 0 → score = 0
	// -10 for high_distraction → still floored at 0
	if got.Score != 0 {
		t.Errorf("pure distracting user: want 0, got %d", got.Score)
	}
	if got.PenaltyReason != "high_distraction" {
		t.Errorf("want penalty reason 'high_distraction', got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_BalancedMix(t *testing.T) {
	// 240 productive, 120 neutral, 120 distracting, 0 idle
	// baseline = (240*1.0 + 120*0.5) / (240+120+120+0) = 300 / 480 = 0.625
	// score = 62.5
	// distracting_ratio = 120/480 = 0.25 → no penalty (threshold 0.30)
	// no violations, no idle, ~1kps keystrokes → no penalty
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  240,
		NeutralAppMinutes:     120,
		DistractingAppMinutes: 120,
		KeystrokeCount:        30000,
	})
	if got.Score < 55 || got.Score > 65 {
		t.Errorf("balanced mix: want ~63, got %d", got.Score)
	}
	if got.PenaltyReason != "" {
		t.Errorf("balanced (below 30%% distraction) should not carry penalty reason, got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_BalancedMixJustOverDistractionThreshold(t *testing.T) {
	// 200 productive, 100 neutral, 180 distracting → distraction ratio = 0.375
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  200,
		NeutralAppMinutes:     100,
		DistractingAppMinutes: 180,
		KeystrokeCount:        30000,
	})
	if got.PenaltyReason != "high_distraction" {
		t.Errorf("expected high_distraction penalty, got %q", got.PenaltyReason)
	}
	// Without penalty: (200 + 50) / 480 = 0.520 → 52
	// With -10: 42
	if got.Score < 38 || got.Score > 48 {
		t.Errorf("over-threshold distraction: want ~42, got %d", got.Score)
	}
}

func TestComputeProductivity_ViolationsCapPenaltyAt20(t *testing.T) {
	// 10 violations × 5 = 50, but cap = 20
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  480,
		NeutralAppMinutes:     0,
		DistractingAppMinutes: 0,
		KeystrokeCount:        50000,
		PolicyViolations:      10,
	})
	// baseline 100 - 20 = 80
	if got.Score != 80 {
		t.Errorf("violations cap: want 80, got %d", got.Score)
	}
	if v := got.Weights["violation_penalty"]; v != -20.0 {
		t.Errorf("violation_penalty weight: want -20, got %v", v)
	}
	if got.PenaltyReason != "violations" {
		t.Errorf("penalty reason: want 'violations', got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_ViolationsBelowCap(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  480,
		KeystrokeCount:        50000,
		PolicyViolations:      2,
	})
	// 100 - (2*5) = 90
	if got.Score != 90 {
		t.Errorf("2 violations: want 90, got %d", got.Score)
	}
}

func TestComputeProductivity_IdleHeavyPenalty(t *testing.T) {
	// idle 240 min > 0.40 * 480 = 192 → penalty applies
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           240,
		ProductiveAppMinutes:  300,
		NeutralAppMinutes:     60,
		DistractingAppMinutes: 0,
		KeystrokeCount:        30000,
	})
	if got.PenaltyReason != "low_active" {
		t.Errorf("expected low_active penalty, got %q", got.PenaltyReason)
	}
	// baseline = (300 + 30) / (300+60+0+120) = 330/480 = 0.6875 → 68.75
	// -10 for idle → ~59
	if got.Score < 55 || got.Score > 62 {
		t.Errorf("idle-heavy: want ~59, got %d", got.Score)
	}
}

func TestComputeProductivity_LowKeystrokePenalty(t *testing.T) {
	// 480 active min, only 500 keystrokes → kps ≈ 0.017 << 0.3
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  480,
		NeutralAppMinutes:     0,
		DistractingAppMinutes: 0,
		KeystrokeCount:        500,
	})
	// 100 - 5 = 95
	if got.Score != 95 {
		t.Errorf("low keystrokes: want 95, got %d", got.Score)
	}
	if got.PenaltyReason != "low_keystrokes" {
		t.Errorf("penalty reason: want 'low_keystrokes', got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_LowKeystrokeSkippedOnShortDay(t *testing.T) {
	// 30 active minutes → kps threshold doesn't apply (active < 60)
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         30,
		IdleMinutes:           0,
		ProductiveAppMinutes:  30,
		NeutralAppMinutes:     0,
		DistractingAppMinutes: 0,
		KeystrokeCount:        0,
	})
	// Should be 100 (pure productive) — no keystroke penalty
	if got.Score != 100 {
		t.Errorf("short day pure-productive no-keys: want 100, got %d", got.Score)
	}
	if got.PenaltyReason != "" {
		t.Errorf("short day: want no penalty, got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_MixedPenaltiesAggregatedToMixed(t *testing.T) {
	// high_distraction + violations + low_active → penalty reason = "mixed"
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           250,
		ProductiveAppMinutes:  100,
		NeutralAppMinutes:     80,
		DistractingAppMinutes: 200, // 200 / 380 = 0.526 > 0.30
		KeystrokeCount:        30000,
		PolicyViolations:      3,
	})
	if got.PenaltyReason != "mixed" {
		t.Errorf("expected 'mixed' penalty reason, got %q", got.PenaltyReason)
	}
	if got.Score < 0 || got.Score > 100 {
		t.Errorf("score out of bounds: %d", got.Score)
	}
}

func TestComputeProductivity_ZeroInputsReturnZero(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{})
	if got.Score != 0 {
		t.Errorf("all zero: want 0, got %d", got.Score)
	}
	if got.PenaltyReason != "" {
		t.Errorf("all zero: want empty penalty, got %q", got.PenaltyReason)
	}
}

func TestComputeProductivity_NegativeInputsClampedSafely(t *testing.T) {
	// Defensive: no panic, no NaN, no negative score
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         -100,
		IdleMinutes:           -50,
		ProductiveAppMinutes:  -30,
		NeutralAppMinutes:     -10,
		DistractingAppMinutes: -5,
		KeystrokeCount:        -1000,
		PolicyViolations:      -3,
	})
	if got.Score != 0 {
		t.Errorf("negative inputs defensive clamp: want 0, got %d", got.Score)
	}
}

func TestComputeProductivity_ClampNeverExceeds100(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         10000,
		ProductiveAppMinutes:  10000,
		KeystrokeCount:        500000,
	})
	if got.Score > 100 {
		t.Errorf("score exceeds 100: %d", got.Score)
	}
	if got.Score < 100 {
		t.Errorf("max productive should be 100, got %d", got.Score)
	}
}

func TestComputeProductivity_WeightsSurfaceFactors(t *testing.T) {
	got := ComputeProductivity(ProductivityInputs{
		ActiveMinutes:         480,
		IdleMinutes:           0,
		ProductiveAppMinutes:  240,
		NeutralAppMinutes:     120,
		DistractingAppMinutes: 120,
		KeystrokeCount:        30000,
	})
	// Verify expected keys are present — UI will render per-factor chart
	for _, key := range []string{"productive", "neutral", "distracting", "idle", "active"} {
		if _, ok := got.Weights[key]; !ok {
			t.Errorf("weights map missing key %q", key)
		}
	}
}
