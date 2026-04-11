// Package endpoint — HTTP handlers.
package endpoint

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		list, err := svc.List(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		// Filter by status query param (active | revoked) if supplied.
		// Phase 1: the store returns all rows; we apply the filter here
		// to avoid a store-level schema change. Keep pagination shape
		// consistent with other list endpoints so the console can read
		// response.pagination.total uniformly.
		status := r.URL.Query().Get("status")
		filtered := list
		if status == "active" {
			filtered = filtered[:0]
			for _, e := range list {
				if e.IsActive {
					filtered = append(filtered, e)
				}
			}
		} else if status == "revoked" {
			filtered = filtered[:0]
			for _, e := range list {
				if !e.IsActive {
					filtered = append(filtered, e)
				}
			}
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": filtered,
			"pagination": map[string]any{
				"page":      1,
				"page_size": len(filtered),
				"total":     len(filtered),
			},
		})
	}
}

func EnrollHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		token, err := svc.Enroll(r.Context(), p)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, token)
	}
}

func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")
		e, err := svc.Get(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, e)
	}
}

func DeleteHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")
		if err := svc.Delete(r.Context(), p, p.TenantID, id); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func RevokeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")
		if err := svc.Revoke(r.Context(), p, p.TenantID, id); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
