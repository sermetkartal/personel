// trends_store.go — Postgres implementation of the TrendStore interface.
//
// Uses the same `employee_daily_stats` roll-up table that the reportspg
// package reads. Tenant scoping is enforced via the JOIN to
// `users(tenant_id)`. Rich-signal metrics (dlp_redactions,
// policy_violations) read from the JSONB `rich_signals` column introduced
// in migration 0030.
package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGTrendStore is the production TrendStore implementation.
type PGTrendStore struct {
	pool *pgxpool.Pool
}

// NewPGTrendStore wraps a pgxpool into a TrendStore.
func NewPGTrendStore(pool *pgxpool.Pool) *PGTrendStore {
	return &PGTrendStore{pool: pool}
}

// FetchDailySeries pulls one aggregated value per day.
//
// For column-backed metrics the SELECT is a straight SUM over the metric
// column; for JSONB-backed metrics we coerce the path to numeric and sum.
// The JOIN to users applies tenant scoping — employee_daily_stats has no
// tenant_id column of its own.
func (s *PGTrendStore) FetchDailySeries(
	ctx context.Context,
	tenantID string,
	metric MetricName,
	from, to time.Time,
) ([]DailyValue, error) {
	src, ok := allMetrics[metric]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownMetric, metric)
	}

	var expr string
	switch src.kind {
	case sourceColumn:
		// Parameterised column injection is not possible in lib/pq; we restrict
		// `src.column` to the literals defined in allMetrics so no user input
		// reaches this format string.
		expr = fmt.Sprintf("COALESCE(SUM(s.%s), 0)::float8", src.column)
	case sourceJSON:
		// JSONB numeric extraction with safe default. The jsonb path is a
		// literal from allMetrics, never user input.
		expr = fmt.Sprintf(
			"COALESCE(SUM(COALESCE((s.rich_signals ->> %q)::float8, 0)), 0)::float8",
			src.jsonPath,
		)
	default:
		return nil, fmt.Errorf("trends: unsupported source kind")
	}

	q := fmt.Sprintf(`
		SELECT s.day::timestamptz, %s AS value
		FROM employee_daily_stats s
		JOIN users u ON u.id = s.user_id
		WHERE u.tenant_id = $1::uuid
		  AND s.day >= $2::date
		  AND s.day <  $3::date
		GROUP BY s.day
		ORDER BY s.day ASC
	`, expr)

	rows, err := s.pool.Query(ctx, q, tenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("trends: query: %w", err)
	}
	defer rows.Close()

	out := make([]DailyValue, 0)
	for rows.Next() {
		var day time.Time
		var value float64
		if err := rows.Scan(&day, &value); err != nil {
			return nil, fmt.Errorf("trends: scan: %w", err)
		}
		out = append(out, DailyValue{Day: day.UTC(), Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trends: rows: %w", err)
	}
	return out, nil
}
