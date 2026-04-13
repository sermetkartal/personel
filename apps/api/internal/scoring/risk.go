// risk.go — Faz 8 #86 risk score (UBA + DLP signals → advisory 0..100).
//
// The risk score answers a single question for human reviewers: which
// users should an Investigator look at first? It is NEVER a basis for
// automated disciplinary action, access revocation, or any decision
// taken without a human in the loop.
//
// KVKK m.11/g: every RiskResult carries AdvisoryOnly=true and the API
// response that surfaces it includes the Turkish disclaimer. The scoring
// package refuses to emit a RiskResult with AdvisoryOnly=false — there
// is no code path that can unset it.

package scoring

// RiskInputs aggregates the per-user-per-day signals used by
// ComputeRisk. All fields are daily counts unless otherwise noted.
type RiskInputs struct {
	// UBAAnomalyScore is the isolation forest output for the day,
	// already normalised to [0.0, 1.0].
	UBAAnomalyScore float64
	// DLPBlockedCount is the number of blocked transfers / blocked
	// uploads / blocked_app / blocked_web events.
	DLPBlockedCount int
	// SensitiveFileAccess is the count of accesses to files tagged
	// sensitive by the SensitivityGuard policy.
	SensitiveFileAccess int
	// USBExternalTransfers is the count of file writes to
	// removable storage (USB mass storage / MTP) for the day.
	USBExternalTransfers int
	// OffHoursActivity is the fraction of activity outside 08-18
	// Istanbul local time, [0.0, 1.0].
	OffHoursActivity float64
	// TamperFindings is the number of anti-tamper detections
	// (service kill attempt, binary hash mismatch, watchdog restart
	// from self-heal path).
	TamperFindings int
	// AVDeactivationCount is the number of Windows Defender or
	// 3rd-party AV product deactivations observed during the day.
	AVDeactivationCount int
}

// RiskResult is the output of ComputeRisk. AdvisoryOnly is hard-coded
// true — there is no constructor path that can set it to false.
type RiskResult struct {
	// Score is the clamped 0..100 risk score.
	Score int
	// Tier is the human-facing bucket:
	// "low" | "medium" | "high" | "critical".
	Tier string
	// TopFactors lists the factors ordered by contribution descending.
	TopFactors []RiskFactor
	// AdvisoryOnly is ALWAYS true. KVKK m.11/g invariant.
	AdvisoryOnly bool
	// KVKK m.11/g disclaimer text in Turkish.
	Disclaimer string
}

// RiskFactor surfaces a single contributing signal for UI + audit
// explainability. Weight is the absolute contribution to the pre-clamp
// risk score, not a percentage.
type RiskFactor struct {
	Name        string
	Weight      float64
	Explanation string
}

// RiskDisclaimer is the Turkish KVKK m.11/g text that accompanies every
// risk response across the API, console and audit log. Single source of
// truth to guarantee consistency.
const RiskDisclaimer = "Bu skor karar destek amaçlıdır. KVKK m.11/g uyarınca otomatik " +
	"karar verme yasaktır; insan incelemesi olmadan aleyhine işlem yapılamaz."

// Tier thresholds (inclusive lower, exclusive upper on the right end).
const (
	tierLowMax      = 25
	tierMediumMax   = 50
	tierHighMax     = 75
	tierCriticalMin = 76
)

