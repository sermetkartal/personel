// Package enricher — data quality monitoring (DQM).
//
// Faz 7 / Roadmap item #80.
//
// The DQM tracks per-tenant counters + latency histograms that cover
// the full enricher hot path:
//
//   events_received        — every Event pulled from a NATS batch
//   events_decoded         — Enrich() returned without error
//   events_dropped{reason} — dropped by dedup / enrich_error / policy / etc.
//   events_dlq             — routed to DLQ stream (future: wired by dlq.go)
//   decode_latency_ms      — Enrich() duration
//   enrich_latency_ms      — sink-write duration (batcher Add)
//
// Prometheus alert rules in infra/compose/prometheus/alerts.yml compare
// the 5m rate of events_received against a 7-day baseline; a drop of
// more than 30% fires `DQMEventsReceivedAnomaly`.
//
// All metrics are labelled by tenant_id so per-customer dashboards and
// alerts can isolate the impact. This is intentionally distinct from
// the existing gateway Metrics struct — the enricher DQM covers the
// post-NATS processing pipeline, while Metrics.EventsReceived covers
// the gRPC ingest path.
package enricher

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DQM exposes Prometheus metrics for per-tenant data-quality tracking.
type DQM struct {
	eventsReceived *prometheus.CounterVec
	eventsDecoded  *prometheus.CounterVec
	eventsDropped  *prometheus.CounterVec
	eventsDLQ      *prometheus.CounterVec
	decodeLatency  *prometheus.HistogramVec
	enrichLatency  *prometheus.HistogramVec
}

// NewDQM registers DQM metrics with the given registry. Pass nil to use
// the default registry.
func NewDQM(reg prometheus.Registerer) *DQM {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)

	return &DQM{
		eventsReceived: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "events_received_total",
			Help:      "Events pulled from NATS into the enricher pipeline, labelled by tenant_id.",
		}, []string{"tenant_id"}),

		eventsDecoded: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "events_decoded_total",
			Help:      "Events that successfully completed the Enrich() step.",
		}, []string{"tenant_id"}),

		eventsDropped: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "events_dropped_total",
			Help:      "Events dropped during enrichment, by reason.",
		}, []string{"tenant_id", "reason"}),

		eventsDLQ: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "events_dlq_total",
			Help:      "Events routed to the dead-letter queue after exhausted retries.",
		}, []string{"tenant_id"}),

		decodeLatency: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_enricher",
			Name:      "decode_latency_ms",
			Help:      "Time spent in Enrich() per event (milliseconds).",
			Buckets:   []float64{0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000},
		}, []string{"tenant_id"}),

		enrichLatency: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_enricher",
			Name:      "enrich_latency_ms",
			Help:      "Time spent writing to the sink (ClickHouse batcher) per event (milliseconds).",
			Buckets:   []float64{0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000},
		}, []string{"tenant_id"}),
	}
}

// RecordReceived increments the per-tenant received counter.
func (d *DQM) RecordReceived(tenantID string) {
	if d == nil {
		return
	}
	d.eventsReceived.WithLabelValues(safeTenant(tenantID)).Inc()
}

// RecordDecoded increments the decoded counter and observes the
// decode latency histogram.
func (d *DQM) RecordDecoded(tenantID string, latencyMs float64) {
	if d == nil {
		return
	}
	t := safeTenant(tenantID)
	d.eventsDecoded.WithLabelValues(t).Inc()
	d.decodeLatency.WithLabelValues(t).Observe(latencyMs)
}

// RecordEnriched observes the enrich/sink write latency.
func (d *DQM) RecordEnriched(tenantID string, latencyMs float64) {
	if d == nil {
		return
	}
	d.enrichLatency.WithLabelValues(safeTenant(tenantID)).Observe(latencyMs)
}

// RecordDropped increments the per-tenant drop counter with the given reason.
func (d *DQM) RecordDropped(tenantID, reason string) {
	if d == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	d.eventsDropped.WithLabelValues(safeTenant(tenantID), reason).Inc()
}

// RecordDLQ increments the per-tenant DLQ counter.
func (d *DQM) RecordDLQ(tenantID string) {
	if d == nil {
		return
	}
	d.eventsDLQ.WithLabelValues(safeTenant(tenantID)).Inc()
}

// safeTenant replaces empty tenant IDs with a sentinel so Prometheus
// labels stay bounded and dashboards don't show mystery empty series.
func safeTenant(tenantID string) string {
	if tenantID == "" {
		return "unknown"
	}
	return tenantID
}
