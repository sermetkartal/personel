// Package logotiger implements the Logo Tiger HRIS connector — the
// Turkish market differentiator adapter per ADR 0018.
//
// Logo Tiger is a major Turkish ERP+HR suite used by thousands of Turkish
// enterprises. Its authentication model is session-ticket-based (not OAuth2),
// and its API is REST-ish but not fully OpenAPI-compliant. This package
// encapsulates those quirks so the rest of Personel doesn't see them.
//
// Phase 2.5 scaffold: interface + Factory registration. Real Logo Tiger
// API calls require a test instance with a Logo partner-level license;
// Phase 2.6 will wire this against the customer's on-prem Logo Tiger
// installation (same datacenter as the Personel platform).
//
// ADR 0018 notes: Logo Tiger does NOT support webhooks. The connector
// uses periodic polling only (hourly by default, configurable).
package logotiger

import (
	"context"
	"fmt"

	"github.com/personel/api/internal/hris"
)

const connectorName = "logo_tiger"

func init() {
	hris.Register(connectorName, New)
}

// Connector is the Logo Tiger adapter.
type Connector struct {
	cfg hris.Config
	// Phase 2.6: httpClient *http.Client
	// Phase 2.6: sessionTicket string (rotated per ticketTTL)
	// Phase 2.6: ticketExpiry time.Time
	// Phase 2.6: firmNumber int (Logo Tiger multi-firm installations)
	// Phase 2.6: periodNumber int (Logo Tiger accounting period)
}

// New constructs a Logo Tiger connector.
func New(cfg hris.Config) (hris.Connector, error) {
	if cfg.Name != connectorName {
		return nil, hris.Wrap(connectorName, hris.ErrorPermanent,
			fmt.Errorf("expected name %q, got %q", connectorName, cfg.Name))
	}
	if cfg.BaseURL == "" {
		return nil, hris.Wrap(connectorName, hris.ErrorPermanent,
			fmt.Errorf("BaseURL required (Logo Tiger on-prem REST endpoint)"))
	}
	return &Connector{cfg: cfg}, nil
}

// Name returns the connector identifier.
func (c *Connector) Name() string { return connectorName }

// TestConnection opens a Logo Tiger session ticket to verify credentials.
// Phase 2.6 will call `POST /api/v1/token` with username/password from
// Vault, receive a session ticket, and verify the returned firm list is
// non-empty.
func (c *Connector) TestConnection(ctx context.Context) error {
	return hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: Logo Tiger TestConnection not yet implemented; " +
			"Phase 2.6 will wire session ticket auth + firm list probe"))
}

// ListEmployees fetches all active employees from Logo Tiger's HR module.
// Phase 2.6 will page through `GET /api/v1/humanResources/employees?page=N`
// and enrich each record with department/title from the `positions` table.
//
// Special handling: Logo Tiger stores email addresses in the `Contact`
// table which requires a separate join per employee. Phase 2.6 uses a
// batch-fetch strategy and falls back to `<sicilNo>@<customer-domain>`
// synthetic emails when Contact rows are missing (Phase 2.6 open question
// flagged in ADR 0018).
func (c *Connector) ListEmployees(ctx context.Context) ([]hris.Employee, error) {
	return nil, hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: Logo Tiger ListEmployees not yet implemented"))
}

// GetEmployee fetches a single employee by Logo Tiger `LOGICALREF`.
func (c *Connector) GetEmployee(ctx context.Context, externalID string) (*hris.Employee, error) {
	return nil, hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: Logo Tiger GetEmployee not yet implemented"))
}

// WatchChanges returns a closed channel — Logo Tiger does not support
// webhooks. The sync orchestrator falls back to polling the PollInterval
// from Capabilities (default 1 hour).
func (c *Connector) WatchChanges(ctx context.Context) (<-chan hris.Change, error) {
	ch := make(chan hris.Change)
	close(ch)
	return ch, nil
}

// Capabilities returns the Logo Tiger feature matrix. Note the
// intentional SupportsWebhooks=false — this is a permanent property of
// the upstream product, not a scaffold gap.
func (c *Connector) Capabilities() hris.Capabilities {
	return hris.Capabilities{
		SupportsWebhooks:     false, // Logo Tiger has no webhook layer
		SupportsManagerChain: true,  // LogicalRef cross-references exist
		SupportsCustomFields: true,  // free text notes + user-defined fields
		MaxPageSize:          500,   // Logo Tiger API default
		PollInterval:         0,     // use global default (1h)
	}
}
