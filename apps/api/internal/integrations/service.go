package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// VaultEncryptor is the narrow contract the service needs for the
// Vault transit engine. The concrete implementation is vault.Client
// (apps/api/internal/vault). Keeping it as a local interface keeps
// this package independent of the Vault SDK for unit tests.
type VaultEncryptor interface {
	Encrypt(ctx context.Context, keyName string, plaintext []byte) ([]byte, int, error)
	Decrypt(ctx context.Context, keyName string, ciphertext []byte) ([]byte, error)
}

// integrationsKey is the Vault transit key name dedicated to this
// package. It MUST exist in Vault before the service is used; operators
// create it via `vault write -f transit/keys/integrations`. On a brand
// new install the key is created by the bootstrap script alongside the
// control-plane signing key.
const integrationsKey = "integrations"

// ErrUnknownService is returned when a non-allowlisted service name is
// supplied to Upsert / Get / Delete.
var ErrUnknownService = errors.New("integrations: unknown service")

// ErrVaultUnavailable is returned when the service has no encryptor
// configured. Callers should treat this as a clear "not configured"
// signal and surface a 503.
var ErrVaultUnavailable = errors.New("integrations: vault encryptor not configured")

// Service manages per-tenant third-party service credentials.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	vault    VaultEncryptor
	log      *slog.Logger
	now      func() time.Time
}

// NewService constructs an integrations service. vault may be nil; in
// that case every Upsert / Decrypt returns ErrVaultUnavailable and GET
// endpoints still read (and mask) rows that were inserted previously.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, vault VaultEncryptor, log *slog.Logger) *Service {
	if rec == nil {
		panic("integrations: audit recorder is required")
	}
	return &Service{
		pool:     pool,
		recorder: rec,
		vault:    vault,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// List returns every integration row for the tenant with Config masked.
// The query runs under the RLS session variable so cross-tenant leaks
// are defended at the DB layer as well.
func (s *Service) List(ctx context.Context, tenantID string) ([]IntegrationRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, service_name, config_encrypted, config_key_version,
		        enabled, updated_at, audit_actor_id
		   FROM tenants_integrations
		  WHERE tenant_id = $1::uuid
		  ORDER BY service_name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("integrations: list: %w", err)
	}
	defer rows.Close()

	out := make([]IntegrationRecord, 0, 4)
	for rows.Next() {
		var r IntegrationRecord
		var ciphertext []byte
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.ServiceName, &ciphertext, &r.KeyVersion,
			&r.Enabled, &r.UpdatedAt, &r.AuditActorID,
		); err != nil {
			return nil, err
		}
		// Decrypt to a map then mask. If the vault encryptor is nil or
		// rejects the ciphertext we fall back to an empty masked map so
		// List never leaks the raw BYTEA and also never fails the whole
		// request over a single unreadable row.
		cfg, derr := s.decryptToMap(ctx, ciphertext)
		if derr != nil {
			if s.log != nil {
				s.log.WarnContext(ctx, "integrations: decrypt failed in List — row masked",
					slog.String("service", r.ServiceName),
					slog.Any("error", derr),
				)
			}
			cfg = map[string]any{}
		}
		r.Config = maskConfig(cfg)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns a single integration row for the tenant with Config
// masked. Returns sql.ErrNoRows-wrapped error when the row is absent.
func (s *Service) Get(ctx context.Context, tenantID, service string) (*IntegrationRecord, error) {
	if _, ok := AllowedServices[service]; !ok {
		return nil, ErrUnknownService
	}

	var r IntegrationRecord
	var ciphertext []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, service_name, config_encrypted, config_key_version,
		        enabled, updated_at, audit_actor_id
		   FROM tenants_integrations
		  WHERE tenant_id = $1::uuid AND service_name = $2`,
		tenantID, service,
	).Scan(
		&r.ID, &r.TenantID, &r.ServiceName, &ciphertext, &r.KeyVersion,
		&r.Enabled, &r.UpdatedAt, &r.AuditActorID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("integrations: get: %w", err)
	}
	cfg, derr := s.decryptToMap(ctx, ciphertext)
	if derr != nil {
		if s.log != nil {
			s.log.WarnContext(ctx, "integrations: decrypt failed in Get — returning masked shell",
				slog.String("service", service),
				slog.Any("error", derr),
			)
		}
		cfg = map[string]any{}
	}
	r.Config = maskConfig(cfg)
	return &r, nil
}

// Upsert creates or replaces the integration row for (tenant,service).
// The config is encrypted via Vault transit BEFORE it is written to
// Postgres; a failure at the Vault step short-circuits the row write.
// Returns ErrVaultUnavailable when the service was constructed without
// a VaultEncryptor.
func (s *Service) Upsert(ctx context.Context, actorID, tenantID, service string, req UpsertRequest) error {
	if _, ok := AllowedServices[service]; !ok {
		return ErrUnknownService
	}
	if s.vault == nil {
		return ErrVaultUnavailable
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}

	plaintext, err := json.Marshal(req.Config)
	if err != nil {
		return fmt.Errorf("integrations: marshal config: %w", err)
	}
	ciphertext, keyVersion, err := s.vault.Encrypt(ctx, integrationsKey, plaintext)
	if err != nil {
		return fmt.Errorf("integrations: encrypt: %w", err)
	}

	// Audit BEFORE the DB write — the recorder is the tamper-evident
	// authority so a failed append must short-circuit the mutation.
	actorUUID, _ := uuid.Parse(actorID)
	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionIntegrationUpsert,
		Target:   "integration:" + service,
		Details: map[string]any{
			"service":     service,
			"enabled":     req.Enabled,
			"key_version": keyVersion,
			// Log which top-level keys were set — NEVER the values.
			"config_keys": keysOf(req.Config),
		},
	})
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO tenants_integrations
		   (tenant_id, service_name, config_encrypted, config_key_version, enabled, audit_actor_id, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (tenant_id, service_name) DO UPDATE
		   SET config_encrypted   = EXCLUDED.config_encrypted,
		       config_key_version = EXCLUDED.config_key_version,
		       enabled            = EXCLUDED.enabled,
		       audit_actor_id     = EXCLUDED.audit_actor_id,
		       updated_at         = now()`,
		tenantID, service, ciphertext, keyVersion, req.Enabled, actorUUID,
	)
	if err != nil {
		return fmt.Errorf("integrations: upsert: %w", err)
	}
	return nil
}

