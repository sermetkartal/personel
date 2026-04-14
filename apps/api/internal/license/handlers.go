// Package license — HTTP handlers for admin license surfacing.
package license

import (
	"net/http"

	"github.com/personel/api/internal/httpx"
)

// GetSummaryHandler returns the current license state. GET /v1/system/license.
// Gated to admin + DPO in the router.
func GetSummaryHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable,
				httpx.ProblemTypeInternal, "License service not configured", "err.license.unavailable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, svc.Summary())
	}
}

// RefreshHandler triggers an on-demand license reload. POST /v1/system/license/refresh.
// Useful after an operator drops a new license file into /etc/personel/.
func RefreshHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable,
				httpx.ProblemTypeInternal, "License service not configured", "err.license.unavailable")
			return
		}
		if err := svc.Refresh(r.Context()); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.license.refresh")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, svc.Summary())
	}
}
