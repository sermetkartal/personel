// Package config — unit tests for Load/validate/defaults.
// Tests do not hit any real external service; they use temporary YAML files
// and environment variable overrides to cover all validation branches.
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeYAML writes content to a temp file and returns the path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// minimalYAML returns a YAML string with all required fields populated.
func minimalYAML() string {
	return `
postgres:
  dsn: "postgres://personel:personel@localhost:5432/personel?sslmode=disable"
clickhouse:
  addr: "localhost:9000"
minio:
  endpoint: "localhost:9000"
nats:
  url: "nats://localhost:4222"
vault:
  addr: "http://localhost:8200"
  app_role_id: "test-role-id"
keycloak:
  issuer_url: "http://localhost:8080/realms/personel"
  client_id: "personel-admin-api"
livekit:
  host: "localhost:7880"
  api_key: "test-key"
`
}

// TestLoad_ValidMinimalConfig verifies a correctly specified config loads without error.
func TestLoad_ValidMinimalConfig(t *testing.T) {
	path := writeYAML(t, minimalYAML())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should succeed with minimal valid config, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg must not be nil on success")
	}
}

// TestLoad_Defaults verifies that missing optional fields get correct defaults.
func TestLoad_Defaults(t *testing.T) {
	path := writeYAML(t, minimalYAML())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr default: got %q, want %q", cfg.HTTP.Addr, ":8080")
	}
	if cfg.HTTP.ReadTimeout != 30*time.Second {
		t.Errorf("HTTP.ReadTimeout default: got %v, want 30s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 60*time.Second {
		t.Errorf("HTTP.WriteTimeout default: got %v, want 60s", cfg.HTTP.WriteTimeout)
	}
	if cfg.Postgres.MaxConns != 20 {
		t.Errorf("Postgres.MaxConns default: got %d, want 20", cfg.Postgres.MaxConns)
	}
	if cfg.NATS.PolicySubject != "policy.v1" {
		t.Errorf("NATS.PolicySubject default: got %q, want %q", cfg.NATS.PolicySubject, "policy.v1")
	}
	if cfg.NATS.LiveViewSubject != "liveview.v1" {
		t.Errorf("NATS.LiveViewSubject default: got %q, want %q", cfg.NATS.LiveViewSubject, "liveview.v1")
	}
	if cfg.LiveKit.MaxSessionDuration != 15*time.Minute {
		t.Errorf("LiveKit.MaxSessionDuration default: got %v, want 15m", cfg.LiveKit.MaxSessionDuration)
	}
	if cfg.Observ.MetricsPath != "/metrics" {
		t.Errorf("Observ.MetricsPath default: got %q, want /metrics", cfg.Observ.MetricsPath)
	}
	if cfg.RateLimit.RequestsPerMinute != 300 {
		t.Errorf("RateLimit.RequestsPerMinute default: got %d, want 300", cfg.RateLimit.RequestsPerMinute)
	}
}

// TestLoad_MissingPostgresDSN verifies that missing postgres.dsn returns an error.
func TestLoad_MissingPostgresDSN(t *testing.T) {
	yaml := `
clickhouse:
  addr: "localhost:9000"
minio:
  endpoint: "localhost:9000"
nats:
  url: "nats://localhost:4222"
vault:
  addr: "http://localhost:8200"
  app_role_id: "role-id"
keycloak:
  issuer_url: "http://localhost:8080/realms/personel"
  client_id: "personel-admin-api"
livekit:
  host: "localhost:7880"
  api_key: "test-key"
`
	path := writeYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must return error when postgres.dsn is missing")
	}
}

// TestLoad_MissingClickHouseAddr verifies that missing clickhouse.addr returns an error.
func TestLoad_MissingClickHouseAddr(t *testing.T) {
	yaml := `
postgres:
  dsn: "postgres://x@localhost/y"
minio:
  endpoint: "localhost:9000"
nats:
  url: "nats://localhost:4222"
vault:
  addr: "http://localhost:8200"
  app_role_id: "role-id"
keycloak:
  issuer_url: "http://localhost:8080/realms/personel"
  client_id: "personel-admin-api"
livekit:
  host: "localhost:7880"
  api_key: "test-key"
`
	path := writeYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must return error when clickhouse.addr is missing")
	}
}