// ComputeRisk maps RiskInputs → RiskResult with a transparent weighted
// sum plus two hard-override compliance gates.
//
// Algorithm (matches brief #86):
//
//	base = (uba × 0.30 +
//	        min(dlp×5, 30) × 0.01 +
//	        min(sens×3, 20) × 0.01 +
//	        min(usb×5, 20) × 0.01 +
//	        offhrs × 20 +
//	        min(tamper×10, 30) × 0.01 +
//	        min(av×20, 40) × 0.01) × 100
//
// Hard overrides (compliance-critical signals never degrade below these):
//
//	tamper_findings ≥ 1  → tier ≥ "high"
//	av_deactivation ≥ 1  → tier ≥ "critical"
//
// Tier mapping:
//
//	0–25   → low
//	26–50  → medium
//	51–75  → high
//	76–100 → critical
func ComputeRisk(in RiskInputs) RiskResult {
	// Defensive: clamp UBAAnomalyScore and OffHoursActivity to [0,1].
	uba := clamp01(in.UBAAnomalyScore)
	offHrs := clamp01(in.OffHoursActivity)

	dlp := nonNegInt(in.DLPBlockedCount)
	sens := nonNegInt(in.SensitiveFileAccess)
	usb := nonNegInt(in.USBExternalTransfers)
	tamper := nonNegInt(in.TamperFindings)
	av := nonNegInt(in.AVDeactivationCount)

	// --- Weighted sum --------------------------------------------------------
	//
	// The scoring weights are chosen so that a fully anomalous UBA signal
	// alone (uba = 1.0) contributes 30 points; the DLP family tops out at
	// 30 + 20 + 20 = 70 combined; tamper family caps at 70; off-hours
	// saturates at 20. The ceiling without overrides is therefore 30 + 70
	// + 20 + 70 = 190; most realistic days will land in 0–60.
	dlpComponent := minFloat(float64(dlp)*5.0, 30.0)
	sensComponent := minFloat(float64(sens)*3.0, 20.0)
	usbComponent := minFloat(float64(usb)*5.0, 20.0)
	tamperComponent := minFloat(float64(tamper)*10.0, 30.0)
	avComponent := minFloat(float64(av)*20.0, 40.0)

	ubaContribution := uba * 30.0                      // UBA × 30
	dlpContribution := dlpComponent                    // already a penalty ≤ 30
	sensContribution := sensComponent                  // ≤ 20
	usbContribution := usbComponent                    // ≤ 20
	offHrsContribution := offHrs * 20.0                // ≤ 20
	tamperContribution := tamperComponent              // ≤ 30
	avContribution := avComponent                      // ≤ 40

	score := ubaContribution +
		dlpContribution +
		sensContribution +
		usbContribution +
		offHrsContribution +
		tamperContribution +
		avContribution

	// Clamp to [0, 100] — realistic full-saturation would top 160+ and
	// land at 100 via clamp. The 100-ceiling is intentional: anything
	// above 100 is "definitely investigate".
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	// --- Tier classification ------------------------------------------------
	tier := classifyTier(int(score + 0.5))

	// --- Hard-override compliance gates -------------------------------------
	// These are floors — they can only elevate the tier, never lower it.
	if tamper >= 1 && tierRank(tier) < tierRank("high") {
		tier = "high"
	}
	if av >= 1 {
		tier = "critical"
	}

	// --- Top factors for UI explainability ----------------------------------
	factors := []RiskFactor{
		{Name: "uba_anomaly", Weight: ubaContribution,
			Explanation: "UBA isolation forest anomaly score"},
		{Name: "dlp_blocked", Weight: dlpContribution,
			Explanation: "DLP blocked transfer count"},
		{Name: "sensitive_file_access", Weight: sensContribution,
			Explanation: "Access to files tagged sensitive"},
		{Name: "usb_transfers", Weight: usbContribution,
			Explanation: "External USB / MTP transfers"},
		{Name: "off_hours", Weight: offHrsContribution,
			Explanation: "Activity outside business hours"},
		{Name: "tamper_findings", Weight: tamperContribution,
			Explanation: "Anti-tamper detections"},
		{Name: "av_deactivation", Weight: avContribution,
			Explanation: "AV product deactivation events"},
	}
	// Sort descending by weight — no stdlib sort dependency, list is
	// tiny (7 entries) so an insertion sort is plenty.
	for i := 1; i < len(factors); i++ {
		for j := i; j > 0 && factors[j].Weight > factors[j-1].Weight; j-- {
			factors[j], factors[j-1] = factors[j-1], factors[j]
		}
	}
	// Trim trailing zero-weight factors so the UI doesn't render noise.
	trimmed := factors[:0]
	for _, f := range factors {
		if f.Weight > 0 {
			trimmed = append(trimmed, f)
		}
	}
	if len(trimmed) == 0 {
		trimmed = nil
	}

	return RiskResult{
		Score:        int(score + 0.5),
		Tier:         tier,
		TopFactors:   trimmed,
		AdvisoryOnly: true, // INVARIANT — KVKK m.11/g
		Disclaimer:   RiskDisclaimer,
	}
}

// classifyTier maps an integer score to one of the 4 tiers.
func classifyTier(score int) string {
	switch {
	case score <= tierLowMax:
		return "low"
	case score <= tierMediumMax:
		return "medium"
	case score <= tierHighMax:
		return "high"
	default:
		return "critical"
	}
}

// tierRank lets the hard-override logic compare tiers numerically.
func tierRank(t string) int {
	switch t {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	default:
		return -1
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
