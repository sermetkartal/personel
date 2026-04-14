// Package backup — backup target catalog + run history.
//
// This file extends the existing backup.Service (evidence collector
// for RecordRun, Phase 3.0.3) with the Wave 9 Sprint 3A settings CRUD
// surface: per-tenant backup destinations and per-target run history.
// The TargetService is intentionally a separate type so main.go can
// construct it with its own dependencies (pgx pool + Vault transit
// client) without disturbing the existing RecordRun wiring.
package backup

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

// VaultEncryptor is the narrow contract needed for target config
// encryption. Implemented by vault.Client. A nil encryptor forces
// Upsert / TriggerRun paths to return ErrVaultUnavailable.
type VaultEncryptor interface {
	Encrypt(ctx context.Context, keyName string, plaintext []byte) ([]byte, int, error)
	Decrypt(ctx context.Context, keyName string, ciphertext []byte) ([]byte, error)
}

// backupTargetsKey is the Vault transit key dedicated to backup
// target configs (bucket creds, SFTP passwords, ...).
const backupTargetsKey = "backup_targets"

// Allowed backup target kinds — mirrors the migration 0044 allowlist.
var AllowedKinds = map[string]struct{}{
	"in_site_local":      {},
	"offsite_s3":         {},
	"offsite_azure":      {},
	"offsite_gcs":        {},
	"offsite_sftp":       {},
	"offsite_nfs":        {},
	"offsite_minio_peer": {},
}

// Allowed run kinds.
var allowedRunKinds = map[string]struct{}{
	"full":        {},
	"incremental": {},
}

// ErrUnknownKind is returned when CreateTarget / UpdateTarget receives
// a kind not in AllowedKinds.
var ErrUnknownKind = errors.New("backup: unknown target kind")

// ErrVaultUnavailable is returned when a mutation that needs
// encryption is invoked on a service constructed without a Vault
// encryptor.
var ErrVaultUnavailable = errors.New("backup: vault encryptor not configured")

// Target is one row from backup_targets, with the config returned in
// masked form (sensitive fields replaced with MaskedValue — the list
// of sensitive fields is fully kind-dependent so we use a coarse
// "anything-that-looks-like-a-secret" mask).
type Target struct {
	ID            uuid.UUID      `json:"id"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Config        map[string]any `json:"config"`
	KeyVersion    int            `json:"key_version"`
	Enabled       bool           `json:"enabled"`
	RetentionDays *int           `json:"retention_days,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// RunRecord is one row from backup_runs.
type RunRecord struct {
	ID           uuid.UUID  `json:"id"`
	TargetID     uuid.UUID  `json:"target_id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	Kind         string     `json:"kind"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Status       string     `json:"status"`
	SizeBytes    *int64     `json:"size_bytes,omitempty"`
	SHA256       string     `json:"sha256,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

// CreateTargetRequest is the body of POST /v1/settings/backup/targets.
type CreateTargetRequest struct {
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Config        map[string]any `json:"config"`
	Enabled       bool           `json:"enabled"`
	RetentionDays *int           `json:"retention_days,omitempty"`
}

// UpdateTargetRequest is the body of PATCH .../targets/{id}.
// Nil fields are left unchanged.
type UpdateTargetRequest struct {
	Name          *string        `json:"name,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	Enabled       *bool          `json:"enabled,omitempty"`
	RetentionDays *int           `json:"retention_days,omitempty"`
}

// MaskedValue is the opaque placeholder for secret config fields.
const MaskedValue = "***masked***"

// secretKeyHints is the set of substring matches applied case-
// insensitively to config keys. Any hit masks the value. This is
// deliberately coarse so operators cannot accidentally leak a secret
// just because we forgot to add its exact key name.
var secretKeyHints = []string{
	"secret", "password", "key", "token", "credential", "auth",
}

func looksSecret(k string) bool {
	lower := k
	// ASCII-fast tolower
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= 'A' && c <= 'Z' {
			lower = lower[:i] + string(c+32) + lower[i+1:]
		}
	}
	for _, h := range secretKeyHints {
		if containsSubstr(lower, h) {
			return true
		}
	}
	return false
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 || len(sub) > len(s) {
		return len(sub) == 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func maskTargetConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if looksSecret(k) {
			if v == nil || v == "" {
				out[k] = ""
			} else {
				out[k] = MaskedValue
			}
			continue
		}
		out[k] = v
	}
	return out
}

// TargetService owns the settings-facing backup target CRUD surface.
type TargetService struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	vault    VaultEncryptor
	log      *slog.Logger
}

// NewTargetService constructs a TargetService. vault may be nil in
// scaffold mode; mutations then return ErrVaultUnavailable.
func NewTargetService(pool *pgxpool.Pool, rec *audit.Recorder, vault VaultEncryptor, log *slog.Logger) *TargetService {
	if rec == nil {
		panic("backup: audit recorder is required")
	}
	return &TargetService{pool: pool, recorder: rec, vault: vault, log: log}
}

