// Package endpoint — endpoint fleet service and enrollment.
package endpoint

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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

// EnrollmentToken is the response returned to the admin operator when they
// request a new endpoint enrollment. The Token field is an opaque
// base64-url-no-padding blob that bundles role_id, secret_id and the
// authoritative enroll URL — the operator hands it verbatim to the agent
// installer, which decodes it client-side. Keeping the three values inside
// one opaque blob means the install command line stays a single argument
// and the operator cannot accidentally swap a wrong gateway.
type EnrollmentToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// enrollmentTokenPayload is the JSON shape that gets base64-url encoded
// into EnrollmentToken.Token.
type enrollmentTokenPayload struct {
	RoleID    string `json:"role_id"`
	SecretID  string `json:"secret_id"`
	EnrollURL string `json:"enroll_url"`
}

// encodeEnrollmentToken serialises the payload as base64-url-no-padding.
func encodeEnrollmentToken(p enrollmentTokenPayload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Service manages endpoints.
type Service struct {
	pool        *pgxpool.Pool
	vaultClient *vault.Client
	recorder    *audit.Recorder
	log         *slog.Logger
	publicURL   string
	gatewayURL  string

	// refreshStoreOverride is ONLY set by SetRefreshStoreForTesting and
	// stands in for the production *pgxpool.Pool-backed pgxRefreshStore
	// so the refresh path can be unit-tested without a live Postgres.
	// nil in production.
	refreshStoreOverride refreshStore

	// pkiOverride is ONLY set by SetPKIForTesting and stands in for the
	// concrete *vault.Client so the refresh path can be unit-tested
	// without a live Vault server. nil in production.
	pkiOverride pkiSigner

	// auditOverride is ONLY set by SetAuditForTesting. nil in production;
	// the concrete *audit.Recorder on s.recorder is used instead.
	auditOverride auditAppender
}

// auditAppend routes through the test override if present, otherwise
// delegates to the production recorder.
func (s *Service) auditAppend(ctx context.Context, e audit.Entry) (int64, error) {
	if s.auditOverride != nil {
		return s.auditOverride.Append(ctx, e)
	}
	return s.recorder.Append(ctx, e)
}

// SetAuditForTesting installs a fake audit recorder. ONLY for use in
// *_test.go.
func (s *Service) SetAuditForTesting(r auditAppender) {
	s.auditOverride = r
}

// vaultPKI returns the PKI signer the refresh path will use. Default
// is the concrete *vault.Client; unit tests install a fake via
// SetPKIForTesting.
func (s *Service) vaultPKI() pkiSigner {
	if s.pkiOverride != nil {
		return s.pkiOverride
	}
	return s.vaultClient
}

// SetPKIForTesting installs a fake PKI signer. ONLY for use in
// *_test.go.
func (s *Service) SetPKIForTesting(pki pkiSigner) {
	s.pkiOverride = pki
}

// NewService creates the endpoint service. publicURL is the externally
// reachable Admin API base URL embedded in enrollment tokens; gatewayURL
// is the gateway endpoint returned to the agent after a successful
// agent-enroll call.
func NewService(pool *pgxpool.Pool, vc *vault.Client, rec *audit.Recorder, log *slog.Logger, publicURL, gatewayURL string) *Service {
	return &Service{
		pool:        pool,
		vaultClient: vc,
		recorder:    rec,
		log:         log,
		publicURL:   strings.TrimRight(publicURL, "/"),
		gatewayURL:  gatewayURL,
	}
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

// Enroll issues a single-use enrollment token from Vault and returns it
// as a single opaque base64-url blob. The blob bundles {role_id,
// secret_id, enroll_url} so the operator only has to copy one value
// into the agent installer command line.
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

	// Store the token record. The agent-enroll handler later looks this
	// row up by vault_secret_id to recover the tenant_id binding (Vault
	// has no idea which tenant a Secret ID belongs to).
	//
	// issued_for_tenant mirrors tenant_id but the name makes it explicit
	// that the tenant is pinned to the issuing operator's Principal —
	// the agent has no ability to influence it. Migration 0031 introduced
	// the column; both are set so downstream queries can rely on the
	// explicit name.
	_, err = s.pool.Exec(ctx,
		`INSERT INTO enrollment_tokens (tenant_id, vault_secret_id, vault_role_id, created_by, expires_at, issued_for_tenant)
		 VALUES ($1::uuid, $2, $3, $4::uuid, $5, $1::uuid)`,
		p.TenantID, secretID, roleID, p.UserID, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("endpoint: store enrollment token: %w", err)
	}

	enrollURL := s.publicURL + "/v1/agent-enroll"
	token, err := encodeEnrollmentToken(enrollmentTokenPayload{
		RoleID:    roleID,
		SecretID:  secretID,
		EnrollURL: enrollURL,
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint: encode enrollment token: %w", err)
	}

	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionEndpointEnrolled,
		Target:   "enrollment-token:issued",
		Details: map[string]any{
			"expires_at": expiresAt,
			"enroll_url": enrollURL,
		},
	})
	if err != nil {
		return nil, err
	}

	return &EnrollmentToken{
		Token:     token,
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
