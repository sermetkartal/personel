package settings

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

func writeErr(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.WriteError(w, r, status, httpx.ProblemTypeValidation, msg, "err.validation")
}

// GetCaModeHandler handles GET /v1/settings/ca-mode.
func GetCaModeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetCaMode(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

// UpdateCaModeHandler handles PATCH /v1/settings/ca-mode.
func UpdateCaModeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req UpdateCaModeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if err := svc.UpdateCaMode(r.Context(), p.UserID, p.TenantID, req); err != nil {
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetRetentionHandler handles GET /v1/settings/retention.
func GetRetentionHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetRetention(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

// UpdateRetentionHandler handles PATCH /v1/settings/retention.
func UpdateRetentionHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req RetentionPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if err := svc.UpdateRetention(r.Context(), p.UserID, p.TenantID, req); err != nil {
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
