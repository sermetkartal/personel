// Package splunk implements the Splunk HTTP Event Collector (HEC) exporter.
//
// ADR 0018 pattern: compile-time registry, Vault-resolved credentials.
// Phase 2.7 scaffold: real HEC calls deferred to Phase 2.8. The exporter
// is wired into the registry, accepts Publish calls, and returns a
// scaffold error per call so the bus records the events as "failed" in
// observability without crashing.
//
// Production behavior (Phase 2.8):
//   - Uses the Splunk HEC /services/collector endpoint with the
//     "Authorization: Splunk <token>" header
//   - Transforms Personel Event → Splunk JSON event with time,
//     source="personel", sourcetype="personel:audit", index=configured
//   - Batch endpoint: POST /services/collector/event with newline-
//     delimited JSON for batch sends
//   - Delivery acknowledgement via /services/collector/ack
package splunk

import (
	"context"
	"fmt"

	"github.com/personel/api/internal/siem"
)

const exporterName = "splunk"

func init() {
	siem.Register(exporterName, New)
}

// Exporter is the Splunk HEC adapter.
type Exporter struct {
	cfg siem.Config
	// Phase 2.8: httpClient, token (from Vault), index, source, sourcetype
}

// New constructs a Splunk exporter.
func New(cfg siem.Config) (siem.Exporter, error) {
	if cfg.Name != exporterName {
		return nil, fmt.Errorf("splunk: expected name %q, got %q", exporterName, cfg.Name)
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("splunk: Endpoint required (e.g. https://splunk.company.com:8088)")
	}
	return &Exporter{cfg: cfg}, nil
}

// Name returns the exporter identifier.
func (e *Exporter) Name() string { return exporterName }

// Publish sends a single event to Splunk HEC.
func (e *Exporter) Publish(ctx context.Context, event siem.Event) error {
	return fmt.Errorf("splunk: phase 2.7 scaffold — Publish not yet implemented (Phase 2.8 work)")
}

// PublishBatch sends multiple events in one HEC request.
func (e *Exporter) PublishBatch(ctx context.Context, events []siem.Event) error {
	return fmt.Errorf("splunk: phase 2.7 scaffold — PublishBatch not yet implemented")
}

// TestConnection pings the HEC health endpoint.
func (e *Exporter) TestConnection(ctx context.Context) error {
	return fmt.Errorf("splunk: phase 2.7 scaffold — TestConnection not yet implemented")
}

// Capabilities returns the HEC feature matrix.
func (e *Exporter) Capabilities() siem.Capabilities {
	return siem.Capabilities{
		SupportsBatch: true,
		MaxBatchSize:  1000,
		SupportsAck:   true,
	}
}
