// Package endpoint — pgx-backed store + service for endpoint_commands.
//
// The CommandService is kept distinct from the legacy Service so that
// its dependencies (store interface, publisher, audit recorder) can be
// mocked in unit tests without having to spin up a full Postgres/Vault
// harness. main.go wires both into the same router.
package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CommandService is the façade the handlers call. It intentionally
// accepts interfaces (CommandStore, CommandPublisher, commandAuditor)
// so tests can substitute in-memory fakes. The concrete wiring in
// cmd/api/main.go passes the real pgx store, the existing nats.Publisher,
// and the real *audit.Recorder adapted via an auditAdapter.
type CommandService struct {
	store     CommandStore
	publisher CommandPublisher
	recorder  commandAuditor
	log       *slog.Logger
}

// NewCommandService constructs the service. recorder may be nil in
// test contexts — issueCommand and BulkOperation gate on it.
func NewCommandService(store CommandStore, pub CommandPublisher, rec commandAuditor, log *slog.Logger) *CommandService {
	return &CommandService{
		store:     store,
		publisher: pub,
		recorder:  rec,
		log:       log,
	}
}

// pgxCommandStore is the production CommandStore backed by a
// pgxpool.Pool. Every query is tenant-scoped to enforce multi-tenant
// isolation at the SQL layer regardless of RLS.
type pgxCommandStore struct {
	pool *pgxpool.Pool
}

// NewPgxCommandStore returns a CommandStore wired to the given pool.
func NewPgxCommandStore(pool *pgxpool.Pool) CommandStore {
	return &pgxCommandStore{pool: pool}
}

// Create inserts a new command row. The caller passes a *Command with
// TenantID, EndpointID, IssuedBy, Kind, Reason, and IssuedAt populated;
// this method assigns the server-generated id + default state.
func (s *pgxCommandStore) Create(ctx context.Context, c *Command) error {
	payload := c.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO endpoint_commands
		   (tenant_id, endpoint_id, issued_by, kind, reason, state, issued_at, payload)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'pending', $6, $7::jsonb)
		 RETURNING id::text, state`,
		c.TenantID, c.EndpointID, c.IssuedBy, string(c.Kind), c.Reason, c.IssuedAt, payload,
	).Scan(&c.ID, &c.State)
	if err != nil {
		return fmt.Errorf("endpoint_commands: create: %w", err)
	}
	return nil
}

// UpdateState transitions a row to a new state. errorMsg is optional —
// pass "" to leave error_message NULL.
func (s *pgxCommandStore) UpdateState(ctx context.Context, id, state, errorMsg string) error {
	var errPtr *string
	if errorMsg != "" {
		errPtr = &errorMsg
	}
	// Stamp acknowledged_at / completed_at based on the target state so
	// the console can render a proper timeline without a second round
	// trip. The CASE-WHEN is safe here: the CHECK constraint limits
	// state to the allowed enum.
	_, err := s.pool.Exec(ctx,
		`UPDATE endpoint_commands
		    SET state = $2,
		        error_message = $3,
		        acknowledged_at = CASE
		            WHEN $2 = 'acknowledged' AND acknowledged_at IS NULL THEN now()
		            ELSE acknowledged_at
		        END,
		        completed_at = CASE
		            WHEN $2 IN ('completed','failed','timeout') AND completed_at IS NULL THEN now()
		            ELSE completed_at
		        END
		  WHERE id = $1::uuid`,
		id, state, errPtr,
	)
	if err != nil {
		return fmt.Errorf("endpoint_commands: update state: %w", err)
	}
	return nil
}

// GetByID fetches a command by id, scoped to tenant. Returns (nil, nil)
// when the row does not exist (callers must check for nil).
func (s *pgxCommandStore) GetByID(ctx context.Context, tenantID, id string) (*Command, error) {
	var c Command
	var kind, state string
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, tenant_id::text, endpoint_id::text, issued_by::text,
		        kind, reason, state, issued_at, acknowledged_at, completed_at,
		        error_message, payload
		   FROM endpoint_commands
		  WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, tenantID,
	).Scan(&c.ID, &c.TenantID, &c.EndpointID, &c.IssuedBy,
		&kind, &c.Reason, &state, &c.IssuedAt, &c.AcknowledgedAt, &c.CompletedAt,
		&c.ErrorMessage, &c.Payload,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("endpoint_commands: get: %w", err)
	}
	c.Kind = CommandKind(kind)
	c.State = CommandState(state)
	return &c, nil
}

// ListByEndpoint returns the most recent commands for a given endpoint.
func (s *pgxCommandStore) ListByEndpoint(ctx context.Context, tenantID, endpointID string, limit int) ([]Command, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tenant_id::text, endpoint_id::text, issued_by::text,
		        kind, reason, state, issued_at, acknowledged_at, completed_at,
		        error_message, payload
		   FROM endpoint_commands
		  WHERE tenant_id = $1::uuid AND endpoint_id = $2::uuid
		  ORDER BY issued_at DESC
		  LIMIT $3`,
		tenantID, endpointID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("endpoint_commands: list by endpoint: %w", err)
	}
	defer rows.Close()

	var out []Command
	for rows.Next() {
		var c Command
		var kind, state string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.EndpointID, &c.IssuedBy,
			&kind, &c.Reason, &state, &c.IssuedAt, &c.AcknowledgedAt, &c.CompletedAt,
			&c.ErrorMessage, &c.Payload); err != nil {
			return nil, err
		}
		c.Kind = CommandKind(kind)
		c.State = CommandState(state)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListByTenant paginates across a tenant. Used by a (future) admin
