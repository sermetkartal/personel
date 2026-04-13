-- 0033_dsr_fulfillment_and_apikeys.up.sql
--
-- Faz 6 items #69 (DSR fulfillment workflow) + #72 (service API key auth).
--
-- #69: extends the users + dsr tables so the DSR fulfilment service can
-- record (a) the signed ZIP export artifact for m.11/b access requests
-- and (b) the crypto-erase report for m.11/f erasure requests.
--
-- pii_erased is a hard-set flag on the users row — after crypto-erase the
-- user's PE-DEK key is destroyed in Vault, so any ciphertext in MinIO /
-- ClickHouse / Postgres backups is mathematically unrecoverable, and
-- this flag tells every downstream reader "treat this row as tombstoned".
-- The row itself is preserved so audit_log FK references stay intact.
--
-- #72: service_api_keys table for non-interactive (service-to-service)
-- auth. Plaintext is never stored — only SHA-256(key). tenant_id may be
-- NULL for cross-tenant / system callers (e.g. the in-cluster gateway).
-- scopes is an array of strings like 'events:ingest' which the handler
-- layer checks via apikey.RequireScope.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS pii_erased BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS pii_erased_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS terminated_at TIMESTAMPTZ;

-- Extend dsr_requests with the artifact + fulfilment details. The
-- response_artifact_ref column already exists (5_dsr.up.sql) so we only
-- add the new persistence surface.
ALTER TABLE dsr_requests
    ADD COLUMN IF NOT EXISTS response_sha256 TEXT,
    ADD COLUMN IF NOT EXISTS fulfillment_details JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS service_api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID REFERENCES tenants(id) ON DELETE CASCADE, -- NULL = cross-tenant
    name            TEXT NOT NULL,
    key_hash        TEXT NOT NULL UNIQUE,
    scopes          TEXT[] NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      UUID NOT NULL REFERENCES users(id),
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS service_api_keys_hash
    ON service_api_keys(key_hash) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS service_api_keys_tenant
    ON service_api_keys(tenant_id) WHERE revoked_at IS NULL;
