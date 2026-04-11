// Package reports — parameterized ClickHouse queries.
//
// RULE: No string concatenation for SQL values. All user-provided values
// are passed via the clickhouse-go parameter binding API.
package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// ProductivityRow is one row of the productivity timeline.
type ProductivityRow struct {
	Hour         time.Time `json:"hour"`
	EndpointID   string    `json:"endpoint_id"`
	ActiveSeconds int64    `json:"active_seconds"`
	IdleSeconds   int64    `json:"idle_seconds"`
}

// TopAppRow is one row of the top-apps report.
type TopAppRow struct {
	AppName      string  `json:"app_name"`
	FocusSeconds int64   `json:"focus_seconds"`
	FocusPct     float64 `json:"focus_pct"`
}

// IdleActiveRow is one row of the idle/active ratio report.
type IdleActiveRow struct {
	Date          time.Time `json:"date"`
	EndpointID    string    `json:"endpoint_id"`
	ActiveRatio   float64   `json:"active_ratio"`
	ActiveSeconds int64     `json:"active_seconds"`
	IdleSeconds   int64     `json:"idle_seconds"`
}

// AppBlockRow represents an app-blocking event row.
type AppBlockRow struct {
	OccurredAt time.Time `json:"occurred_at"`
	EndpointID string    `json:"endpoint_id"`
	AppName    string    `json:"app_name"`
	Count      int64     `json:"count"`
}

// Client wraps the ClickHouse connection.
type Client interface {
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
}

// Rows abstracts clickhouse-go rows for testability.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// Querier provides parameterized ClickHouse queries.
type Querier struct {
	ch clickhouse.Conn
}

// NewQuerier creates a Querier.
func NewQuerier(ch clickhouse.Conn) *Querier {
	return &Querier{ch: ch}
}

