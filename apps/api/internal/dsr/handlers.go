// Package dsr — HTTP handlers for DSR endpoints.
package dsr

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// SubmitHandler — POST /v1/dsr
// Callable by employees (via portal) and DPO.
func SubmitHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		RequestType   RequestType    `json:"request_type" validate:"required,oneof=access rectify erase object restrict portability"`
		ScopeJSON     map[string]any `json:"scope"`
		Justification string         `json:"justification" validate:"required,min=10,max=2000"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}

		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}

		req, err := svc.Submit(r.Context(), SubmitInput{
			TenantID:       p.TenantID,
			EmployeeUserID: p.UserID,
			RequestType:    body.RequestType,
			ScopeJSON:      body.ScopeJSON,
			Justification:  body.Justification,
			ActorUA:        r.UserAgent(),
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusCreated, req)
	}
}

// ListHandler — GET /v1/dsr?state=open|overdue|closed (DPO only)
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())

		var states []State
		if raw := r.URL.Query().Get("state"); raw != "" {
			for _, s := range strings.Split(raw, ",") {
				states = append(states, State(strings.TrimSpace(s)))
			}
		}

		list, err := svc.List(r.Context(), p.TenantID, states)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		stats, _ := svc.Stats(r.Context(), p.TenantID)

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": list,
			"stats": stats,
			"pagination": map[string]any{
				"page":      1,
				"page_size": len(list),
				"total":     len(list),
			},
		})
	}
}

// GetHandler — GET /v1/dsr/{dsrID}
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "dsrID")

		req, err := svc.Get(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, req)
	}
}

// AssignHandler — POST /v1/dsr/{dsrID}/assign
func AssignHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		AssigneeID string `json:"assignee_id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "dsrID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AssigneeID == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Assign(r.Context(), p.TenantID, id, p.UserID, body.AssigneeID); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RespondHandler — POST /v1/dsr/{dsrID}/respond
func RespondHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		ArtifactRef string `json:"artifact_ref"` // MinIO path to response PDF
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "dsrID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ArtifactRef == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Respond(r.Context(), p.TenantID, id, p.UserID, body.ArtifactRef); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ExtendHandler — POST /v1/dsr/{dsrID}/extend
func ExtendHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Reason string `json:"reason"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "dsrID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Reason == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Extend(r.Context(), p.TenantID, id, p.UserID, body.Reason); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RejectHandler — POST /v1/dsr/{dsrID}/reject
func RejectHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Reason string `json:"reason"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "dsrID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Reason == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Reject(r.Context(), p.TenantID, id, p.UserID, body.Reason); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
