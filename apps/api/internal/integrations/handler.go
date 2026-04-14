package integrations

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

func writeErr(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.WriteError(w, r, status, httpx.ProblemTypeValidation, msg, "err.validation")
}

// ListHandler handles GET /v1/settings/integrations.
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		items, err := svc.List(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// GetHandler handles GET /v1/settings/integrations/{service}.
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		service := chi.URLParam(r, "service")
		rec, err := svc.Get(r.Context(), p.TenantID, service)
		if err != nil {
			if errors.Is(err, ErrUnknownService) {
				writeErr(w, r, http.StatusBadRequest, err.Error())
				return
			}
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, r, http.StatusNotFound, "not found")
				return
			}
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, rec)
	}
}

// UpsertHandler handles PUT /v1/settings/integrations/{service}.
func UpsertHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		service := chi.URLParam(r, "service")
		var req UpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if err := svc.Upsert(r.Context(), p.UserID, p.TenantID, service, req); err != nil {
			status := http.StatusUnprocessableEntity
			switch {
			case errors.Is(err, ErrUnknownService):
				status = http.StatusBadRequest
			case errors.Is(err, ErrVaultUnavailable):
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// TestHandler handles POST /v1/settings/integrations/{service}/test.
// It runs the per-service connection probe and returns a TestResult
// JSON body. Unknown services return 400; the probe itself never
// returns an error — operator-readable "fail" rows carry the detail.
func TestHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		service := chi.URLParam(r, "service")
		result, err := svc.TestConnection(r.Context(), p.TenantID, service)
		if err != nil {
			if errors.Is(err, ErrUnknownService) {
				writeErr(w, r, http.StatusBadRequest, err.Error())
				return
			}
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

// DeleteHandler handles DELETE /v1/settings/integrations/{service}.
func DeleteHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		service := chi.URLParam(r, "service")
		if err := svc.Delete(r.Context(), p.UserID, p.TenantID, service); err != nil {
			if errors.Is(err, ErrUnknownService) {
				writeErr(w, r, http.StatusBadRequest, err.Error())
				return
			}
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
