//go:build integration

// Package integration contains end-to-end integration tests that spin up real
// infrastructure via testcontainers-go.
//
// Run with:
//
//	go test -tags=integration -race -timeout=300s ./test/integration/...
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/personel/api/internal/audit"
	cfgpkg "github.com/personel/api/internal/config"
	pginternal "github.com/personel/api/internal/postgres"
)

// testLogger returns a structured logger suitable for test output.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// testDB spins up a Postgres 16 container, runs all migrations, and returns:
//   - a pgxpool connected to it
//   - a cleanup function (register with t.Cleanup)
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "personel_test",
			"POSTGRES_USER":     "personel",
			"POSTGRES_PASSWORD": "personel",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "start postgres container")
	t.Cleanup(func() {
		if err := ctr.Terminate(ctx); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	dsn := fmt.Sprintf("postgres://personel:personel@%s:%s/personel_test?sslmode=disable", host, port.Port())

	log := testLogger(t)
	require.NoError(t, pginternal.RunMigrations(dsn, log), "run migrations")

	pgCfg := &cfgpkg.PostgresConfig{
		DSN:             dsn,
		MaxConns:        5,
		MinConns:        1,
		MaxConnLifetime: time.Minute,
		MaxConnIdleTime: 30 * time.Second,
	}

	pool, err := pginternal.NewPool(ctx, pgCfg, log)
	require.NoError(t, err, "open pool")
	t.Cleanup(pool.Close)

	return pool
}

// testRecorder returns a real Recorder wired to pool.
func testRecorder(pool *pgxpool.Pool, log *slog.Logger) *audit.Recorder {
	return audit.NewRecorder(pool, log)
}

// seedTenant inserts a tenant and returns its UUID.
func seedTenant(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants(name, slug) VALUES($1,$2) RETURNING id`,
		slug+" Corp", slug,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedUser inserts a user and returns their UUID.
func seedUser(t *testing.T, pool *pgxpool.Pool, tenantID, role, email string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO users(tenant_id, keycloak_sub, username, email, role)
		 VALUES($1, $2, $3, $4, $5) RETURNING id`,
		tenantID,
		fmt.Sprintf("kc-%s-%s", role, email),
		fmt.Sprintf("%s-%s", role, email),
		email,
		role,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// mustUUID returns a deterministic UUID for tests without importing uuid packages.
func mustUUID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}
