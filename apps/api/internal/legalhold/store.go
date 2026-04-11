// Package legalhold — Postgres store for legal holds (DPO-only).
package legalhold

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// Hold represents a legal hold record.
type Hold struct {
	ID            string
	TenantID      string
	DPOUserID     string
	ReasonCode    string
	TicketID      string
	Justification string
	// Scope fields
	EndpointID    *string
	UserSID       *string
	DateRangeFrom *time.Time
	DateRangeTo   *time.Time
	EventTypes    []string
	// Lifecycle
	PlacedAt      time.Time
	ExpiresAt     time.Time // max 2 years
	ReleasedAt    *time.Time
	ReleaseReason *string
	IsActive      bool
	// Metrics (approximate)
	AffectedRowCount *int64
}

// Store handles legal hold persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Place creates a new legal hold.
func (s *Store) Place(ctx context.Context, h *Hold) (string, error) {
	id := ulid.Make().String()
	eventTypesJSON, _ := json.Marshal(h.EventTypes)

	_, err := s.pool.Exec(ctx,
		`INSERT INTO legal_holds
		 (id, tenant_id, dpo_user_id, reason_code, ticket_id, justification,
		  endpoint_id, user_sid, date_range_from, date_range_to, event_types,
		  placed_at, expires_at, is_active)
		 VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6,
		         $7::uuid, $8, $9, $10, $11::jsonb,
		         $12, $13, true)`,
		id, h.TenantID, h.DPOUserID, h.ReasonCode, h.TicketID, h.Justification,
		h.EndpointID, h.UserSID, h.DateRangeFrom, h.DateRangeTo, eventTypesJSON,
		h.PlacedAt, h.ExpiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("legalhold: place: %w", err)
	}
	return id, nil
}

// Get retrieves a hold by ID.
func (s *Store) Get(ctx context.Context, id, tenantID string) (*Hold, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id::text, dpo_user_id::text, reason_code, ticket_id,
		        justification, endpoint_id::text, user_sid,
		        date_range_from, date_range_to, event_types::text,
		        placed_at, expires_at, released_at, release_reason, is_active, affected_row_count
		 FROM legal_holds
		 WHERE id = $1 AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return scanHold(row)
}

// List returns active legal holds for a tenant.
func (s *Store) List(ctx context.Context, tenantID string, activeOnly bool) ([]*Hold, error) {
	var query string
	if activeOnly {
		query = `SELECT id, tenant_id::text, dpo_user_id::text, reason_code, ticket_id,
		                justification, endpoint_id::text, user_sid,
		                date_range_from, date_range_to, event_types::text,
		                placed_at, expires_at, released_at, release_reason, is_active, affected_row_count
		         FROM legal_holds
		         WHERE tenant_id = $1::uuid AND is_active = true
		         ORDER BY placed_at DESC`
	} else {
		query = `SELECT id, tenant_id::text, dpo_user_id::text, reason_code, ticket_id,
		                justification, endpoint_id::text, user_sid,
		                date_range_from, date_range_to, event_types::text,
		                placed_at, expires_at, released_at, release_reason, is_active, affected_row_count
		         FROM legal_holds
		         WHERE tenant_id = $1::uuid
		         ORDER BY placed_at DESC`
	}

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("legalhold: list: %w", err)
	}
	defer rows.Close()

	var out []*Hold
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Release marks a hold as released.
func (s *Store) Release(ctx context.Context, id, tenantID, reason string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE legal_holds
		 SET is_active = false, released_at = $1, release_reason = $2
		 WHERE id = $3 AND tenant_id = $4::uuid AND is_active = true`,
		now, reason, id, tenantID,
	)
	return wrapErr("legalhold: release", err)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanHold(row rowScanner) (*Hold, error) {
	var h Hold
	var eventTypesJSON string
	err := row.Scan(
		&h.ID, &h.TenantID, &h.DPOUserID, &h.ReasonCode, &h.TicketID,
		&h.Justification, &h.EndpointID, &h.UserSID,
		&h.DateRangeFrom, &h.DateRangeTo, &eventTypesJSON,
		&h.PlacedAt, &h.ExpiresAt, &h.ReleasedAt, &h.ReleaseReason, &h.IsActive, &h.AffectedRowCount,
	)
	if err != nil {
		return nil, fmt.Errorf("legalhold: scan: %w", err)
	}
	_ = json.Unmarshal([]byte(eventTypesJSON), &h.EventTypes)
	return &h, nil
}

func wrapErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
