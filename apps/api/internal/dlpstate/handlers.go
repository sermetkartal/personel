package dlpstate

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// GetDLPStateHandler — GET /v1/system/dlp-state
//
// Returns the current DLP deployment state. Readable by all authenticated roles;
// the portal reads this via the employee service account to display the banner.
// This endpoint does NOT write an audit entry because GET is non-mutating.
func GetDLPStateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.GetStatus(r.Context())
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, status)
	}
}

// TransitionHandler — POST /v1/system/dlp-transition
//
// Atomically updates DLP state + writes audit entry + surfaces banner for the
// transparency portal. Called by infra/scripts/dlp-enable.sh and dlp-disable.sh
// after the script has completed all out-of-API side effects (Vault Secret ID,
// container start, form verification).
//
// Accepts actions: "enable-complete", "enable-failed", "disable-complete".
//
// Required role: dlp-admin
func TransitionHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TransitionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}

		resp, err := svc.Transition(r.Context(), req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Transition Failed", "err.validation")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

// BootstrapPEDEKsHandler — POST /v1/system/dlp-bootstrap-keys
//
// Generates fresh PE-DEKs for all enrolled endpoints that do not yet have one.
// Authenticated as the dlp-admin service account (Vault-issued short-lived token).
// Idempotent: endpoints that already have a key are skipped.
//
// Required role: dlp-admin
func BootstrapPEDEKsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())

		// Discard body — spec says empty body.
		_ = json.NewDecoder(r.Body).Decode(&struct{}{})

		result, err := svc.BootstrapPEDEKs(r.Context(), p.TenantID, p.UserID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}
