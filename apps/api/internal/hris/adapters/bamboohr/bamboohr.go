// Package bamboohr implements the BambooHR HRIS connector.
// ADR 0018 first international adapter. OAuth2 authentication, REST API,
// webhook-based incremental sync.
//
// Phase 2.5 scaffold: interface + Factory registration, real HTTP calls
// and OAuth2 flow are Phase 2.6 work. All methods currently return
// ErrorUnknown with a "phase 2.5 scaffold" message so the sync orchestrator
// surfaces a clear error rather than silently accepting an empty employee
// list.
package bamboohr

import (
	"context"
	"fmt"

	"github.com/personel/api/internal/hris"
)

const connectorName = "bamboohr"

// init registers the BambooHR factory with the global registry.
func init() {
	hris.Register(connectorName, New)
}

// Connector is the BambooHR adapter.
type Connector struct {
	cfg hris.Config
	// Phase 2.6: httpClient *http.Client
	// Phase 2.6: oauth2 *oauth2.Config
	// Phase 2.6: companyDomain string (e.g. "acmecorp" for acmecorp.bamboohr.com)
}

// New constructs a BambooHR connector. Exported for the registry Factory
// signature; most callers should use hris.Build(cfg) instead.
func New(cfg hris.Config) (hris.Connector, error) {
	if cfg.Name != connectorName {
		return nil, hris.Wrap(connectorName, hris.ErrorPermanent,
			fmt.Errorf("expected name %q, got %q", connectorName, cfg.Name))
	}
	if cfg.BaseURL == "" {
		return nil, hris.Wrap(connectorName, hris.ErrorPermanent,
			fmt.Errorf("BaseURL required (e.g. https://api.bamboohr.com/api/gateway.php/<company>/v1)"))
	}
	return &Connector{cfg: cfg}, nil
}

// Name returns the connector identifier.
func (c *Connector) Name() string { return connectorName }

// TestConnection verifies credentials against BambooHR's `/employees/directory`.
// Phase 2.5: returns a scaffold error explaining the endpoint needs Phase 2.6
// implementation.
func (c *Connector) TestConnection(ctx context.Context) error {
	return hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: BambooHR TestConnection not yet implemented; " +
			"Phase 2.6 will wire OAuth2 + /employees/directory call"))
}

// ListEmployees fetches all employees from BambooHR. Phase 2.5 stub.
// Phase 2.6 will paginate via `/employees/directory` (no pagination in
// BambooHR's directory API; single call returns all) and enrich via
// `/employees/{id}/tables/jobInfo` for current department + manager.
func (c *Connector) ListEmployees(ctx context.Context) ([]hris.Employee, error) {
	return nil, hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: ListEmployees not yet implemented"))
}

// GetEmployee fetches a single employee by BambooHR employee ID.
// Phase 2.6 will call `/employees/{id}?fields=firstName,lastName,workEmail,...`.
func (c *Connector) GetEmployee(ctx context.Context, externalID string) (*hris.Employee, error) {
	return nil, hris.Wrap(connectorName, hris.ErrorUnknown,
		fmt.Errorf("phase 2.5 scaffold: GetEmployee not yet implemented"))
}

// WatchChanges subscribes to BambooHR webhooks. BambooHR supports webhooks
// on `employees.{created,updated,terminated}` events per Employment Status
// table. Phase 2.6 will register the webhook subscription during setup and
// expose an inbound HTTP handler that the customer's load balancer forwards
// to this connector.
func (c *Connector) WatchChanges(ctx context.Context) (<-chan hris.Change, error) {
	// Phase 2.5: return a closed channel so the orchestrator falls back
	// to polling.
	ch := make(chan hris.Change)
	close(ch)
	return ch, nil
}

// Capabilities returns the BambooHR feature matrix.
func (c *Connector) Capabilities() hris.Capabilities {
	return hris.Capabilities{
		SupportsWebhooks:     true, // Phase 2.6 will wire this
		SupportsManagerChain: true,
		SupportsCustomFields: true,
		MaxPageSize:          0, // directory endpoint returns all in one call
		PollInterval:         0, // use global default
	}
}
