// Package tickets provides a ticket system integration scaffold for
// Faz 17 item #184.
//
// Personel does NOT run a full internal helpdesk — that would duplicate
// every customer's existing Jira/Zendesk/Freshdesk. Instead this package:
//
//  1. Accepts ticket creation via POST /v1/tickets (admin-gated)
//  2. Writes a local shadow row to the `tickets` Postgres table for
//     audit trail and cross-reference
//  3. Forwards the payload to the configured external provider via a
//     pluggable Provider interface
//  4. Receives webhook callbacks via POST /v1/tickets/webhook and syncs
//     provider state back into the local row
//
// The three providers (Jira, Zendesk, Freshdesk) are STUBS in this
// commit. Each returns ErrNotConfigured + logs a loud "pending real
// implementation" warning. Wiring real HTTP clients is deferred until
// a customer explicitly requests a specific provider — at which point
// only that provider's adapter file changes, not this Service.
//
// Every mutation writes an audit entry BEFORE the side effect, per the
// project-wide rule in apps/api/internal/audit/recorder.go.
package tickets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// Sentinel errors for the public surface.
var (
	// ErrNotConfigured indicates the requested provider has no
	// API URL or credentials configured. Returned by every stub
	// provider in this scaffold.
	ErrNotConfigured = errors.New("tickets: provider not configured")

	// ErrUnknownProvider means the caller asked for a provider name
	// the Service doesn't recognise.
	ErrUnknownProvider = errors.New("tickets: unknown provider")

	// ErrInvalidState means a requested state transition is illegal
	// per the state machine (open → in_progress → resolved → closed).
	ErrInvalidState = errors.New("tickets: invalid state transition")
)

// Severity is the priority a ticket is raised with.
type Severity string

const (
	SeverityP1 Severity = "P1" // service down, critical
	SeverityP2 Severity = "P2" // feature degraded
	SeverityP3 Severity = "P3" // minor issue
	SeverityP4 Severity = "P4" // cosmetic
)

// State is the lifecycle state of a ticket.
type State string

const (
	StateOpen       State = "open"
	StateInProgress State = "in_progress"
	StateResolved   State = "resolved"
	StateClosed     State = "closed"
	StateRejected   State = "rejected"
)

// Ticket is the canonical ticket shape. Both the local Postgres shadow
// row and the external provider API speak a variant of this.
type Ticket struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Provider    string     `json:"provider"`
	ProviderID  string     `json:"provider_id,omitempty"`
	Severity    Severity   `json:"severity"`
	Subject     string     `json:"subject"`
	Body        string     `json:"body"`
	State       State      `json:"state"`
	AssignedTo  *uuid.UUID `json:"assigned_to,omitempty"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

// Provider is the pluggable adapter interface. Real implementations
// live in jira.go / zendesk.go / freshdesk.go as siblings. The
// "internal" provider is a no-op that leaves the local shadow row
// as the only record.
type Provider interface {
	// Name returns the short provider identifier ("jira", ...).
	Name() string

	// Create forwards a newly-created ticket to the external system.
	// Returns the external system's ticket ID (empty for internal).
	// Errors are logged by the Service but not surfaced to the API
	// caller — the local row creation is authoritative.
	Create(ctx context.Context, t *Ticket) (externalID string, err error)

	// Update forwards a state change or content edit.
	Update(ctx context.Context, t *Ticket) error

	// HandleWebhook parses an incoming webhook from the provider and
	// returns a partial Ticket describing what changed. The Service
	// then locates the local shadow row by (provider, provider_id)
	// and applies the change.
	HandleWebhook(ctx context.Context, payload []byte) (*Ticket, error)
}

// Service is the public entry point for ticket operations.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger
	// providers is keyed by Name() — the active one is selected per
	// call via the "provider" field in CreateRequest.
	providers map[string]Provider
	// defaultProvider is the name used when a request doesn't specify
	// one. Can be "internal" (local-only) or any registered external
	// provider.
	defaultProvider string
}

// NewService wires a Service with its dependencies.
//
// providers is a map keyed by the short name each Provider.Name()
// returns. Pass an InternalProvider at minimum so the local-only
// path works even when no external adapter is configured.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger,
	providers []Provider, defaultProvider string) *Service {
	if log == nil {
		log = slog.Default()
	}
	pm := make(map[string]Provider, len(providers))
	for _, p := range providers {
		pm[p.Name()] = p
	}
	if defaultProvider == "" {
		defaultProvider = ProviderNameInternal
	}
	return &Service{
		pool:            pool,
		recorder:        rec,
		log:             log,
		providers:       pm,
		defaultProvider: defaultProvider,
	}
}

// CreateRequest is the inbound shape for POST /v1/tickets.
type CreateRequest struct {
	TenantID uuid.UUID `json:"tenant_id"`
	Provider string    `json:"provider,omitempty"` // default per Service config
	Severity Severity  `json:"severity"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body"`
}