// ProductivityTimeline returns hourly active/idle seconds per endpoint.
// All filter values are passed as parameters — no string interpolation.
func (q *Querier) ProductivityTimeline(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]ProductivityRow, error) {
	// Build parameterized query.
	// clickhouse-go v2 uses ? placeholders for positional parameters.
	var rows interface {
		Next() bool
		Scan(...any) error
		Err() error
		Close() error
	}
	var err error

	if len(endpointIDs) > 0 {
		rows, err = q.ch.Query(ctx,
			`SELECT
			   toStartOfHour(occurred_at)   AS hour,
			   toString(endpoint_id)        AS endpoint_id,
			   sumIf(duration_seconds, event_type = 'session.active_end')  AS active_seconds,
			   sumIf(duration_seconds, event_type = 'session.idle_end')    AS idle_seconds
			 FROM events
			 WHERE tenant_id = ?
			   AND occurred_at BETWEEN ? AND ?
			   AND endpoint_id IN ?
			 GROUP BY hour, endpoint_id
			 ORDER BY hour ASC, endpoint_id ASC`,
			tenantID, from, to, endpointIDs,
		)
	} else {
		rows, err = q.ch.Query(ctx,
			`SELECT
			   toStartOfHour(occurred_at)   AS hour,
			   toString(endpoint_id)        AS endpoint_id,
			   sumIf(duration_seconds, event_type = 'session.active_end')  AS active_seconds,
			   sumIf(duration_seconds, event_type = 'session.idle_end')    AS idle_seconds
			 FROM events
			 WHERE tenant_id = ?
			   AND occurred_at BETWEEN ? AND ?
			 GROUP BY hour, endpoint_id
			 ORDER BY hour ASC, endpoint_id ASC`,
			tenantID, from, to,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("reports: productivity timeline: %w", err)
	}
	defer rows.Close()

	var out []ProductivityRow
	for rows.Next() {
		var r ProductivityRow
		if err := rows.Scan(&r.Hour, &r.EndpointID, &r.ActiveSeconds, &r.IdleSeconds); err != nil {
			return nil, fmt.Errorf("reports: productivity scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TopApps returns top apps by focus time for a tenant in a time window.
func (q *Querier) TopApps(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]TopAppRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := q.ch.Query(ctx,
		`SELECT
		   exe_name         AS app_name,
		   sum(duration_seconds) AS focus_seconds,
		   100.0 * sum(duration_seconds) / sum(sum(duration_seconds)) OVER () AS focus_pct
		 FROM events
		 WHERE tenant_id = ?
		   AND event_type = 'process.foreground_change'
		   AND occurred_at BETWEEN ? AND ?
		 GROUP BY app_name
		 ORDER BY focus_seconds DESC
		 LIMIT ?`,
		tenantID, from, to, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("reports: top apps: %w", err)
	}
	defer rows.Close()

	var out []TopAppRow
	for rows.Next() {
		var r TopAppRow
		if err := rows.Scan(&r.AppName, &r.FocusSeconds, &r.FocusPct); err != nil {
			return nil, fmt.Errorf("reports: top apps scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IdleActive returns daily idle/active ratio per endpoint.
func (q *Querier) IdleActive(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]IdleActiveRow, error) {
	rows, err := q.ch.Query(ctx,
		`SELECT
		   toDate(occurred_at) AS date,
		   toString(endpoint_id) AS endpoint_id,
		   sumIf(duration_seconds, event_type = 'session.active_end') AS active_s,
		   sumIf(duration_seconds, event_type = 'session.idle_end')   AS idle_s,
		   if(active_s + idle_s > 0, active_s / (active_s + idle_s), 0) AS active_ratio
		 FROM events
		 WHERE tenant_id = ?
		   AND occurred_at BETWEEN ? AND ?
		 GROUP BY date, endpoint_id
		 ORDER BY date ASC`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("reports: idle active: %w", err)
	}
	defer rows.Close()

	var out []IdleActiveRow
	for rows.Next() {
		var r IdleActiveRow
		if err := rows.Scan(&r.Date, &r.EndpointID, &r.ActiveSeconds, &r.IdleSeconds, &r.ActiveRatio); err != nil {
			return nil, fmt.Errorf("reports: idle active scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EndpointActivitySummary returns a summary per endpoint for the last N days.
func (q *Querier) EndpointActivitySummary(ctx context.Context, tenantID string, from, to time.Time) ([]map[string]any, error) {
	rows, err := q.ch.Query(ctx,
		`SELECT
		   toString(endpoint_id) AS endpoint_id,
		   count() AS event_count,
		   max(occurred_at) AS last_seen,
		   min(occurred_at) AS first_seen
		 FROM events
		 WHERE tenant_id = ?
		   AND occurred_at BETWEEN ? AND ?
		 GROUP BY endpoint_id
		 ORDER BY last_seen DESC`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("reports: endpoint activity: %w", err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var endpointID string
		var eventCount int64
		var lastSeen, firstSeen time.Time
		if err := rows.Scan(&endpointID, &eventCount, &lastSeen, &firstSeen); err != nil {
			return nil, fmt.Errorf("reports: endpoint activity scan: %w", err)
		}
		out = append(out, map[string]any{
			"endpoint_id": endpointID,
			"event_count": eventCount,
			"last_seen":   lastSeen,
			"first_seen":  firstSeen,
		})
	}
	return out, rows.Err()
}

// AppBlocks returns app-blocking event counts in a time window.
func (q *Querier) AppBlocks(ctx context.Context, tenantID string, from, to time.Time) ([]AppBlockRow, error) {
	rows, err := q.ch.Query(ctx,
		`SELECT
		   toStartOfHour(occurred_at) AS hour,
		   toString(endpoint_id)      AS endpoint_id,
		   policy_match_value         AS app_name,
		   count()                    AS block_count
		 FROM events
		 WHERE tenant_id = ?
		   AND event_type = 'policy.app_blocked'
		   AND occurred_at BETWEEN ? AND ?
		 GROUP BY hour, endpoint_id, app_name
		 ORDER BY hour ASC`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("reports: app blocks: %w", err)
	}
	defer rows.Close()

	var out []AppBlockRow
	for rows.Next() {
		var r AppBlockRow
		if err := rows.Scan(&r.OccurredAt, &r.EndpointID, &r.AppName, &r.Count); err != nil {
			return nil, fmt.Errorf("reports: app blocks scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
