# Personel Ingest Gateway

Go 1.22 gRPC ingest gateway and event enrichment pipeline for the Personel UAM platform.

## Binaries

| Binary | Description |
|---|---|
| `cmd/gateway` | mTLS gRPC server. Accepts agent bidi streams, validates certs and key versions, publishes to NATS JetStream. |
| `cmd/enricher` | JetStream consumer. Enriches events with tenant metadata, applies KVKK m.6 sensitivity rules, sinks to ClickHouse and records blob refs. |

## Quick start (local dev)

```bash
# Start dependencies.
make compose-up

# Generate proto stubs (requires buf CLI).
make proto

# Build.
make build

# Run gateway.
./bin/gateway --config configs/gateway.yaml

# Run enricher.
./bin/enricher --config configs/enricher.yaml
```

## Architecture decisions

See `docs/architecture/` in the monorepo root for the full design. Key decisions made here:

- **Unified ClickHouse schema** with a `payload` JSON column plus indexed envelope columns. Separate tables for each sensitive-flagged event category (window, file, keystroke, clipboard) with shorter TTLs per the retention matrix.
- **Backpressure window of 16** unacked batches per stream. Configurable via `backpressure.max_unacked_batches`. This caps in-flight events at ~3200 per agent at the default 200-events-per-batch size while keeping memory usage bounded.
- **Rate limits**: 5000 events/sec per endpoint, 100k/sec per tenant. Tunable.
- **Flow 7 heartbeat monitor** transitions: online → degraded (90s) → offline (5m) → offline_extended (2h). DPO alert at 4h during business hours, 24h overnight (Turkey TRT UTC+3).

## Postgres schema expected from backend-developer

The gateway reads these tables. They must be created by the Admin API migration:

```sql
-- Endpoint identity, cert serial binding, revocation flag.
CREATE TABLE endpoints (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    cert_serial     TEXT NOT NULL UNIQUE,
    revoked         BOOLEAN NOT NULL DEFAULT FALSE,
    hw_fingerprint  BYTEA NOT NULL,
    hostname        TEXT,
    os_version      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Keystroke key version handshake (see key-hierarchy.md).
CREATE TABLE keystroke_keys (
    endpoint_id          UUID NOT NULL REFERENCES endpoints(id),
    wrapped_dek          BYTEA NOT NULL,
    nonce                BYTEA NOT NULL,
    pe_dek_version       INT NOT NULL,
    tmk_version          INT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (endpoint_id, pe_dek_version)
);

-- Gateway audit log.
CREATE TABLE gateway_audit (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID,
    endpoint_id  UUID,
    event_type   TEXT NOT NULL,
    details      JSONB NOT NULL DEFAULT '{}',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Open questions for devops-engineer

1. **ClickHouse topology**: Phase 1 pilot uses single-node `MergeTree`. The schemas are written for single-node. When Stage 2 (`ReplicatedMergeTree`) is deployed, the `CREATE TABLE` DDL in `internal/clickhouse/schemas.go` must be updated to `ReplicatedMergeTree('/clickhouse/tables/{shard}/{table}', '{replica}')` and the batcher's connection string must point to the `Distributed` table.
2. **NATS cluster size**: the publisher is coded for a single-node NATS server. For HA, supply multiple URLs in `nats.urls`; the client will round-robin.
3. **MinIO topology**: the lifecycle rules are applied via the S3 API. If MinIO is deployed in distributed mode with erasure coding, the bucket names remain the same.
4. **TLS cert paths**: gateway expects certs at `/etc/personel/tls/{gateway.crt,gateway.key,tenant_ca.crt}`. These are managed by vault-agent sidecar.
