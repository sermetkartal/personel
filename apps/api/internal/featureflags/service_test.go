package featureflags

import (
	"context"
	"fmt"
	"testing"
)

// Pure-function evaluator tests: no Postgres, no audit recorder. These
// exercise every branch of evaluate() so the hot-path contract is
// locked down.

func TestEvaluateMasterSwitch(t *testing.T) {
	f := Flag{Key: "x", Enabled: false, DefaultValue: true, RolloutPercentage: 100}
	if evaluate(f, EvalContext{TenantID: "t", UserID: "u"}) {
		t.Fatal("Enabled=false must override everything")
	}
}

func TestEvaluateRolloutZero(t *testing.T) {
	f := Flag{Key: "x", Enabled: true, DefaultValue: false, RolloutPercentage: 0}
	if evaluate(f, EvalContext{TenantID: "t", UserID: "u"}) {
		t.Fatal("rollout=0 must return default")
	}
	f.DefaultValue = true
	if !evaluate(f, EvalContext{TenantID: "t", UserID: "u"}) {
		t.Fatal("rollout=0 with default=true must return true")
	}
}

func TestEvaluateRolloutHundred(t *testing.T) {
	f := Flag{Key: "x", Enabled: true, DefaultValue: false, RolloutPercentage: 100}
	if !evaluate(f, EvalContext{TenantID: "t", UserID: "u"}) {
		t.Fatal("rollout=100 must return true")
	}
}

func TestEvaluateTenantOverride(t *testing.T) {
	f := Flag{
		Key:             "x",
		Enabled:         true,
		DefaultValue:    false,
		TenantOverrides: map[string]bool{"tenant-A": true},
	}
	if !evaluate(f, EvalContext{TenantID: "tenant-A"}) {
		t.Fatal("tenant-A override should force true")
	}
	if evaluate(f, EvalContext{TenantID: "tenant-B"}) {
		t.Fatal("tenant-B should fall through to default (false)")
	}
}

func TestEvaluateUserOverrideBeatsTenant(t *testing.T) {
	f := Flag{
		Key:             "x",
		Enabled:         true,
		DefaultValue:    false,
		TenantOverrides: map[string]bool{"t": true},
		UserOverrides:   map[string]bool{"u-skeptic": false},
	}
	if evaluate(f, EvalContext{TenantID: "t", UserID: "u-skeptic"}) {
		t.Fatal("user override must beat tenant override")
	}
	if !evaluate(f, EvalContext{TenantID: "t", UserID: "u-other"}) {
		t.Fatal("other user should still see tenant=true")
	}
}

func TestEvaluateRoleOverrideBeatsTenant(t *testing.T) {
	f := Flag{
		Key:             "x",
		Enabled:         true,
		DefaultValue:    false,
		TenantOverrides: map[string]bool{"t": true},
		RoleOverrides:   map[string]bool{"dpo": false},
	}
	if evaluate(f, EvalContext{TenantID: "t", Role: "dpo"}) {
		t.Fatal("role override must beat tenant")
	}
	// But user override beats role override.
	f.UserOverrides = map[string]bool{"u": true}
	if !evaluate(f, EvalContext{TenantID: "t", Role: "dpo", UserID: "u"}) {
		t.Fatal("user override must beat role override")
	}
}

func TestEvaluateRolloutDeterministic(t *testing.T) {
	// Same input must produce the same output across calls.
	f := Flag{Key: "x", Enabled: true, RolloutPercentage: 50}
	ec := EvalContext{TenantID: "t", UserID: "u"}
	first := evaluate(f, ec)
	for i := 0; i < 10; i++ {
		if evaluate(f, ec) != first {
			t.Fatal("evaluate must be deterministic for identical inputs")
		}
	}
}

func TestEvaluateRolloutDistribution(t *testing.T) {
	// Spot-check that rollout=50 actually bisects a sample population.
	// Not a statistical test — just a smoke check that the hash doesn't
	// return the same bucket for everyone.
	f := Flag{Key: "dashboard-v2", Enabled: true, RolloutPercentage: 50}
	trueCount := 0
	for i := 0; i < 1000; i++ {
		ec := EvalContext{TenantID: "t", UserID: fmt.Sprintf("u%d", i)}
		if evaluate(f, ec) {
			trueCount++
		}
	}
	if trueCount < 400 || trueCount > 600 {
		t.Fatalf("rollout=50 distribution seems off: got %d/1000", trueCount)
	}
}

func TestStableBucket(t *testing.T) {
	// Two identical inputs → same bucket; different inputs → (very likely)
	// different buckets.
	b1 := stableBucket("t", "u", "k")
	b2 := stableBucket("t", "u", "k")
	if b1 != b2 {
		t.Fatal("stableBucket must be deterministic")
	}
	if stableBucket("t", "u", "k") == stableBucket("t", "u2", "k") &&
		stableBucket("t", "u3", "k") == stableBucket("t", "u2", "k") {
		t.Fatal("all buckets collided — hash quality suspect")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		f       Flag
		wantErr bool
	}{
		{"empty key", Flag{}, true},
		{"ok", Flag{Key: "x", RolloutPercentage: 50}, false},
		{"rollout negative", Flag{Key: "x", RolloutPercentage: -1}, true},
		{"rollout too high", Flag{Key: "x", RolloutPercentage: 101}, true},
		{"key too long", Flag{Key: longString(129), RolloutPercentage: 10}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.f)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate(%+v) err=%v, wantErr=%v", tc.f, err, tc.wantErr)
			}
		})
	}
}

func TestIsEnabledNilPool(t *testing.T) {
	// Service with nil pool must never panic and must return default.
	s := &Service{cache: map[string]cachedFlag{}, cacheTTL: 0}
	ctx := context.Background()
	if s.IsEnabled(ctx, "unknown", EvalContext{}, false) != false {
		t.Fatal("unknown flag with def=false should be false")
	}
	if s.IsEnabled(ctx, "unknown", EvalContext{}, true) != true {
		t.Fatal("unknown flag with def=true should be true")
	}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
