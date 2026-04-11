// Package policy — Postgres store for policies.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// Policy is the stored policy aggregate.
type Policy struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	Rules       json.RawMessage
	Version     int64
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	IsDefault   bool
}

// Store handles policy persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts a new policy.
func (s *Store) Create(ctx context.Context, p *Policy) (string, error) {
	id := ulid.Make().String()
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO policies (id, tenant_id, name, description, rules, version, created_by, created_at, updated_at, is_default)
		 VALUES ($1, $2::uuid, $3, $4, $5::jsonb, 1, $6::uuid, $7, $7, $8)`,
		id, p.TenantID, p.Name, p.Description, p.Rules, p.CreatedBy, now, p.IsDefault,
	)
	if err != nil {
		return "", fmt.Errorf("policy: create: %w", err)
	}
	return id, nil
}

// Get retrieves a policy by ID.
func (s *Store) Get(ctx context.Context, id, tenantID string) (*Policy, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id::text, name, description, rules, version, created_by::text, created_at, updated_at, is_default
		 FROM policies WHERE id = $1 AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return scanPolicy(row)
}

// List returns all policies for a tenant.
func (s *Store) List(ctx context.Context, tenantID string) ([]*Policy, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id::text, name, description, rules, version, created_by::text, created_at, updated_at, is_default
		 FROM policies WHERE tenant_id = $1::uuid ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("policy: list: %w", err)
	}
	defer rows.Close()
	var out []*Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update updates the rules and increments version.
func (s *Store) Update(ctx context.Context, id, tenantID string, rules json.RawMessage, name, description string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE policies
		 SET rules = $1::jsonb, name = $2, description = $3, version = version + 1, updated_at = now()
		 WHERE id = $4 AND tenant_id = $5::uuid`,
		rules, name, description, id, tenantID,
	)
	return wrapErr("policy: update", err)
}

// Delete removes a policy.
func (s *Store) Delete(ctx context.Context, id, tenantID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM policies WHERE id = $1 AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return wrapErr("policy: delete", err)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPolicy(row rowScanner) (*Policy, error) {
	var p Policy
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.Rules,
		&p.Version, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.IsDefault)
	if err != nil {
		return nil, fmt.Errorf("policy: scan: %w", err)
	}
	return &p, nil
}

func wrapErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
