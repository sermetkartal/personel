package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus counters and histograms for the gateway.
// Created once at startup and passed through the application.
type Metrics struct {
	// EventsReceived counts events accepted from agents, labelled by tenant and type.
	EventsReceived *prometheus.CounterVec
	// EventsPublished counts events successfully published to NATS JetStream.
	EventsPublished *prometheus.CounterVec
	// NATSPublishDuration tracks JetStream publish latency.
	NATSPublishDuration *prometheus.HistogramVec
	// ClickHouseInsertDuration tracks ClickHouse batch insert latency (enricher).
	ClickHouseInsertDuration *prometheus.HistogramVec
	// ActiveStreams is the current number of open agent gRPC streams.
	ActiveStreams prometheus.Gauge
	// AuthFailures counts mTLS auth rejections by reason.
	AuthFailures *prometheus.CounterVec
	// KeyVersionMismatch counts Hello messages that failed key-version checks.
	KeyVersionMismatch *prometheus.CounterVec
	// RateLimitDrops counts events dropped by the token bucket rate limiter.
	RateLimitDrops *prometheus.CounterVec
	// BatchAckLatency tracks the time from EventBatch receipt to BatchAck send.
	BatchAckLatency *prometheus.HistogramVec
	// HeartbeatGap tracks the time since last heartbeat per endpoint (observe on
	// monitor sweep; high values signal Flow 7 / silent endpoint).
	HeartbeatGapSeconds *prometheus.HistogramVec
	// EnricherLag tracks NATS consumer lag (enricher).
	EnricherLag prometheus.Gauge
}

// NewMetrics registers and returns all gateway Prometheus metrics.
// The reg parameter allows injection of a custom registry for tests.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)

	return &Metrics{
		EventsReceived: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_gateway",
			Name:      "events_received_total",
			Help:      "Total number of events received from agents.",
		}, []string{"tenant_id", "event_type"}),

		EventsPublished: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_gateway",
			Name:      "events_published_total",
			Help:      "Total number of events successfully published to NATS JetStream.",
		}, []string{"tenant_id", "stream"}),

		NATSPublishDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_gateway",
			Name:      "nats_publish_duration_seconds",
			Help:      "JetStream publish round-trip duration.",
			Buckets:   prometheus.ExponentialBuckets(0.0005, 2, 14),
		}, []string{"stream", "status"}),

		ClickHouseInsertDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_enricher",
			Name:      "clickhouse_insert_duration_seconds",
			Help:      "ClickHouse batch insert duration.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 14),
		}, []string{"table", "status"}),

		ActiveStreams: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "personel_gateway",
			Name:      "stream_active",
			Help:      "Current number of open agent gRPC bidirectional streams.",
		}),

		AuthFailures: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_gateway",
			Name:      "auth_failures_total",
			Help:      "Total mTLS authentication rejections by reason.",
		}, []string{"reason"}),

		KeyVersionMismatch: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_gateway",
			Name:      "key_version_mismatch_total",
			Help:      "Hello messages rejected due to stale PE-DEK or TMK version.",
		}, []string{"tenant_id", "mismatch_type"}),

		RateLimitDrops: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_gateway",
			Name:      "rate_limit_drops_total",
			Help:      "Events or batches dropped by the token-bucket rate limiter.",
		}, []string{"tenant_id", "scope"}),

		BatchAckLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_gateway",
			Name:      "batch_ack_latency_seconds",
			Help:      "Time from EventBatch received to BatchAck sent (includes NATS publish).",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 14),
		}, []string{"tenant_id"}),

		HeartbeatGapSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_gateway",
			Name:      "heartbeat_gap_seconds",
			Help:      "Gap between consecutive heartbeats per endpoint. High values indicate potential Flow 7.",
			Buckets:   []float64{30, 60, 90, 120, 300, 600, 1800, 3600, 7200, 14400, 28800},
		}, []string{"tenant_id"}),

		EnricherLag: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "personel_enricher",
			Name:      "nats_consumer_lag",
			Help:      "Approximate number of unprocessed messages pending in the NATS JetStream consumer.",
		}),
	}
}
