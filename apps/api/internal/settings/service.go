package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// Service owns tenant-level CA mode + retention policy reads/writes.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService constructs the settings service.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger) *Service {
	if rec == nil {
		panic("settings: audit recorder is required")
	}
	return &Service{pool: pool, recorder: rec, log: log}
}

// GetCaMode returns the tenant's current CA mode + config. Missing
// rows fall back to CaModeInternal with an empty config.
func (s *Service) GetCaMode(ctx context.Context, tenantID string) (CaModeInfo, error) {
	var mode string
	var cfgJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ca_mode, 'internal'), ca_config
		   FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&mode, &cfgJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CaModeInfo{Mode: CaModeInternal, Config: map[string]any{}}, nil
		}
		return CaModeInfo{}, fmt.Errorf("settings: get ca_mode: %w", err)
	}
	out := CaModeInfo{Mode: CaMode(mode), Config: map[string]any{}}
	if len(cfgJSON) > 0 {
		_ = json.Unmarshal(cfgJSON, &out.Config)
	}
	return out, nil
}

// UpdateCaMode validates the request, writes the audit entry, and
// updates the tenants row. A validation failure short-circuits before
// the audit entry is appended — malformed requests are not considered
// state transitions.
func (s *Service) UpdateCaMode(ctx context.Context, actorID, tenantID string, req UpdateCaModeRequest) error {
	if err := validateCaMode(req); err != nil {
		return err
	}

	prev, err := s.GetCaMode(ctx, tenantID)
	if err != nil {
		return err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionCaModeUpdate,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"previous_mode": prev.Mode,
			"new_mode":      req.Mode,
			"config_keys":   keysOf(req.Config),
		},
	}); err != nil {
		return err
	}

	cfgJSON, err := json.Marshal(req.Config)
	if err != nil {
		return fmt.Errorf("settings: marshal ca_config: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE tenants
		    SET ca_mode   = $1,
		        ca_config = $2::jsonb,
		        updated_at = now()
		  WHERE id = $3::uuid`,
		string(req.Mode), cfgJSON, tenantID,
	)
	if err != nil {
		return fmt.Errorf("settings: update ca_mode: %w", err)
	}
	return nil
}

// GetRetention returns the effective retention policy for the tenant.
// If the tenant has no override the system default is returned (the
// caller never sees nil).
func (s *Service) GetRetention(ctx context.Context, tenantID string) (RetentionPolicy, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT retention_policy FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultRetentionPolicy, nil
		}
		return RetentionPolicy{}, fmt.Errorf("settings: get retention: %w", err)
	}
	if len(raw) == 0 {
		return DefaultRetentionPolicy, nil
	}
	// Start from the default so missing keys inherit sane values.
	out := DefaultRetentionPolicy
	if err := json.Unmarshal(raw, &out); err != nil {
		// A malformed JSON in the column is a DB-level problem; log
		// and fall back to the default so the console keeps working.
		if s.log != nil {
			s.log.WarnContext(ctx, "settings: retention_policy JSON unmarshal failed — using default",
				slog.String("tenant_id", tenantID),
				slog.Any("error", err),
			)
		}
		return DefaultRetentionPolicy, nil
	}
	return out, nil
}

// UpdateRetention validates the full policy against KVKK minimums,
// writes an audit entry, then UPDATEs the tenants row.
func (s *Service) UpdateRetention(ctx context.Context, actorID, tenantID string, req RetentionPolicy) error {
	if err := validateRetention(req); err != nil {
		return err
	}

	prev, err := s.GetRetention(ctx, tenantID)
	if err != nil {
		return err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionRetentionUpdate,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"previous": prev,
			"new":      req,
		},
	}); err != nil {
		return err
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("settings: marshal retention: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE tenants
		    SET retention_policy = $1::jsonb,
		        updated_at       = now()
		  WHERE id = $2::uuid`,
		raw, tenantID,
	)
	if err != nil {
		return fmt.Errorf("settings: update retention: %w", err)
	}
	return nil
}

// keysOf returns the top-level keys of m (deterministic order not
// guaranteed — audit details are a map for operator consumption).
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
