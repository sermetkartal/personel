-- 002_endpoints.up.sql
-- Endpoints (enrolled agents) and enrollment tokens.

CREATE TABLE IF NOT EXISTS endpoints (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    hostname            TEXT NOT NULL,
    os_version          TEXT,
    agent_version       TEXT,
    hardware_fingerprint BYTEA,
    assigned_user_id    UUID REFERENCES users(id),
    cert_serial         TEXT,
    enrolled_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at        TIMESTAMPTZ,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    revoked_at          TIMESTAMPTZ,
    revoke_reason       TEXT
);

CREATE INDEX IF NOT EXISTS endpoints_tenant_id ON endpoints(tenant_id);
CREATE INDEX IF NOT EXISTS endpoints_assigned_user ON endpoints(assigned_user_id);
CREATE INDEX IF NOT EXISTS endpoints_cert_serial ON endpoints(cert_serial);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    vault_secret_id TEXT NOT NULL, -- single-use secret ID
    vault_role_id   TEXT NOT NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    used_by_endpoint UUID REFERENCES endpoints(id)
);

CREATE INDEX IF NOT EXISTS enrollment_tokens_tenant ON enrollment_tokens(tenant_id);
