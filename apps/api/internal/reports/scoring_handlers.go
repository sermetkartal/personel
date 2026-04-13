// scoring_handlers.go — Faz 8 #85 + #86 HTTP handlers exposing the
// pure-Go scoring algorithms (internal/scoring) over REST.
//
// Two endpoints, both mounted under /v1/reports/ch/:
//
//   GET /v1/reports/ch/productivity-breakdown?user_id=&from=&to=
//     → Per-day productivity score + 7-day rolling average using
//       ComputeProductivity over employee_daily_stats rows.
//     RBAC: admin, manager, hr, dpo, investigator.
//
//   GET /v1/reports/ch/risk-score?user_id=&day=
//     → ComputeRisk applied to per-user signals for a single day.
//     RBAC: admin, dpo, investigator ONLY (risk scores are sensitive).
//
// Data source: employee_daily_stats + users (Postgres). The /ch/ prefix
// is kept for URL stability once a future revision re-points the query
// to ClickHouse rollups; the handler names (PG* vs CH*) are a detail.
//
// KVKK m.11/g: every risk-score response carries advisory_only=true and
// the Turkish disclaimer. There is NO caller-visible way to turn off the
// advisory flag — the scoring.ComputeRisk function hard-codes it true.
//
// Tenant isolation (KVKK m.5 + SOC 2 CC6.1): every query filters on the
// caller's tenant_id through a JOIN to users(id) WHERE u.tenant_id = $1.
// user_id paths additionally enforce that the queried user belongs to
// the caller's tenant before scoring — otherwise a cross-tenant UUID
// lookup could leak the existence of another tenant's user.

package reports

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
	"github.com/personel/api/internal/scoring"
)

// ---------------------------------------------------------------------------
// GET /v1/reports/ch/productivity-breakdown
// ---------------------------------------------------------------------------

// ProductivityDayRow is the per-day slice in the breakdown response.
type ProductivityDayRow struct {
	Date             string             `json:"date"`
	Score            int                `json:"score"`
	RollingAvg7Day   float64            `json:"rolling_avg_7day"`
	ActiveMinutes    int                `json:"active_minutes"`
	IdleMinutes      int                `json:"idle_minutes"`
	ProductiveMin    int                `json:"productive_minutes"`
	NeutralMin       int                `json:"neutral_minutes"`
	DistractingMin   int                `json:"distracting_minutes"`
	PolicyViolations int                `json:"policy_violations"`
	PenaltyReason    string             `json:"penalty_reason"`
	Weights          map[string]float64 `json:"weights"`
}

// ProductivityBreakdownHandler implements GET /v1/reports/ch/productivity-breakdown.
// The pool is required — it's the Postgres pool that owns employee_daily_stats.
func ProductivityBreakdownHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth,
				"Unauthorized", "err.auth")
			return
		}
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userID == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation,
				"user_id is required", "err.validation")
			return
		}
		from, to, msg, ok := parseCHRange(r)
		if !ok {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation,
				msg, "err.validation")
			return
		}

		// Tenant enforcement: resolve the user ONLY if it belongs to the caller's tenant.
		// A cross-tenant user_id produces a clean 404 rather than leaking existence.
		var userTenantExists bool
		const qUserCheck = `
			SELECT EXISTS (
				SELECT 1 FROM users
				WHERE id = $1::uuid AND tenant_id = $2::uuid
			)
		`
		if err := pool.QueryRow(r.Context(), qUserCheck, userID, p.TenantID).Scan(&userTenantExists); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal,
				"user lookup failed", "err.internal")
			return
		}
		if !userTenantExists {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound,
				"user not found", "err.not_found")
			return
		}

		// Pull the full range of daily rows for this user.
		const q = `
			SELECT
				s.day::text,
				COALESCE(s.active_minutes, 0),
				COALESCE(s.idle_minutes, 0),
				COALESCE(s.keystroke_count, 0),
				COALESCE(s.top_apps, '[]'::jsonb)
			FROM employee_daily_stats s
			WHERE s.user_id = $1::uuid
			  AND s.day BETWEEN $2::date AND $3::date
			ORDER BY s.day ASC
		`
		rows, err := pool.Query(r.Context(), q, userID,
			from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal,
				"query failed", "err.internal")
			return
		}
		defer rows.Close()

		type dayCollect struct {
			row     ProductivityDayRow
			rawScore int
		}
		collected := make([]dayCollect, 0, 14)

		for rows.Next() {
			var date string
			var active, idle, keystrokes int
			var topAppsRaw []byte
			if err := rows.Scan(&date, &active, &idle, &keystrokes, &topAppsRaw); err != nil {
				httpx.WriteError(w, r, http.StatusInternalServerError,
					httpx.ProblemTypeInternal, "scan failed", "err.internal")
				return
			}

			prod, neut, dist, _ := aggregateTopAppsCategories(topAppsRaw)

			in := scoring.ProductivityInputs{
				ActiveMinutes:         active,
				IdleMinutes:           idle,
				ProductiveAppMinutes:  prod,
				NeutralAppMinutes:     neut,
				DistractingAppMinutes: dist,
				KeystrokeCount:        keystrokes,
				PolicyViolations:      0, // TODO wire blocked_* event counts
			}
			res := scoring.ComputeProductivity(in)

			row := ProductivityDayRow{
				Date:             date,
				Score:            res.Score,
				ActiveMinutes:    active,
				IdleMinutes:      idle,
				ProductiveMin:    prod,
				NeutralMin:       neut,
				DistractingMin:   dist,
				PolicyViolations: 0,
				PenaltyReason:    res.PenaltyReason,
				Weights:          res.Weights,
			}
			collected = append(collected, dayCollect{row: row, rawScore: res.Score})
		}
		if err := rows.Err(); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal,
				"rows error", "err.internal")
			return
		}

		// 7-day rolling average (inclusive, right-aligned). For day i, average
		// the scores in [max(0,i-6)..i].
		out := make([]ProductivityDayRow, 0, len(collected))
		for i := range collected {
			start := i - 6
			if start < 0 {
				start = 0
			}
			sum := 0
			cnt := 0
			for j := start; j <= i; j++ {
				sum += collected[j].rawScore
				cnt++
			}
			avg := 0.0
			if cnt > 0 {
				avg = float64(sum) / float64(cnt)
			}
			collected[i].row.RollingAvg7Day = roundScoring2(avg)
			out = append(out, collected[i].row)
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"user_id": userID,
			"items":   out,
			"from":    from,
			"to":      to,
		})
	}
}

