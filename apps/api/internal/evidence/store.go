package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// Store persists evidence items in Postgres. The table is defined by
// migration 0025 (Phase 3.0 TBD — for now we assume a table named
// evidence_items with columns matching the Item struct).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Insert writes a single item inside a transaction. Caller must provide
// a pre-signed item (Signature + SignatureKeyVersion populated).
func (s *Store) Insert(ctx context.Context, item Item) (string, error) {
	if item.ID == "" {
		item.ID = ulid.Make().String()
	}
	if item.CollectionPeriod == "" {
		item.CollectionPeriod = item.RecordedAt.Format("2006-01")
	}

	payloadBytes := item.Payload
	if payloadBytes == nil {
		payloadBytes = json.RawMessage("{}")
	}

	// Phase 3.0 TODO: real insert. Table schema:
	//   evidence_items (id, tenant_id, control, kind, collection_period,
	//     recorded_at, actor, summary_tr, summary_en, payload jsonb,
	//     referenced_audit_ids bigint[], attachment_refs text[],
	//     signature_key_version text, signature bytea)
	//   PRIMARY KEY (id)
	//   INDEX (tenant_id, collection_period, control)
	//   INDEX (tenant_id, kind, recorded_at DESC)
	//   RLS: tenant_id = current_setting('personel.tenant_id')::uuid
	//
	// For Phase 2.11 scaffold we treat the insert as a no-op to avoid
	// coupling to a migration that doesn't exist yet. The item is
	// still signed and its ID returned, so callers can log evidence
	// IDs and Phase 3.0 can backfill once the table exists.
	_ = payloadBytes

	return item.ID, nil
}

// ListByPeriod returns all evidence items for a tenant in a specific
// collection period + optional control filter. Used by PackBuilder.
func (s *Store) ListByPeriod(ctx context.Context, tenantID, period string, controls []ControlID) ([]Item, error) {
	// Phase 2.11 scaffold: return empty slice. Phase 3.0 adds the real
	// query with RLS enforcement.
	return []Item{}, nil
}

// CountByControl returns the number of evidence items per control in
// a tenant+period. Used by /healthz coverage checks.
func (s *Store) CountByControl(ctx context.Context, tenantID, period string) (map[ControlID]int, error) {
	return map[ControlID]int{}, nil
}

// ensureTx opens a transaction for caller-provided nested work.
func (s *Store) ensureTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("evidence: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
