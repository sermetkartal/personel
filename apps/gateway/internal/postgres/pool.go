// Package postgres provides a thin wrapper around pgx/v5 connection pool
// for metadata reads: tenant/endpoint lookup, key version checks, deny-list.
package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/gateway/internal/config"
)

// Pool wraps a pgxpool.Pool with domain-typed query methods.
type Pool struct {
	p *pgxpool.Pool
}

// EndpointRecord holds the metadata the gateway needs per agent connection.
type EndpointRecord struct {
	TenantID         uuid.UUID
	EndpointID       uuid.UUID
	CertSerial       string
	Revoked          bool
	HWFingerprint    []byte
	ExpectedPEDEKVer uint32
	ExpectedTMKVer   uint32
}

// KeyVersionRecord holds the expected crypto versions for an endpoint.
type KeyVersionRecord struct {
	ExpectedPEDEKVersion uint32
	ExpectedTMKVersion   uint32
}

// New creates a new Pool from config. The context controls the initial
// connection attempt; it is not retained after New returns.
func New(ctx context.Context, cfg config.PostgresConfig) (*Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}
	pcfg.MaxConns = int32(cfg.MaxConns)
	pcfg.MinConns = int32(cfg.MinConns)
	if cfg.ConnTimeout > 0 {
		pcfg.ConnConfig.ConnectTimeout = cfg.ConnTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Pool{p: pool}, nil
}

// Close closes all connections in the pool.
func (p *Pool) Close() { p.p.Close() }

// GetEndpointByCertSerial looks up an endpoint by the cert serial extracted
// from the mTLS client certificate. Used in the auth interceptor.
//
// Expected Postgres schema (owned by backend-developer):
//
//	CREATE TABLE endpoints (
//	    id              UUID PRIMARY KEY,
//	    tenant_id       UUID NOT NULL REFERENCES tenants(id),
//	    cert_serial     TEXT NOT NULL UNIQUE,
//	    revoked         BOOLEAN NOT NULL DEFAULT FALSE,
//	    hw_fingerprint  BYTEA NOT NULL,
//	    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
func (p *Pool) GetEndpointByCertSerial(ctx context.Context, serial string) (*EndpointRecord, error) {
	// Schema ownership note: the endpoints table lives in init.sql with
	// columns `is_active`, `revoked_at`, `hardware_fingerprint` — not the
	// `revoked` + `hw_fingerprint` shape this comment block originally
	// assumed. We derive `revoked` from `NOT is_active` at query time so
	// the downstream code (EndpointRecord.Revoked) keeps its existing
	// semantics. Tracked under CLAUDE.md §10 schema unification.
	const q = `
		SELECT e.id, e.tenant_id, e.cert_serial, (NOT e.is_active) AS revoked, e.hardware_fingerprint
		FROM endpoints e
		WHERE e.cert_serial = $1
		LIMIT 1`

	row := p.p.QueryRow(ctx, q, serial)
	rec := &EndpointRecord{}
	var tenantIDBytes, endpointIDBytes []byte
	err := row.Scan(
		&endpointIDBytes,
		&tenantIDBytes,
		&rec.CertSerial,
		&rec.Revoked,
		&rec.HWFingerprint,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrEndpointNotFound
		}
		return nil, fmt.Errorf("postgres: get endpoint by serial: %w", err)
	}
	if err := rec.TenantID.UnmarshalBinary(tenantIDBytes); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal tenant_id: %w", err)
	}
	if err := rec.EndpointID.UnmarshalBinary(endpointIDBytes); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal endpoint_id: %w", err)
	}
	return rec, nil
}

