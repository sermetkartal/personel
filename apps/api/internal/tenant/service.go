// Package tenant — tenant CRUD service.
package tenant

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/audit"
)

// Tenant is the tenant aggregate.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service manages tenants.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService creates the tenant service.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{pool: pool, recorder: rec, log: log}
}

// List returns all tenants.
func (s *Service) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, name, slug, is_active, created_at, updated_at FROM tenants ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("tenant: list: %w", err)
	}
	defer rows.Close()

	var out []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// Get returns a single tenant.
func (s *Service) Get(ctx context.Context, id string) (*Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, slug, is_active, created_at, updated_at FROM tenants WHERE id = $1::uuid`, id,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("tenant: get: %w", err)
	}
	return &t, nil
}

// Create inserts a new tenant.
func (s *Service) Create(ctx context.Context, actorID, name, slug string) (*Tenant, error) {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: "00000000-0000-0000-0000-000000000000",
		Action:   audit.ActionUserCreated,
		Target:   "tenant:" + slug,
		Details:  map[string]any{"name": name, "slug": slug},
	})
	if err != nil {
		return nil, err
	}
	var t Tenant
	err = s.pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug) VALUES ($1, $2) RETURNING id::text, name, slug, is_active, created_at, updated_at`,
		name, slug,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("tenant: create: %w", err)
	}
	return &t, nil
}

// Update updates a tenant.
func (s *Service) Update(ctx context.Context, id, actorID, name string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants SET name = $1, updated_at = now() WHERE id = $2::uuid`, name, id)
	return err
}
