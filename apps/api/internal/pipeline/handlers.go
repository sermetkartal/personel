package pipeline

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// ListDLQHandler handles GET /v1/pipeline/dlq.
//
// Query params:
//   - tenant_id        — optional; ignored unless caller is DPO
//   - all_tenants=true — DPO only; disables tenant filtering
//   - from, to         — RFC3339 bounds on failed_at
//   - error_kind       — filter on DLQKind* enum
//   - page_size        — 1..500 (default 100)
//   - page_token       — opaque cursor (JetStream seq)
//
// RBAC is enforced by the chi router (admin, dpo, investigator).
func ListDLQHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.unauthenticated")
			return
		}

		params, err := parseListParams(r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}

		result, err := svc.ListDLQ(r.Context(), p, params)
		if err != nil {
			if errors.Is(err, ErrForbiddenAllTenants) {
				httpx.WriteError(w, r, http.StatusForbidden,
					httpx.ProblemTypeForbidden, err.Error(), "err.forbidden")
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, err.Error(), "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

// ReplayHandler handles POST /v1/pipeline/replay.
// RBAC is enforced by the chi router (admin, dpo ONLY — replay can
// create load + has compliance implications).
func ReplayHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.unauthenticated")
			return
		}

		var req ReplayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}

		result, err := svc.Replay(r.Context(), p, req)
		if err != nil {
			switch {
			case errors.Is(err, ErrForbiddenAllTenants):
				httpx.WriteError(w, r, http.StatusForbidden,
					httpx.ProblemTypeForbidden, err.Error(), "err.forbidden")
			case errors.Is(err, ErrTenantIsolation):
				httpx.WriteError(w, r, http.StatusForbidden,
					httpx.ProblemTypeForbidden, "cross-tenant access denied", "err.forbidden")
			case errors.Is(err, ErrDLQNotFound):
				httpx.WriteError(w, r, http.StatusNotFound,
					httpx.ProblemTypeNotFound, "DLQ entry not found", "err.not_found")
			default:
				var ve *validationErr
				if errors.As(err, &ve) {
					httpx.WriteError(w, r, http.StatusBadRequest,
						httpx.ProblemTypeValidation, ve.Error(), "err.validation")
					return
				}
				httpx.WriteError(w, r, http.StatusInternalServerError,
					httpx.ProblemTypeInternal, err.Error(), "err.internal")
			}
			return
		}

		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

// parseListParams extracts ListParams from the request URL.
func parseListParams(r *http.Request) (ListParams, error) {
	q := r.URL.Query()
	params := ListParams{
		TenantID:   q.Get("tenant_id"),
		AllTenants: q.Get("all_tenants") == "true",
		ErrorKind:  q.Get("error_kind"),
		PageToken:  q.Get("page_token"),
	}

	if ps := q.Get("page_size"); ps != "" {
		n, err := strconv.Atoi(ps)
		if err != nil || n < 1 {
			return params, errInvalid("page_size", "must be a positive integer")
		}
		params.PageSize = n
	}

	if from := q.Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return params, errInvalid("from", "must be RFC3339")
		}
		params.From = t
	}
	if to := q.Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return params, errInvalid("to", "must be RFC3339")
		}
		params.To = t
	}
	return params, nil
}
