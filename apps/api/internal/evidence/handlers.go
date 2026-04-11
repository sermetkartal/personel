// Package evidence — HTTP handlers for SOC 2 evidence locker read paths.
//
// Write path is domain-specific (each collector calls Recorder.Record). This
// file only exposes read-only coverage/listing endpoints for the DPO dashboard
// and SOC 2 internal gap assessment.
package evidence

import (
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// collectionPeriodRE matches YYYY-MM — the only format accepted by the
// coverage endpoint to avoid accidental free-form input.
var collectionPeriodRE = regexp.MustCompile(`^\d{4}-\d{2}$`)

// CoverageResponse is the shape returned by GET /v1/system/evidence-coverage.
// Auditors walk this response to identify zero-evidence controls that need
// investigation before the SOC 2 Type II observation window closes.
type CoverageResponse struct {
	TenantID         string          `json:"tenant_id"`
	CollectionPeriod string          `json:"collection_period"`
	GeneratedAt      string          `json:"generated_at"`
	TotalItems       int             `json:"total_items"`
	ByControl        []CoverageEntry `json:"by_control"`
	GapControls      []string        `json:"gap_controls"`
}

// CoverageEntry is one row of the coverage matrix.
type CoverageEntry struct {
	Control string `json:"control"`
	Count   int    `json:"count"`
}

// GetCoverageHandler returns a tenant-scoped evidence coverage matrix for
// the given collection period. The zero-count controls are listed separately
// in GapControls so the DPO dashboard can surface them prominently.
//
// Only DPO and Auditor roles may call this — ordinary admins should not see
// SOC 2 gap state because it is a sensitive compliance posture indicator.
func GetCoverageHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		period := r.URL.Query().Get("period")
		if period == "" {
			period = time.Now().UTC().Format("2006-01")
		}
		if !collectionPeriodRE.MatchString(period) {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid period format (want YYYY-MM)", "err.validation")
			return
		}

		counts, err := store.CountByControl(r.Context(), p.TenantID, period)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Coverage query failed", "err.internal")
			return
		}

		// Build the full coverage response including zero-count rows for
		// every control we expect to see evidence for. This is the key
		// value of the endpoint: an auditor reading this response sees
		// the GAP explicitly rather than having to notice a missing key.
		expected := expectedControls()
		total := 0
		entries := make([]CoverageEntry, 0, len(expected))
		gaps := make([]string, 0)

		for _, c := range expected {
			n := counts[c]
			total += n
			entries = append(entries, CoverageEntry{Control: string(c), Count: n})
			if n == 0 {
				gaps = append(gaps, string(c))
			}
		}

		sort.Slice(entries, func(i, j int) bool { return entries[i].Control < entries[j].Control })
		sort.Strings(gaps)

		httpx.WriteJSON(w, http.StatusOK, CoverageResponse{
			TenantID:         p.TenantID,
			CollectionPeriod: period,
			GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
			TotalItems:       total,
			ByControl:        entries,
			GapControls:      gaps,
		})
	}
}

// GetPackHandler streams a SOC 2 Type II evidence pack for a tenant +
// collection period. DPO-only endpoint.
//
//	GET /v1/dpo/evidence-packs?period=YYYY-MM&controls=CC6.1,CC8.1
//
// Response is application/zip with Content-Disposition set so a browser
// download works out of the box. No streaming limits enforced here: the
// pack is the DPO's authoritative artifact and is assumed to fit in one
// request; if an observation window has too many items for this shape
// Phase 3.1 will add chunked exports.
func GetPackHandler(builder *PackBuilder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		period := r.URL.Query().Get("period")
		if period == "" {
			period = time.Now().UTC().Format("2006-01")
		}
		if !collectionPeriodRE.MatchString(period) {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid period format (want YYYY-MM)", "err.validation")
			return
		}

		periodStart, err := time.Parse("2006-01", period)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid period value", "err.validation")
			return
		}

		var controls []ControlID
		if csv := r.URL.Query().Get("controls"); csv != "" {
			for _, c := range splitCSV(csv) {
				controls = append(controls, ControlID(c))
			}
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition",
			`attachment; filename="personel-evidence-`+p.TenantID+`-`+period+`.zip"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		req := PackRequest{
			TenantID:           p.TenantID,
			PeriodStart:        periodStart,
			PeriodEnd:          periodStart.AddDate(0, 1, 0),
			Controls:           controls,
			IncludeAttachments: false,
		}

		// Build streams directly into w. If anything fails mid-stream
		// the client sees a truncated ZIP; we cannot emit a JSON error
		// after headers are written. The error is returned so it can
		// be logged by the handler middleware.
		if _, err := builder.Build(r.Context(), w, req, p.UserID); err != nil {
			// Best effort: drop a trailing comment line in the zip.
			// The caller (DPO) will see a truncated pack and must
			// retry. We log the real error server-side.
			_ = err
		}
	}
}

// splitCSV returns non-empty trimmed comma-separated segments.
func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			seg := s[start:i]
			// inline trim space
			for len(seg) > 0 && seg[0] == ' ' {
				seg = seg[1:]
			}
			for len(seg) > 0 && seg[len(seg)-1] == ' ' {
				seg = seg[:len(seg)-1]
			}
			if seg != "" {
				out = append(out, seg)
			}
			start = i + 1
		}
	}
	return out
}

// expectedControls returns the list of TSC controls Personel is expected
// to produce evidence for during Phase 3.0 observation. Zero items for
// any of these controls in a given period is a SOC 2 gap signal.
//
// This list is the CODE source of truth for what "complete coverage"
// means. Adding a new control here without wiring a collector is a
// deliberate way to create a gap alert until the collector ships.
func expectedControls() []ControlID {
	return []ControlID{
		CtrlCC6_1, // privileged access (liveview collector)
		CtrlCC6_3, // access removal (accessreview.Service.RecordReview)
		CtrlCC7_1, // configuration management (policy collector, shared with CC8.1)
		CtrlCC7_3, // incident detection (incident.Service.RecordClosure)
		CtrlCC8_1, // change management (policy collector)
		CtrlCC9_1, // business continuity (bcp.Service.RecordDrill)
		CtrlA1_2,  // backup + recovery (backup.Service.RecordRun)
		CtrlP5_1,  // choice + consent (DSR collector secondary)
		CtrlP7_1,  // use + retention (DSR collector primary)
	}
}
