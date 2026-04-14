// Package featureflags — minimal, purpose-built feature flag evaluator.
//
// Faz 16 #173 — Pure Go, zero external SDK (no OpenFeature, no LaunchDarkly).
// The contract is deliberately narrow: a flag has a key, a default, an
// enabled bit, a rollout percentage, and an optional per-tenant override
// list. Evaluation is deterministic (stable hash over tenant_id + user_id
// + flag key) so the same user always sees the same rollout decision.
//
// Design goals:
//  1. Default-deny: unknown flag ALWAYS returns the caller's supplied
//     default, which is false almost everywhere. Evaluation never panics.
//  2. Audit-loud: every admin-initiated flip writes an audit entry with
//     the old + new state AND the actor BEFORE the DB update, so a
//     failed DB write still leaves a forensic trail.
//  3. No background workers: the cache is request-lifetime; admins
//     hit the DB every call. Flag reads by services use a ~30s in-process
//     cache so hot paths don't pay the latency.
//  4. KVKK-neutral: flag evaluation does NOT log user_id or tenant_id;
//     only aggregate counts go to Prometheus.
package featureflags

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// Sentinel errors.
var (
	// ErrNotFound is returned when a flag key does not exist in the
	// database. Callers should treat this as "use the default".
	ErrNotFound = errors.New("featureflags: flag not found")

	// ErrInvalidInput is returned when a caller tries to create or
	// update a flag with an illegal rollout percentage, empty key,
	// etc.
	ErrInvalidInput = errors.New("featureflags: invalid input")
)

// Flag is the full DB row plus JSON bindings for the admin UI.
type Flag struct {
	Key               string            `json:"key"`
	Description       string            `json:"description"`
	Enabled           bool              `json:"enabled"`
	DefaultValue      bool              `json:"default_value"`
	RolloutPercentage int               `json:"rollout_percentage"`
	TenantOverrides   map[string]bool   `json:"tenant_overrides,omitempty"`
	RoleOverrides     map[string]bool   `json:"role_overrides,omitempty"`
	UserOverrides     map[string]bool   `json:"user_overrides,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	UpdatedBy         string            `json:"updated_by,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// EvalContext is the minimal state required to evaluate a flag.
//
// TenantID + UserID + Role are pulled from the caller's Principal in the
// request handler. Callers that need an out-of-request evaluation (e.g.
// a cron job) pass "" for UserID and Role and the tenant-level rules
// are applied.
type EvalContext struct {
	TenantID string
	UserID   string
	Role     string
}

// Service is the feature flag evaluator + admin.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger

	mu       sync.RWMutex
	cache    map[string]cachedFlag
	cacheTTL time.Duration
}

type cachedFlag struct {
	flag    Flag
	fetched time.Time
}

// NewService creates the feature flag service. pool may be nil for unit
// tests — the service then refuses all reads with ErrNotFound and lets
// evaluation fall through to the supplied default.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{
		pool:     pool,
		recorder: rec,
		log:      log,
		cache:    make(map[string]cachedFlag),
		cacheTTL: 30 * time.Second,
	}
}

// IsEnabled is the hot-path evaluator. Returns def when the flag is
// unknown, the DB is unreachable, or any other non-fatal error occurs.
// This method NEVER returns an error — caller-visible failure modes
// must never break a feature flag lookup.
func (s *Service) IsEnabled(ctx context.Context, key string, ec EvalContext, def bool) bool {
	if key == "" {
		return def
	}
	flag, err := s.getCached(ctx, key)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && s.log != nil {
			s.log.WarnContext(ctx, "featureflags: evaluation fell back to default",
				slog.String("flag", key),
				slog.String("error", err.Error()))
		}
		return def
	}
	return evaluate(flag, ec)
}

