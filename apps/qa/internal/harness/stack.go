// Package harness provides a testcontainers-go based test harness that
// spins up the full Personel server stack for e2e tests.
//
// The stack consists of:
//   - PostgreSQL 15 (metadata store)
//   - ClickHouse 24.x (event time-series)
//   - NATS 2.x with JetStream (event bus)
//   - MinIO (object store)
//   - HashiCorp Vault dev-mode (secrets / PKI)
//   - Gateway (built from apps/gateway/ if binary exists, else skipped)
//   - API (built from apps/api/ if binary exists, else skipped)
//
// The harness is designed to be shared across multiple tests in a package
// via TestMain. Individual tests receive a *Stack from which they can
// obtain connection strings and clients.
//
// Tests that require the full stack are gated behind the QA_INTEGRATION env
// variable to prevent accidental heavy runs in unit-test mode.
package harness

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/vault"
	"github.com/testcontainers/testcontainers-go/wait"
)

// RequireIntegration skips the test if the QA_INTEGRATION env variable is not
// set. All tests that use the full stack must call this at the top.
func RequireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("QA_INTEGRATION") == "" {
		t.Skip("skipping integration test; set QA_INTEGRATION=1 to run")
	}
}

// RequireGateway skips the test if the gateway binary is not present.
// Gateway tests require the compiled gateway binary alongside the harness.
func RequireGateway(t *testing.T) {
	t.Helper()
	if os.Getenv("GATEWAY_ADDR") == "" {
		t.Skip("skipping gateway test; set GATEWAY_ADDR=host:port or QA_INTEGRATION=1 with gateway binary")
	}
}

// Stack holds connection information for all running containers.
type Stack struct {
	// Connection strings and endpoints.
	PostgresDSN     string
	ClickHouseAddr  string
	NATSAddr        string
	MinIOEndpoint   string
	MinIOAccessKey  string
	MinIOSecretKey  string
	VaultAddr       string
	VaultToken      string
	GatewayAddr     string // empty if gateway not started

	// Container references for lifecycle management.
	containers []testcontainers.Container
	log        *slog.Logger
}

// StackOptions configures which components to start.
type StackOptions struct {
	// WithGateway starts the Personel gateway container (requires binary).
	WithGateway bool
	// WithAPI starts the Personel API container.
	WithAPI bool
}

// DefaultStackOptions starts the infrastructure services only.
func DefaultStackOptions() StackOptions {
	return StackOptions{
		WithGateway: false,
		WithAPI:     false,
	}
}

// Start spins up the test stack. Returns a Stack and a cleanup function.
// The cleanup function MUST be called (typically via t.Cleanup).
func Start(ctx context.Context, opts StackOptions) (*Stack, func(), error) {
	stack := &Stack{
		log: slog.Default().With("component", "test_harness"),
	}

	cleanup := func() {
		stack.log.Info("tearing down test stack")
		for i := len(stack.containers) - 1; i >= 0; i-- {
			if err := stack.containers[i].Terminate(ctx); err != nil {
				stack.log.Error("terminate container", "error", err)
			}
		}
	}

	// PostgreSQL
	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("personel_test"),
		postgres.WithUsername("personel"),
		postgres.WithPassword("personel_test_pw"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("start postgres: %w", err)
	}
	stack.containers = append(stack.containers, pgContainer)

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("postgres connection string: %w", err)
	}
	stack.PostgresDSN = pgConnStr
	stack.log.Info("postgres started", "dsn", pgConnStr)

	// ClickHouse
	chContainer, err := clickhouse.RunContainer(ctx,
		testcontainers.WithImage("clickhouse/clickhouse-server:24.3-alpine"),
		clickhouse.WithDatabase("personel_test"),
		clickhouse.WithUsername("default"),
		clickhouse.WithPassword(""),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/ping").
				WithPort("8123/tcp").
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("start clickhouse: %w", err)
	}
	stack.containers = append(stack.containers, chContainer)

	chHost, err := chContainer.Host(ctx)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("clickhouse host: %w", err)
	}
	chPort, err := chContainer.MappedPort(ctx, "9000")
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("clickhouse port: %w", err)
	}
	stack.ClickHouseAddr = fmt.Sprintf("%s:%s", chHost, chPort.Port())
	stack.log.Info("clickhouse started", "addr", stack.ClickHouseAddr)

	// NATS with JetStream
	natsContainer, err := nats.RunContainer(ctx,
		testcontainers.WithImage("nats:2.10-alpine"),
		nats.WithArgument("jetstream", ""),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server is ready").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("start nats: %w", err)
	}
	stack.containers = append(stack.containers, natsContainer)

	natsConnStr, err := natsContainer.ConnectionString(ctx)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("nats connection string: %w", err)
	}
	stack.NATSAddr = natsConnStr
	stack.log.Info("nats started", "addr", stack.NATSAddr)

	// MinIO
	minioContainer, err := minio.RunContainer(ctx,
		testcontainers.WithImage("minio/minio:latest"),
		minio.WithUsername("minioadmin"),
		minio.WithPassword("minioadmin"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("start minio: %w", err)
	}
	stack.containers = append(stack.containers, minioContainer)

	minioEndpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("minio connection string: %w", err)
	}
	stack.MinIOEndpoint = minioEndpoint
	stack.MinIOAccessKey = "minioadmin"
	stack.MinIOSecretKey = "minioadmin"
	stack.log.Info("minio started", "endpoint", stack.MinIOEndpoint)

	// Vault in dev mode.
	vaultContainer, err := vault.RunContainer(ctx,
		testcontainers.WithImage("hashicorp/vault:1.16"),
		vault.WithToken("root-test-token"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Vault server started!").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("start vault: %w", err)
	}
	stack.containers = append(stack.containers, vaultContainer)

	// testcontainers-go Vault module exposes HttpHostAddress (current API).
	// Older versions had ConnectionString; we use HttpHostAddress for compatibility.
	vaultAddr, err := vaultContainer.HttpHostAddress(ctx)
	if err != nil {
		cleanup()
		return nil, cleanup, fmt.Errorf("vault connection string: %w", err)
	}
	stack.VaultAddr = vaultAddr
	stack.VaultToken = "root-test-token"
	stack.log.Info("vault started", "addr", stack.VaultAddr)

	// Optionally start gateway and API containers if binaries are available.
	if opts.WithGateway {
		if addr := os.Getenv("GATEWAY_ADDR"); addr != "" {
			stack.GatewayAddr = addr
		} else {
			stack.log.Warn("WithGateway=true but GATEWAY_ADDR not set; gateway not started")
		}
	}

	stack.log.Info("test stack ready",
		"postgres", stack.PostgresDSN != "",
		"clickhouse", stack.ClickHouseAddr != "",
		"nats", stack.NATSAddr != "",
		"minio", stack.MinIOEndpoint != "",
		"vault", stack.VaultAddr != "",
		"gateway", stack.GatewayAddr != "",
	)

	return stack, cleanup, nil
}

// MustStart is like Start but calls t.Fatal on error and registers cleanup.
func MustStart(t *testing.T, opts StackOptions) *Stack {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	stack, cleanup, err := Start(ctx, opts)
	if err != nil {
		cleanup()
		t.Fatalf("start test stack: %v", err)
	}
	t.Cleanup(cleanup)
	return stack
}