// GetKeyVersions returns the expected PE-DEK and TMK versions for the given
// endpoint, used in the key-version handshake.
//
// Expected schema (owned by backend-developer, see key-hierarchy.md):
//
//	CREATE TABLE keystroke_keys (
//	    endpoint_id          UUID NOT NULL REFERENCES endpoints(id),
//	    wrapped_dek          BYTEA NOT NULL,
//	    nonce                BYTEA NOT NULL,
//	    pe_dek_version       INT NOT NULL,
//	    tmk_version          INT NOT NULL,
//	    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    PRIMARY KEY (endpoint_id, pe_dek_version)
//	);
func (p *Pool) GetKeyVersions(ctx context.Context, endpointID uuid.UUID) (*KeyVersionRecord, error) {
	// Schema ownership note: migration 0022_keystroke_keys.up.sql uses a
	// single `key_version TEXT` column (values like 'v1'), not the numeric
	// pe_dek_version + tmk_version pair the original handshake spec assumed.
	// Until key rotation is wired end-to-end we parse the text version
	// suffix ("v<N>") into a monotonic integer used for both PE-DEK and
	// TMK comparisons. When the row is absent (no DLP enrollment yet,
	// Phase 1 ADR 0013 default-OFF) we return zeros so Hello.pe_dek=0
	// and Hello.tmk=0 pass the handshake without rejection.
	const q = `
		SELECT key_version
		FROM keystroke_keys
		WHERE endpoint_id = $1
		LIMIT 1`

	row := p.p.QueryRow(ctx, q, endpointID)
	var keyVersion string
	err := row.Scan(&keyVersion)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &KeyVersionRecord{}, nil
		}
		return nil, fmt.Errorf("postgres: get key versions: %w", err)
	}
	// Parse "v1" → 1, "v2" → 2, etc. Anything unrecognised becomes 0 so
	// the handshake treats it as "endpoint has no active DLP state".
	var numeric uint32
	if strings.HasPrefix(keyVersion, "v") {
		if n, perr := strconv.ParseUint(keyVersion[1:], 10, 32); perr == nil {
			numeric = uint32(n)
		}
	}
	return &KeyVersionRecord{
		ExpectedPEDEKVersion: numeric,
		ExpectedTMKVersion:   numeric,
	}, nil
}

// GetEndpointMetadata returns enrichment fields for an endpoint, used by the
// enricher pipeline to stamp events with tenant/endpoint context.
//
// The cache layer above this function (enricher/enrich.go) handles TTL-based
// caching so this is not called on the hot path per event.
func (p *Pool) GetEndpointMetadata(ctx context.Context, endpointID uuid.UUID) (*EndpointMeta, error) {
	const q = `
		SELECT e.id, e.tenant_id, t.name AS tenant_name, e.hostname, e.os_version
		FROM endpoints e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE e.id = $1
		LIMIT 1`

	row := p.p.QueryRow(ctx, q, endpointID)
	meta := &EndpointMeta{}
	var endpointIDBytes, tenantIDBytes []byte
	err := row.Scan(&endpointIDBytes, &tenantIDBytes, &meta.TenantName, &meta.Hostname, &meta.OSVersion)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrEndpointNotFound
		}
		return nil, fmt.Errorf("postgres: get endpoint metadata: %w", err)
	}
	if err := meta.EndpointID.UnmarshalBinary(endpointIDBytes); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal endpoint_id: %w", err)
	}
	if err := meta.TenantID.UnmarshalBinary(tenantIDBytes); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal tenant_id: %w", err)
	}
	return meta, nil
}

// EndpointMeta holds the enrichment metadata for one endpoint.
type EndpointMeta struct {
	EndpointID uuid.UUID
	TenantID   uuid.UUID
	TenantName string
	Hostname   string
	OSVersion  string
}

// WriteAuditEntry writes a structured audit entry to the Postgres audit log.
// Caller is expected to include sufficient context: who, what, when, why.
//
// Schema (owned by backend-developer):
//
//	CREATE TABLE gateway_audit (
//	    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
//	    tenant_id    UUID,
//	    endpoint_id  UUID,
//	    event_type   TEXT NOT NULL,
//	    details      JSONB NOT NULL DEFAULT '{}',
//	    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
func (p *Pool) WriteAuditEntry(ctx context.Context, entry AuditEntry) error {
	const q = `
		INSERT INTO gateway_audit (tenant_id, endpoint_id, event_type, details, occurred_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := p.p.Exec(ctx, q,
		entry.TenantID,
		entry.EndpointID,
		entry.EventType,
		entry.Details,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: write audit entry: %w", err)
	}
	return nil
}

// AuditEntry is a structured audit record written by the gateway.
type AuditEntry struct {
	TenantID   *uuid.UUID
	EndpointID *uuid.UUID
	EventType  string
	// Details is a JSON-serializable map of event-specific attributes.
	Details map[string]any
}

// Sentinel errors.
var (
	ErrEndpointNotFound = fmt.Errorf("postgres: endpoint not found")
)
