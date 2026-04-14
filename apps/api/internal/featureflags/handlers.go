// Package featureflags — HTTP handlers for the admin console.
//
// Routes (wired in httpserver/server.go under /v1/system/feature-flags):
//
//	GET    /v1/system/feature-flags             — list all
//	GET    /v1/system/feature-flags/{key}       — get one
//	PUT    /v1/system/feature-flags/{key}       — create / update
//	DELETE /v1/system/feature-flags/{key}       — remove
//
// RBAC: admin + dpo. Gated at the router layer.
package featureflags

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// ListHandler — GET /v1/system/feature-flags
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flags, err := svc.List(r.Context())
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "list failed", "err.internal")
			return
		}
		if flags == nil {
			flags = []Flag{}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"flags": flags,
		})
	}
}

// GetHandler — GET /v1/system/feature-flags/{key}
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		f, err := svc.Get(r.Context(), key)
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound,
				httpx.ProblemTypeNotFound, "flag not found", "err.not_found")
			return
		}
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "get failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, f)
	}
}

// SetHandler — PUT /v1/system/feature-flags/{key}
func SetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "unauthenticated", "err.unauthenticated")
			return
		}

		key := chi.URLParam(r, "key")
		var body Flag
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "invalid body", "err.validation")
			return
		}
		// URL key is authoritative — body cannot rename.
		body.Key = key

		if err := svc.Set(r.Context(), p.UserID, body); err != nil {
			if errors.Is(err, ErrInvalidInput) {
				httpx.WriteError(w, r, http.StatusUnprocessableEntity,
					httpx.ProblemTypeValidation, err.Error(), "err.validation")
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "set failed", "err.internal")
			return
		}

		updated, err := svc.Get(r.Context(), key)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "read-after-write failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updated)
	}
}

// DeleteHandler — DELETE /v1/system/feature-flags/{key}
func DeleteHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "unauthenticated", "err.unauthenticated")
			return
		}
		key := chi.URLParam(r, "key")
		if err := svc.Delete(r.Context(), p.UserID, key); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "delete failed", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
