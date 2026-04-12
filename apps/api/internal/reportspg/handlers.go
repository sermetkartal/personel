// Package reportspg — Postgres-backed reports for Phase 1 MVP.
//
// The production reports pipeline lives in internal/reports and queries
// ClickHouse. In Phase 1 dev/pilot we have not yet hooked the agent →
// ClickHouse event stream, so we expose identically-shaped reports
// read from the employee_daily_stats + employee_hourly_stats tables
// seeded by the dev stack. The console pages consume these routes
// (/v1/reports-preview/*) so Phase 2 can swap to ClickHouse without
// touching the UI.
//
// All queries are RLS-tenant-scoped via the JOIN to users(tenant_id).
package reportspg

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// maxRangeDays caps preview lookback. Phase 1 Postgres aggregates the series
// in-memory, so unbounded windows would turn a report pull into a slow-query
// storm. ClickHouse-backed production reports (/v1/reports/*) carry no cap.
const maxRangeDays = 92

// parseRange pulls `from` / `to` from query params with a 7-day default lookback.
// Invalid parameters are surfaced as a 400 instead of being silently dropped —
// reports feeding KVKK m.11 responses must not lie about which window was used.
func parseRange(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -7)
	parse := func(name, s string) (time.Time, error) {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, nil
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid %s: expected RFC3339 or YYYY-MM-DD", name)
	}
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := parse("from", s)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = t
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := parse("to", s)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to = t
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, errors.New("invalid range: from must be <= to")
	}
	if to.Sub(from) > time.Duration(maxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("range too large: max %d days", maxRangeDays)
	}
	return from, to, nil
}

// -----------------------------------------------------------------------
// Productivity timeline — hourly active/idle aggregated across employees.
// -----------------------------------------------------------------------

type ProductivityHourRow struct {
	Hour          string `json:"hour"`           // "2026-04-12T09:00:00Z"
	ActiveSeconds int64  `json:"active_seconds"`
	IdleSeconds   int64  `json:"idle_seconds"`
}

func ProductivityHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		from, to, err := parseRange(r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}

		const q = `
			SELECT
				(h.day::timestamptz + (h.hour || ' hours')::interval) AT TIME ZONE 'UTC' AS bucket,
				COALESCE(SUM(h.active_minutes), 0)::bigint * 60 AS active_sec,
				COALESCE(SUM(h.idle_minutes), 0)::bigint * 60 AS idle_sec
			FROM employee_hourly_stats h
			JOIN users u ON u.id = h.user_id
			WHERE u.tenant_id = $1::uuid
			  AND h.day BETWEEN $2::date AND $3::date
			GROUP BY bucket
			ORDER BY bucket ASC
		`
		rows, err := pool.Query(r.Context(), q, p.TenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Query failed", "err.internal")
			return
		}
		defer rows.Close()

		out := make([]ProductivityHourRow, 0)
		for rows.Next() {
			var bucket time.Time
			var active, idle int64
			if err := rows.Scan(&bucket, &active, &idle); err != nil {
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Scan failed", "err.internal")
				return
			}
			out = append(out, ProductivityHourRow{
				Hour:          bucket.UTC().Format(time.RFC3339),
				ActiveSeconds: active,
				IdleSeconds:   idle,
			})
		}
		if err := rows.Err(); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Rows error", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": out,
			"from":  from,
			"to":    to,
		})
	}
}

// -----------------------------------------------------------------------
// Top apps — cumulative app minutes across all employees in the range.
// -----------------------------------------------------------------------

type TopAppRow struct {
	AppName      string  `json:"app_name"`
	Category     string  `json:"category"`
	FocusSeconds int64   `json:"focus_seconds"`
	FocusPct     float64 `json:"focus_pct"`
}

func TopAppsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		from, to, err := parseRange(r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}

		limit := 10
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		// Pull all top_apps JSONB payloads in range, then aggregate server-side.
		// This is fine for dev (<50 employees × 7 days); production will live
		// in ClickHouse with materialised views.
		const q = `
			SELECT s.top_apps
			FROM employee_daily_stats s
			JOIN users u ON u.id = s.user_id
			WHERE u.tenant_id = $1::uuid
			  AND s.day BETWEEN $2::date AND $3::date
		`
		rows, err := pool.Query(r.Context(), q, p.TenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Query failed", "err.internal")
			return
		}
		defer rows.Close()

		type agg struct {
			minutes  int64
			category string
		}
		byName := map[string]*agg{}
		var total int64
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Scan failed", "err.internal")
				return
			}
			var apps []struct {
				Name     string `json:"name"`
				Minutes  int64  `json:"minutes"`
				Category string `json:"category"`
			}
			if err := json.Unmarshal(raw, &apps); err != nil {
				// Bozuk bir top_apps satırı evidence-grade sistemde sessiz
				// kalamaz: raporda underflow oluşturur ve DPO yanlış sonuç
				// çıkarır. Logla ve counter ile ortaya çıkar.
				slog.Warn("reportspg: top_apps unmarshal failed — row skipped",
					slog.String("tenant_id", p.TenantID),
					slog.Any("error", err))
				continue
			}
			for _, a := range apps {
				cur, ok := byName[a.Name]
				if !ok {
					cur = &agg{category: a.Category}
					byName[a.Name] = cur
				}
				cur.minutes += a.Minutes
				total += a.Minutes
			}
		}
		if err := rows.Err(); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Rows error", "err.internal")
			return
		}

		// Sort and take top N.
		type nameMinutes struct {
			name string
			a    *agg
		}
		list := make([]nameMinutes, 0, len(byName))
		for n, a := range byName {
			list = append(list, nameMinutes{n, a})
		}
		// Simple insertion-sort since len is small.
		for i := 1; i < len(list); i++ {
			for j := i; j > 0 && list[j].a.minutes > list[j-1].a.minutes; j-- {
				list[j], list[j-1] = list[j-1], list[j]
			}
		}
		if len(list) > limit {
			list = list[:limit]
		}

		out := make([]TopAppRow, 0, len(list))
		for _, item := range list {
			pct := 0.0
			if total > 0 {
				pct = float64(item.a.minutes) / float64(total) * 100
			}
			out = append(out, TopAppRow{
				AppName:      item.name,
				Category:     item.a.category,
				FocusSeconds: item.a.minutes * 60,
				FocusPct:     pct,
			})
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": out,
			"from":  from,
			"to":    to,
		})
	}
}

// -----------------------------------------------------------------------
// Idle/Active — daily totals across the employee population.
// -----------------------------------------------------------------------

type IdleActiveDayRow struct {
	Date          string  `json:"date"`
	ActiveSeconds int64   `json:"active_seconds"`
	IdleSeconds   int64   `json:"idle_seconds"`
	ActiveRatio   float64 `json:"active_ratio"`
	EmployeeCount int     `json:"employee_count"`
}

func IdleActiveHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		from, to, err := parseRange(r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}

		const q = `
			SELECT
				s.day::text,
				COALESCE(SUM(s.active_minutes), 0)::bigint * 60 AS active_sec,
				COALESCE(SUM(s.idle_minutes), 0)::bigint * 60 AS idle_sec,
				COUNT(DISTINCT s.user_id) AS employees
			FROM employee_daily_stats s
			JOIN users u ON u.id = s.user_id
			WHERE u.tenant_id = $1::uuid
			  AND s.day BETWEEN $2::date AND $3::date
			GROUP BY s.day
			ORDER BY s.day ASC
		`
		rows, err := pool.Query(r.Context(), q, p.TenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Query failed", "err.internal")
			return
		}
		defer rows.Close()

		out := make([]IdleActiveDayRow, 0)
		for rows.Next() {
			var row IdleActiveDayRow
			if err := rows.Scan(&row.Date, &row.ActiveSeconds, &row.IdleSeconds, &row.EmployeeCount); err != nil {
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Scan failed", "err.internal")
				return
			}
			totalSec := row.ActiveSeconds + row.IdleSeconds
			if totalSec > 0 {
				row.ActiveRatio = float64(row.ActiveSeconds) / float64(totalSec)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Rows error", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": out,
			"from":  from,
			"to":    to,
		})
	}
}

// -----------------------------------------------------------------------
// App blocks — no data source in Phase 1 dev, returns an empty list
// with a hint field so the console can render an informative empty
// state instead of hiding the card entirely.
// -----------------------------------------------------------------------

type AppBlockRow struct {
	OccurredAt string `json:"occurred_at"`
	AppName    string `json:"app_name"`
	Count      int64  `json:"count"`
}

func AppBlocksHandler(_ *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		from, to, err := parseRange(r)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items":        []AppBlockRow{},
			"from":         from,
			"to":           to,
			"notice_code":  "reports.app_blocks.no_source",
			"notice_hint":  "Agent policy enforcement events will populate this list once the Phase 2 event pipeline wires into ClickHouse.",
		})
	}
}

