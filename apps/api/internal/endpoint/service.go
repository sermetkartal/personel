// Package endpoint — endpoint fleet service and enrollment.
package endpoint

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/vault"
)

// Endpoint represents an enrolled agent endpoint.
type Endpoint struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	Hostname            string     `json:"hostname"`
	OSVersion           string     `json:"os_version"`
	AgentVersion        string     `json:"agent_version"`
	AssignedUserID      *string    `json:"assigned_user_id"`
	CertSerial          *string    `json:"cert_serial"`
	EnrolledAt          time.Time  `json:"enrolled_at"`
	LastSeenAt          *time.Time `json:"last_seen_at"`
	IsActive            bool       `json:"is_active"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
	RevokeReason        *string    `json:"revoke_reason,omitempty"`
}

// EnrollmentToken is issued to an operator to run the agent installer.
type EnrollmentToken struct {
	RoleID   string    `json:"role_id"`
	SecretID string    `json:"secret_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Service manages endpoints.
type Service struct {
	pool         *pgxpool.Pool
	vaultClient  *vault.Client
	recorder     *audit.Recorder
	log          *slog.Logger
}

// NewService creates the endpoint service.
func NewService(pool *pgxpool.Pool, vc *vault.Client, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{pool: pool, vaultClient: vc, recorder: rec, log: log}
}

// List returns all endpoints for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]*Endpoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tenant_id::text, hostname, COALESCE(os_version,''), COALESCE(agent_version,''),
		        assigned_user_id::text, cert_serial, enrolled_at, last_seen_at, is_active, revoked_at, revoke_reason
		 FROM endpoints WHERE tenant_id = $1::uuid ORDER BY hostname`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("endpoint: list: %w", err)
	}
	defer rows.Close()

	var out []*Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Hostname, &e.OSVersion, &e.AgentVersion,
			&e.AssignedUserID, &e.CertSerial, &e.EnrolledAt, &e.LastSeenAt, &e.IsActive,
			&e.RevokedAt, &e.RevokeReason); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// Get returns a single endpoint.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Endpoint, error) {
	var e Endpoint
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, tenant_id::text, hostname, COALESCE(os_version,''), COALESCE(agent_version,''),
		        assigned_user_id::text, cert_serial, enrolled_at, last_seen_at, is_active, revoked_at, revoke_reason
		 FROM endpoints WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, tenantID,
	).Scan(&e.ID, &e.TenantID, &e.Hostname, &e.OSVersion, &e.AgentVersion,
		&e.AssignedUserID, &e.CertSerial, &e.EnrolledAt, &e.LastSeenAt, &e.IsActive,
		&e.RevokedAt, &e.RevokeReason)
	if err != nil {
		return nil, fmt.Errorf("endpoint: get: %w", err)
	}
	return &e, nil
}

// Enroll issues a single-use enrollment token from Vault.
func (s *Service) Enroll(ctx context.Context, p *auth.Principal) (*EnrollmentToken, error) {
	roleID, err := s.vaultClient.GetEnrollmentRoleID(ctx)
	if err != nil {
		return nil, err
	}
	secretID, err := s.vaultClient.IssueEnrollmentSecretID(ctx)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().UTC().Add(15 * time.Minute)

	// Store the token record.
	_, err = s.pool.Exec(ctx,
		`INSERT INTO enrollment_tokens (tenant_id, vault_secret_id, vault_role_id, created_by, expires_at)
		 VALUES ($1::uuid, $2, $3, $4::uuid, $5)`,
		p.TenantID, secretID, roleID, p.UserID, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("endpoint: store enrollment token: %w", err)
	}

	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionEndpointEnrolled,
		Target:   "enrollment-token:issued",
		Details:  map[string]any{"expires_at": expiresAt},
	})
	if err != nil {
		return nil, err
	}

	return &EnrollmentToken{
		RoleID:    roleID,
		SecretID:  secretID,
		ExpiresAt: expiresAt,
	}, nil
}

// Revoke revokes an endpoint's certificate.
func (s *Service) Revoke(ctx context.Context, p *auth.Principal, tenantID, id string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionEndpointRevoked,
		Target:   "endpoint:" + id,
		Details:  nil,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx,
		`UPDATE endpoints SET is_active = false, revoked_at = $1, revoke_reason = 'admin_revoke'
		 WHERE id = $2::uuid AND tenant_id = $3::uuid`,
		now, id, tenantID,
	)
	return err
}

// Delete removes an endpoint record.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, tenantID, id string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionEndpointDeleted,
		Target:   "endpoint:" + id,
		Details:  nil,
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM endpoints WHERE id = $1::uuid AND tenant_id = $2::uuid`, id, tenantID)
	return err
}
