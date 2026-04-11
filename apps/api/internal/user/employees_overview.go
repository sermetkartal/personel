// Package user — employee monitoring overview handler.
//
// GET /v1/employees/overview — returns one row per employee with today's
// rolled-up activity metrics (active/idle minutes, top 3 apps,
// productivity score, screenshot count). Consumed by /tr/employees page
// in the console. RBAC: manager/hr/dpo/admin can view.
//
// The underlying employee_daily_stats table is populated by a nightly
// roll-up job over ClickHouse events in production; in the dev stack
// it is seeded directly via dev-seed-employee-stats.sh.
package user

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// EmployeeOverviewRow is the shape returned per row.
type EmployeeOverviewRow struct {
	UserID             string          `json:"user_id"`
	Username           string          `json:"username"`
	FullName           string          `json:"full_name"`
	Email              string          `json:"email"`
	Department         string          `json:"department"`
	JobTitle           string          `json:"job_title"`
	Today              DailyStats      `json:"today"`
	Last7DaysActiveMin int             `json:"last_7_days_active_minutes"`
	Last7DaysAvgScore  int             `json:"last_7_days_avg_score"`
	IsCurrentlyActive  bool            `json:"is_currently_active"`
	AssignedEndpoints  int             `json:"assigned_endpoint_count"`
}

type DailyStats struct {
	Day                string    `json:"day"`
	ActiveMinutes      int       `json:"active_minutes"`
	IdleMinutes        int       `json:"idle_minutes"`
	ScreenshotCount    int       `json:"screenshot_count"`
	KeystrokeCount     int       `json:"keystroke_count"`
	ProductivityScore  int       `json:"productivity_score"`
	TopApps            []TopApp  `json:"top_apps"`
	FirstActivityAt    time.Time `json:"first_activity_at"`
	LastActivityAt     time.Time `json:"last_activity_at"`
}

type TopApp struct {
	Name     string `json:"name"`
	Minutes  int    `json:"minutes"`
	Category string `json:"category"`
}

// EmployeesOverviewHandler returns one row per employee for the given
// tenant. The `day` query parameter selects the reference day (defaults
// to today in server timezone). Currently ignores pagination — Phase 2
// adds offset/limit when the roll-up table grows.
func EmployeesOverviewHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		day := r.URL.Query().Get("day")
		if day == "" {
			day = time.Now().UTC().Format("2006-01-02")
		}

		rows, err := queryEmployeeOverview(r.Context(), pool, p.TenantID, day)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Overview query failed", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": rows,
			"day":   day,
			"pagination": map[string]any{
				"page":      1,
				"page_size": len(rows),
				"total":     len(rows),
			},
		})
	}
}

func queryEmployeeOverview(ctx context.Context, pool *pgxpool.Pool, tenantID, day string) ([]EmployeeOverviewRow, error) {
	const q = `
		WITH today AS (
			SELECT user_id, active_minutes, idle_minutes, screenshot_count, keystroke_count,
			       productivity_score, top_apps, first_activity_at, last_activity_at, day
			FROM employee_daily_stats
			WHERE day = $2::date
		),
		last7 AS (
			SELECT user_id,
			       COALESCE(SUM(active_minutes), 0) AS sum_active,
			       COALESCE(AVG(productivity_score)::int, 0) AS avg_score
			FROM employee_daily_stats
			WHERE day >= ($2::date - INTERVAL '7 days') AND day <= $2::date
			GROUP BY user_id
		),
		endpoints_per_user AS (
			SELECT assigned_user_id, COUNT(*) AS cnt
			FROM endpoints
			WHERE is_active = true AND tenant_id = $1::uuid
			GROUP BY assigned_user_id
		)
		SELECT
			u.id::text,
			u.username,
			COALESCE(u.job_title, '') || ' — ' || COALESCE(u.department, '') AS full_name_synth,
			u.email,
			COALESCE(u.department, ''),
			COALESCE(u.job_title, ''),
			COALESCE(t.active_minutes, 0),
			COALESCE(t.idle_minutes, 0),
			COALESCE(t.screenshot_count, 0),
			COALESCE(t.keystroke_count, 0),
			COALESCE(t.productivity_score, 0),
			COALESCE(t.top_apps, '[]'::jsonb),
			t.first_activity_at,
			t.last_activity_at,
			COALESCE(l.sum_active, 0),
			COALESCE(l.avg_score, 0),
			COALESCE(ep.cnt, 0)
		FROM users u
		LEFT JOIN today t ON t.user_id = u.id
		LEFT JOIN last7 l ON l.user_id = u.id
		LEFT JOIN endpoints_per_user ep ON ep.assigned_user_id = u.id
		WHERE u.tenant_id = $1::uuid
		  AND u.role IN ('employee', 'manager')
		  AND u.is_active = true
		ORDER BY COALESCE(t.active_minutes, 0) DESC, u.username ASC
	`

	rows, err := pool.Query(ctx, q, tenantID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EmployeeOverviewRow
	for rows.Next() {
		var r EmployeeOverviewRow
		var topAppsRaw []byte
		var firstAt, lastAt *time.Time
		if err := rows.Scan(
			&r.UserID, &r.Username, &r.FullName, &r.Email,
			&r.Department, &r.JobTitle,
			&r.Today.ActiveMinutes, &r.Today.IdleMinutes,
			&r.Today.ScreenshotCount, &r.Today.KeystrokeCount,
			&r.Today.ProductivityScore,
			&topAppsRaw,
			&firstAt, &lastAt,
			&r.Last7DaysActiveMin, &r.Last7DaysAvgScore,
			&r.AssignedEndpoints,
		); err != nil {
			return nil, err
		}
		r.Today.Day = day
		_ = json.Unmarshal(topAppsRaw, &r.Today.TopApps)
		if firstAt != nil {
			r.Today.FirstActivityAt = *firstAt
		}
		if lastAt != nil {
			r.Today.LastActivityAt = *lastAt
			// Consider "currently active" if last activity was within
			// the last 5 minutes (same threshold the mobile BFF uses).
			r.IsCurrentlyActive = time.Since(*lastAt) < 5*time.Minute
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
