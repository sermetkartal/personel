// Package policy — PreviewHandler provides a dry-run preview of a policy
// before sign-and-push. Called by the SensitivityGuard editor as a preflight
// step (ADR 0013 amendment requirement).
package policy

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// policyGetter is the narrow interface PreviewHandler depends on.
// *Service satisfies it. The interface exists so tests can inject a stub
// without a live Postgres pool.
type policyGetter interface {
	Get(ctx context.Context, tenantID, id string) (*Policy, error)
}

// previewResponse is the JSON shape returned by PreviewHandler.
// valid=false means the console should block sign-and-push.
// errors contains field-level validation failures (keyed by field path).
// warnings contains advisory messages that do not block push.
type previewResponse struct {
	PolicyID string            `json:"policyId"`
	Version  int64             `json:"version"`
	Valid    bool              `json:"valid"`
	Errors   map[string]string `json:"errors"`
	Warnings []string          `json:"warnings"`
	Summary  previewSummary    `json:"summary"`
}

type previewSummary struct {
	RuleCount        int                     `json:"ruleCount"`
	Signed           bool                    `json:"signed"`
	SensitivityGuard sensitivityGuardSummary `json:"sensitivityGuard"`
}

type sensitivityGuardSummary struct {
	WindowTitleSensitiveRegexCount int `json:"windowTitleSensitiveRegexCount"`
	ScreenshotExcludeAppsCount     int `json:"screenshotExcludeAppsCount"`
}

// adr0013Warning is the human-readable ADR 0013 invariant warning message
// surfaced in the preview response when dlp_enabled=false AND
// keystroke.content_enabled=true. Unlike ValidateBundle (which hard-rejects
// at sign time), the preview surfaces this as a warning so the editor can
// show an explanatory UI before the user attempts push.
const adr0013Warning = "ADR 0013: keystroke.content_enabled=true requires dlp_enabled=true — push will be rejected until DLP is enabled via the opt-in ceremony"

// PreviewHandler — GET /v1/policies/{policyID}/preview
//
// Dry-runs the policy validation pipeline without persisting anything.
// Returns HTTP 200 with a structured response even when the policy has
// errors (valid=false). Only 404 (policy not found) and 401 (unauthenticated)
// return non-200 status codes.
func PreviewHandler(svc *Service) http.HandlerFunc {
	return previewHandlerFrom(svc)
}

// previewHandlerFrom accepts the narrow policyGetter interface so tests can
// inject a stub without a live Postgres pool.
func previewHandlerFrom(getter policyGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "policyID")

		pol, err := getter.Get(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}

		resp := previewResponse{
			PolicyID: pol.ID,
			Version:  pol.Version,
			Errors:   make(map[string]string),
			Warnings: []string{},
		}

		// --- Step 1: Validate PolicyRules (regex, intervals, app lists) ---
		fieldErrs, err := Validate(pol.Rules)
		if err != nil {
			// Unmarshal failure: rules JSON is malformed.
			resp.Errors["rules"] = err.Error()
		} else {
			for k, v := range fieldErrs {
				resp.Errors[k] = v
			}
		}

		// --- Step 2: ADR 0013 invariant check (warning, not hard error in preview) ---
		var inv BundleInvariants
		if jsonErr := json.Unmarshal(pol.Rules, &inv); jsonErr == nil {
			if !inv.DLPEnabled && inv.Keystroke.ContentEnabled {
				resp.Warnings = append(resp.Warnings, adr0013Warning)
			}
		}

		// --- Step 3: Assemble summary ---
		var rules PolicyRules
		if jsonErr := json.Unmarshal(pol.Rules, &rules); jsonErr == nil {
			ruleCount := 0
			if rules.ScreenshotEnabled {
				ruleCount++
			}
			if rules.KeystrokeEnabled {
				ruleCount++
			}
			if rules.NetworkFlowEnabled {
				ruleCount++
			}
			if rules.FileEventEnabled {
				ruleCount++
			}
			if rules.USBBlockEnabled {
				ruleCount++
			}
			if len(rules.AppBlockList) > 0 {
				ruleCount++
			}
			if len(rules.URLBlockList) > 0 {
				ruleCount++
			}

			resp.Summary = previewSummary{
				RuleCount: ruleCount,
				Signed:    pol.Version > 0, // any persisted version implies at least one sign cycle
				SensitivityGuard: sensitivityGuardSummary{
					WindowTitleSensitiveRegexCount: len(rules.SensitivityGuard.WindowTitleSensitiveRegex),
					ScreenshotExcludeAppsCount:     len(rules.SensitivityGuard.ScreenshotExcludeApps),
				},
			}
		}

		resp.Valid = len(resp.Errors) == 0

		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}
