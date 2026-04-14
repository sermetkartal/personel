// Package config loads and validates gateway and enricher configuration
// from YAML files and environment variable overrides using koanf.
package config

import (
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// GatewayConfig holds the full runtime configuration for the ingest gateway.
type GatewayConfig struct {
	GRPC        GRPCConfig        `koanf:"grpc"`
	NATS        NATSConfig        `koanf:"nats"`
	Postgres    PostgresConfig    `koanf:"postgres"`
	Vault       VaultConfig       `koanf:"vault"`
	Observ      ObservConfig      `koanf:"observability"`
	RateLimit   RateLimitConfig   `koanf:"rate_limit"`
	Backpressure BackpressureConfig `koanf:"backpressure"`
	Heartbeat   HeartbeatConfig   `koanf:"heartbeat"`
	LiveView    LiveViewConfig    `koanf:"live_view"`
	ServerVersion string          `koanf:"server_version"`
}

// EnricherConfig holds configuration for the enricher binary.
type EnricherConfig struct {
	NATS       NATSConfig       `koanf:"nats"`
	ClickHouse ClickHouseConfig `koanf:"clickhouse"`
	MinIO      MinIOConfig      `koanf:"minio"`
	Postgres   PostgresConfig   `koanf:"postgres"`
	Observ     ObservConfig     `koanf:"observability"`
	Batch      BatchConfig      `koanf:"batch"`
	GeoIP      GeoIPConfig      `koanf:"geoip"`
}

// GeoIPConfig configures the optional MaxMind GeoLite2 lookup used by
// the enricher for server-side network event geolocation. When
// MMDBPath is empty (the default), geo enrichment is disabled and the
// enricher continues without it — no customer-facing failure.
type GeoIPConfig struct {
	// MMDBPath is the absolute path to a GeoLite2-City.mmdb file on
	// the enricher host. Populated by the weekly
	// personel-maxmind-download.timer systemd unit; see
	// `infra/scripts/maxmind-download.sh` and
	// `docs/operations/maxmind-setup.md`.
	MMDBPath string `koanf:"mmdb_path"`
}

// GRPCConfig configures the gRPC server.
type GRPCConfig struct {
	ListenAddr      string        `koanf:"listen_addr"`
	TLSCertFile     string        `koanf:"tls_cert_file"`
	TLSKeyFile      string        `koanf:"tls_key_file"`
	TLSClientCAFile string        `koanf:"tls_client_ca_file"`
	MaxRecvMsgSize  int           `koanf:"max_recv_msg_size"`
	GracefulStop    time.Duration `koanf:"graceful_stop_timeout"`
}

// NATSConfig configures the NATS JetStream connection.
type NATSConfig struct {
	URLs           []string      `koanf:"urls"`
	CredsFile      string        `koanf:"creds_file"`
	ConnectTimeout time.Duration `koanf:"connect_timeout"`
	MaxReconnect   int           `koanf:"max_reconnect"`
	// PublishTimeout is how long to wait for a JetStream publish ACK.
	PublishTimeout time.Duration `koanf:"publish_timeout"`
}

// PostgresConfig configures the Postgres connection pool.
type PostgresConfig struct {
	DSN          string        `koanf:"dsn"`
	MaxConns     int           `koanf:"max_conns"`
	MinConns     int           `koanf:"min_conns"`
	ConnTimeout  time.Duration `koanf:"conn_timeout"`
	QueryTimeout time.Duration `koanf:"query_timeout"`
}

// VaultConfig configures the Vault client used for cert serial deny-list reads
// and agent cert issuance (CsrSubmit flow).
type VaultConfig struct {
	Addr      string `koanf:"addr"`
	RoleID    string `koanf:"role_id"`
	SecretID  string `koanf:"secret_id"`
	Namespace string `koanf:"namespace"`
	// CACert is the path to the Vault server CA certificate.
	CACert string `koanf:"ca_cert"`
}

// ObservConfig configures observability (OTel, Prometheus).
type ObservConfig struct {
	OTLPEndpoint    string `koanf:"otlp_endpoint"`
	PrometheusAddr  string `koanf:"prometheus_addr"`
	LogLevel        string `koanf:"log_level"`
	ServiceName     string `koanf:"service_name"`
}

// RateLimitConfig configures per-tenant and per-endpoint rate limiters.
type RateLimitConfig struct {
	// PerEndpointEventsPerSec is the token bucket rate per endpoint.
	PerEndpointEventsPerSec float64 `koanf:"per_endpoint_events_per_sec"`
	// PerEndpointBurst is the burst allowance per endpoint.
	PerEndpointBurst int `koanf:"per_endpoint_burst"`
	// PerTenantEventsPerSec is the aggregate rate limit for a tenant.
	PerTenantEventsPerSec float64 `koanf:"per_tenant_events_per_sec"`
	// PerTenantBurst is the aggregate burst for a tenant.
	PerTenantBurst int `koanf:"per_tenant_burst"`
}

// BackpressureConfig configures the ACK windowing scheme.
type BackpressureConfig struct {
	// MaxUnackedBatches is the maximum number of batches in-flight before
	// the stream handler stops reading new EventBatch messages.
	MaxUnackedBatches int `koanf:"max_unacked_batches"`
}

// HeartbeatConfig configures the Flow 7 heartbeat monitor.
type HeartbeatConfig struct {
	// DegradedAfter is the gap after which an endpoint is marked degraded.
	DegradedAfter time.Duration `koanf:"degraded_after"`
	// OfflineAfter is the gap for the offline transition.
	OfflineAfter time.Duration `koanf:"offline_after"`
	// OfflineExtendedAfter is the gap for the offline_extended transition.
	OfflineExtendedAfter time.Duration `koanf:"offline_extended_after"`
	// SilenceAlertAfter triggers a dpo.endpoint_silence_alert.
	SilenceAlertAfter time.Duration `koanf:"silence_alert_after"`
	// CheckInterval is how often the monitor runs its sweep.
	CheckInterval time.Duration `koanf:"check_interval"`
}

// LiveViewConfig configures the live view control message router.
type LiveViewConfig struct {
	LiveKitServerURL string `koanf:"livekit_server_url"`
	LiveKitAPIKey    string `koanf:"livekit_api_key"`
	LiveKitAPISecret string `koanf:"livekit_api_secret"`
}

// ClickHouseConfig configures the ClickHouse client.
type ClickHouseConfig struct {
	Addrs    []string `koanf:"addrs"`
	Database string   `koanf:"database"`
	Username string   `koanf:"username"`
	Password string   `koanf:"password"`
	// AsyncInsert enables ClickHouse async_insert mode for high-throughput.
	AsyncInsert bool `koanf:"async_insert"`
}

// MinIOConfig configures the MinIO client.
type MinIOConfig struct {
	Endpoint  string `koanf:"endpoint"`
	AccessKey string `koanf:"access_key"`
	SecretKey string `koanf:"secret_key"`
	UseSSL    bool   `koanf:"use_ssl"`
	Region    string `koanf:"region"`
}

// BatchConfig configures the enricher batcher.
type BatchConfig struct {
	// MaxSize is the maximum number of events per ClickHouse batch.
	MaxSize int `koanf:"max_size"`
	// FlushInterval is the maximum time before an incomplete batch is flushed.
	FlushInterval time.Duration `koanf:"flush_interval"`
}

// LoadGateway loads the gateway configuration from a YAML file with
// environment variable overrides. Environment variables are prefixed with
// PERSONEL_GATEWAY_ and use double underscores as path delimiters.
func LoadGateway(cfgFile string) (*GatewayConfig, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(cfgFile), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load file %s: %w", cfgFile, err)
	}
	if err := k.Load(env.Provider("PERSONEL_GATEWAY_", ".", func(s string) string {
		return s
	}), nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}
	cfg := &GatewayConfig{}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return cfg, cfg.validate()
}

