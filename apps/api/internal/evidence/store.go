package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// EvidenceWORM is the narrow contract the Store needs from the WORM sink.
// The concrete implementation is *audit.WORMSink; this interface exists so
// the evidence package does not import the audit package (which would be
// a layering cycle once audit reaches back for evidence verification) and
// so tests can supply an in-memory fake.
type EvidenceWORM interface {
	// PutEvidence writes canonical signed bytes to the WORM bucket and
	// returns the resulting object key. Compliance mode retention is
	// enforced by the implementation.
	PutEvidence(ctx context.Context, tenantID, collectionPeriod, id string, canonical []byte) (string, error)
}

// Store persists evidence items via dual-write: first the canonical signed
// bytes go to the WORM bucket (MinIO Object Lock Compliance mode), only then
// does the metadata row land in Postgres. See migration 0025 for the schema
// and docs/adr/0023-soc2-type2-controls.md for the integrity model.
//
// The dual-write order is load-bearing: WORM first, Postgres second. If WORM
// succeeds and Postgres fails, the canonical bytes are still safe and a
// reconciliation job can rebuild the metadata row by scanning the bucket. If
// Postgres succeeded first and WORM failed, a DBA could silently "succeed" an
// evidence write with no tamper-evident anchor — unacceptable for CC7.1.
type Store struct {
	pool *pgxpool.Pool
	worm EvidenceWORM
}

// NewStore wraps a pool and a WORM sink. worm may be nil in scaffold mode;
// in that case Insert returns an error so callers fail loudly rather than
// silently writing evidence without an integrity anchor. Production wiring
// must provide a real WORM sink.
func NewStore(pool *pgxpool.Pool, worm EvidenceWORM) *Store {
	return &Store{pool: pool, worm: worm}
}

// Insert writes a single evidence item. Caller must provide a pre-signed
// item (Signature + SignatureKeyVersion populated by the Recorder).
//
// Dual-write sequence:
//  1. Assign ID and CollectionPeriod if missing.
//  2. Re-canonicalise the item (same function the Recorder used to sign).
//  3. PUT canonical bytes to WORM bucket with Compliance mode retention.
//  4. INSERT metadata row in Postgres referencing the WORM object key.
//
// If step 3 fails, the function returns the error and no Postgres row is
// created. If step 4 fails after step 3 succeeded, the WORM object remains
// (orphaned until a reconciliation job rebuilds the metadata row or the
// 5-year retention expires).
func (s *Store) Insert(ctx context.Context, item Item) (string, error) {
	if s.worm == nil {
		return "", errors.New("evidence: Store has no WORM sink configured — refusing to write evidence without an integrity anchor")
	}

	if item.ID == "" {
		item.ID = ulid.Make().String()
	}
	if item.RecordedAt.IsZero() {
		item.RecordedAt = time.Now().UTC()
	}
	if item.CollectionPeriod == "" {
		item.CollectionPeriod = item.RecordedAt.Format("2006-01")
	}
	if len(item.Signature) == 0 || item.SignatureKeyVersion == "" {
		return "", errors.New("evidence: Insert requires a pre-signed item (call Recorder.Record, not Store.Insert directly)")
	}

	// Re-canonicalise for the WORM payload. This is the same function the
	// Recorder called before signing, so the bytes here are byte-for-byte
	// identical to what the signature covers. Anyone retrieving the object
	// from WORM can independently verify the signature without trusting
	// the Postgres metadata row.
	canonical := canonicalize(item)

	wormKey, err := s.worm.PutEvidence(ctx, item.TenantID, item.CollectionPeriod, item.ID, canonical)
	if err != nil {
		return "", fmt.Errorf("evidence: worm put: %w", err)
	}
	wormWrittenAt := time.Now().UTC()

	payloadBytes := item.Payload
	if len(payloadBytes) == 0 {
		payloadBytes = json.RawMessage("{}")
	}

	// referenced_audit_ids is a bigint[]; pgx maps []int64 directly. The
	// column in migration 0025 is NOT NULL DEFAULT '{}', so we must pass
	// an empty slice rather than nil to avoid the NULL path (which would
	// technically satisfy the default but is less explicit).
	auditIDs := item.ReferencedAuditIDs
	if auditIDs == nil {
		auditIDs = []int64{}
	}
	attRefs := item.AttachmentRefs
	if attRefs == nil {
		attRefs = []string{}
	}

	const q = `
		INSERT INTO evidence_items (
			id, tenant_id, control, kind, collection_period,
			recorded_at, actor, summary_tr, summary_en, payload,
			referenced_audit_ids, attachment_refs,
			signature_key_version, signature,
			worm_key, worm_written_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12,
			$13, $14,
			$15, $16
		)
	`

	_, err = s.pool.Exec(ctx, q,
		item.ID, item.TenantID, string(item.Control), string(item.Kind), item.CollectionPeriod,
		item.RecordedAt, item.Actor, item.SummaryTR, item.SummaryEN, payloadBytes,
		auditIDs, attRefs,
		item.SignatureKeyVersion, item.Signature,
		wormKey, wormWrittenAt,
	)
	if err != nil {
		// The WORM object is already written and locked. The caller sees
		// the Postgres error and should treat it as a partial write: the
		// evidence exists in the WORM bucket but is not yet indexed. A
		// reconciliation job is expected to rebuild the row from the
		// bucket listing.
		return "", fmt.Errorf("evidence: postgres insert (worm_key=%q already locked): %w", wormKey, err)
	}

	return item.ID, nil
}