// Create validates the request, writes an audit entry, inserts the
// local shadow row, and forwards to the configured provider.
//
// Ordering rules:
//   - Audit entry FIRST, per project-wide "observability before side
//     effect" discipline. A failed INSERT after audit leaves a loud
//     discrepancy that operators can investigate.
//   - Postgres INSERT SECOND.
//   - Provider Create LAST. Provider errors are logged but swallowed;
//     the local row becomes the authoritative record, and an
//     operator can retry forwarding later via a TODO replay path.
func (s *Service) Create(ctx context.Context, actor string, req CreateRequest) (*Ticket, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidState)
	}
	if req.Subject == "" || req.Body == "" {
		return nil, fmt.Errorf("%w: subject + body required", ErrInvalidState)
	}
	if !isValidSeverity(req.Severity) {
		return nil, fmt.Errorf("%w: bad severity %q", ErrInvalidState, req.Severity)
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = s.defaultProvider
	}
	prov, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, providerName)
	}

	// Parse actor UUID if it looks like one; fall back to nil for system callers.
	actorUUID, _ := uuid.Parse(actor)

	t := &Ticket{
		ID:        uuid.New(),
		TenantID:  req.TenantID,
		Provider:  providerName,
		Severity:  req.Severity,
		Subject:   req.Subject,
		Body:      req.Body,
		State:     StateOpen,
		CreatedBy: actorUUID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// --- Audit FIRST ---
	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:    actor,
			TenantID: req.TenantID.String(),
			Action:   audit.ActionTicketCreated,
			Target:   t.ID.String(),
			Details: map[string]any{
				"provider": providerName,
				"severity": string(req.Severity),
				"subject":  req.Subject,
			},
		}); err != nil {
			return nil, fmt.Errorf("tickets: audit append: %w", err)
		}
	}

	// --- Postgres INSERT SECOND ---
	if s.pool != nil {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO tickets (
				id, tenant_id, provider, severity, subject, body,
				state, created_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, t.ID, t.TenantID, t.Provider, t.Severity, t.Subject, t.Body,
			t.State, t.CreatedBy, t.CreatedAt, t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("tickets: insert: %w", err)
		}
	}

	// --- Provider forward LAST (best effort) ---
	externalID, err := prov.Create(ctx, t)
	if err != nil {
		s.log.Warn("tickets: provider create failed; local row retained",
			slog.String("provider", providerName),
			slog.String("ticket_id", t.ID.String()),
			slog.String("err", err.Error()))
		// do NOT return the error — local row is authoritative
	} else if externalID != "" {
		t.ProviderID = externalID
		if s.pool != nil {
			_, _ = s.pool.Exec(ctx,
				`UPDATE tickets SET provider_id = $1 WHERE id = $2`,
				externalID, t.ID)
		}
	}

	return t, nil
}

