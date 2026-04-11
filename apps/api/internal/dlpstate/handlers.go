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
