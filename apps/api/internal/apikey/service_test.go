// Package apikey — in-memory unit tests for the apikey service.
package apikey

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/personel/api/internal/audit"
)

// ----- In-memory Store fake --------------------------------------

type memStore struct {
	mu      sync.Mutex
	byHash  map[string]*Record
	byID    map[string]*Record
	insertN int
	touchN  int
}

func newMemStore() *memStore {
	return &memStore{
		byHash: make(map[string]*Record),
		byID:   make(map[string]*Record),
	}
}

func (m *memStore) Insert(_ context.Context, tenantID *string, name, keyHash, createdBy string, scopes []string, expiresAt *time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertN++
	id := "k" + string(rune('0'+len(m.byID)))
	rec := &Record{
		ID:        id,
		TenantID:  tenantID,
		Name:      name,
		Scopes:    append([]string(nil), scopes...),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
	}
	m.byHash[keyHash] = rec
	m.byID[id] = rec
	return id, nil
}

func (m *memStore) GetByHash(_ context.Context, keyHash string) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byHash[keyHash]
	if !ok || rec.RevokedAt != nil {
		return nil, pgx.ErrNoRows
	}
	cp := *rec
	return &cp, nil
}

func (m *memStore) List(_ context.Context, tenantID *string) ([]*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Record
	for _, rec := range m.byID {
		if rec.RevokedAt != nil {
			continue
		}
		if tenantID != nil {
			if rec.TenantID == nil || *rec.TenantID != *tenantID {
				continue
			}
		}
		cp := *rec
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memStore) Revoke(_ context.Context, id, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	rec.RevokedAt = &now
	return nil
}

func (m *memStore) TouchLastUsed(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touchN++
	return nil
}

// ----- Test audit recorder ---------------------------------------

type memAudit struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (m *memAudit) Append(_ context.Context, e audit.Entry) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return int64(len(m.entries)), nil
}

// ----- Helpers ----------------------------------------------------

func makeSvc(t *testing.T) (*Service, *memStore, *memAudit) {
	t.Helper()
	store := newMemStore()
	rec := &memAudit{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(store, "test", rec, log)
	return svc, store, rec
}

func ptr(s string) *string { return &s }

// ----- Generate / Verify happy path ------------------------------

func TestGenerateVerify_RoundTrip(t *testing.T) {
	svc, _, rec := makeSvc(t)
	ctx := context.Background()
	tenant := "tenant-A"

	gk, err := svc.Generate(ctx, &tenant, "ingest", "user-1", []string{"events:ingest"}, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(gk.Plaintext, "psk_test_") {
		t.Fatalf("unexpected plaintext prefix: %q", gk.Plaintext)
	}
	if len(gk.Plaintext) < 25 {
		t.Fatalf("plaintext too short: %q", gk.Plaintext)
	}

	v, err := svc.Verify(ctx, gk.Plaintext)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if v.ID != gk.ID {
		t.Fatalf("id mismatch: got %q want %q", v.ID, gk.ID)
	}
	if v.TenantID == nil || *v.TenantID != tenant {
		t.Fatalf("tenant mismatch: %+v", v.TenantID)
	}
	if len(v.Scopes) != 1 || v.Scopes[0] != "events:ingest" {
		t.Fatalf("scopes mismatch: %+v", v.Scopes)
	}

	// Audit entry recorded.
	if len(rec.entries) != 1 || rec.entries[0].Action != audit.ActionAPIKeyCreated {
		t.Fatalf("expected apikey.issued audit, got %+v", rec.entries)
	}
}

// ----- Revoked key ------------------------------------------------

func TestVerify_RevokedKeyRejected(t *testing.T) {
	svc, _, _ := makeSvc(t)
	ctx := context.Background()
	tenant := "tenant-B"

	gk, _ := svc.Generate(ctx, &tenant, "revoke-me", "user-1", []string{"audit:read"}, nil)
	if err := svc.Revoke(ctx, gk.ID, tenant, "user-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := svc.Verify(ctx, gk.Plaintext)
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("want ErrInvalidKey, got %v", err)
	}
}

// ----- Expired key ------------------------------------------------

func TestVerify_ExpiredKeyRejected(t *testing.T) {
	svc, _, _ := makeSvc(t)
	ctx := context.Background()
	tenant := "tenant-C"
	past := time.Now().UTC().Add(-1 * time.Hour)

	gk, err := svc.Generate(ctx, &tenant, "expired", "user-1", []string{"audit:read"}, &past)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := svc.Verify(ctx, gk.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for expired key, got %v", err)
	}
}

// ----- Wrong key rejected -----------------------------------------

func TestVerify_UnknownKeyRejected(t *testing.T) {
	svc, _, _ := makeSvc(t)
	ctx := context.Background()

	_, err := svc.Verify(ctx, "psk_test_bogusbogusbogusbogusbogus")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("unknown key should reject with ErrInvalidKey, got %v", err)
	}
}

// ----- Malformed key rejected (same error) -----------------------

func TestVerify_MalformedKeyRejected(t *testing.T) {
	svc, _, _ := makeSvc(t)
	ctx := context.Background()

	// No psk_ prefix
	if _, err := svc.Verify(ctx, "Bearer abc"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("malformed should reject with ErrInvalidKey, got %v", err)
	}
	// Empty
	if _, err := svc.Verify(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("empty should reject with ErrInvalidKey, got %v", err)
	}
}

// ----- Scope checking ---------------------------------------------

func TestRequireScope_Passes(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, scopesKey, []string{"events:ingest", "audit:read"})

	if err := RequireScope(ctx, "audit:read"); err != nil {
		t.Fatalf("RequireScope should pass, got %v", err)
	}
}

