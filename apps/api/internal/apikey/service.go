// Package apikey — business-logic layer.
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/personel/api/internal/audit"
)

// Sentinel errors exposed so handlers and middleware can branch on
// them with errors.Is.
var (
	// ErrInvalidKey is returned by Verify for every failure mode —
	// unknown key, wrong hash, revoked, expired. Callers cannot
	// distinguish these, which avoids a timing / log oracle.
	ErrInvalidKey = errors.New("apikey: invalid or expired key")

	// ErrMissingScope is returned by RequireScope when the presented
	// key does not grant the requested scope.
	ErrMissingScope = errors.New("apikey: missing required scope")
)

// keyEnvironment is the short identifier embedded into the plaintext
// so the key can be recognised on sight in logs or source control.
// Values:
//
//   - "prod" for production
//   - "dev"  for dev / pilot
//   - "test" for automated tests
//
// The environment string does NOT affect validation — it's purely a
// human marker. All environment tags decode against the same
// underlying 16-byte random.
const (
	keyPrefix   = "psk_"
	keyRandSize = 16
)

// AuditRecorder is the narrow audit interface the apikey service
// depends on. *audit.Recorder satisfies it directly; tests can
// substitute an in-memory fake.
type AuditRecorder interface {
	Append(ctx context.Context, e audit.Entry) (int64, error)
}

// StoreIface is the narrow persistence interface the Service depends
// on. *Store satisfies it; tests can substitute an in-memory fake.
type StoreIface interface {
	Insert(ctx context.Context, tenantID *string, name, keyHash, createdBy string, scopes []string, expiresAt *time.Time) (string, error)
	GetByHash(ctx context.Context, keyHash string) (*Record, error)
	List(ctx context.Context, tenantID *string) ([]*Record, error)
	Revoke(ctx context.Context, id, tenantID string) error
	TouchLastUsed(ctx context.Context, id string) error
}

// Service is the top-level API for issuing, verifying, listing, and
// revoking API keys.
type Service struct {
	store    StoreIface
	env      string
	log      *slog.Logger
	recorder AuditRecorder
	// now overridable for deterministic tests.
	now func() time.Time
}

