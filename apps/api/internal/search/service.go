// Package search — service layer that validates requests, enforces
// tenant-id injection via the principal, and delegates to the client.
//
// The service is the ONLY path through which a request reaches the
// OpenSearch REST surface. Handlers must never call the client directly;
// the role separation is:
//
//   - Handler:  parses http.Request, extracts principal, writes response.
//   - Service:  validates bounds, short-circuits on nil client, logs.
//   - Client:   speaks OpenSearch REST; hardcoded tenant_id injection.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/personel/api/internal/audit"
)

// Service is the tenant-scoped search orchestrator. It holds a queryClient
// interface so unit tests can inject a fake without reaching OpenSearch.
// The real Client is wired from cmd/api/main.go.
type Service struct {
	client queryClient
	log    *slog.Logger
	// actionAllow is the set of audit.Action values accepted by the
	// AuditQuery.Action filter. Built once at construction time from
	// audit.AllActions so adding a new action to actions.go also
	// propagates to the search filter without a second edit.
	actionAllow map[string]struct{}
}

// ServiceConfig holds limits that the handler layer needs to enforce
// before calling the OpenSearch backend. All fields have safe defaults
// applied in NewService if left zero.
type ServiceConfig struct {
	MaxQueryLen     int           // default 200
	MaxRangeDays    int           // default 90 (KVKK retention window)
	MaxPageSize     int           // default 100
	MinPageSize     int           // default 10
	DefaultPageSize int           // default 25
	MaxPage         int           // default 1000
	DefaultLookback time.Duration // default 7d
}

func (c *ServiceConfig) applyDefaults() {
	if c.MaxQueryLen == 0 {
		c.MaxQueryLen = 200
	}
	if c.MaxRangeDays == 0 {
		c.MaxRangeDays = 90
	}
	if c.MaxPageSize == 0 {
		c.MaxPageSize = 100
	}
	if c.MinPageSize == 0 {
		c.MinPageSize = 10
	}
	if c.DefaultPageSize == 0 {
		c.DefaultPageSize = 25
	}
	if c.MaxPage == 0 {
		c.MaxPage = 1000
	}
	if c.DefaultLookback == 0 {
		c.DefaultLookback = 7 * 24 * time.Hour
	}
}

// defaultServiceConfig is package-visible so handler tests can reference
// the bounds without duplicating literals.
var defaultServiceConfig = ServiceConfig{
	MaxQueryLen:     200,
	MaxRangeDays:    90,
	MaxPageSize:     100,
	MinPageSize:     10,
	DefaultPageSize: 25,
	MaxPage:         1000,
	DefaultLookback: 7 * 24 * time.Hour,
}

// NewService constructs the search service. A nil client is legal and
// indicates degraded mode — every subsequent call will return
// ErrSearchUnavailable, which the handlers translate to 503.
func NewService(client *Client, log *slog.Logger) *Service {
	// We store the interface, not the concrete client, so tests can
	// drop in a fake. The typed *Client parameter makes the wiring in
	// main.go unambiguous (no chance of passing a random queryClient).
	var qc queryClient
	if client != nil {
		qc = client
	}
	allow := make(map[string]struct{}, len(audit.AllActions))
	for _, a := range audit.AllActions {
		allow[string(a)] = struct{}{}
	}
	return &Service{
		client:      qc,
		log:         log,
		actionAllow: allow,
	}
}

// newServiceWithClient is used by unit tests to inject a fake queryClient
// directly. Production code must use NewService.
func newServiceWithClient(qc queryClient, log *slog.Logger) *Service {
	allow := make(map[string]struct{}, len(audit.AllActions))
	for _, a := range audit.AllActions {
		allow[string(a)] = struct{}{}
	}
	return &Service{client: qc, log: log, actionAllow: allow}
}