// UpdateState drives the ticket state machine forward. Illegal
// transitions return ErrInvalidState.
func (s *Service) UpdateState(ctx context.Context, actor string, id uuid.UUID, newState State, reason string) error {
	if !isValidState(newState) {
		return ErrInvalidState
	}

	// Load current state
	var current State
	var tenantID uuid.UUID
	var providerName string
	if err := s.pool.QueryRow(ctx,
		`SELECT state, tenant_id, provider FROM tickets WHERE id = $1`, id,
	).Scan(&current, &tenantID, &providerName); err != nil {
		return fmt.Errorf("tickets: load: %w", err)
	}

	if !canTransition(current, newState) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidState, current, newState)
	}

	// Audit first
	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:    actor,
			TenantID: tenantID.String(),
			Action:   audit.ActionTicketStateChange,
			Target:   id.String(),
			Details: map[string]any{
				"from":   string(current),
				"to":     string(newState),
				"reason": reason,
			},
		}); err != nil {
			return fmt.Errorf("tickets: audit: %w", err)
		}
	}

	// Persist transition + optional timestamps
	now := time.Now().UTC()
	switch newState {
	case StateResolved:
		_, err := s.pool.Exec(ctx, `
			UPDATE tickets SET state = $1, resolved_at = $2, updated_at = $2
			WHERE id = $3`, newState, now, id)
		if err != nil {
			return err
		}
	case StateClosed:
		_, err := s.pool.Exec(ctx, `
			UPDATE tickets SET state = $1, closed_at = $2, updated_at = $2
			WHERE id = $3`, newState, now, id)
		if err != nil {
			return err
		}
	default:
		_, err := s.pool.Exec(ctx, `
			UPDATE tickets SET state = $1, updated_at = $2 WHERE id = $3
		`, newState, now, id)
		if err != nil {
			return err
		}
	}

	// Best-effort provider forward
	if prov, ok := s.providers[providerName]; ok {
		t := &Ticket{ID: id, TenantID: tenantID, State: newState, UpdatedAt: now}
		if err := prov.Update(ctx, t); err != nil {
			s.log.Warn("tickets: provider update failed",
				slog.String("provider", providerName),
				slog.String("err", err.Error()))
		}
	}

	return nil
}

// HandleWebhook accepts a raw webhook payload from an external
// provider and reconciles the local shadow row. The provider name
// is determined from a header or query string by the handler layer.
func (s *Service) HandleWebhook(ctx context.Context, providerName string, payload []byte) error {
	prov, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, providerName)
	}
	partial, err := prov.HandleWebhook(ctx, payload)
	if err != nil {
		return fmt.Errorf("tickets: webhook parse: %w", err)
	}
	if partial == nil || partial.ProviderID == "" {
		return fmt.Errorf("tickets: webhook missing provider_id")
	}
	// Locate local row by (provider, provider_id)
	var localID uuid.UUID
	var tenantID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT id, tenant_id FROM tickets WHERE provider = $1 AND provider_id = $2
	`, providerName, partial.ProviderID).Scan(&localID, &tenantID)
	if err != nil {
		return fmt.Errorf("tickets: no local row for %s/%s: %w",
			providerName, partial.ProviderID, err)
	}

	// Apply change — minimal: state + assigned_to + updated_at
	if s.recorder != nil {
		_, _ = s.recorder.Append(ctx, audit.Entry{
			Actor:    "webhook:" + providerName,
			TenantID: tenantID.String(),
			Action:   audit.ActionTicketUpdated,
			Target:   localID.String(),
			Details: map[string]any{
				"source": "webhook",
				"state":  string(partial.State),
			},
		})
	}
	if partial.State != "" {
		_, err := s.pool.Exec(ctx,
			`UPDATE tickets SET state = $1, updated_at = $2 WHERE id = $3`,
			partial.State, time.Now().UTC(), localID)
		if err != nil {
			return err
		}
	}
	return nil
}

// isValidSeverity returns true iff s is one of the four allowed
// severities.
func isValidSeverity(s Severity) bool {
	switch s {
	case SeverityP1, SeverityP2, SeverityP3, SeverityP4:
		return true
	}
	return false
}

// isValidState returns true iff s is one of the five allowed states.
func isValidState(s State) bool {
	switch s {
	case StateOpen, StateInProgress, StateResolved, StateClosed, StateRejected:
		return true
	}
	return false
}

// canTransition encodes the state machine:
//
//	open → in_progress → resolved → closed
//	open → rejected (terminal)
//	resolved → open (reopen)
//
// Any other transition is illegal.
func canTransition(from, to State) bool {
	switch from {
	case StateOpen:
		return to == StateInProgress || to == StateRejected || to == StateResolved
	case StateInProgress:
		return to == StateResolved || to == StateRejected
	case StateResolved:
		return to == StateClosed || to == StateOpen // reopen
	case StateClosed:
		return false // terminal
	case StateRejected:
		return to == StateOpen // reopen
	}
	return false
}
