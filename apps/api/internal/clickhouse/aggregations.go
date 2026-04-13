// aggregations.go — real ClickHouse aggregation queries for /v1/reports/ch/*.
//
// Item #68 of the 190-point production roadmap: wire real CH aggregation
// queries so the console reports pages can render production-scale metrics
// instead of the Postgres-backed preview (reportspg). Each method is
// defensive against empty tables — on seed / fresh installations the
// queries return an empty slice rather than erroring.
//
// Tenant isolation (KVKK m.5 + SOC 2 CC6.1) is NON-NEGOTIABLE: every query
// binds tenant_id via the positional parameter API. There is no string
// interpolation of tenant_id anywhere. The `Client` wrapper is the only
// supported call path for /v1/reports/ch handlers.
//
// Schema assumptions (from apps/gateway/internal/clickhouse/schemas.go):
//   - events_raw(tenant_id UUID, endpoint_id UUID, occurred_at DateTime64,
//                event_type LowCardinality(String), user_sid String,
//                payload String)   -- JSON-encoded payload per event_type
//
// Payload JSON extraction uses simpleJSONExtractString which tolerates
// missing keys (empty string) so policy-evolution drift doesn't break the
// aggregation.
//
// Hard caps:
//   - LIMIT 10000 on every query to protect CH from runaway scans.
//   - Date range validation lives in the service layer (reports/ch_service.go).
//   - When a required table is missing (fresh install), the query returns
//     an empty slice instead of a 500 — auditors see 200 + [] not an error.
package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// aggQueryLimit is the absolute hard cap on rows returned by any aggregation
// query. Runaway queries against events_raw on a populated cluster can scan
// billions of rows; the LIMIT clause + this constant is the last line of
// defence after tenant_id + time range filters.
const aggQueryLimit = 10000

// missingTableErrorCodes are ClickHouse server error codes / substrings
// emitted when a referenced table does not exist. Fresh installations (no
// gateway running yet, no enricher writes) hit these — we swallow them and
// return an empty result so the UI can render "no data yet" instead of a
// hard error.
var missingTableErrorCodes = []string{
	"UNKNOWN_TABLE",
	"code: 60",  // ClickHouse "Table does not exist" error code
	"code: 81",  // "Database does not exist"
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, pat := range missingTableErrorCodes {
		if strings.Contains(msg, pat) {
			return true
		}
	}
	return false
}

// queryRunner is the minimum surface of clickhouse.Conn that the
// aggregation queries need. It is declared here so unit tests can inject
// a fake without standing up a full ClickHouse driver.
type queryRunner interface {
	Query(ctx context.Context, query string, args ...any) (driverRows, error)
}

// driverRows aliases the clickhouse-go rows iterator. An unexported alias
// keeps the test fake surface narrow while preserving the production
// driver type.
type driverRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// chConnAdapter bridges a real clickhouse.Conn to the internal queryRunner
// interface so the production path does not allocate extra wrappers per
// call — only one allocation per Client construction.
type chConnAdapter struct {
	conn clickhouse.Conn
}

