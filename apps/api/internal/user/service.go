// Package user — user management service.
package user

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// User is the user aggregate.
type User struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	KeycloakSub string    `json:"keycloak_sub"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Service manages users.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService creates the user service.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{pool: pool, recorder: rec, log: log}
}

// List returns all users for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]*User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tenant_id::text, keycloak_sub, username, email, role, is_active, created_at, updated_at
		 FROM users WHERE tenant_id = $1::uuid ORDER BY username`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.KeycloakSub, &u.Username, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// Get returns a single user.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, tenant_id::text, keycloak_sub, username, email, role, is_active, created_at, updated_at
		 FROM users WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, tenantID,
	).Scan(&u.ID, &u.TenantID, &u.KeycloakSub, &u.Username, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user: get: %w", err)
	}
	return &u, nil
}

// Create creates a user.
func (s *Service) Create(ctx context.Context, p *auth.Principal, tenantID, keycloakSub, username, email, role string) (*User, error) {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionUserCreated,
		Target:   "email:" + email,
		Details:  map[string]any{"username": username, "role": role},
	})
	if err != nil {
		return nil, err
	}
	var u User
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, keycloak_sub, username, email, role)
		 VALUES ($1::uuid, $2, $3, $4, $5)
		 RETURNING id::text, tenant_id::text, keycloak_sub, username, email, role, is_active, created_at, updated_at`,
		tenantID, keycloakSub, username, email, role,
	).Scan(&u.ID, &u.TenantID, &u.KeycloakSub, &u.Username, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user: create: %w", err)
	}
	return &u, nil
}

// Update updates a user's role.
func (s *Service) ChangeRole(ctx context.Context, p *auth.Principal, tenantID, id, newRole string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionUserRoleChanged,
		Target:   "user:" + id,
		Details:  map[string]any{"new_role": newRole},
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE users SET role = $1, updated_at = now() WHERE id = $2::uuid AND tenant_id = $3::uuid`,
		newRole, id, tenantID,
	)
	return err
}

// Disable disables a user account.
func (s *Service) Disable(ctx context.Context, p *auth.Principal, tenantID, id string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionUserDisabled,
		Target:   "user:" + id,
		Details:  nil,
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE users SET is_active = false, updated_at = now() WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return err
}

// Delete deletes a user.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, tenantID, id string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionUserDeleted,
		Target:   "user:" + id,
		Details:  nil,
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM users WHERE id = $1::uuid AND tenant_id = $2::uuid`, id, tenantID)
	return err
}

// Update is a stub for future field updates.
func (s *Service) Update(ctx context.Context, p *auth.Principal, tenantID, id, username, email string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET username = $1, email = $2, updated_at = now() WHERE id = $3::uuid AND tenant_id = $4::uuid`,
		username, email, id, tenantID,
	)
	return err
}