// LoadEnricher loads enricher configuration similarly.
func LoadEnricher(cfgFile string) (*EnricherConfig, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(cfgFile), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load file %s: %w", cfgFile, err)
	}
	if err := k.Load(env.Provider("PERSONEL_ENRICHER_", ".", func(s string) string {
		return s
	}), nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}
	cfg := &EnricherConfig{}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return cfg, cfg.validate()
}

func (c *GatewayConfig) validate() error {
	if c.GRPC.ListenAddr == "" {
		return fmt.Errorf("config: grpc.listen_addr is required")
	}
	if c.GRPC.TLSCertFile == "" {
		return fmt.Errorf("config: grpc.tls_cert_file is required")
	}
	if c.GRPC.TLSKeyFile == "" {
		return fmt.Errorf("config: grpc.tls_key_file is required")
	}
	if c.GRPC.TLSClientCAFile == "" {
		return fmt.Errorf("config: grpc.tls_client_ca_file is required")
	}
	if len(c.NATS.URLs) == 0 {
		return fmt.Errorf("config: nats.urls is required")
	}
	if c.Postgres.DSN == "" {
		return fmt.Errorf("config: postgres.dsn is required")
	}
	if c.Vault.Addr == "" {
		return fmt.Errorf("config: vault.addr is required")
	}
	return nil
}

func (c *EnricherConfig) validate() error {
	if len(c.NATS.URLs) == 0 {
		return fmt.Errorf("config: nats.urls is required")
	}
	if len(c.ClickHouse.Addrs) == 0 {
		return fmt.Errorf("config: clickhouse.addrs is required")
	}
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("config: minio.endpoint is required")
	}
	return nil
}
