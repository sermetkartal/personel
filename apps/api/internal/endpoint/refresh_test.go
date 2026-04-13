// Package endpoint — unit tests for the Faz 6 #63 refresh-token
// path. The tests use in-memory fakes for Postgres (via the
// refreshStore interface) and Vault (via the pkiSigner interface) so
// they run without testcontainers. Integration coverage for the real
// Postgres RLS + real Vault flow lives in apps/api/test/integration.
package endpoint

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/vault"
)

// ──────────────────────────────────────────────────────────────────
// fakes
// ──────────────────────────────────────────────────────────────────

type fakeRefreshStore struct {
	mu         sync.Mutex
	rows       map[string]*refreshSnapshot // key: tenantID|endpointID
	markCalls  int
	lastSerial string
}

func (f *fakeRefreshStore) key(tenantID, endpointID string) string {
	return tenantID + "|" + endpointID
}

func (f *fakeRefreshStore) put(tenantID, endpointID string, snap *refreshSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = make(map[string]*refreshSnapshot)
	}
	f.rows[f.key(tenantID, endpointID)] = snap
}

func (f *fakeRefreshStore) LoadForRefresh(_ context.Context, tenantID, endpointID string) (*refreshSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap, ok := f.rows[f.key(tenantID, endpointID)]
	if !ok {
		return nil, nil
	}
	// Return a copy so tests can't accidentally mutate the fixture.
	clone := *snap
	if snap.LastRefreshAt != nil {
		t := *snap.LastRefreshAt
		clone.LastRefreshAt = &t
	}
	return &clone, nil
}

func (f *fakeRefreshStore) MarkRefreshed(_ context.Context, endpointID, newSerial string, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCalls++
	f.lastSerial = newSerial
	// Mutate every row that matches endpointID (regardless of tenant;
	// in practice only one row per endpoint per tenant exists).
	suffix := "|" + endpointID
	for k, row := range f.rows {
		if len(k) >= len(suffix) && k[len(k)-len(suffix):] == suffix {
			t := now
			row.LastRefreshAt = &t
			row.CertSerial = newSerial
		}
	}
	return nil
}

type fakePKI struct {
	mu            sync.Mutex
	signedCount   int
	revokedCount  int
	revokedSerial string
	// failMode toggles which call returns an error (0=none).
	failAt string
	// issuedCert is the cert returned from SignAgentCSR.
	issuedCert *vault.IssuedAgentCert
}

func (f *fakePKI) GetEnrollmentRoleID(_ context.Context) (string, error) {
	if f.failAt == "role_id" {
		return "", errors.New("boom")
	}
	return "fake-role-id", nil
}

func (f *fakePKI) IssueEnrollmentSecretID(_ context.Context) (string, error) {
	if f.failAt == "secret_id" {
		return "", errors.New("boom")
	}
	return "fake-secret-id", nil
}

func (f *fakePKI) LoginWithAppRole(_ context.Context, _, _ string) (*vaultapi.Client, error) {
	if f.failAt == "login" {
		return nil, errors.New("boom")
	}
	// Return a nil opaque client — our fakePKI.SignAgentCSR stub does
	// not dereference it, so a nil value is safe and avoids relying
	// on vaultapi.Client's (unexported) zero-value behaviour.
	return nil, nil
}

func (f *fakePKI) SignAgentCSR(_ context.Context, _ *vaultapi.Client, _, _, _ string) (*vault.IssuedAgentCert, error) {
	if f.failAt == "sign" {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	f.signedCount++
	f.mu.Unlock()
	if f.issuedCert != nil {
		return f.issuedCert, nil
	}
	return &vault.IssuedAgentCert{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\nFAKE\n-----END CERTIFICATE-----\n",
		CAChainPEM:     "-----BEGIN CERTIFICATE-----\nCHAIN\n-----END CERTIFICATE-----\n",
		SerialNumber:   "aa:bb:cc:dd:ee:ff",
		NotAfter:       time.Now().Add(720 * time.Hour),
	}, nil
}

func (f *fakePKI) RevokeCert(_ context.Context, serial string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokedCount++
	f.revokedSerial = serial
	if f.failAt == "revoke" {
		return errors.New("crl boom")
	}
	return nil
}

type fakeAudit struct {
	mu    sync.Mutex
	calls []audit.Entry
}

func (f *fakeAudit) Append(_ context.Context, e audit.Entry) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, e)
	return int64(len(f.calls)), nil
}

