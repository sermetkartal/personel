// Package scoring — Faz 8 #85 + #86.
//
// This package centralises two algorithms that the rest of Personel has
// historically computed ad-hoc in seed scripts, the enricher, and the
// employee_daily_stats rollup:
//
//  1. ComputeProductivity — 0..100 explainable productivity score
//     powering employee_daily_stats.productivity_score (#85).
//
//  2. ComputeRisk — 0..100 insider-threat risk score combining UBA
//     anomaly output + DLP signals (#86).
//
// Both functions are pure (no I/O, no globals) so they can be exercised
// directly in unit tests. The HTTP surface that exposes them lives in
// internal/reports/scoring_handlers.go.
//
// KVKK m.11/g: the risk score is ADVISORY ONLY. The scoring package does
// not make any access-control decision. The RiskResult.AdvisoryOnly field
// is hard-coded to true and cannot be disabled — the pack exporter, the
// console UI, and the API response all carry the disclaimer.
package scoring

// ProductivityInputs aggregates the signals needed to compute a single
// day's productivity score for one employee. All field semantics match
// employee_daily_stats (migration 0026) + policy.blocked_* events.
type ProductivityInputs struct {
	// ActiveMinutes is the wall-clock minutes the employee was actively
	// using the endpoint (foreground app + keystrokes/mouse present).
	ActiveMinutes int
	// IdleMinutes is the wall-clock minutes the endpoint reported idle.
	IdleMinutes int
	// ProductiveAppMinutes is the time spent in apps classified as
	// productive (work tools, business software, IDE, etc).
	ProductiveAppMinutes int
	// NeutralAppMinutes is time in apps classified as neutral
	// (browser homepage, unknown).
	NeutralAppMinutes int
	// DistractingAppMinutes is time in distracting categories
	// (social media, video streaming, gaming).
	DistractingAppMinutes int
	// KeystrokeCount is the keystroke total for the day (used as a
	// sanity signal, NOT a performance metric — content is never read).
	KeystrokeCount int
	// PolicyViolations is the count of blocked_app + blocked_web +
	// dlp_match events on the day.
	PolicyViolations int
}

// ProductivityResult is the output of ComputeProductivity.
type ProductivityResult struct {
	// Score is the clamped 0..100 productivity score.
	Score int
	// Weights surfaces each factor's contribution for UI explainability.
	// Keys: "productive", "neutral", "distracting", "idle", "active",
	// "distraction_penalty", "violation_penalty", "idle_penalty",
	// "keystroke_penalty".
	Weights map[string]float64
	// PenaltyReason is a short enum-like tag describing the dominant
	// penalty, or "" when none apply. One of:
	//   "high_distraction", "violations", "low_active",
	//   "low_keystrokes", "mixed", "".
	PenaltyReason string
}

// ComputeProductivity maps ProductivityInputs to a 0..100 score using
// a transparent weighted baseline + three explicit penalties. The
// algorithm is documented verbatim in the package doc and the #85
// brief in CLAUDE.md §0 so auditors and employees can reconstruct any
// given day's score by hand.
//
// Algorithm:
//
//	baseline = (productive × 1.0 + neutral × 0.5) /
//	           (productive + neutral + distracting + idle/2)
//	score    = baseline × 100
//	  -10 if distracting_ratio > 0.30
//	  -min(20, violations × 5) if violations > 0
//	  -10 if idle > 0.40 × active
//	  -5  if kps < 0.3 and active > 60
//	score    = clamp(score, 0, 100)
func ComputeProductivity(in ProductivityInputs) ProductivityResult {
	weights := map[string]float64{
		"productive":          0,
		"neutral":             0,
		"distracting":         0,
		"idle":                0,
		"active":              float64(in.ActiveMinutes),
		"distraction_penalty": 0,
		"violation_penalty":   0,
		"idle_penalty":        0,
		"keystroke_penalty":   0,
	}

	// Defensive: clamp negative inputs to zero so a malformed rollup
	// row doesn't silently sink the score below zero.
	prod := nonNegInt(in.ProductiveAppMinutes)
	neut := nonNegInt(in.NeutralAppMinutes)
	dist := nonNegInt(in.DistractingAppMinutes)
	idle := nonNegInt(in.IdleMinutes)
	active := nonNegInt(in.ActiveMinutes)
	viols := nonNegInt(in.PolicyViolations)
	keys := nonNegInt(in.KeystrokeCount)

	// Denominator: total "accountable" minutes, with idle weighted at
	// half because idle time isn't a true productivity penalty — just
	// a neutral drag.
	denom := float64(prod) + float64(neut) + float64(dist) + float64(idle)/2.0
	if denom <= 0 {
		// Nothing to measure — return neutral zero, no penalty reason.
		return ProductivityResult{Score: 0, Weights: weights, PenaltyReason: ""}
	}

	baseline := (float64(prod)*1.0 + float64(neut)*0.5) / denom
	score := baseline * 100.0

	weights["productive"] = float64(prod) / denom
	weights["neutral"] = float64(neut) / denom
	weights["distracting"] = float64(dist) / denom
	weights["idle"] = float64(idle) / denom

	// --- Penalty 1: distraction ratio ------------------------------------
	totalAppMin := prod + neut + dist
	distractingRatio := 0.0
	if totalAppMin > 0 {
		distractingRatio = float64(dist) / float64(totalAppMin)
	}
	var reasons []string
	if distractingRatio > 0.30 {
		score -= 10
		weights["distraction_penalty"] = -10
		reasons = append(reasons, "high_distraction")
	}

	// --- Penalty 2: policy violations ------------------------------------
	if viols > 0 {
		p := float64(viols) * 5.0
		if p > 20.0 {
			p = 20.0
		}
		score -= p
		weights["violation_penalty"] = -p
		reasons = append(reasons, "violations")
	}

	// --- Penalty 3: excessive idle ---------------------------------------
	if active > 0 && float64(idle) > 0.40*float64(active) {
		score -= 10
		weights["idle_penalty"] = -10
		reasons = append(reasons, "low_active")
	}

	// --- Penalty 4: suspiciously low typing ------------------------------
	// Only applies if there was meaningful activity (>60 active minutes)
	// AND the keystrokes-per-second rate looks pathologically low.
	// This catches "window focus but no real work" patterns, but doesn't
	// punish roles where typing isn't the primary work output
	// (designers, ops personnel running scripts) unless paired with
	// distraction — those typically trip the distraction penalty first.
	if active > 60 {
		activeSeconds := float64(active) * 60.0
		kps := float64(keys) / activeSeconds
		if kps < 0.3 {
			score -= 5
			weights["keystroke_penalty"] = -5
			reasons = append(reasons, "low_keystrokes")
		}
	}

	// Clamp to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	var reason string
	switch len(reasons) {
	case 0:
		reason = ""
	case 1:
		reason = reasons[0]
	default:
		reason = "mixed"
	}

	return ProductivityResult{
		Score:         int(score + 0.5), // round half-up
		Weights:       weights,
		PenaltyReason: reason,
	}
}

// nonNegInt clamps negative inputs to zero. This is a defensive guard
// rather than panicking — malformed seed data should not 500 the API.
func nonNegInt(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
