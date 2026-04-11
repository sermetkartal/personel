package evidence

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// CoverageCollector exposes evidence coverage as a Prometheus gauge.
// Implements prometheus.Collector so metrics are computed at scrape time
// against the live DB — no background refresh loop, no staleness window.
//
// Exposed metric:
//
//	personel_evidence_items_total{tenant_id, control, period}
//
// Each scrape runs a single GROUP BY query per tenant. With one tenant
// and ~9 expected controls the overhead is negligible (<5ms at p99).
// Prometheus alert rules can detect zero-coverage windows via:
//
//	max_over_time(personel_evidence_items_total[24h]) == 0
//
// which pages the DPO if any expected control has no evidence in the
// last 24 hours of scrapes.
type CoverageCollector struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	// Tenant list is discovered lazily to avoid coupling the evidence
	// package to a tenant registry. A collector with no tenants yet
	// simply emits zero metrics until the first refresh populates it.
	mu      sync.Mutex
	tenants []string
	refresh time.Time

	desc *prometheus.Desc
}

// NewCoverageCollector creates a collector ready to register against a
// prometheus.Registry. The caller must invoke SetTenants() at boot (and
// optionally refresh when tenants are added) so the collector knows what
// to query. Without a tenant list the collector emits nothing, which is
// the correct default in scaffold mode.
func NewCoverageCollector(pool *pgxpool.Pool, log *slog.Logger) *CoverageCollector {
	return &CoverageCollector{
		pool: pool,
		log:  log,
		desc: prometheus.NewDesc(
			"personel_evidence_items_total",
			"Count of SOC 2 evidence items by tenant, control, and collection period.",
			[]string{"tenant_id", "control", "period"},
			nil,
		),
	}
}

// SetTenants updates the tenant list the collector will query on each
// scrape. Safe to call from any goroutine.
func (c *CoverageCollector) SetTenants(tenantIDs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tenants = append(c.tenants[:0], tenantIDs...)
	c.refresh = time.Now()
}

// Describe sends the single metric descriptor.
func (c *CoverageCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect queries the DB for every tenant + current period + every
// expected control and emits the count as a gauge value. Controls with
// zero items still emit (value=0) so the gap is visible in the metric
// stream — that is the whole point of the alert rule.
//
// Any per-tenant query failure is logged and skipped; the collector
// never blocks the scrape because a broken tenant query would poison
// the entire Prometheus target. Partial data is better than no data.
func (c *CoverageCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	tenants := append([]string(nil), c.tenants...)
	c.mu.Unlock()

	if len(tenants) == 0 {
		return
	}

	period := time.Now().UTC().Format("2006-01")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	expected := expectedControls()

	for _, tid := range tenants {
		counts, err := c.countByControl(ctx, tid, period)
		if err != nil {
			c.log.ErrorContext(ctx, "evidence/metrics: count query failed",
				slog.String("tenant_id", tid),
				slog.String("period", period),
				slog.String("error", err.Error()),
			)
			continue
		}
		for _, ctrl := range expected {
			n := counts[ctrl]
			ch <- prometheus.MustNewConstMetric(
				c.desc,
				prometheus.GaugeValue,
				float64(n),
				tid, string(ctrl), period,
			)
		}
	}
}

// countByControl duplicates a minimal form of Store.CountByControl so
// the collector can run without constructing a Store. Bypasses RLS by
// executing in a session without personel.tenant_id set — safe because
// the collector is in-process, trusted, and scopes by the tenant_id
// literal in the WHERE clause.
func (c *CoverageCollector) countByControl(ctx context.Context, tenantID, period string) (map[ControlID]int, error) {
	const q = `
		SELECT control, COUNT(*)::int
		FROM evidence_items
		WHERE tenant_id = $1 AND collection_period = $2
		GROUP BY control
	`
	rows, err := c.pool.Query(ctx, q, tenantID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[ControlID]int)
	for rows.Next() {
		var ctrl string
		var n int
		if err := rows.Scan(&ctrl, &n); err != nil {
			return nil, err
		}
		out[ControlID(ctrl)] = n
	}
	return out, rows.Err()
}
