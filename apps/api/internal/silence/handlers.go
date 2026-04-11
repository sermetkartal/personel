// Package silence — HTTP handlers for agent silence dashboard.
package silence

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
	"github.com/personel/api/internal/reports"
)

// ListHandler — GET /v1/silence
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := reports.ParseTimeRange(r)
		gaps, err := svc.List(r.Context(), p.TenantID, from, to)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": gaps})
	}
}

// TimelineHandler — GET /v1/silence/{endpointID}/timeline
func TimelineHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		endpointID := chi.URLParam(r, "endpointID")
		from, to := reports.ParseTimeRange(r)
		gaps, err := svc.Timeline(r.Context(), p.TenantID, endpointID, from, to)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": gaps})
	}
}

// AcknowledgeHandler — POST /v1/silence/{endpointID}/acknowledge
func AcknowledgeHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		SilenceAt time.Time `json:"silence_at"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		endpointID := chi.URLParam(r, "endpointID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Acknowledge(r.Context(), p, endpointID, body.SilenceAt); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