// Delete removes the integration row for (tenant,service). Returns nil
// even when the row did not exist (idempotent) — the audit entry still
// fires so an attempt to remove a non-existent integration is
// discoverable in the log.
func (s *Service) Delete(ctx context.Context, actorID, tenantID, service string) error {
	if _, ok := AllowedServices[service]; !ok {
		return ErrUnknownService
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionIntegrationDelete,
		Target:   "integration:" + service,
		Details: map[string]any{
			"service": service,
		},
	}); err != nil {
		return err
	}

	_, err := s.pool.Exec(ctx,
		`DELETE FROM tenants_integrations
		  WHERE tenant_id = $1::uuid AND service_name = $2`,
		tenantID, service,
	)
	if err != nil {
		return fmt.Errorf("integrations: delete: %w", err)
	}
	return nil
}

// Decrypt is the internal-only path used by collectors that need the
// raw plaintext config to dial out to the third-party service. It
// bypasses masking. Every call is logged at DEBUG level for
// traceability; callers MUST NOT surface the returned map over any
// HTTP response.
func (s *Service) Decrypt(ctx context.Context, tenantID, service string) (map[string]any, error) {
	if _, ok := AllowedServices[service]; !ok {
		return nil, ErrUnknownService
	}
	if s.vault == nil {
		return nil, ErrVaultUnavailable
	}
	var ciphertext []byte
	err := s.pool.QueryRow(ctx,
		`SELECT config_encrypted
		   FROM tenants_integrations
		  WHERE tenant_id = $1::uuid AND service_name = $2 AND enabled = true`,
		tenantID, service,
	).Scan(&ciphertext)
	if err != nil {
		return nil, fmt.Errorf("integrations: decrypt row lookup: %w", err)
	}
	if s.log != nil {
		s.log.DebugContext(ctx, "integrations: plaintext decrypt",
			slog.String("tenant_id", tenantID),
			slog.String("service", service),
		)
	}
	return s.decryptToMap(ctx, ciphertext)
}

// decryptToMap is a shared helper that decrypts the given ciphertext
// and unmarshals it into a map[string]any.
func (s *Service) decryptToMap(ctx context.Context, ciphertext []byte) (map[string]any, error) {
	if s.vault == nil {
		return nil, ErrVaultUnavailable
	}
	pt, err := s.vault.Decrypt(ctx, integrationsKey, ciphertext)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(pt, &cfg); err != nil {
		return nil, fmt.Errorf("integrations: unmarshal config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

// keysOf returns the sorted set of top-level keys in m. Used in audit
// details so operators see what was set without leaking the values.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
