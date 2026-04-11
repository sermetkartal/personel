package evidence

import (
	"testing"
)

func TestExpectedControlsSnapshot(t *testing.T) {
	// The CODE source of truth for "complete coverage" — every entry
	// here represents a control that MUST produce at least one evidence
	// item per period. Changes to this list are a deliberate gap signal
	// until the corresponding collector ships. Snapshot test guards
	// against accidental removal.
	got := expectedControls()
	required := map[ControlID]bool{
		CtrlCC6_1: false,
		CtrlCC6_3: false,
		CtrlCC7_1: false,
		CtrlCC7_3: false,
		CtrlCC8_1: false,
		CtrlCC9_1: false,
		CtrlA1_2:  false,
		CtrlP5_1:  false,
		CtrlP7_1:  false,
	}
	for _, c := range got {
		if _, ok := required[c]; ok {
			required[c] = true
		}
	}
	for c, seen := range required {
		if !seen {
			t.Errorf("expected control %q missing from expectedControls()", c)
		}
	}
}

func TestCollectionPeriodValidation(t *testing.T) {
	cases := map[string]bool{
		"2026-04":      true,
		"2025-12":      true,
		"2026-4":       false, // single-digit month
		"2026-04-01":   false, // full date
		"April 2026":   false,
		"":             false,
		"2026/04":      false,
	}
	for in, want := range cases {
		if got := collectionPeriodRE.MatchString(in); got != want {
			t.Errorf("period %q: got %v, want %v", in, got, want)
		}
	}
}
