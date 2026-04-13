// Package search — HTTP handlers for /v1/search/audit and /v1/search/events.
//
// The handlers are deliberately thin: parse query string, pull the verified
// principal out of the request context, hand off to the service. All
// validation, tenant injection, and redaction logic lives in service.go
// and client.go.
package search

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// AuditHandler returns GET /v1/search/audit.
//
//   - Requires an OIDC-verified principal with a tenant_id claim.
//   - Accepts: q, from, to (RFC3339), action, actor_id, page, page_size.
//   - IGNORES any tenant_id passed by the client (server reads from
//     Principal.TenantID).
//   - On nil service / downed OpenSearch → 503 Service Unavailable.
//   - On validation failure → 400 Bad Request (problem+json).
//   - Empty result → 200 with hits: [].
func AuditHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.unauthenticated")
			return
		}

		q := AuditQuery{
			Q:        r.URL.Query().Get("q"),
			Action:   r.URL.Query().Get("action"),
			ActorID:  r.URL.Query().Get("actor_id"),
			Page:     parseInt(r.URL.Query().Get("page"), 1),
			PageSize: parseInt(r.URL.Query().Get("page_size"), 0),
		}
		q.From, q.To = parseRange(r)

		result, err := svc.SearchAudit(r.Context(), p.TenantID, q)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

// EventsHandler returns GET /v1/search/events.
//
// Accepts: q, from, to, event_kind, endpoint_id, user_id, process_name,
// page, page_size. Tenant isolation and role gates are identical to the
// audit variant.
func EventsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.unauthenticated")
			return
		}

		q := EventQuery{
			Q:           r.URL.Query().Get("q"),
			EventKind:   r.URL.Query().Get("event_kind"),
			EndpointID:  r.URL.Query().Get("endpoint_id"),
			UserID:      r.URL.Query().Get("user_id"),
			ProcessName: r.URL.Query().Get("process_name"),
			Page:        parseInt(r.URL.Query().Get("page"), 1),
			PageSize:    parseInt(r.URL.Query().Get("page_size"), 0),
		}
		q.From, q.To = parseRange(r)

		result, err := svc.SearchEvents(r.Context(), p.TenantID, q)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

// writeServiceError maps service-layer errors to HTTP status codes.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrSearchUnavailable):
		httpx.WriteError(w, r, http.StatusServiceUnavailable,
			httpx.ProblemTypeInternal, "Search Unavailable", "err.internal")
	case errors.Is(err, ErrValidation):
		httpx.WriteError(w, r, http.StatusBadRequest,
			httpx.ProblemTypeValidation, "Invalid Search Parameters", "err.validation")
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError,
			httpx.ProblemTypeInternal, "Internal Error", "err.internal")
	}
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func parseRange(r *http.Request) (time.Time, time.Time) {
	var from, to time.Time
	if s := r.URL.Query().Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			from = t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			to = t
		}
	}
	return from, to
}