// SearchAudit validates the query, refuses missing principal / missing
// tenant, and delegates to the client. The tenantID is ALWAYS taken
// from the verified principal; callers must not synthesise one.
func (s *Service) SearchAudit(ctx context.Context, tenantID string, q AuditQuery) (*AuditResult, error) {
	if s == nil || s.client == nil {
		return nil, ErrSearchUnavailable
	}
	if tenantID == "" {
		return nil, fmt.Errorf("%w: missing tenant id", ErrValidation)
	}
	if err := s.validateCommon(q.Q, q.From, q.To, q.Page, q.PageSize); err != nil {
		return nil, err
	}
	if q.Action != "" {
		if _, ok := s.actionAllow[q.Action]; !ok {
			return nil, fmt.Errorf("%w: unknown action %q", ErrValidation, q.Action)
		}
	}
	normaliseRange(&q.From, &q.To)
	normalisePage(&q.Page, &q.PageSize)
	return s.client.SearchAudit(ctx, tenantID, q)
}

// SearchEvents validates and delegates. The event_kind allowlist is
// deliberately NOT enforced here — the proto EventKind enum is large,
// still evolving, and the query builder only uses the value in a
// `term` clause so there's no injection surface. An unknown value
// simply matches zero documents.
func (s *Service) SearchEvents(ctx context.Context, tenantID string, q EventQuery) (*EventResult, error) {
	if s == nil || s.client == nil {
		return nil, ErrSearchUnavailable
	}
	if tenantID == "" {
		return nil, fmt.Errorf("%w: missing tenant id", ErrValidation)
	}
	if err := s.validateCommon(q.Q, q.From, q.To, q.Page, q.PageSize); err != nil {
		return nil, err
	}
	// Reject any attempt to search the keystroke event stream directly.
	// ADR 0013: admins must not be able to query keystroke content even
	// by metadata.
	if strings.Contains(strings.ToLower(q.EventKind), "keystroke") {
		return nil, fmt.Errorf("%w: keystroke events are not searchable via this API", ErrValidation)
	}
	normaliseRange(&q.From, &q.To)
	normalisePage(&q.Page, &q.PageSize)
	return s.client.SearchEvents(ctx, tenantID, q)
}

// validateCommon enforces the bounds shared by both endpoints. Returns
// an ErrValidation-wrapped error the handler translates to 400.
func (s *Service) validateCommon(q string, from, to time.Time, page, pageSize int) error {
	if len(q) > defaultServiceConfig.MaxQueryLen {
		return fmt.Errorf("%w: q exceeds %d chars", ErrValidation, defaultServiceConfig.MaxQueryLen)
	}
	// Zero times are allowed — normaliseRange fills in defaults.
	if !from.IsZero() && !to.IsZero() {
		if !from.Before(to) && !from.Equal(to) {
			return fmt.Errorf("%w: from must be <= to", ErrValidation)
		}
		delta := to.Sub(from)
		maxDelta := time.Duration(defaultServiceConfig.MaxRangeDays) * 24 * time.Hour
		if delta > maxDelta {
			return fmt.Errorf("%w: date range exceeds %d days", ErrValidation, defaultServiceConfig.MaxRangeDays)
		}
	}
	if page < 0 || page > defaultServiceConfig.MaxPage {
		return fmt.Errorf("%w: page out of range", ErrValidation)
	}
	if pageSize != 0 {
		if pageSize < defaultServiceConfig.MinPageSize || pageSize > defaultServiceConfig.MaxPageSize {
			return fmt.Errorf("%w: page_size must be %d..%d",
				ErrValidation, defaultServiceConfig.MinPageSize, defaultServiceConfig.MaxPageSize)
		}
	}
	return nil
}

// normaliseRange fills in the default 7-day lookback if from/to were zero.
func normaliseRange(from, to *time.Time) {
	now := time.Now().UTC()
	if to.IsZero() {
		*to = now
	}
	if from.IsZero() {
		*from = to.Add(-defaultServiceConfig.DefaultLookback)
	}
}

// normalisePage fills in the default page + page_size if the caller
// left them at zero, and re-asserts the bounds defensively.
func normalisePage(page, pageSize *int) {
	if *page < 1 {
		*page = 1
	}
	if *pageSize < 1 {
		*pageSize = defaultServiceConfig.DefaultPageSize
	}
	if *pageSize > defaultServiceConfig.MaxPageSize {
		*pageSize = defaultServiceConfig.MaxPageSize
	}
}