// CreateTarget validates the request, audit-logs it, encrypts the
// config via Vault transit, and inserts the backup_targets row.
func (s *TargetService) CreateTarget(ctx context.Context, actorID, tenantID string, req CreateTargetRequest) (*Target, error) {
	if _, ok := AllowedKinds[req.Kind]; !ok {
		return nil, ErrUnknownKind
	}
	if req.Name == "" {
		return nil, fmt.Errorf("backup: target name is required")
	}
	if s.vault == nil {
		return nil, ErrVaultUnavailable
	}

	cfg := req.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	pt, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("backup: marshal target config: %w", err)
	}
	ct, keyVersion, err := s.vault.Encrypt(ctx, backupTargetsKey, pt)
	if err != nil {
		return nil, fmt.Errorf("backup: encrypt target config: %w", err)
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionBackupTargetCreate,
		Target:   "backup_target:" + req.Name,
		Details: map[string]any{
			"name":           req.Name,
			"kind":           req.Kind,
			"enabled":        req.Enabled,
			"retention_days": req.RetentionDays,
			"config_keys":    keysOf(cfg),
		},
	}); err != nil {
		return nil, err
	}

	var t Target
	err = s.pool.QueryRow(ctx,
		`INSERT INTO backup_targets
		   (tenant_id, name, kind, config_encrypted, config_key_version, enabled, retention_days)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		 RETURNING id, tenant_id, name, kind, config_key_version, enabled, retention_days, created_at, updated_at`,
		tenantID, req.Name, req.Kind, ct, keyVersion, req.Enabled, req.RetentionDays,
	).Scan(&t.ID, &t.TenantID, &t.Name, &t.Kind, &t.KeyVersion, &t.Enabled, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("backup: insert target: %w", err)
	}
	t.Config = maskTargetConfig(cfg)
	return &t, nil
}

// ListTargets returns every backup target for the tenant with config
// masked. Decrypt failures degrade individual rows to an empty config
// rather than failing the whole request.
func (s *TargetService) ListTargets(ctx context.Context, tenantID string) ([]Target, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, kind, config_encrypted, config_key_version,
		        enabled, retention_days, created_at, updated_at
		   FROM backup_targets
		  WHERE tenant_id = $1::uuid
		  ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("backup: list targets: %w", err)
	}
	defer rows.Close()

	out := make([]Target, 0, 4)
	for rows.Next() {
		var t Target
		var ct []byte
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.Name, &t.Kind, &ct, &t.KeyVersion,
			&t.Enabled, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		cfg, err := s.decryptToMap(ctx, ct)
		if err != nil {
			if s.log != nil {
				s.log.WarnContext(ctx, "backup: list target decrypt failed — row masked",
					slog.String("name", t.Name),
					slog.Any("error", err),
				)
			}
			cfg = map[string]any{}
		}
		t.Config = maskTargetConfig(cfg)
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateTarget applies the partial update request. At least one field
// must be non-nil or the function returns a validation error.
func (s *TargetService) UpdateTarget(ctx context.Context, actorID, tenantID string, targetID uuid.UUID, req UpdateTargetRequest) error {
	if req.Name == nil && req.Config == nil && req.Enabled == nil && req.RetentionDays == nil {
		return fmt.Errorf("backup: update requires at least one field")
	}

	// Fetch existing row for audit before/after.
	existing, err := s.getTargetRaw(ctx, tenantID, targetID)
	if err != nil {
		return err
	}

	// Optional re-encrypt.
	var newCt []byte
	newKeyVersion := existing.KeyVersion
	if req.Config != nil {
		if s.vault == nil {
			return ErrVaultUnavailable
		}
		pt, err := json.Marshal(req.Config)
		if err != nil {
			return fmt.Errorf("backup: marshal update config: %w", err)
		}
		ct, ver, err := s.vault.Encrypt(ctx, backupTargetsKey, pt)
		if err != nil {
			return fmt.Errorf("backup: encrypt update config: %w", err)
		}
		newCt = ct
		newKeyVersion = ver
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionBackupTargetUpdate,
		Target:   "backup_target:" + existing.Name,
		Details: map[string]any{
			"name":           req.Name,
			"enabled":        req.Enabled,
			"retention_days": req.RetentionDays,
			"config_keys":    keysOf(req.Config),
		},
	}); err != nil {
		return err
	}

	// Build the UPDATE with COALESCE-by-parameter so we only touch
	// supplied fields.
	nameParam := existing.Name
	if req.Name != nil {
		nameParam = *req.Name
	}
	enabledParam := existing.Enabled
	if req.Enabled != nil {
		enabledParam = *req.Enabled
	}
	retentionParam := existing.RetentionDays
	if req.RetentionDays != nil {
		retentionParam = req.RetentionDays
	}

	if newCt != nil {
		_, err = s.pool.Exec(ctx,
			`UPDATE backup_targets
			    SET name               = $1,
			        config_encrypted   = $2,
			        config_key_version = $3,
			        enabled            = $4,
			        retention_days     = $5,
			        updated_at         = now()
			  WHERE tenant_id = $6::uuid AND id = $7`,
			nameParam, newCt, newKeyVersion, enabledParam, retentionParam, tenantID, targetID,
		)
	} else {
		_, err = s.pool.Exec(ctx,
			`UPDATE backup_targets
			    SET name           = $1,
			        enabled        = $2,
			        retention_days = $3,
			        updated_at     = now()
			  WHERE tenant_id = $4::uuid AND id = $5`,
			nameParam, enabledParam, retentionParam, tenantID, targetID,
		)
	}
	if err != nil {
		return fmt.Errorf("backup: update target: %w", err)
	}
	return nil
}

