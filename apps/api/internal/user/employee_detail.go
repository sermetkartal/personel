// Package user — employee detail handler.
//
// GET /v1/employees/{userID}/detail?day=YYYY-MM-DD
//
// Returns a single-employee rollup: profile, today's daily stats, a
// 24-entry hourly activity breakdown for the chart, last 7 days of
// daily stats, and the assigned endpoints list. Drives the
// /tr/employees/[id] console page.
package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

type EmployeeDetail struct {
	Profile           EmployeeProfile      `json:"profile"`
	Today             DailyStats           `json:"today"`
	Hourly            []HourlyBucket       `json:"hourly"`
	Last7Days         []DailyStatsCompact  `json:"last_7_days"`
	AssignedEndpoints []AssignedEndpoint   `json:"assigned_endpoints"`
	IsCurrentlyActive bool                 `json:"is_currently_active"`
}

type EmployeeProfile struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Department string `json:"department"`
	JobTitle   string `json:"job_title"`
	Role       string `json:"role"`
}

type HourlyBucket struct {
	Hour            int    `json:"hour"`
	ActiveMinutes   int    `json:"active_minutes"`
	IdleMinutes     int    `json:"idle_minutes"`
	TopApp          string `json:"top_app"`
	ScreenshotCount int    `json:"screenshot_count"`
}

type DailyStatsCompact struct {
	Day               string `json:"day"`
	ActiveMinutes     int    `json:"active_minutes"`
	IdleMinutes       int    `json:"idle_minutes"`
	ProductivityScore int    `json:"productivity_score"`
}

type AssignedEndpoint struct {
	EndpointID string    `json:"endpoint_id"`
	Hostname   string    `json:"hostname"`
	LastSeenAt time.Time `json:"last_seen_at"`
	IsOnline   bool      `json:"is_online"`
}

func EmployeeDetailHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		userID := chi.URLParam(r, "userID")
		if userID == "" {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "userID required", "err.validation")
			return
		}

		day := r.URL.Query().Get("day")
		if day == "" {
			day = time.Now().UTC().Format("2006-01-02")
		}

		detail, err := queryEmployeeDetail(r.Context(), pool, p.TenantID, userID, day)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpx.WriteError(w, r, http.StatusNotFound,
					httpx.ProblemTypeNotFound, "Employee not found", "err.not_found")
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Detail query failed", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, detail)
	}
}

func queryEmployeeDetail(ctx context.Context, pool *pgxpool.Pool, tenantID, userID, day string) (*EmployeeDetail, error) {
	detail := &EmployeeDetail{
		Hourly:            make([]HourlyBucket, 0, 24),
		Last7Days:         make([]DailyStatsCompact, 0, 7),
		AssignedEndpoints: make([]AssignedEndpoint, 0),
	}

	const profileQ = `
		SELECT id::text, username, email,
		       COALESCE(department, ''), COALESCE(job_title, ''),
		       COALESCE(role, 'employee')
		FROM users
		WHERE tenant_id = $1::uuid AND id = $2::uuid AND is_active = true
	`
	if err := pool.QueryRow(ctx, profileQ, tenantID, userID).Scan(
		&detail.Profile.UserID, &detail.Profile.Username, &detail.Profile.Email,
		&detail.Profile.Department, &detail.Profile.JobTitle, &detail.Profile.Role,
	); err != nil {
		return nil, err
	}

	const todayQ = `
		SELECT active_minutes, idle_minutes, screenshot_count, keystroke_count,
		       productivity_score, top_apps, first_activity_at, last_activity_at
		FROM employee_daily_stats
		WHERE user_id = $1::uuid AND day = $2::date
	`
	var topAppsRaw []byte
	var firstAt, lastAt *time.Time
	err := pool.QueryRow(ctx, todayQ, userID, day).Scan(
		&detail.Today.ActiveMinutes, &detail.Today.IdleMinutes,
		&detail.Today.ScreenshotCount, &detail.Today.KeystrokeCount,
		&detail.Today.ProductivityScore, &topAppsRaw,
		&firstAt, &lastAt,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	detail.Today.Day = day
	if topAppsRaw != nil {
		_ = json.Unmarshal(topAppsRaw, &detail.Today.TopApps)
	}
	if firstAt != nil {
		detail.Today.FirstActivityAt = *firstAt
	}
	if lastAt != nil {
		detail.Today.LastActivityAt = *lastAt
		detail.IsCurrentlyActive = time.Since(*lastAt) < 5*time.Minute
	}

	const hourlyQ = `
		SELECT hour, active_minutes, idle_minutes, COALESCE(top_app, ''), screenshot_count
		FROM employee_hourly_stats
		WHERE user_id = $1::uuid AND day = $2::date
		ORDER BY hour ASC
	`
	rows, err := pool.Query(ctx, hourlyQ, userID, day)
	if err != nil {
		return nil, err
	}
	hourMap := map[int]HourlyBucket{}
	for rows.Next() {
		var hb HourlyBucket
		if err := rows.Scan(&hb.Hour, &hb.ActiveMinutes, &hb.IdleMinutes, &hb.TopApp, &hb.ScreenshotCount); err != nil {
			rows.Close()
			return nil, err
		}
		hourMap[hb.Hour] = hb
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Fill all 24 hours so the chart has a consistent x-axis.
	for h := 0; h < 24; h++ {
		if bucket, ok := hourMap[h]; ok {
			detail.Hourly = append(detail.Hourly, bucket)
		} else {
			detail.Hourly = append(detail.Hourly, HourlyBucket{Hour: h})
		}
	}

	const last7Q = `
		SELECT day::text, active_minutes, idle_minutes, productivity_score
		FROM employee_daily_stats
		WHERE user_id = $1::uuid
		  AND day >= ($2::date - INTERVAL '6 days') AND day <= $2::date
		ORDER BY day ASC
	`
	rows2, err := pool.Query(ctx, last7Q, userID, day)
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		var d DailyStatsCompact
		if err := rows2.Scan(&d.Day, &d.ActiveMinutes, &d.IdleMinutes, &d.ProductivityScore); err != nil {
			rows2.Close()
			return nil, err
		}
		detail.Last7Days = append(detail.Last7Days, d)
	}
	rows2.Close()
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	const endpointsQ = `
		SELECT id::text, hostname, COALESCE(last_seen_at, '1970-01-01'::timestamptz)
		FROM endpoints
		WHERE tenant_id = $1::uuid AND assigned_user_id = $2::uuid AND is_active = true
		ORDER BY last_seen_at DESC NULLS LAST
		LIMIT 20
	`
	rows3, err := pool.Query(ctx, endpointsQ, tenantID, userID)
	if err != nil {
		return nil, err
	}
	for rows3.Next() {
		var e AssignedEndpoint
		if err := rows3.Scan(&e.EndpointID, &e.Hostname, &e.LastSeenAt); err != nil {
			rows3.Close()
			return nil, err
		}
		e.IsOnline = time.Since(e.LastSeenAt) < 5*time.Minute
		detail.AssignedEndpoints = append(detail.AssignedEndpoints, e)
	}
	rows3.Close()
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	return detail, nil
}
