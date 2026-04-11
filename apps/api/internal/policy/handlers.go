// Package policy — HTTP handlers for policy endpoints.
package policy

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// ListHandler — GET /v1/policies
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		list, err := svc.List(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// CreateHandler — POST /v1/policies
func CreateHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Rules       json.RawMessage `json:"rules"`
		IsDefault   bool            `json:"is_default"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}

		pol, err := svc.Create(r.Context(), p, CreateInput{
			TenantID:    p.TenantID,
			Name:        body.Name,
			Description: body.Description,
			Rules:       body.Rules,
			CreatedBy:   p.UserID,
			IsDefault:   body.IsDefault,
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, pol)
	}
}

// GetHandler — GET /v1/policies/{policyID}
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "policyID")
		pol, err := svc.Get(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, pol)
	}
}

// UpdateHandler — PATCH /v1/policies/{policyID}
func UpdateHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Rules       json.RawMessage `json:"rules"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "policyID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Update(r.Context(), p, id, p.TenantID, body.Rules, body.Name, body.Description); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteHandler — DELETE /v1/policies/{policyID}
func DeleteHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "policyID")
		if err := svc.Delete(r.Context(), p, id, p.TenantID); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// PushHandler — POST /v1/policies/{policyID}/push
func PushHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		EndpointID string `json:"endpoint_id"` // empty = broadcast
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "policyID")
		var body reqBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := svc.Push(r.Context(), p, id, p.TenantID, body.EndpointID); err != nil {
			if errors.Is(err, ErrInvalidInvariantDLPKeystroke) {
				httpx.WriteValidationError(w, r, map[string]string{
					"keystroke.content_enabled": httpx.TRString("err.policy_invariant_dlp"),
				})
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
