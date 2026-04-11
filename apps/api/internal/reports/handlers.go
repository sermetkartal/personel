// Package reports — HTTP handlers for ClickHouse report endpoints.
package reports

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

const defaultLookbackDays = 7

// parseTimeRange extracts from/to from query params with a default lookback.
func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -defaultLookbackDays)

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

func parseEndpointIDs(r *http.Request) []string {
	raw := r.URL.Query().Get("endpoint_ids")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ProductivityHandler — GET /v1/reports/productivity
func ProductivityHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := parseTimeRange(r)
		endpointIDs := parseEndpointIDs(r)

		rows, err := svc.ProductivityTimeline(r.Context(), p.TenantID, from, to, endpointIDs)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "from": from, "to": to})
	}
}

// TopAppsHandler — GET /v1/reports/top-apps
func TopAppsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := parseTimeRange(r)
		limit := 10
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}

		rows, err := svc.TopApps(r.Context(), p.TenantID, from, to, limit)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "from": from, "to": to})
	}
}

// IdleActiveHandler — GET /v1/reports/idle-active
func IdleActiveHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := parseTimeRange(r)
		endpointIDs := parseEndpointIDs(r)

		rows, err := svc.IdleActive(r.Context(), p.TenantID, from, to, endpointIDs)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "from": from, "to": to})
	}
}

// EndpointActivityHandler — GET /v1/reports/endpoint-activity
func EndpointActivityHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := parseTimeRange(r)

		rows, err := svc.EndpointActivitySummary(r.Context(), p.TenantID, from, to)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "from": from, "to": to})
	}
}

// AppBlocksHandler — GET /v1/reports/app-blocks
func AppBlocksHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		from, to := parseTimeRange(r)

		rows, err := svc.AppBlocks(r.Context(), p.TenantID, from, to)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "from": from, "to": to})
	}
}
