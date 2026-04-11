// Package observability — Prometheus metrics registry.
package observability

import "github.com/prometheus/client_golang/prometheus"

// NewRegistry creates a non-default Prometheus registry.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}
