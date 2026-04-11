// Package config loads and validates livrec-service configuration.
// All sensitive values (secrets, credentials) are sourced from environment
// variables only — never from configuration files committed to source control.
// Per ADR 0019: no hardcoded secrets.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all livrec-service runtime configuration.
type Config struct {
	// HTTP server
	ListenAddr string // default :8080
	// Vault
	VaultAddr             string
	VaultRoleID           string
	VaultSecretID         string
	VaultCACert           string
	VaultLVMKPath         string // e.g. transit/derive/lvmk (per ADR 0019)
	VaultSignerPath       string // transit/sign/control-plane-signer
	VaultRenewInterval    time.Duration
	// MinIO
	MinIOEndpoint   string
	MinIOAccessKey  string
	MinIOSecretKey  string
	MinIOUseTLS     bool
	MinIOBucket     string // live-view-recordings
	// Postgres (for audit forwarding only — livrec has no own Postgres schema)
	DatabaseURL string
	// Admin API — used for dual-control approval checks and audit forwarding
	AdminAPIBaseURL string
	AdminAPIToken   string // internal service-to-service bearer token
	// Retention
	DefaultRetentionDays int // 30
	// Observability
	LogLevel   string
	OTelTarget string
}

// Load reads configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:           envOrDefault("LIVREC_LISTEN_ADDR", ":8080"),
		VaultAddr:            os.Getenv("VAULT_ADDR"),
		VaultRoleID:          os.Getenv("VAULT_ROLE_ID"),
		VaultSecretID:        os.Getenv("VAULT_SECRET_ID"),
		VaultCACert:          os.Getenv("VAULT_CACERT"),
		VaultLVMKPath:        envOrDefault("VAULT_LVMK_PATH", "transit/derive/lvmk"),
		VaultSignerPath:      envOrDefault("VAULT_SIGNER_PATH", "transit/sign/control-plane-signer"),
		VaultRenewInterval:   durationOrDefault("VAULT_RENEW_INTERVAL", 5*time.Minute),
		MinIOEndpoint:        os.Getenv("MINIO_ENDPOINT"),
		MinIOAccessKey:       os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:       os.Getenv("MINIO_SECRET_KEY"),
		MinIOUseTLS:          os.Getenv("MINIO_USE_TLS") == "true",
		MinIOBucket:          envOrDefault("MINIO_BUCKET", "live-view-recordings"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		AdminAPIBaseURL:      os.Getenv("ADMIN_API_BASE_URL"),
		AdminAPIToken:        os.Getenv("ADMIN_API_INTERNAL_TOKEN"),
		DefaultRetentionDays: intOrDefault("LIVREC_RETENTION_DAYS", 30),
		LogLevel:             envOrDefault("LOG_LEVEL", "info"),
		OTelTarget:           os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}

	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

func (c *Config) validate() error {
	var errs []string
	if c.VaultAddr == "" {
		errs = append(errs, "VAULT_ADDR is required")
	}
	if c.VaultRoleID == "" {
		errs = append(errs, "VAULT_ROLE_ID is required")
	}
	if c.VaultSecretID == "" {
		errs = append(errs, "VAULT_SECRET_ID is required")
	}
	if c.MinIOEndpoint == "" {
		errs = append(errs, "MINIO_ENDPOINT is required")
	}
	if c.MinIOAccessKey == "" {
		errs = append(errs, "MINIO_ACCESS_KEY is required")
	}
	if c.MinIOSecretKey == "" {
		errs = append(errs, "MINIO_SECRET_KEY is required")
	}
	if c.AdminAPIBaseURL == "" {
		errs = append(errs, "ADMIN_API_BASE_URL is required")
	}
	if c.AdminAPIToken == "" {
		errs = append(errs, "ADMIN_API_INTERNAL_TOKEN is required")
	}
	if c.DefaultRetentionDays < 1 {
		errs = append(errs, "LIVREC_RETENTION_DAYS must be >= 1")
	}
	if len(errs) > 0 {
		return errors.New(joinStrings(errs, "; "))
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func durationOrDefault(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
