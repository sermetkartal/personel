// Package dlpstate provides read access to the DLP deployment state and the
// PE-DEK bootstrap operation for already-enrolled endpoints.
//
// ADR 0013 — DLP is DISABLED by default. State transitions are performed by
// infra/scripts/dlp-enable.sh and dlp-disable.sh, NOT by the Admin API. The
// API only reads the state and exposes the bootstrap endpoint that those
// scripts invoke.
package dlpstate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DLPStateValue is the machine-readable DLP deployment state.
type DLPStateValue string

const (
	StateDisabled  DLPStateValue = "disabled"
	StateEnabling  DLPStateValue = "enabling"
	StateEnabled   DLPStateValue = "enabled"
	StateDisabling DLPStateValue = "disabling"
	StateError     DLPStateValue = "error"
)

// StateRow is the single row from the dlp_state table.
type StateRow struct {
	State              DLPStateValue
	EnabledAt          *time.Time
	EnabledBy          *string
	CeremonyFormHash   *string
	LastAuditEventID   *string
	Message            string
}

// BootstrapRecord is a row from keystroke_keys.
type BootstrapRecord struct {
	EndpointID string
	TenantID   string
}

// Store handles dlp_state and keystroke_keys persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpdateState writes a new state row. All fields are overwritten; pass nil
// pointers for clear-to-null. Uses the single-row constraint on dlp_state.
func (s *Store) UpdateState(ctx context.Context, state DLPStateValue,
	enabledAt *time.Time, enabledBy, ceremonyFormHash, lastAuditEventID *string,
	message string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE dlp_state
		 SET state = $1,
		     enabled_at = $2,
		     enabled_by = $3,
		     ceremony_form_hash = $4,
		     last_audit_event_id = $5,
		     message = $6
		 WHERE id = TRUE`,
		state, enabledAt, enabledBy, ceremonyFormHash, lastAuditEventID, message,
	)
	if err != nil {
		return fmt.Errorf("dlpstate: update state: %w", err)
	}
	return nil
}

// GetState returns the current DLP state row.
func (s *Store) GetState(ctx context.Context) (*StateRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT state, enabled_at, enabled_by, ceremony_form_hash, last_audit_event_id, message
		 FROM dlp_state
		 WHERE id = TRUE`,
	)
	var st StateRow
	if err := row.Scan(
		&st.State,
		&st.EnabledAt,
		&st.EnabledBy,
		&st.CeremonyFormHash,
		&st.LastAuditEventID,
		&st.Message,
	); err != nil {
		return nil, fmt.Errorf("dlpstate: get state: %w", err)
	}
	return &st, nil
}

// ListEndpoints returns all enrolled endpoint IDs for a tenant.
func (s *Store) ListEndpoints(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text FROM endpoints WHERE tenant_id = $1::uuid AND is_active = TRUE`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("dlpstate: list endpoints: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("dlpstate: scan endpoint: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// KeyExists returns true if a wrapped PE-DEK already exists for endpointID.
func (s *Store) KeyExists(ctx context.Context, endpointID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM keystroke_keys WHERE endpoint_id = $1::uuid)`,
		endpointID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("dlpstate: key exists: %w", err)
	}
	return exists, nil
}

// InsertKey stores a newly bootstrapped wrapped PE-DEK for an endpoint.
// The tenantID is stored alongside for indexed lookup.
func (s *Store) InsertKey(ctx context.Context, endpointID, tenantID, keyVersion string, wrappedPEDEK []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO keystroke_keys (endpoint_id, tenant_id, wrapped_pe_dek, key_version)
		 VALUES ($1::uuid, $2::uuid, $3, $4)
		 ON CONFLICT (endpoint_id) DO NOTHING`,
		endpointID, tenantID, wrappedPEDEK, keyVersion,
	)
	if err != nil {
		return fmt.Errorf("dlpstate: insert key: %w", err)
	}
	return nil
}