func (a *chConnAdapter) Query(ctx context.Context, query string, args ...any) (driverRows, error) {
	rows, err := a.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Client wraps a clickhouse.Conn to provide aggregation-only, tenant-scoped
// read operations. Construct via NewClient(conn). A nil receiver returns
// ErrClientUnavailable from every method — handlers map this to 503.
type Client struct {
	runner queryRunner
}

// NewClient wraps an established clickhouse.Conn. conn may be nil only in
// tests that exercise the nil-client 503 path.
func NewClient(conn clickhouse.Conn) *Client {
	if conn == nil {
		return &Client{runner: nil}
	}
	return &Client{runner: &chConnAdapter{conn: conn}}
}

// newClientWithRunner is a test-only constructor that injects a fake
// queryRunner. Keeping it unexported makes the production path the only
// externally reachable constructor.
func newClientWithRunner(r queryRunner) *Client {
	return &Client{runner: r}
}

// ErrClientUnavailable is returned by every aggregation method when the
// underlying CH connection is nil. Handlers translate this to HTTP 503.
var ErrClientUnavailable = errors.New("clickhouse: client unavailable")

// -----------------------------------------------------------------------
// TopApps
// -----------------------------------------------------------------------

// TopAppsParams narrows AggTopApps by time window, category, user list,
// and result size. Zero values are normalised: From == zero → last 7 days,
// Limit <= 0 → 10, Limit > 100 → 100.
type TopAppsParams struct {
	From     time.Time
	To       time.Time
	Limit    int      // 1..100
	Category string   // "", "productive", "neutral", "distracting"
	UserIDs  []string // empty = all users
}

// TopAppRow is one row of the top-apps aggregation.
type TopAppRow struct {
	AppName       string `json:"app_name"`
	Category      string `json:"category"`
	ActiveMinutes int64  `json:"active_minutes"`
	UniqueUsers   int64  `json:"unique_users"`
	UniqueHosts   int64  `json:"unique_hosts"`
}

// AggTopApps returns the top N applications by active minutes over the
// requested window, scoped strictly to the given tenant. Category filter
// is matched against JSON-extracted payload.category (empty = no filter).
func (c *Client) AggTopApps(ctx context.Context, tenantID string, p TopAppsParams) ([]TopAppRow, error) {
	if c == nil || c.runner == nil {
		return nil, ErrClientUnavailable
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	// Query: events_raw rows with event_type='process.foreground_change',
	// payload JSON contains {"app_name","category","active_seconds"}.
	// We aggregate active_seconds → minutes, count distinct users + hosts.
	//
	// NB: simpleJSONExtractString is used rather than JSONExtract() so the
	// query remains compatible with ClickHouse 22.x (the pilot version).
	var (
		query string
		args  []any
	)
	args = append(args, tenantID, p.From, p.To)

	baseFilters := `WHERE tenant_id = ?
		  AND occurred_at BETWEEN ? AND ?
		  AND event_type = 'process.foreground_change'`

	if p.Category != "" {
		baseFilters += ` AND simpleJSONExtractString(payload, 'category') = ?`
		args = append(args, p.Category)
	}
	if len(p.UserIDs) > 0 {
		baseFilters += ` AND user_sid IN ?`
		args = append(args, p.UserIDs)
	}

	query = fmt.Sprintf(`
		SELECT
			simpleJSONExtractString(payload, 'app_name')                           AS app_name,
			simpleJSONExtractString(payload, 'category')                           AS category,
			intDiv(sum(simpleJSONExtractUInt(payload, 'active_seconds')), 60)      AS active_minutes,
			uniqExact(user_sid)                                                    AS unique_users,
			uniqExact(endpoint_id)                                                 AS unique_hosts
		FROM events_raw
		%s
		GROUP BY app_name, category
		HAVING app_name != ''
		ORDER BY active_minutes DESC
		LIMIT %d`, baseFilters, p.Limit)

	rows, err := c.runner.Query(ctx, query, args...)
	if err != nil {
		if isMissingTable(err) {
			return []TopAppRow{}, nil
		}
		return nil, fmt.Errorf("clickhouse: agg_top_apps: %w", err)
	}
	defer rows.Close()

	out := make([]TopAppRow, 0)
	for rows.Next() {
		var r TopAppRow
		if err := rows.Scan(&r.AppName, &r.Category, &r.ActiveMinutes, &r.UniqueUsers, &r.UniqueHosts); err != nil {
			return nil, fmt.Errorf("clickhouse: agg_top_apps: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: agg_top_apps: rows: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------
// IdleActive
// -----------------------------------------------------------------------

// IdleActiveParams defines the idle/active daily breakdown query inputs.
type IdleActiveParams struct {
	From    time.Time
	To      time.Time
	UserIDs []string
}

// DailyIdleActiveRow is one day's idle-vs-active breakdown for the tenant.
type DailyIdleActiveRow struct {
	Date          time.Time `json:"date"`
	ActiveSeconds int64     `json:"active_seconds"`
	IdleSeconds   int64     `json:"idle_seconds"`
	ActiveRatio   float64   `json:"active_ratio"`
	UniqueUsers   int64     `json:"unique_users"`
}

// AggIdleActive returns one row per day in the requested range with active
// and idle second totals aggregated across all endpoints (or a filtered
// user_id subset). The ratio is active / (active+idle), 0 when both zero.
func (c *Client) AggIdleActive(ctx context.Context, tenantID string, p IdleActiveParams) ([]DailyIdleActiveRow, error) {
	if c == nil || c.runner == nil {
		return nil, ErrClientUnavailable
	}

	args := []any{tenantID, p.From, p.To}
	filters := `WHERE tenant_id = ?
		  AND occurred_at BETWEEN ? AND ?
		  AND event_type IN ('session.active_end', 'session.idle_end')`
	if len(p.UserIDs) > 0 {
		filters += ` AND user_sid IN ?`
		args = append(args, p.UserIDs)
	}

	query := fmt.Sprintf(`
		SELECT
			toDate(occurred_at) AS day,
			sumIf(simpleJSONExtractUInt(payload, 'duration_seconds'), event_type = 'session.active_end') AS active_s,
			sumIf(simpleJSONExtractUInt(payload, 'duration_seconds'), event_type = 'session.idle_end')   AS idle_s,
			if(active_s + idle_s > 0, active_s / (active_s + idle_s), 0)                                  AS active_ratio,
			uniqExact(user_sid)                                                                           AS unique_users
		FROM events_raw
		%s
		GROUP BY day
		ORDER BY day ASC
		LIMIT %d`, filters, aggQueryLimit)

	rows, err := c.runner.Query(ctx, query, args...)
	if err != nil {
		if isMissingTable(err) {
			return []DailyIdleActiveRow{}, nil
		}
		return nil, fmt.Errorf("clickhouse: agg_idle_active: %w", err)
	}
	defer rows.Close()

	out := make([]DailyIdleActiveRow, 0)
	for rows.Next() {
		var r DailyIdleActiveRow
		if err := rows.Scan(&r.Date, &r.ActiveSeconds, &r.IdleSeconds, &r.ActiveRatio, &r.UniqueUsers); err != nil {
			return nil, fmt.Errorf("clickhouse: agg_idle_active: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: agg_idle_active: rows: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------
// Productivity
// -----------------------------------------------------------------------

// ProductivityParams defines the rolling 7-day productivity score query.
type ProductivityParams struct {
	From    time.Time
	To      time.Time
	UserIDs []string
}

// ProductivityRow is one user's productivity score (0..100) over the window,
// computed as productive_minutes / (productive + neutral + distracting).
type ProductivityRow struct {
	UserSID            string  `json:"user_sid"`
	Date               time.Time `json:"date"`
	ProductiveMinutes  int64   `json:"productive_minutes"`
	NeutralMinutes     int64   `json:"neutral_minutes"`
	DistractingMinutes int64   `json:"distracting_minutes"`
	Score              float64 `json:"score"` // 0..100
}

// AggProductivityScore aggregates per-user per-day productivity breakdown.
// This is the raw input for the rolling 7-day score that the UI computes
// client-side. Returns rows ordered by (date ASC, user_sid ASC).
func (c *Client) AggProductivityScore(ctx context.Context, tenantID string, p ProductivityParams) ([]ProductivityRow, error) {
	if c == nil || c.runner == nil {
		return nil, ErrClientUnavailable
	}

	args := []any{tenantID, p.From, p.To}
	filters := `WHERE tenant_id = ?
		  AND occurred_at BETWEEN ? AND ?
		  AND event_type = 'process.foreground_change'`
	if len(p.UserIDs) > 0 {
		filters += ` AND user_sid IN ?`
		args = append(args, p.UserIDs)
	}

	query := fmt.Sprintf(`
		SELECT
			user_sid,
			toDate(occurred_at) AS day,
			intDiv(sumIf(simpleJSONExtractUInt(payload, 'active_seconds'),
				simpleJSONExtractString(payload, 'category') = 'productive'), 60) AS prod_min,
			intDiv(sumIf(simpleJSONExtractUInt(payload, 'active_seconds'),
				simpleJSONExtractString(payload, 'category') = 'neutral'), 60)    AS neu_min,
			intDiv(sumIf(simpleJSONExtractUInt(payload, 'active_seconds'),
				simpleJSONExtractString(payload, 'category') = 'distracting'), 60) AS dist_min,
			if(prod_min + neu_min + dist_min > 0,
				100.0 * prod_min / (prod_min + neu_min + dist_min),
				0) AS score
		FROM events_raw
		%s
		GROUP BY user_sid, day
		HAVING user_sid != ''
		ORDER BY day ASC, user_sid ASC
		LIMIT %d`, filters, aggQueryLimit)

	rows, err := c.runner.Query(ctx, query, args...)
	if err != nil {
		if isMissingTable(err) {
			return []ProductivityRow{}, nil
		}
		return nil, fmt.Errorf("clickhouse: agg_productivity: %w", err)
	}
	defer rows.Close()

	out := make([]ProductivityRow, 0)
	for rows.Next() {
		var r ProductivityRow
		if err := rows.Scan(&r.UserSID, &r.Date, &r.ProductiveMinutes, &r.NeutralMinutes, &r.DistractingMinutes, &r.Score); err != nil {
			return nil, fmt.Errorf("clickhouse: agg_productivity: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: agg_productivity: rows: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------
// AppBlocks
// -----------------------------------------------------------------------

// AppBlocksParams defines the app-blocks aggregation query inputs.
type AppBlocksParams struct {
	From  time.Time
	To    time.Time
	Limit int
}

// AppBlockRow is one row of the policy-blocked-app-launch aggregation.
type AppBlockRow struct {
	AppName     string `json:"app_name"`
	BlockCount  int64  `json:"block_count"`
	UniqueUsers int64  `json:"unique_users"`
}

// AggAppBlocks counts policy.app_blocked events grouped by app_name over
// the time window. The top N (by count) are returned, ordered desc.
func (c *Client) AggAppBlocks(ctx context.Context, tenantID string, p AppBlocksParams) ([]AppBlockRow, error) {
	if c == nil || c.runner == nil {
		return nil, ErrClientUnavailable
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	query := fmt.Sprintf(`
		SELECT
			simpleJSONExtractString(payload, 'app_name') AS app_name,
			count()                                       AS block_count,
			uniqExact(user_sid)                           AS unique_users
		FROM events_raw
		WHERE tenant_id = ?
		  AND occurred_at BETWEEN ? AND ?
		  AND event_type = 'policy.app_blocked'
		GROUP BY app_name
		HAVING app_name != ''
		ORDER BY block_count DESC
		LIMIT %d`, p.Limit)

	rows, err := c.runner.Query(ctx, query, tenantID, p.From, p.To)
	if err != nil {
		if isMissingTable(err) {
			return []AppBlockRow{}, nil
		}
		return nil, fmt.Errorf("clickhouse: agg_app_blocks: %w", err)
	}
	defer rows.Close()

	out := make([]AppBlockRow, 0)
	for rows.Next() {
		var r AppBlockRow
		if err := rows.Scan(&r.AppName, &r.BlockCount, &r.UniqueUsers); err != nil {
			return nil, fmt.Errorf("clickhouse: agg_app_blocks: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: agg_app_blocks: rows: %w", err)
	}
	return out, nil
}
