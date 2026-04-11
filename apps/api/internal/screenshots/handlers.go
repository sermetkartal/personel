// Package screenshots — HTTP handlers for screenshot gallery.
package screenshots

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
	"github.com/personel/api/internal/reports"
)

// ListHandler — GET /v1/screenshots
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		endpointID := r.URL.Query().Get("endpoint_id")
		from, to := reports.ParseTimeRange(r)

		list, err := svc.List(r.Context(), p.TenantID, endpointID, from, to)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// PresignedURLHandler — GET /v1/screenshots/{screenshotID}/url
// Requires reason_code query param. Every call is audited.
func PresignedURLHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "screenshotID")
		reasonCode := r.URL.Query().Get("reason_code")

		if reasonCode == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, httpx.ProblemTypeValidation, "reason_code is required", "err.validation")
			return
		}

		// In a full implementation we'd look up the MinIO key from metadata store.
		// Here we accept the key as a query param from the admin console (which got it from the list endpoint).
		minioKey := r.URL.Query().Get("key")
		if minioKey == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "key is required", "err.validation")
			return
		}

		url, err := svc.IssuePresignedURL(r.Context(), p, PresignedURLRequest{
			ScreenshotID: id,
			TenantID:     p.TenantID,
			MinIOKey:     minioKey,
			ReasonCode:   reasonCode,
			ActorID:      p.UserID,
		})
		if err != nil {
			if err == auth.ErrForbidden {
				httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			} else {
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			}
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"url":        url,
			"expires_in": 60,
		})
	}
}