// TestLoad_MissingKeycloakIssuerURL verifies keycloak.issuer_url is required.
func TestLoad_MissingKeycloakIssuerURL(t *testing.T) {
	yaml := `
postgres:
  dsn: "postgres://x@localhost/y"
clickhouse:
  addr: "localhost:9000"
minio:
  endpoint: "localhost:9000"
nats:
  url: "nats://localhost:4222"
vault:
  addr: "http://localhost:8200"
  app_role_id: "role-id"
keycloak:
  client_id: "personel-admin-api"
livekit:
  host: "localhost:7880"
  api_key: "test-key"
`
	path := writeYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must return error when keycloak.issuer_url is missing")
	}
}

// TestLoad_LiveKitMaxSessionDurationHardCap verifies that >60m duration is rejected.
func TestLoad_LiveKitMaxSessionDurationHardCap(t *testing.T) {
	yaml := minimalYAML() + `
livekit:
  host: "localhost:7880"
  api_key: "test-key"
  max_session_duration: 61m
`
	path := writeYAML(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must reject livekit.max_session_duration > 60m")
	}
}

// TestLoad_EnvVarOverride verifies that PERSONEL_* environment variables override YAML.
func TestLoad_EnvVarOverride(t *testing.T) {
	path := writeYAML(t, minimalYAML())

	// Override HTTP address via environment variable.
	t.Setenv("PERSONEL_HTTP_ADDR", ":9999")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.Addr != ":9999" {
		t.Errorf("env override: HTTP.Addr got %q, want %q", cfg.HTTP.Addr, ":9999")
	}
}

// TestLoad_NonExistentFile verifies that a missing config file returns an error.
func TestLoad_NonExistentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does_not_exist.yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load must return error for missing config file")
	}
}

// TestValidate_MissingNATSURL verifies that missing nats.url is caught.
func TestValidate_MissingNATSURL(t *testing.T) {
	cfg := &Config{
		Postgres:   PostgresConfig{DSN: "postgres://x@localhost/y"},
		ClickHouse: ClickHouseConfig{Addr: "localhost:9000"},
		MinIO:      MinIOConfig{Endpoint: "localhost:9000"},
		NATS:       NATSConfig{URL: ""}, // missing
		Vault:      VaultConfig{Addr: "http://localhost:8200", AppRoleID: "role"},
		Keycloak:   KeycloakConfig{IssuerURL: "http://localhost/realms/x", ClientID: "c"},
		LiveKit:    LiveKitConfig{Host: "localhost:7880", APIKey: "k", MaxSessionDuration: 15 * time.Minute},
	}
	if err := validate(cfg); err == nil {
		t.Error("validate must fail when nats.url is empty")
	}
}

// TestValidate_MissingVaultAddr verifies that missing vault.addr is caught.
func TestValidate_MissingVaultAddr(t *testing.T) {
	cfg := &Config{
		Postgres:   PostgresConfig{DSN: "postgres://x@localhost/y"},
		ClickHouse: ClickHouseConfig{Addr: "localhost:9000"},
		MinIO:      MinIOConfig{Endpoint: "localhost:9000"},
		NATS:       NATSConfig{URL: "nats://localhost:4222"},
		Vault:      VaultConfig{Addr: ""}, // missing
		Keycloak:   KeycloakConfig{IssuerURL: "http://localhost/realms/x", ClientID: "c"},
		LiveKit:    LiveKitConfig{Host: "localhost:7880", APIKey: "k", MaxSessionDuration: 15 * time.Minute},
	}
	if err := validate(cfg); err == nil {
		t.Error("validate must fail when vault.addr is empty")
	}
}
