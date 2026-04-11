// Package sentinel implements the Microsoft Sentinel Log Ingestion API
// exporter via Azure Monitor Data Collection Rules (DCR).
//
// ADR 0018 pattern: compile-time registry, Vault-resolved Azure SPN
// credentials. Phase 2.7 scaffold; real DCR calls deferred to Phase 2.8.
//
// Production behavior (Phase 2.8):
//   - Uses Azure Monitor Log Ingestion API (POST to the DCE endpoint)
//   - Authenticates via Azure AD service principal (client_id +
//     client_secret from Vault, or managed identity if running in Azure)
//   - Transforms Event → DCR custom log schema columns
//   - Batch endpoint native in the DCR contract
//   - No delivery ack — Sentinel ingestion is fire-and-forget
package sentinel

import (
	"context"
	"fmt"

	"github.com/personel/api/internal/siem"
)

const exporterName = "sentinel"

func init() {
	siem.Register(exporterName, New)
}

// Exporter is the Microsoft Sentinel adapter.
type Exporter struct {
	cfg siem.Config
	// Phase 2.8: httpClient, aadClient, dceEndpoint, dcrImmutableID, streamName
}

// New constructs a Sentinel exporter.
func New(cfg siem.Config) (siem.Exporter, error) {
	if cfg.Name != exporterName {
		return nil, fmt.Errorf("sentinel: expected name %q, got %q", exporterName, cfg.Name)
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("sentinel: Endpoint required (Azure DCE ingestion URL)")
	}
	return &Exporter{cfg: cfg}, nil
}

// Name returns the exporter identifier.
func (e *Exporter) Name() string { return exporterName }

// Publish sends a single event to Sentinel Log Ingestion.
func (e *Exporter) Publish(ctx context.Context, event siem.Event) error {
	return fmt.Errorf("sentinel: phase 2.7 scaffold — Publish not yet implemented (Phase 2.8 work)")
}

// PublishBatch sends multiple events in one DCR ingestion request.
func (e *Exporter) PublishBatch(ctx context.Context, events []siem.Event) error {
	return fmt.Errorf("sentinel: phase 2.7 scaffold — PublishBatch not yet implemented")
}

// TestConnection verifies the DCE endpoint and AAD token acquisition.
func (e *Exporter) TestConnection(ctx context.Context) error {
	return fmt.Errorf("sentinel: phase 2.7 scaffold — TestConnection not yet implemented")
}

// Capabilities returns the DCR ingestion feature matrix.
func (e *Exporter) Capabilities() siem.Capabilities {
	return siem.Capabilities{
		SupportsBatch: true,
		MaxBatchSize:  1000, // DCR batch limit ~1MB, ~1000 events typical
		SupportsAck:   false, // fire-and-forget
	}
}
