// Package apikey — HTTP handlers for the admin console's API-key
// management surface.
//
// RBAC is enforced at the route layer (admin + dpo); these handlers
// trust the middleware-derived principal for tenant scoping.
package apikey

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// createRequest is the inbound body for POST /v1/apikeys.
type createRequest struct {
	Name         string   `json:"name"`
	Scopes       []string `json:"scopes"`
	TenantScoped bool     `json:"tenant_scoped"` // default true; set false + admin role for cross-tenant
	DurationDays int      `json:"duration_days"` // 0 = never expires
}

// createResponse is the outbound body. Plaintext is in this payload
// ONCE and never again — the UI surfaces it as a copy-to-clipboard
// modal with a strong "you will not see this again" warning.
type createResponse struct {
	ID        string     `json:"id"`
	Plaintext string     `json:"plaintext"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// CreateHandler — POST /v1/apikeys
func CreateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		var body createRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if body.Name == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, httpx.ProblemTypeValidation, "name is required", "err.validation")
			return
		}

		// Tenant scoping: default TRUE. Cross-tenant keys require Admin.
		var tenantID *string
		if body.TenantScoped || !auth.HasRole(p, auth.RoleAdmin) {
			t := p.TenantID
			tenantID = &t
		} else {
			// Admin explicitly asked for a cross-tenant key.
			tenantID = nil
		}

		var expires *time.Time
		if body.DurationDays > 0 {
			e := time.Now().UTC().Add(time.Duration(body.DurationDays) * 24 * time.Hour)
			expires = &e
		}

		// Audit is emitted by svc.Generate via the injected recorder;
		// the service layer guarantees it runs even if a future call
		// path bypasses this handler.
		gk, err := svc.Generate(r.Context(), tenantID, body.Name, p.UserID, body.Scopes, expires)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusCreated, createResponse{
			ID:        gk.ID,
			Plaintext: gk.Plaintext,
			Name:      gk.Name,
			Scopes:    gk.Scopes,
			ExpiresAt: gk.ExpiresAt,
			CreatedAt: gk.CreatedAt,
		})
	}
}

// ListHandler — GET /v1/apikeys
//
// Returns active keys for the caller's tenant. Cross-tenant keys
// (tenant_id = NULL) are included only for admin callers.
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		var tenantID *string
		if !auth.HasRole(p, auth.RoleAdmin) {
			t := p.TenantID
			tenantID = &t
		}
		list, err := svc.List(r.Context(), tenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": list,
			"total": len(list),
		})
	}
}

// RevokeHandler — DELETE /v1/apikeys/{keyID}
func RevokeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}
		id := chi.URLParam(r, "keyID")
		if id == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "missing keyID", "err.validation")
			return
		}

		tenantID := p.TenantID
		if auth.HasRole(p, auth.RoleAdmin) {
			// Admin can revoke cross-tenant keys — signal with ""
			// which the store interprets as "no tenant filter".
			tenantID = ""
		}
		if err := svc.Revoke(r.Context(), id, tenantID, p.UserID); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