// fleet-wide command history page; wired now so the shape is stable.
func (s *pgxCommandStore) ListByTenant(ctx context.Context, tenantID string, page, pageSize int) ([]Command, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM endpoint_commands WHERE tenant_id = $1::uuid`,
		tenantID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("endpoint_commands: count: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tenant_id::text, endpoint_id::text, issued_by::text,
		        kind, reason, state, issued_at, acknowledged_at, completed_at,
		        error_message, payload
		   FROM endpoint_commands
		  WHERE tenant_id = $1::uuid
		  ORDER BY issued_at DESC
		  LIMIT $2 OFFSET $3`,
		tenantID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("endpoint_commands: list by tenant: %w", err)
	}
	defer rows.Close()

	var out []Command
	for rows.Next() {
		var c Command
		var kind, state string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.EndpointID, &c.IssuedBy,
			&kind, &c.Reason, &state, &c.IssuedAt, &c.AcknowledgedAt, &c.CompletedAt,
			&c.ErrorMessage, &c.Payload); err != nil {
			return nil, 0, err
		}
		c.Kind = CommandKind(kind)
		c.State = CommandState(state)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// EndpointExists returns true iff (endpoint_id, tenant_id) is a row in
// endpoints. Used for tenant-isolated 404.
func (s *pgxCommandStore) EndpointExists(ctx context.Context, tenantID, endpointID string) (bool, error) {
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM endpoints WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		endpointID, tenantID,
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("endpoint_commands: endpoint exists: %w", err)
	}
	return true, nil
}

// IsUnderLegalHold returns true iff the endpoint has at least one active
// legal_holds row. legal_holds.endpoint_id is nullable (holds can be
// tenant-wide) but wipe must be rejected only when an endpoint-scoped
// hold is in place, so we filter on endpoint_id IS NOT NULL.
func (s *pgxCommandStore) IsUnderLegalHold(ctx context.Context, tenantID, endpointID string) (bool, error) {
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1
		   FROM legal_holds
		  WHERE tenant_id = $1::uuid
		    AND endpoint_id = $2::uuid
		    AND is_active = true
		  LIMIT 1`,
		tenantID, endpointID,
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("endpoint_commands: legal hold check: %w", err)
	}
	return true, nil
}