// evaluate is the pure-function evaluator. Extracted so tests can exercise
// every branch without touching Postgres.
func evaluate(f Flag, ec EvalContext) bool {
	// Master kill-switch. If Enabled=false the flag is universally off.
	if !f.Enabled {
		return false
	}

	// Per-user override beats role + tenant.
	if ec.UserID != "" {
		if v, ok := f.UserOverrides[ec.UserID]; ok {
			return v
		}
	}
	// Per-role override beats tenant.
	if ec.Role != "" {
		if v, ok := f.RoleOverrides[ec.Role]; ok {
			return v
		}
	}
	// Per-tenant override beats rollout percentage.
	if ec.TenantID != "" {
		if v, ok := f.TenantOverrides[ec.TenantID]; ok {
			return v
		}
	}

	// Rollout percentage: 0 = off for everyone, 100 = on for everyone.
	// Anything in between hashes (tenant||user||key) into [0, 100).
	// Stable — same input always yields the same bucket.
	switch {
	case f.RolloutPercentage <= 0:
		return f.DefaultValue
	case f.RolloutPercentage >= 100:
		return true
	}

	bucket := stableBucket(ec.TenantID, ec.UserID, f.Key)
	if bucket < f.RolloutPercentage {
		return true
	}
	return f.DefaultValue
}

// stableBucket returns a value in [0, 100) derived from tenant+user+key.
func stableBucket(tenant, user, key string) int {
	h := sha256.Sum256([]byte(tenant + "|" + user + "|" + key))
	// Take first 4 bytes as uint32, mod 100. Distribution is uniform
	// enough for rollout purposes.
	n := binary.BigEndian.Uint32(h[:4])
	return int(n % 100)
}

// --- Admin API: List / Get / Set ---