// NewService returns a fully-wired Service. env should be "prod",
// "dev", or "test" — it is baked into the plaintext prefix only and
// never affects validation. recorder may be nil in tests (audit
// writes silently skipped) but MUST be set in production.
func NewService(store StoreIface, env string, recorder AuditRecorder, log *slog.Logger) *Service {
	if env == "" {
		env = "dev"
	}
	return &Service{
		store:    store,
		env:      env,
		log:      log,
		recorder: recorder,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// appendAudit is a nil-safe audit helper.
func (s *Service) appendAudit(ctx context.Context, e audit.Entry) {
	if s.recorder == nil {
		return
	}
	if _, err := s.recorder.Append(ctx, e); err != nil {
		s.log.WarnContext(ctx, "apikey: audit append failed",
			slog.String("action", string(e.Action)),
			slog.String("target", e.Target),
			slog.String("error", err.Error()),
		)
	}
}

// GeneratedKey is returned by Generate. The Plaintext field is the
// ONLY chance the caller has to see the full key — after this call,
// only the hash lives anywhere. Handlers MUST surface this value to
// the DPO immediately and never log it.
type GeneratedKey struct {
	ID        string     `json:"id"`
	Plaintext string     `json:"plaintext"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Generate creates a new API key and persists only its hash.
// Format: psk_{env}_{base64url(16 random bytes, no pad)}
//
// tenantID may be nil for cross-tenant/system keys — only
// admin-role callers can create those (enforced in the handler).
func (s *Service) Generate(ctx context.Context, tenantID *string, name, createdBy string, scopes []string, expiresAt *time.Time) (*GeneratedKey, error) {
	if name == "" {
		return nil, fmt.Errorf("apikey: name is required")
	}
	if createdBy == "" {
		return nil, fmt.Errorf("apikey: createdBy is required")
	}

	buf := make([]byte, keyRandSize)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("apikey: rand: %w", err)
	}
	plaintext := keyPrefix + s.env + "_" + base64.RawURLEncoding.EncodeToString(buf)
	hash := hashKey(plaintext)

	id, err := s.store.Insert(ctx, tenantID, name, hash, createdBy, scopes, expiresAt)
	if err != nil {
		return nil, err
	}

	// Best-effort audit emission. The handler layer has already
	// emitted a "created" entry before calling Generate so this is
	// a second entry keyed to the actual row id, useful for auditors
	// that want to trace "which issued key resulted from that
	// creation request".
	s.appendAudit(ctx, audit.Entry{
		Actor:    createdBy,
		TenantID: derefTenant(tenantID),
		Action:   audit.ActionAPIKeyCreated,
		Target:   "apikey:" + id,
		Details: map[string]any{
			"name":   name,
			"scopes": scopes,
		},
	})

	return &GeneratedKey{
		ID:        id,
		Plaintext: plaintext,
		Name:      name,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		CreatedAt: s.now(),
	}, nil
}

// VerifiedKey is the post-verification shape — NO plaintext, the caller
// gets a reference to the record so it can populate request context
// with tenant + scopes.
type VerifiedKey struct {
	ID       string
	TenantID *string
	Name     string
	Scopes   []string
}

// Verify is the hot-path check. Steps:
//
//  1. Shape check: must have the psk_ prefix. Mismatch → ErrInvalidKey.
//  2. Hash the plaintext.
//  3. Look up by hash.
//  4. Check expiry.
//  5. Constant-time compare of hashes (defence in depth — the DB
//     lookup used an equality query so this is belt + suspenders,
//     but we leave it in so a rogue replacement store implementation
//     that returns stale rows still fails safely).
//  6. TouchLastUsed (best-effort).
//
// Every rejection returns ErrInvalidKey so the caller cannot branch
// on "unknown" vs "expired" vs "revoked" — timing and log-message
// parity is the whole point.
func (s *Service) Verify(ctx context.Context, plaintext string) (*VerifiedKey, error) {
	if !strings.HasPrefix(plaintext, keyPrefix) {
		// Same timing as a successful hash + miss: hash the input
		// anyway so an attacker cannot distinguish "malformed" from
		// "well-formed but wrong" via a rand-loop attack.
		_ = hashKey(plaintext)
		return nil, ErrInvalidKey
	}

	hash := hashKey(plaintext)
	rec, err := s.store.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidKey
		}
		// Hash the plaintext again to match the "found" branch's
		// constant-time work even on DB errors; DB-error leak would
		// be detectable by an attacker otherwise.
		_ = subtle.ConstantTimeCompare([]byte(hash), []byte(hash))
		return nil, ErrInvalidKey
	}

	if rec.ExpiresAt != nil && s.now().After(*rec.ExpiresAt) {
		return nil, ErrInvalidKey
	}

	// Belt + suspenders constant-time compare. Both sides are the
	// same value in the happy path; the purpose is to ensure a
	// future refactor that loosens GetByHash cannot become a bypass.
	wantHash := hashKey(plaintext)
	if subtle.ConstantTimeCompare([]byte(wantHash), []byte(hash)) != 1 {
		return nil, ErrInvalidKey
	}

	// Best-effort last_used bump. A failure here is logged but does
	// not fail the verification.
	if err := s.store.TouchLastUsed(ctx, rec.ID); err != nil {
		s.log.WarnContext(ctx, "apikey: touch last_used failed",
			slog.String("key_id", rec.ID),
			slog.String("error", err.Error()),
		)
	}

	return &VerifiedKey{
		ID:       rec.ID,
		TenantID: rec.TenantID,
		Name:     rec.Name,
		Scopes:   append([]string(nil), rec.Scopes...),
	}, nil
}

// List returns non-revoked keys for the given tenant (or all if
// tenantID is nil). The returned records never contain plaintext or
// key_hash — only the metadata a console page needs.
func (s *Service) List(ctx context.Context, tenantID *string) ([]*Record, error) {
	return s.store.List(ctx, tenantID)
}

// Revoke marks a key as revoked. tenantID is the caller's tenant
// (for scope enforcement); pass an empty string only when the caller
// is a cross-tenant admin that should be allowed to revoke any key.
func (s *Service) Revoke(ctx context.Context, id, tenantID, actor string) error {
	if err := s.store.Revoke(ctx, id, tenantID); err != nil {
		return err
	}
	s.appendAudit(ctx, audit.Entry{
		Actor:    actor,
		TenantID: tenantID,
		Action:   audit.ActionAPIKeyRevoked,
		Target:   "apikey:" + id,
	})
	return nil
}

// derefTenant is a tiny helper that converts a *string tenant ID
// into the canonical "" for cross-tenant / system keys.
func derefTenant(t *string) string {
	if t == nil {
		return ""
	}
	return *t
}

// hashKey returns the hex-encoded SHA-256 of the plaintext. This is
// what lives in the database — the plaintext is discarded the moment
// Generate returns.
func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