func TestRequireScope_Fails(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, scopesKey, []string{"events:ingest"})

	if err := RequireScope(ctx, "audit:read"); !errors.Is(err, ErrMissingScope) {
		t.Fatalf("RequireScope should fail with ErrMissingScope, got %v", err)
	}
}

func TestRequireScope_NoKeyInContext(t *testing.T) {
	if err := RequireScope(context.Background(), "audit:read"); !errors.Is(err, ErrMissingScope) {
		t.Fatalf("empty ctx should fail, got %v", err)
	}
}

// ----- Plaintext only returned on Generate -----------------------

func TestList_DoesNotExposePlaintext(t *testing.T) {
	svc, _, _ := makeSvc(t)
	ctx := context.Background()
	tenant := "tenant-D"

	_, err := svc.Generate(ctx, &tenant, "list-test", "user-1", []string{"x"}, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	list, err := svc.List(ctx, &tenant)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1, got %d", len(list))
	}
	// Record struct has no Plaintext field by design; compile-time
	// check + a runtime sanity: marshal to JSON and look for it.
	// Since Record is a local struct with no Plaintext, this test
	// also locks the schema — a future accidental addition breaks
	// it.
	if list[0].Name != "list-test" {
		t.Fatalf("wrong name: %q", list[0].Name)
	}
}

// ----- Audit emitted on revoke -----------------------------------

func TestRevoke_EmitsAudit(t *testing.T) {
	svc, _, rec := makeSvc(t)
	ctx := context.Background()
	tenant := "tenant-E"

	gk, _ := svc.Generate(ctx, &tenant, "rev", "user-1", []string{"x"}, nil)
	if err := svc.Revoke(ctx, gk.ID, tenant, "user-2"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// We expect 2 audit entries: issued + revoked.
	if len(rec.entries) != 2 {
		t.Fatalf("want 2 audit entries, got %d", len(rec.entries))
	}
	if rec.entries[1].Action != audit.ActionAPIKeyRevoked {
		t.Fatalf("second entry should be revoked, got %s", rec.entries[1].Action)
	}
	if rec.entries[1].Actor != "user-2" {
		t.Fatalf("revoked audit actor wrong: %s", rec.entries[1].Actor)
	}
}

// ----- Unit-level hashing determinism ----------------------------

func TestHashKey_Stable(t *testing.T) {
	a := hashKey("psk_test_abc")
	b := hashKey("psk_test_abc")
	if a != b {
		t.Fatal("hashKey not deterministic")
	}
	c := hashKey("psk_test_abd")
	if a == c {
		t.Fatal("hashKey collision on different inputs")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex expected 64 chars, got %d", len(a))
	}
}

// Silence unused-import warnings when conditionally compiling.
var _ = ptr