// List returns all flags for the console's admin page.
func (s *Service) List(ctx context.Context) ([]Flag, error) {
	if s.pool == nil {
		return []Flag{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT key, description, enabled, default_value, rollout_percentage,
		       tenant_overrides, role_overrides, user_overrides,
		       created_at, updated_at, updated_by, metadata
		FROM feature_flags
		ORDER BY key
	`)
	if err != nil {
		return nil, fmt.Errorf("featureflags: list: %w", err)
	}
	defer rows.Close()

	var out []Flag
	for rows.Next() {
		var f Flag
		var tenantJSON, roleJSON, userJSON, metaJSON []byte
		if err := rows.Scan(
			&f.Key, &f.Description, &f.Enabled, &f.DefaultValue, &f.RolloutPercentage,
			&tenantJSON, &roleJSON, &userJSON,
			&f.CreatedAt, &f.UpdatedAt, &f.UpdatedBy, &metaJSON,
		); err != nil {
			return nil, fmt.Errorf("featureflags: scan: %w", err)
		}
		_ = json.Unmarshal(tenantJSON, &f.TenantOverrides)
		_ = json.Unmarshal(roleJSON, &f.RoleOverrides)
		_ = json.Unmarshal(userJSON, &f.UserOverrides)
		_ = json.Unmarshal(metaJSON, &f.Metadata)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Get returns a single flag by key.
func (s *Service) Get(ctx context.Context, key string) (Flag, error) {
	if s.pool == nil {
		return Flag{}, ErrNotFound
	}
	var f Flag
	var tenantJSON, roleJSON, userJSON, metaJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT key, description, enabled, default_value, rollout_percentage,
		       tenant_overrides, role_overrides, user_overrides,
		       created_at, updated_at, updated_by, metadata
		FROM feature_flags
		WHERE key = $1
	`, key).Scan(
		&f.Key, &f.Description, &f.Enabled, &f.DefaultValue, &f.RolloutPercentage,
		&tenantJSON, &roleJSON, &userJSON,
		&f.CreatedAt, &f.UpdatedAt, &f.UpdatedBy, &metaJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flag{}, ErrNotFound
	}
	if err != nil {
		return Flag{}, fmt.Errorf("featureflags: get: %w", err)
	}
	_ = json.Unmarshal(tenantJSON, &f.TenantOverrides)
	_ = json.Unmarshal(roleJSON, &f.RoleOverrides)
	_ = json.Unmarshal(userJSON, &f.UserOverrides)
	_ = json.Unmarshal(metaJSON, &f.Metadata)
	return f, nil
}

// Set creates or updates a flag. All writes go through here so the audit
// entry is emitted once per change.
func (s *Service) Set(ctx context.Context, actor string, f Flag) error {
	if err := validate(f); err != nil {
		return err
	}
	if s.pool == nil {
		return fmt.Errorf("featureflags: no database configured")
	}

	// Fetch previous state for the audit diff. A missing row is fine —
	// it's a create and the old state is simply empty.
	prev, _ := s.Get(ctx, f.Key)

	// AUDIT BEFORE WRITE — contract in recorder.go.
	tenantOverrides, _ := json.Marshal(f.TenantOverrides)
	roleOverrides, _ := json.Marshal(f.RoleOverrides)
	userOverrides, _ := json.Marshal(f.UserOverrides)
	metadata, _ := json.Marshal(f.Metadata)

	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:  actor,
		Action: audit.ActionFeatureFlagSet,
		Target: fmt.Sprintf("feature_flag:%s", f.Key),
		Details: map[string]any{
			"old": prev,
			"new": f,
		},
	})
	if err != nil {
		return fmt.Errorf("featureflags: audit: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO feature_flags (
		    key, description, enabled, default_value, rollout_percentage,
		    tenant_overrides, role_overrides, user_overrides,
		    created_at, updated_at, updated_by, metadata
		) VALUES (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8,
		    NOW(), NOW(), $9, $10
		)
		ON CONFLICT (key) DO UPDATE SET
		    description        = EXCLUDED.description,
		    enabled            = EXCLUDED.enabled,
		    default_value      = EXCLUDED.default_value,
		    rollout_percentage = EXCLUDED.rollout_percentage,
		    tenant_overrides   = EXCLUDED.tenant_overrides,
		    role_overrides     = EXCLUDED.role_overrides,
		    user_overrides     = EXCLUDED.user_overrides,
		    updated_at         = NOW(),
		    updated_by         = EXCLUDED.updated_by,
		    metadata           = EXCLUDED.metadata
	`, f.Key, f.Description, f.Enabled, f.DefaultValue, f.RolloutPercentage,
		tenantOverrides, roleOverrides, userOverrides, actor, metadata)
	if err != nil {
		return fmt.Errorf("featureflags: upsert: %w", err)
	}

	s.invalidate(f.Key)
	return nil
}

// Delete removes a flag. Unknown flags evaluate to their default from
// that moment forward.
func (s *Service) Delete(ctx context.Context, actor, key string) error {
	if s.pool == nil {
		return fmt.Errorf("featureflags: no database configured")
	}
	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:  actor,
		Action: audit.ActionFeatureFlagDeleted,
		Target: fmt.Sprintf("feature_flag:%s", key),
	}); err != nil {
		return fmt.Errorf("featureflags: audit: %w", err)
	}

	_, err := s.pool.Exec(ctx, `DELETE FROM feature_flags WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("featureflags: delete: %w", err)
	}
	s.invalidate(key)
	return nil
}

// --- internal helpers ---

func (s *Service) getCached(ctx context.Context, key string) (Flag, error) {
	s.mu.RLock()
	cf, ok := s.cache[key]
	s.mu.RUnlock()
	if ok && time.Since(cf.fetched) < s.cacheTTL {
		return cf.flag, nil
	}

	f, err := s.Get(ctx, key)
	if err != nil {
		return Flag{}, err
	}

	s.mu.Lock()
	s.cache[key] = cachedFlag{flag: f, fetched: time.Now()}
	s.mu.Unlock()
	return f, nil
}

func (s *Service) invalidate(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
}

func validate(f Flag) error {
	if f.Key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidInput)
	}
	if len(f.Key) > 128 {
		return fmt.Errorf("%w: key too long (max 128)", ErrInvalidInput)
	}
	if f.RolloutPercentage < 0 || f.RolloutPercentage > 100 {
		return fmt.Errorf("%w: rollout_percentage must be 0..100", ErrInvalidInput)
	}
	return nil
}
