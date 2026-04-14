// Package config loads and validates the Admin API configuration via koanf.
// Sources: api.yaml < environment variables (PERSONEL_ prefix).
package config

import (
	"strings"
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config is the validated root configuration.
type Config struct {
	HTTP        HTTPConfig        `koanf:"http"`
	Server      ServerConfig      `koanf:"server"`
	Postgres    PostgresConfig    `koanf:"postgres"`
	ClickHouse  ClickHouseConfig  `koanf:"clickhouse"`
	MinIO       MinIOConfig       `koanf:"minio"`
	NATS        NATSConfig        `koanf:"nats"`
	Vault       VaultConfig       `koanf:"vault"`
	Keycloak    KeycloakConfig    `koanf:"keycloak"`
	Observ      ObservConfig      `koanf:"observability"`
	LiveKit     LiveKitConfig     `koanf:"livekit"`
	OpenSearch  OpenSearchConfig  `koanf:"opensearch"`
	Tenant      TenantConfig      `koanf:"tenant"`
	RateLimit   RateLimitConfig   `koanf:"rate_limit"`
}

// OpenSearchConfig holds the connection parameters for the search tier.
// Consumed by apps/api/internal/search which exposes /v1/search/audit
// and /v1/search/events for the admin console full-text search UI.
//
// If Enabled is false or Addr is empty the API boots normally; the
// search routes are still mounted but return 503 Service Unavailable.
type OpenSearchConfig struct {
	Addr     string        `koanf:"addr"`     // e.g. "http://opensearch:9200"
	Enabled  bool          `koanf:"enabled"`  // feature flag
	Username string        `koanf:"username"` // optional basic auth
	Password string        `koanf:"password"`
	Timeout  time.Duration `koanf:"timeout"`  // default 10s
}

type HTTPConfig struct {
	Addr            string        `koanf:"addr"`             // default ":8080"
	ReadTimeout     time.Duration `koanf:"read_timeout"`     // default 30s
	WriteTimeout    time.Duration `koanf:"write_timeout"`    // default 60s
	IdleTimeout     time.Duration `koanf:"idle_timeout"`     // default 120s
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"` // default 15s
	CORSOrigins     []string      `koanf:"cors_origins"`
	TLSCert         string        `koanf:"tls_cert"`
	TLSKey          string        `koanf:"tls_key"`
}

// ServerConfig holds public-facing URLs that the API embeds in tokens and
// enrollment responses. Both must be reachable by Windows agents during
// the enrollment ceremony.
//
// PublicURL is the authoritative base URL for this Admin API, used as the
// "enroll_url" embedded inside the opaque enrollment token the operator
// hands to the agent installer.
//
// GatewayURL is the gateway endpoint the agent will dial after a successful
// enroll. Format: "tls://host:port".
type ServerConfig struct {
	PublicURL  string `koanf:"public_url"`  // e.g. "http://192.168.5.44:8000"
	GatewayURL string `koanf:"gateway_url"` // e.g. "tls://192.168.5.44:9443"
	// InternalToken is a shared secret used by the in-cluster gateway
	// to authenticate calls to /v1/internal/* endpoints (e.g. the
	// command acknowledgement path used by Faz 6 #64/#65 remote
	// commands). Rotated out-of-band via systemd credential injection;
	// must be >=32 bytes when set. Empty value disables the internal
	// route group (gateway can't call back) — safe default for dev.
	InternalToken string `koanf:"internal_token"`
}

type PostgresConfig struct {
	DSN             string        `koanf:"dsn"`             // postgres://...
	MaxConns        int32         `koanf:"max_conns"`       // default 20
	MinConns        int32         `koanf:"min_conns"`       // default 2
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime"` // default 1h
	MaxConnIdleTime time.Duration `koanf:"max_conn_idle_time"` // default 30m
	MigrationsDir   string        `koanf:"migrations_dir"`  // default "internal/postgres/migrations"
}

type ClickHouseConfig struct {
	Addr     string `koanf:"addr"`     // host:port
	Database string `koanf:"database"` // default "personel"
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	TLSEnable bool  `koanf:"tls_enable"`
}

type MinIOConfig struct {
	Endpoint        string `koanf:"endpoint"` // host:port
	AccessKeyID     string `koanf:"access_key_id"`
	SecretAccessKey string `koanf:"secret_access_key"`
	UseSSL          bool   `koanf:"use_ssl"`
	BucketScreenshots string `koanf:"bucket_screenshots"`
	BucketDSR         string `koanf:"bucket_dsr"`
	BucketDestruction string `koanf:"bucket_destruction"`
	PresignTTL        time.Duration `koanf:"presign_ttl"` // default 60s
	// Audit WORM sink (ADR 0014) — separate service account with PutObject +
	// GetObject only. No DeleteObject, no bypass-governance-retention.
	AuditSinkAccessKey string `koanf:"audit_sink_access_key"`
	AuditSinkSecretKey string `koanf:"audit_sink_secret_key"`
}

type NATSConfig struct {
	URL            string        `koanf:"url"`           // nats://...
	CredsFile      string        `koanf:"creds_file"`    // optional NATS creds
	ConnectTimeout time.Duration `koanf:"connect_timeout"` // default 5s
	PolicySubject  string        `koanf:"policy_subject"`  // default "policy.v1"
	LiveViewSubject string       `koanf:"live_view_subject"` // default "liveview.v1"
}

type VaultConfig struct {
	Addr            string `koanf:"addr"`             // https://vault.internal:8200
	AppRoleID       string `koanf:"app_role_id"`
	AppRoleSecretID string `koanf:"app_role_secret_id"` // injected via systemd creds
	TLSCACert       string `koanf:"tls_ca_cert"`
	// Transit key paths
	ControlPlaneSigningKey string `koanf:"control_plane_signing_key"` // transit/keys/control-plane-signing
	TokenRenewInterval     time.Duration `koanf:"token_renew_interval"` // default 10m
}

type KeycloakConfig struct {
	IssuerURL string `koanf:"issuer_url"` // https://keycloak.internal/realms/personel
	ClientID  string `koanf:"client_id"`  // personel-admin-api
}

type ObservConfig struct {
	MetricsPath    string `koanf:"metrics_path"`    // default "/metrics"
	TracingEnabled bool   `koanf:"tracing_enabled"`
	TracingEndpoint string `koanf:"tracing_endpoint"` // OTLP gRPC endpoint
	ServiceName    string `koanf:"service_name"`    // default "personel-admin-api"
	ServiceVersion string `koanf:"service_version"`
}

type LiveKitConfig struct {
	Host      string `koanf:"host"`      // livekit.internal:7880
	APIKey    string `koanf:"api_key"`
	APISecret string `koanf:"api_secret"`
	// MaxSessionDuration default 15m, hard cap 60m
	MaxSessionDuration time.Duration `koanf:"max_session_duration"`
	ApprovalTimeout    time.Duration `koanf:"approval_timeout"` // default 10m
}

type TenantConfig struct {
	// Default tenant ID used in single-tenant MVP mode.
	DefaultTenantID string `koanf:"default_tenant_id"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `koanf:"requests_per_minute"` // per-IP, default 300
	BurstSize         int `koanf:"burst_size"`          // per-IP, default 50
	// TenantRequestsPerMinute is the second-layer per-tenant token-bucket
	// rate (Faz 6 #71). Applied AFTER AuthMiddleware so the principal is
	// populated. Must be >= 10× RequestsPerMinute because a single tenant
	// has many users / sessions concurrently; a sanity check in validate()
	// rejects configs that would gate tenants tighter than single IPs.
	// A value of 0 disables the per-tenant layer (defensive default when
	// upgrading from an older api.yaml that does not declare it).
	TenantRequestsPerMinute int `koanf:"tenant_requests_per_minute"` // default 6000
	TenantBurst             int `koanf:"tenant_burst"`               // default 200
}

// Load reads config from path, then overrides with PERSONEL_ env vars.
// All required fields must be non-empty after loading or Load returns an error.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load file %s: %w", path, err)
	}

	// Override with PERSONEL_* env vars, e.g. PERSONEL_HTTP_ADDR -> http.addr
	if err := k.Load(env.Provider("PERSONEL_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "PERSONEL_")), "_", ".")
	}), nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}

	cfg := defaults()
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		HTTP: HTTPConfig{
			Addr:            ":8080",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    60 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		Server: ServerConfig{
			PublicURL:  "http://localhost:8000",
			GatewayURL: "tls://localhost:9443",
		},
		Postgres: PostgresConfig{
			MaxConns:        20,
			MinConns:        2,
			MaxConnLifetime: time.Hour,
			MaxConnIdleTime: 30 * time.Minute,
		},
		MinIO: MinIOConfig{
			BucketScreenshots: "screenshots",
			BucketDSR:         "dsr-responses",
			BucketDestruction: "destruction-reports",
			PresignTTL:        60 * time.Second,
		},
		NATS: NATSConfig{
			ConnectTimeout:  5 * time.Second,
			PolicySubject:   "policy.v1",
			LiveViewSubject: "liveview.v1",
		},
		Vault: VaultConfig{
			TokenRenewInterval: 10 * time.Minute,
		},
		Observ: ObservConfig{
			MetricsPath: "/metrics",
			ServiceName: "personel-admin-api",
		},
		LiveKit: LiveKitConfig{
			MaxSessionDuration: 15 * time.Minute,
			ApprovalTimeout:    10 * time.Minute,
		},
		OpenSearch: OpenSearchConfig{
			Enabled: false,
			Timeout: 10 * time.Second,
		},
		RateLimit: RateLimitConfig{
			RequestsPerMinute:       300,
			BurstSize:               50,
			TenantRequestsPerMinute: 6000,
			TenantBurst:             200,
		},
	}
}

func validate(c *Config) error {
	if c.Postgres.DSN == "" {
		return fmt.Errorf("config: postgres.dsn is required")
	}
	if c.ClickHouse.Addr == "" {
		return fmt.Errorf("config: clickhouse.addr is required")
	}
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("config: minio.endpoint is required")
	}
	if c.NATS.URL == "" {
		return fmt.Errorf("config: nats.url is required")
	}
	if c.Vault.Addr == "" {
		return fmt.Errorf("config: vault.addr is required")
	}
	if c.Vault.AppRoleID == "" {
		return fmt.Errorf("config: vault.app_role_id is required")
	}
	if c.Keycloak.IssuerURL == "" {
		return fmt.Errorf("config: keycloak.issuer_url is required")
	}
	if c.Keycloak.ClientID == "" {
		return fmt.Errorf("config: keycloak.client_id is required")
	}
	if c.LiveKit.Host == "" {
		return fmt.Errorf("config: livekit.host is required")
	}
	if c.LiveKit.APIKey == "" {
		return fmt.Errorf("config: livekit.api_key is required")
	}
	if c.LiveKit.MaxSessionDuration > 60*time.Minute {
		return fmt.Errorf("config: livekit.max_session_duration exceeds hard cap of 60 minutes")
	}
	// OpenSearch is optional: if the operator enables it they must
	// also supply an address. If disabled we skip the check entirely
	// and the API will mount /v1/search handlers in degraded mode.
	if c.OpenSearch.Enabled {
		if c.OpenSearch.Addr == "" {
			return fmt.Errorf("config: opensearch.addr is required when opensearch.enabled=true")
		}
		if !strings.HasPrefix(c.OpenSearch.Addr, "http://") && !strings.HasPrefix(c.OpenSearch.Addr, "https://") {
			return fmt.Errorf("config: opensearch.addr must start with http:// or https:// (got %q) — set via PERSONEL_OPENSEARCH_ADDR", c.OpenSearch.Addr)
		}
		if c.OpenSearch.Timeout <= 0 {
			return fmt.Errorf("config: opensearch.timeout must be > 0 when opensearch.enabled=true")
		}
	}
	// Per-tenant rate limit (Faz 6 #71). A non-zero tenant limit must be
	// >= 10× the per-IP limit because a tenant aggregates many users /
	// sessions / devices. Operators who want to disable the layer can
	// set tenant_requests_per_minute: 0 explicitly.
	if c.RateLimit.TenantRequestsPerMinute > 0 {
		if c.RateLimit.TenantRequestsPerMinute < 10*c.RateLimit.RequestsPerMinute {
			return fmt.Errorf("config: rate_limit.tenant_requests_per_minute (%d) must be >= 10× rate_limit.requests_per_minute (%d)",
				c.RateLimit.TenantRequestsPerMinute, c.RateLimit.RequestsPerMinute)
		}
		if c.RateLimit.TenantBurst <= 0 {
			return fmt.Errorf("config: rate_limit.tenant_burst must be > 0 when tenant_requests_per_minute > 0")
		}
	}
	return nil
}
