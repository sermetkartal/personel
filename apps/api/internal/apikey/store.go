// Package apikey — service-to-service API key issuance and
// verification (Faz 6 #72).
//
// Design highlights:
//
//   - Plaintext is ONLY ever returned from Generate and never stored.
//     Only SHA-256(plaintext) lives in the database.
//
//   - Verify runs in constant time against the hash. Lookup miss +
//     tampered hash are both rejected with the same ErrInvalidKey so
//     callers cannot distinguish the two from a timing or log-message
//     standpoint.
//
//   - Keys carry an ordered scopes list. Handlers that accept ApiKey
//     auth call RequireScope on the request context to enforce
//     least-privilege per-endpoint.
//
//   - The tenant_id column is nullable: a NULL tenant means a
//     cross-tenant / system-level caller (e.g. the in-cluster gateway)
//     and such keys may only be minted by RoleAdmin. All other keys
//     are strictly tenant-scoped and the principal produced during
//     verification carries that tenant_id.
package apikey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Record is one row in the service_api_keys table (no plaintext).
type Record struct {
	ID         string     `json:"id"`
	TenantID   *string    `json:"tenant_id,omitempty"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	CreatedBy  string     `json:"created_by"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Store is the pgx CRUD layer. It is deliberately kept thin so the
// service layer owns all business rules (hashing, constant-time
// compare, expiry calculation).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Insert persists a new key row. keyHash MUST be the hex-encoded
// SHA-256 of the plaintext — the caller's responsibility.
func (s *Store) Insert(ctx context.Context, tenantID *string, name, keyHash, createdBy string, scopes []string, expiresAt *time.Time) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO service_api_keys
		   (tenant_id, name, key_hash, scopes, created_by, expires_at)
		 VALUES ($1::uuid, $2, $3, $4::text[], $5::uuid, $6)
		 RETURNING id::text`,
		tenantID, name, keyHash, scopes, createdBy, expiresAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("apikey: insert: %w", err)
	}
	return id, nil
}

// GetByHash looks up a non-revoked row by hash. Returns
// (nil, pgx.ErrNoRows) when the key is unknown so callers can
// distinguish "never existed" from a real error.
func (s *Store) GetByHash(ctx context.Context, keyHash string) (*Record, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id::text, tenant_id::text, name, scopes, created_at,
		        created_by::text, expires_at, last_used_at, revoked_at
		 FROM service_api_keys
		 WHERE key_hash = $1 AND revoked_at IS NULL`,
		keyHash,
	)
	return scanRecord(row)
}

// List returns all keys for a tenant (or all if tenantID is nil).
// Revoked keys are excluded so the console sees the active set.
func (s *Store) List(ctx context.Context, tenantID *string) ([]*Record, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if tenantID != nil {
		rows, err = s.pool.Query(ctx,
			`SELECT id::text, tenant_id::text, name, scopes, created_at,
			        created_by::text, expires_at, last_used_at, revoked_at
			 FROM service_api_keys
			 WHERE tenant_id = $1::uuid AND revoked_at IS NULL
			 ORDER BY created_at DESC`,
			*tenantID,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id::text, tenant_id::text, name, scopes, created_at,
			        created_by::text, expires_at, last_used_at, revoked_at
			 FROM service_api_keys
			 WHERE revoked_at IS NULL
			 ORDER BY created_at DESC`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("apikey: list: %w", err)
	}
	defer rows.Close()
	var out []*Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Revoke sets revoked_at on a key. Revocation is immediate and
// permanent — there is no un-revoke (create a new key instead).
func (s *Store) Revoke(ctx context.Context, id, tenantID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE service_api_keys
		 SET revoked_at = now()
		 WHERE id = $1::uuid
		   AND (tenant_id = $2::uuid OR $2 = '')
		   AND revoked_at IS NULL`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("apikey: revoke: %w", err)
	}
	return nil
}

// TouchLastUsed updates last_used_at to now(). Best-effort — failures
// are logged but never propagated (a DB hiccup shouldn't block the
// request the key was valid for).
func (s *Store) TouchLastUsed(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE service_api_keys SET last_used_at = now() WHERE id = $1::uuid`,
		id,
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row rowScanner) (*Record, error) {
	var r Record
	var tenantID *string
	err := row.Scan(
		&r.ID, &tenantID, &r.Name, &r.Scopes, &r.CreatedAt,
		&r.CreatedBy, &r.ExpiresAt, &r.LastUsedAt, &r.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("apikey: scan: %w", err)
	}
	r.TenantID = tenantID
	return &r, nil
}
