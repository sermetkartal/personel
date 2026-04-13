// ch_handlers.go — HTTP handlers for /v1/reports/ch/* (real ClickHouse
// aggregation endpoints). Roadmap item #68.
//
// These handlers complement (NOT replace) the Postgres-backed preview
// endpoints under /v1/reports-preview/. The preview path remains in
// place for the console pages that already render against seeded PG
// tables; the /ch/ path is the production-scale CH aggregation surface
// that admins/HR/managers will switch to once the event pipeline has
// real volume.
//
// Validation rules (defence-in-depth over the query-layer caps):
//   - from < to, both RFC3339 UTC
//   - window ≤ 90 days (KVKK proportionality + operator cost control)
//   - limit 1..100
//   - user_ids: comma-separated, max 50, trimmed
//
// Auth: OIDC + RBAC gate (admin, manager, hr, dpo, investigator). IT
// operators are EXCLUDED for the same KVKK m.5 proportionality reason
// the preview endpoints document — device-support staff have no
// legitimate need for personnel productivity analytics.
//
// 503 handling: when the CH client is nil at startup (vault/compose
// partial bring-up), every handler short-circuits to 503 + problem+json
// so the console can render a degraded-mode banner rather than a blank
// 500.
package reports

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/personel/api/internal/auth"
	chclient "github.com/personel/api/internal/clickhouse"
	"github.com/personel/api/internal/httpx"
)

// Maximum allowed query window in days. Matches KVKK proportionality
// guidance in docs/compliance/kvkk-framework.md §5 — analytics queries
// over >90 days are treated as research rather than operations and
// require DPO-scoped export instead.
const chMaxRangeDays = 90

// Maximum number of user_id filter values accepted in a single query.
// Beyond this the caller should paginate by date range instead.
const chMaxUserIDFilter = 50

// CHHandlers bundles the aggregation client for the four endpoints.
// Accepts a nil client — handlers return 503 problem+json for every
// request so startup can proceed without ClickHouse available.
type CHHandlers struct {
	Client *chclient.Client
}

// NewCHHandlers constructs the handler bundle. client may be nil.
func NewCHHandlers(client *chclient.Client) *CHHandlers {
	return &CHHandlers{Client: client}
}

// --- shared parsing --------------------------------------------------------

type chCommonParams struct {
	from    time.Time
	to      time.Time
	userIDs []string
}

func parseCHRange(r *http.Request) (time.Time, time.Time, string, bool) {
	q := r.URL.Query()
	now := time.Now().UTC()

	from := now.AddDate(0, 0, -7)
	to := now

	if s := strings.TrimSpace(q.Get("from")); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, time.Time{}, "invalid 'from' (expected RFC3339)", false
		}
		from = t.UTC()
	}
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, time.Time{}, "invalid 'to' (expected RFC3339)", false
		}
		to = t.UTC()
	}

	if !from.Before(to) {
		return time.Time{}, time.Time{}, "'from' must be before 'to'", false
	}
	if to.Sub(from) > time.Duration(chMaxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, "range exceeds 90 day cap", false
	}
	return from, to, "", true
}

func parseCHUserIDs(r *http.Request) ([]string, string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("user_ids"))
	if raw == "" {
		return nil, "", true
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) > chMaxUserIDFilter {
		return nil, "user_ids exceeds 50-entry cap", false
	}
	return out, "", true
}

func parseCHLimit(r *http.Request, def, max int) int {
	s := strings.TrimSpace(r.URL.Query().Get("limit"))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func writeCHValidation(w http.ResponseWriter, r *http.Request, reason string) {
	httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, reason, "err.validation")
}

func writeCHUnavailable(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusServiceUnavailable,
		httpx.ProblemTypeInternal, "ClickHouse aggregation client unavailable",
		"err.internal")
}

func writeCHInternal(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusInternalServerError,
		httpx.ProblemTypeInternal, "aggregation query failed", "err.internal")
}

// --- handlers --------------------------------------------------------------