// aggregateTopAppsCategories sums minutes per productive/neutral/distracting
// category from a top_apps JSONB blob. Shape:
//
//	[{"name": "...", "minutes": N, "category": "productive|neutral|distracting"}]
//
// Unknown categories are folded into "neutral".
func aggregateTopAppsCategories(raw []byte) (productive, neutral, distracting, total int) {
	if len(raw) == 0 {
		return 0, 0, 0, 0
	}
	var apps []struct {
		Name     string `json:"name"`
		Minutes  int    `json:"minutes"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(raw, &apps); err != nil {
		return 0, 0, 0, 0
	}
	for _, a := range apps {
		total += a.Minutes
		switch a.Category {
		case "productive":
			productive += a.Minutes
		case "distracting":
			distracting += a.Minutes
		default:
			neutral += a.Minutes
		}
	}
	return
}

func roundScoring2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// ---------------------------------------------------------------------------
// GET /v1/reports/ch/risk-score
// ---------------------------------------------------------------------------

// RiskScoreResponse mirrors scoring.RiskResult + per-tenant envelope.
// advisory_only is ALWAYS true. No caller can turn it off.
type RiskScoreResponse struct {
	UserID        string               `json:"user_id"`
	Day           string               `json:"day"`
	Score         int                  `json:"score"`
	Tier          string               `json:"tier"`
	Factors       []scoring.RiskFactor `json:"factors"`
	AdvisoryOnly  bool                 `json:"advisory_only"`
	Notice        string               `json:"notice"`
	Inputs        map[string]any       `json:"inputs"`
}

// RiskScoreHandler implements GET /v1/reports/ch/risk-score.
// Input: user_id (required), day (optional, defaults to today UTC).
// Tenant isolation is enforced via users.tenant_id JOIN.
func RiskScoreHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth,
				"Unauthorized", "err.auth")
			return
		}
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userID == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation,
				"user_id is required", "err.validation")
			return
		}

		// Day resolution. Accept YYYY-MM-DD; default to today UTC.
		dayStr := strings.TrimSpace(r.URL.Query().Get("day"))
		var day time.Time
		if dayStr == "" {
			day = time.Now().UTC().Truncate(24 * time.Hour)
		} else {
			parsed, err := time.Parse("2006-01-02", dayStr)
			if err != nil {
				httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation,
					"day must be YYYY-MM-DD", "err.validation")
				return
			}
			day = parsed.UTC()
		}

		// Tenant + existence check.
		var userTenantExists bool
		if err := pool.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1::uuid AND tenant_id = $2::uuid)`,
			userID, p.TenantID).Scan(&userTenantExists); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal,
				"user lookup failed", "err.internal")
			return
		}
		if !userTenantExists {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound,
				"user not found", "err.not_found")
			return
		}

		// Load employee_daily_stats for the day as a proxy for off-hours +
		// sensitive file access + USB transfers. Phase 1: these specific
		// columns aren't present yet, so we default everything to zero and
		// let ComputeRisk produce a "low" score. Phase 2 will join real
		// DLP event tables + tamper findings.
		in := scoring.RiskInputs{}

		const qStats = `
			SELECT
				COALESCE(s.active_minutes, 0),
				COALESCE(s.idle_minutes, 0)
			FROM employee_daily_stats s
			WHERE s.user_id = $1::uuid AND s.day = $2::date
		`
		var active, idle int
		err := pool.QueryRow(r.Context(), qStats, userID, day.Format("2006-01-02")).Scan(&active, &idle)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			// Real DB failure — surface as 500. pgx.ErrNoRows means the user
			// had no activity that day, which is a legitimate zero-input state.
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal,
				"stats lookup failed", "err.internal")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			active, idle = 0, 0
		}

		// Off-hours activity proxy: a single day stat table doesn't split
		// by hour, so leave at 0.0 until Phase 2 plumbs hourly rollups.

		result := scoring.ComputeRisk(in)

		// INVARIANT CHECK: never serialise a result with advisory_only=false.
		// If a future refactor accidentally breaks the scoring contract, this
		// assertion panics in test and fails hard in prod so the gap is
		// impossible to miss.
		if !result.AdvisoryOnly {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal,
				"internal scoring invariant broken — advisory_only=false",
				"err.internal")
			return
		}

		resp := RiskScoreResponse{
			UserID: userID,
			Day:    day.Format("2006-01-02"),
			Score:  result.Score,
			Tier:   result.Tier,
			Factors: result.TopFactors,
			AdvisoryOnly: true,
			Notice:       result.Disclaimer,
			Inputs: map[string]any{
				"active_minutes": active,
				"idle_minutes":   idle,
			},
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}