// newRefreshTestService wires a Service with in-memory fakes. The pool field
// is left nil — the fakes cover every code path touched by the
// refresh handler.
func newRefreshTestService() (*Service, *fakeRefreshStore, *fakePKI, *fakeAudit) {
	s := &Service{
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		publicURL: "https://example.com",
	}
	store := &fakeRefreshStore{}
	pki := &fakePKI{}
	rec := &fakeAudit{}
	s.SetRefreshStoreForTesting(store)
	s.SetPKIForTesting(pki)
	s.SetAuditForTesting(rec)
	return s, store, pki, rec
}

// ──────────────────────────────────────────────────────────────────
// tests
// ──────────────────────────────────────────────────────────────────

func TestRefreshToken_HappyPath(t *testing.T) {
	t.Parallel()
	svc, store, pki, rec := newRefreshTestService()
	const tenantID = "tenant-A"
	const endpointID = "endpoint-1"
	store.put(tenantID, endpointID, &refreshSnapshot{
		TenantID:   tenantID,
		Hostname:   "dev-box-01",
		CertSerial: "oldserial",
		IsActive:   true,
	})

	csr := generateTestCSR(t, "dev-box-01.personel.internal")
	p := &auth.Principal{UserID: "admin-1", TenantID: tenantID}
	res, err := svc.RefreshToken(context.Background(), p, endpointID, csr, "10.0.0.1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res == nil || res.SerialNumber == "" {
		t.Fatal("empty result")
	}
	// Cert and chain propagate through.
	if res.CertPEM == "" || res.ChainPEM == "" {
		t.Error("cert/chain missing")
	}
	// New serial is the normalized form.
	if res.SerialNumber != "aabbccddeeff" {
		t.Errorf("serial normalization: got %q want aabbccddeeff", res.SerialNumber)
	}
	if pki.signedCount != 1 {
		t.Errorf("expected 1 sign call, got %d", pki.signedCount)
	}
	if pki.revokedCount != 1 || pki.revokedSerial != "oldserial" {
		t.Errorf("expected revoke of oldserial, got count=%d serial=%q", pki.revokedCount, pki.revokedSerial)
	}
	if store.markCalls != 1 {
		t.Errorf("expected 1 mark call, got %d", store.markCalls)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(rec.calls))
	}
	got := rec.calls[0]
	if got.Action != audit.ActionEndpointTokenRefreshed {
		t.Errorf("audit action: %q", got.Action)
	}
	if got.Actor != "admin-1" || got.TenantID != tenantID {
		t.Errorf("audit actor/tenant: %+v", got)
	}
	if got.Target != "endpoint:"+endpointID {
		t.Errorf("audit target: %q", got.Target)
	}
	// Audit MUST include both serials but never the PEM.
	if got.Details["old_serial"] != "oldserial" || got.Details["new_serial"] != "aabbccddeeff" {
		t.Errorf("audit details missing serials: %+v", got.Details)
	}
	for k, v := range got.Details {
		if s, ok := v.(string); ok && len(s) > 0 && (k == "cert_pem" || k == "chain_pem") {
			t.Errorf("audit must not contain PEM material; found %q", k)
		}
	}
}

func TestRefreshToken_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newRefreshTestService()
	// No rows in the store.
	csr := generateTestCSR(t, "ghost.personel.internal")
	p := &auth.Principal{UserID: "admin-1", TenantID: "tenant-A"}
	_, err := svc.RefreshToken(context.Background(), p, "does-not-exist", csr, "")
	if !errors.Is(err, errRefreshNotFound) {
		t.Errorf("want errRefreshNotFound, got %v", err)
	}
}