// CHTopAppsHandler — GET /v1/reports/ch/top-apps
// Query: from, to (RFC3339), limit (1..100), category
// (productive|neutral|distracting|<empty>), user_ids (comma csv).
func (h *CHHandlers) CHTopAppsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.Client == nil {
			writeCHUnavailable(w, r)
			return
		}
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "authentication required", "err.unauthenticated")
			return
		}

		from, to, msg, ok := parseCHRange(r)
		if !ok {
			writeCHValidation(w, r, msg)
			return
		}
		userIDs, msg2, ok := parseCHUserIDs(r)
		if !ok {
			writeCHValidation(w, r, msg2)
			return
		}
		category := strings.TrimSpace(r.URL.Query().Get("category"))
		switch category {
		case "", "productive", "neutral", "distracting":
		default:
			writeCHValidation(w, r, "invalid category")
			return
		}
		limit := parseCHLimit(r, 10, 100)

		rows, err := h.Client.AggTopApps(r.Context(), p.TenantID, chclient.TopAppsParams{
			From:     from,
			To:       to,
			Limit:    limit,
			Category: category,
			UserIDs:  userIDs,
		})
		if err != nil {
			if errors.Is(err, chclient.ErrClientUnavailable) {
				writeCHUnavailable(w, r)
				return
			}
			writeCHInternal(w, r)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": rows, "from": from, "to": to,
		})
	}
}

// CHIdleActiveHandler — GET /v1/reports/ch/idle-active
// Query: from, to (RFC3339), user_ids (comma csv).
func (h *CHHandlers) CHIdleActiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.Client == nil {
			writeCHUnavailable(w, r)
			return
		}
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "authentication required", "err.unauthenticated")
			return
		}

		from, to, msg, ok := parseCHRange(r)
		if !ok {
			writeCHValidation(w, r, msg)
			return
		}
		userIDs, msg2, ok := parseCHUserIDs(r)
		if !ok {
			writeCHValidation(w, r, msg2)
			return
		}

		rows, err := h.Client.AggIdleActive(r.Context(), p.TenantID, chclient.IdleActiveParams{
			From: from, To: to, UserIDs: userIDs,
		})
		if err != nil {
			if errors.Is(err, chclient.ErrClientUnavailable) {
				writeCHUnavailable(w, r)
				return
			}
			writeCHInternal(w, r)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": rows, "from": from, "to": to,
		})
	}
}

// CHProductivityHandler — GET /v1/reports/ch/productivity
// Query: from, to (RFC3339), user_ids (comma csv).
func (h *CHHandlers) CHProductivityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.Client == nil {
			writeCHUnavailable(w, r)
			return
		}
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "authentication required", "err.unauthenticated")
			return
		}

		from, to, msg, ok := parseCHRange(r)
		if !ok {
			writeCHValidation(w, r, msg)
			return
		}
		userIDs, msg2, ok := parseCHUserIDs(r)
		if !ok {
			writeCHValidation(w, r, msg2)
			return
		}

		rows, err := h.Client.AggProductivityScore(r.Context(), p.TenantID, chclient.ProductivityParams{
			From: from, To: to, UserIDs: userIDs,
		})
		if err != nil {
			if errors.Is(err, chclient.ErrClientUnavailable) {
				writeCHUnavailable(w, r)
				return
			}
			writeCHInternal(w, r)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": rows, "from": from, "to": to,
		})
	}
}

// CHAppBlocksHandler — GET /v1/reports/ch/app-blocks
// Query: from, to (RFC3339), limit (1..100).
func (h *CHHandlers) CHAppBlocksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.Client == nil {
			writeCHUnavailable(w, r)
			return
		}
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "authentication required", "err.unauthenticated")
			return
		}

		from, to, msg, ok := parseCHRange(r)
		if !ok {
			writeCHValidation(w, r, msg)
			return
		}
		limit := parseCHLimit(r, 50, 100)

		rows, err := h.Client.AggAppBlocks(r.Context(), p.TenantID, chclient.AppBlocksParams{
			From: from, To: to, Limit: limit,
		})
		if err != nil {
			if errors.Is(err, chclient.ErrClientUnavailable) {
				writeCHUnavailable(w, r)
				return
			}
			writeCHInternal(w, r)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": rows, "from": from, "to": to,
		})
	}
}
