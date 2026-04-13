// Package endpoint — dependency interfaces.
//
// The endpoint service is constructed against concrete *pgxpool.Pool,
// *vault.Client, and *audit.Recorder in production (see
// cmd/api/main.go and NewService in service.go). For unit tests that
// cannot spin up Postgres+Vault testcontainers, this file exposes the
// narrow interfaces that the refresh path depends on so a fake
// implementation can stand in.
//
// NOTE: the interfaces are intentionally MINIMAL and scoped to the
// surface the tests cover — they do NOT cover the full agent enroll
// ceremony (that path has integration-test coverage in
// apps/api/test/integration). Adding a new method here should be
// accompanied by updating any fakes in _test.go files.
package endpoint

import (
	"context"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/vault"
)

// auditAppender is the slice of *audit.Recorder used by the refresh
// handler. The concrete *audit.Recorder satisfies this shape.
type auditAppender interface {
	Append(ctx context.Context, e audit.Entry) (int64, error)
}

// pkiSigner is the slice of *vault.Client used by the enroll/refresh
// handlers. The concrete *vault.Client satisfies this shape. The
// production Service embeds a concrete *vault.Client (see service.go)
// and the refresh path uses the vaultPKI() accessor so unit tests can
// install a fake implementation of this interface. The enroll path
// still hits the concrete pointer directly — integration tests cover
// it.
type pkiSigner interface {
	GetEnrollmentRoleID(ctx context.Context) (string, error)
	IssueEnrollmentSecretID(ctx context.Context) (string, error)
	LoginWithAppRole(ctx context.Context, roleID, secretID string) (*vaultapi.Client, error)
	SignAgentCSR(ctx context.Context, signClient *vaultapi.Client, csrPEM, commonName, ttl string) (*vault.IssuedAgentCert, error)
	RevokeCert(ctx context.Context, serial string) error
}

// refreshStore is the postgres surface the refresh path needs. It is
// deliberately narrow — LoadForRefresh fetches everything the handler
// needs in one round trip (serial, tenant, last refresh) AND the
// MarkRefreshed UPDATE sets the rate-limit pivot. A fake implementation
// in endpoint_test.go stands in for unit tests.
type refreshStore interface {
	LoadForRefresh(ctx context.Context, tenantID, endpointID string) (*refreshSnapshot, error)
	MarkRefreshed(ctx context.Context, endpointID, newSerial string, now time.Time) error
}

// refreshSnapshot captures the subset of columns the refresh handler
// reads from the endpoints table.
type refreshSnapshot struct {
	TenantID      string
	Hostname      string
	CertSerial    string
	IsActive      bool
	LastRefreshAt *time.Time
}

// Compile-time assertions. If either fails, the concrete type drifted
// away from the interface and production wiring is broken.
var (
	_ pkiSigner     = (*vault.Client)(nil)
	_ auditAppender = (*audit.Recorder)(nil)
)