// DeleteTarget removes the target row. Cascading FK on backup_runs
// removes every run history for the target.
func (s *TargetService) DeleteTarget(ctx context.Context, actorID, tenantID string, targetID uuid.UUID) error {
	existing, err := s.getTargetRaw(ctx, tenantID, targetID)
	if err != nil {
		return err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionBackupTargetDelete,
		Target:   "backup_target:" + existing.Name,
		Details: map[string]any{
			"name": existing.Name,
			"kind": existing.Kind,
		},
	}); err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`DELETE FROM backup_targets WHERE tenant_id = $1::uuid AND id = $2`,
		tenantID, targetID,
	)
	if err != nil {
		return fmt.Errorf("backup: delete target: %w", err)
	}
	return nil
}

// TriggerRun inserts a "running" row in backup_runs and writes the
// audit entry. The out-of-API backup cron picks up pending rows by
// scanning status='running' with a recent started_at and performs the
// actual backup work. Returns the new run record.
func (s *TargetService) TriggerRun(ctx context.Context, actorID, tenantID string, targetID uuid.UUID, kind string) (*RunRecord, error) {
	if _, ok := allowedRunKinds[kind]; !ok {
		return nil, fmt.Errorf("backup: unknown run kind %q (expected full|incremental)", kind)
	}

	existing, err := s.getTargetRaw(ctx, tenantID, targetID)
	if err != nil {
		return nil, err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionBackupRunTrigger,
		Target:   "backup_target:" + existing.Name,
		Details: map[string]any{
			"target_id": targetID.String(),
			"kind":      kind,
		},
	}); err != nil {
		return nil, err
	}

	var rec RunRecord
	err = s.pool.QueryRow(ctx,
		`INSERT INTO backup_runs (target_id, tenant_id, kind, status)
		 VALUES ($1, $2::uuid, $3, 'running')
		 RETURNING id, target_id, tenant_id, kind, started_at, completed_at, status, size_bytes, sha256, COALESCE(error_message, '')`,
		targetID, tenantID, kind,
	).Scan(&rec.ID, &rec.TargetID, &rec.TenantID, &rec.Kind, &rec.StartedAt, &rec.CompletedAt, &rec.Status, &rec.SizeBytes, &rec.SHA256, &rec.ErrorMessage)
	if err != nil {
		return nil, fmt.Errorf("backup: insert run: %w", err)
	}
	return &rec, nil
}

// ListRuns returns the most recent run history for a target, newest
// first, bounded to 100 rows.
func (s *TargetService) ListRuns(ctx context.Context, tenantID string, targetID uuid.UUID) ([]RunRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, target_id, tenant_id, kind, started_at, completed_at, status, size_bytes, COALESCE(sha256, ''), COALESCE(error_message, '')
		   FROM backup_runs
		  WHERE tenant_id = $1::uuid AND target_id = $2
		  ORDER BY started_at DESC
		  LIMIT 100`,
		tenantID, targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("backup: list runs: %w", err)
	}
	defer rows.Close()

	out := make([]RunRecord, 0, 16)
	for rows.Next() {
		var r RunRecord
		if err := rows.Scan(
			&r.ID, &r.TargetID, &r.TenantID, &r.Kind, &r.StartedAt, &r.CompletedAt, &r.Status, &r.SizeBytes, &r.SHA256, &r.ErrorMessage,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// getTargetRaw fetches a single target row (with the ciphertext not
// yet decrypted). Used by Update / Delete / TriggerRun so every path
// verifies tenant ownership before mutating.
func (s *TargetService) getTargetRaw(ctx context.Context, tenantID string, targetID uuid.UUID) (*Target, error) {
	var t Target
	var ct []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, kind, config_encrypted, config_key_version,
		        enabled, retention_days, created_at, updated_at
		   FROM backup_targets
		  WHERE tenant_id = $1::uuid AND id = $2`,
		tenantID, targetID,
	).Scan(
		&t.ID, &t.TenantID, &t.Name, &t.Kind, &ct, &t.KeyVersion,
		&t.Enabled, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("backup: fetch target: %w", err)
	}
	return &t, nil
}

// decryptToMap is a shared helper that decrypts the given ciphertext
// and unmarshals into a map[string]any.
func (s *TargetService) decryptToMap(ctx context.Context, ct []byte) (map[string]any, error) {
	if s.vault == nil {
		return nil, ErrVaultUnavailable
	}
	pt, err := s.vault.Decrypt(ctx, backupTargetsKey, ct)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(pt, &cfg); err != nil {
		return nil, fmt.Errorf("backup: unmarshal target config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