// ListByPeriod returns all evidence items for a tenant in a specific
// collection period + optional control filter. Used by PackBuilder.
//
// Phase 3.0 real implementation: executes the RLS-enforced query. Caller
// must have set personel.tenant_id in the session (done by the middleware).
func (s *Store) ListByPeriod(ctx context.Context, tenantID, period string, controls []ControlID) ([]Item, error) {
	var (
		rows pgx.Rows
		err  error
	)

	if len(controls) == 0 {
		const q = `
			SELECT id, tenant_id, control, kind, collection_period,
			       recorded_at, actor, summary_tr, summary_en, payload,
			       referenced_audit_ids, attachment_refs,
			       signature_key_version, signature
			FROM evidence_items
			WHERE tenant_id = $1 AND collection_period = $2
			ORDER BY recorded_at ASC
		`
		rows, err = s.pool.Query(ctx, q, tenantID, period)
	} else {
		controlStrs := make([]string, len(controls))
		for i, c := range controls {
			controlStrs[i] = string(c)
		}
		const q = `
			SELECT id, tenant_id, control, kind, collection_period,
			       recorded_at, actor, summary_tr, summary_en, payload,
			       referenced_audit_ids, attachment_refs,
			       signature_key_version, signature
			FROM evidence_items
			WHERE tenant_id = $1 AND collection_period = $2 AND control = ANY($3)
			ORDER BY recorded_at ASC
		`
		rows, err = s.pool.Query(ctx, q, tenantID, period, controlStrs)
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: list by period: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var controlStr, kindStr string
		if err := rows.Scan(
			&it.ID, &it.TenantID, &controlStr, &kindStr, &it.CollectionPeriod,
			&it.RecordedAt, &it.Actor, &it.SummaryTR, &it.SummaryEN, &it.Payload,
			&it.ReferencedAuditIDs, &it.AttachmentRefs,
			&it.SignatureKeyVersion, &it.Signature,
		); err != nil {
			return nil, fmt.Errorf("evidence: scan: %w", err)
		}
		it.Control = ControlID(controlStr)
		it.Kind = ItemKind(kindStr)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence: rows: %w", err)
	}
	return items, nil
}

// CountByControl returns the number of evidence items per control in a
// tenant+period. Used by /healthz coverage checks and the DPO dashboard
// ("which controls have zero evidence this month?").
func (s *Store) CountByControl(ctx context.Context, tenantID, period string) (map[ControlID]int, error) {
	const q = `
		SELECT control, COUNT(*)::int
		FROM evidence_items
		WHERE tenant_id = $1 AND collection_period = $2
		GROUP BY control
	`
	rows, err := s.pool.Query(ctx, q, tenantID, period)
	if err != nil {
		return nil, fmt.Errorf("evidence: count by control: %w", err)
	}
	defer rows.Close()

	out := make(map[ControlID]int)
	for rows.Next() {
		var ctrl string
		var n int
		if err := rows.Scan(&ctrl, &n); err != nil {
			return nil, fmt.Errorf("evidence: count scan: %w", err)
		}
		out[ControlID(ctrl)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence: count rows: %w", err)
	}
	return out, nil
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