func TestRefreshToken_CrossTenantIsNotFound(t *testing.T) {
	t.Parallel()
	svc, store, _, _ := newRefreshTestService()
	// Endpoint belongs to tenant A.
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: true,
	})
	// Caller is in tenant B — must get 404, not 403, so we don't leak
	// endpoint-ID existence across tenants.
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "adminB", TenantID: "tenant-B"}
	_, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, "")
	if !errors.Is(err, errRefreshNotFound) {
		t.Errorf("cross-tenant must be not-found, got %v", err)
	}
}

func TestRefreshToken_NotActive(t *testing.T) {
	t.Parallel()
	svc, store, _, _ := newRefreshTestService()
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: false,
	})
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}
	_, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, "")
	if !errors.Is(err, errRefreshNotActive) {
		t.Errorf("want errRefreshNotActive, got %v", err)
	}
}

func TestRefreshToken_RateLimited(t *testing.T) {
	t.Parallel()
	svc, store, pki, _ := newRefreshTestService()
	// First call succeeds; second call immediately after must 429.
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: true,
	})
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}

	if _, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Sanity check: first call actually signed something.
	if pki.signedCount != 1 {
		t.Fatalf("first call didn't sign")
	}
	// Second call within the window must be rate-limited.
	_, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, "")
	if !errors.Is(err, errRefreshRateLimited) {
		t.Errorf("want rate-limited, got %v", err)
	}
	if pki.signedCount != 1 {
		t.Errorf("rate-limited call must not reach Vault; signedCount=%d", pki.signedCount)
	}
}

func TestRefreshToken_RateLimitRelease(t *testing.T) {
	t.Parallel()
	svc, store, pki, _ := newRefreshTestService()
	old := time.Now().Add(-2 * refreshMinInterval)
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: true, LastRefreshAt: &old,
	})
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}
	if _, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, ""); err != nil {
		t.Fatalf("past the window should succeed, got %v", err)
	}
	if pki.signedCount != 1 {
		t.Error("did not sign after window release")
	}
}

func TestRefreshToken_VaultFailure(t *testing.T) {
	t.Parallel()
	svc, store, pki, _ := newRefreshTestService()
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: true,
	})
	pki.failAt = "sign"
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}
	_, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, "")
	if !errors.Is(err, errRefreshVaultFailure) {
		t.Errorf("want errRefreshVaultFailure, got %v", err)
	}
}

func TestRefreshToken_RevokeFailureDoesNotBreakRefresh(t *testing.T) {
	t.Parallel()
	svc, store, pki, rec := newRefreshTestService()
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", CertSerial: "oldserial", IsActive: true,
	})
	pki.failAt = "revoke" // CRL update fails
	csr := generateTestCSR(t, "h.personel.internal")
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}
	res, err := svc.RefreshToken(context.Background(), p, "endpoint-1", csr, "")
	if err != nil {
		t.Fatalf("revoke failure must not fail the refresh, got %v", err)
	}
	if res == nil || res.SerialNumber == "" {
		t.Error("result missing despite revoke failure")
	}
	// Audit still fires.
	if len(rec.calls) != 1 {
		t.Errorf("audit should still fire on partial success, got %d calls", len(rec.calls))
	}
}

func TestRefreshToken_InvalidCSR(t *testing.T) {
	t.Parallel()
	svc, store, _, _ := newRefreshTestService()
	store.put("tenant-A", "endpoint-1", &refreshSnapshot{
		TenantID: "tenant-A", Hostname: "h", IsActive: true,
	})
	p := &auth.Principal{UserID: "admin", TenantID: "tenant-A"}
	_, err := svc.RefreshToken(context.Background(), p, "endpoint-1", "not a real csr", "")
	if !errors.Is(err, errRefreshCSRInvalid) {
		t.Errorf("want errRefreshCSRInvalid, got %v", err)
	}
}

func TestRefreshToken_NilPrincipal(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newRefreshTestService()
	_, err := svc.RefreshToken(context.Background(), nil, "endpoint-1", "csr", "")
	if !errors.Is(err, errRefreshNotFound) {
		t.Errorf("nil principal must be rejected, got %v", err)
	}
}
