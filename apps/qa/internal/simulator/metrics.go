// metrics.go registers and exposes Prometheus metrics for the simulator.
//
// Metrics are designed to be scraped by the load test runner to compute
// Phase 1 exit criteria pass/fail:
//   - events_sent_total{type} → event loss rate (exit criterion #6)
//   - ack_latency_seconds     → end-to-end event latency proxy (exit criterion #7)
//   - agents_active           → pool size during test
//   - errors_total{reason}    → reliability signal
package simulator

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SimulatorMetrics holds all Prometheus metrics for the simulator.
type SimulatorMetrics struct {
	AgentsActive    prometheus.Gauge
	EventsSentTotal *prometheus.CounterVec
	AcksReceived    *prometheus.CounterVec
	AckLatency      *prometheus.HistogramVec
	ErrorsTotal     *prometheus.CounterVec
	StreamRestarts  prometheus.Counter
	BatchesSent     prometheus.Counter
	BatchesAcked    prometheus.Counter
	ConnectLatency  prometheus.Histogram
	QueueDepth      prometheus.Gauge
}

// NewSimulatorMetrics creates and registers all simulator metrics.
// The registry parameter allows tests to use isolated registries.
func NewSimulatorMetrics(reg prometheus.Registerer) *SimulatorMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)

	return &SimulatorMetrics{
		AgentsActive: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "personel_sim",
			Name:      "agents_active",
			Help:      "Number of simulated agents currently connected to the gateway.",
		}),

		EventsSentTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "events_sent_total",
			Help:      "Total events sent by simulated agents, partitioned by event type.",
		}, []string{"event_type"}),

		AcksReceived: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "acks_received_total",
			Help:      "BatchAck messages received from gateway, partitioned by outcome.",
		}, []string{"outcome"}), // outcome: "accepted" | "rejected"

		AckLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "personel_sim",
			Name:      "ack_latency_seconds",
			Help:      "Time from EventBatch send to BatchAck receipt. Proxy for gateway processing latency.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"agent_id"}),

		ErrorsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "errors_total",
			Help:      "Errors encountered by simulated agents, partitioned by reason.",
		}, []string{"reason"}), // reason: "connect_failed" | "stream_broken" | "tls_error" | "send_failed" | "recv_failed"

		StreamRestarts: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "stream_restarts_total",
			Help:      "Number of stream reconnections triggered by transport errors or server signals.",
		}),

		BatchesSent: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "batches_sent_total",
			Help:      "Total EventBatch messages sent to the gateway.",
		}),

		BatchesAcked: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "personel_sim",
			Name:      "batches_acked_total",
			Help:      "Total EventBatch messages acknowledged by the gateway.",
		}),

		ConnectLatency: factory.NewHistogram(prometheus.HistogramOpts{
			Namespace: "personel_sim",
			Name:      "connect_latency_seconds",
			Help:      "Time from dial initiation to stream Hello/Welcome handshake completion.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0},
		}),

		QueueDepth: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "personel_sim",
			Name:      "queue_depth",
			Help:      "Aggregate synthetic event queue depth across all active agents.",
		}),
	}
}

// RecordError is a convenience method to increment the errors counter.
func (m *SimulatorMetrics) RecordError(reason string) {
	m.ErrorsTotal.WithLabelValues(reason).Inc()
}

// RecordEventSent increments the sent counter for the given event type.
func (m *SimulatorMetrics) RecordEventSent(eventType string) {
	m.EventsSentTotal.WithLabelValues(eventType).Inc()
}
