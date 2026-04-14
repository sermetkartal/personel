// Package statuspage — HTTP handlers.
package statuspage

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// PublicHandler — GET /public/status.json
//
// This handler is mounted OUTSIDE the /v1/* auth group so it is
// reachable without a bearer token. The payload contains no tenant
// data, no PII, and no internal hostnames — only the public-facing
// component health + incident titles. Operators can safely expose
// this URL to monitoring tools or customer dashboards.
func PublicHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			// Degraded mode — return a minimal "unknown" shape so
			// consumers don't see a 404 when the Service wasn't wired.
			httpx.WriteJSON(w, http.StatusOK, PublicStatus{
				Overall: "unknown",
			})
			return
		}
		status, err := svc.GetPublicStatus(r.Context())
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "status query failed", "err.statuspage")
			return
		}
		// Aggressive caching hint so dashboards don't hammer us
		w.Header().Set("Cache-Control", "public, max-age=30")
		httpx.WriteJSON(w, http.StatusOK, status)
	}
}

// CreateIncidentHandler — POST /v1/system/status/incidents
//
// Admin-gated. Accepts CreateIncidentRequest body.
func CreateIncidentHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		var body CreateIncidentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}
		inc, err := svc.CreateIncident(r.Context(), p.UserID, body)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.statuspage")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, inc)
	}
}

// ResolveIncidentHandler — POST /v1/system/status/incidents/{id}/resolve
func ResolveIncidentHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			idStr = r.URL.Path // best effort — router normally supplies it
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "invalid id", "err.validation")
			return
		}
		if err := svc.ResolveIncident(r.Context(), p.UserID, id); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.statuspage")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
