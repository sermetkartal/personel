-- 006_legal_holds.up.sql
-- Legal hold records (DPO-only placement; max 2 years).

CREATE TABLE IF NOT EXISTS legal_holds (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    dpo_user_id     UUID NOT NULL REFERENCES users(id),
    reason_code     TEXT NOT NULL,
    ticket_id       TEXT NOT NULL,
    justification   TEXT NOT NULL,
    endpoint_id     UUID REFERENCES endpoints(id),
    user_sid        TEXT,
    date_range_from TIMESTAMPTZ,
    date_range_to   TIMESTAMPTZ,
    event_types     JSONB NOT NULL DEFAULT '[]',
    placed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    released_at     TIMESTAMPTZ,
    release_reason  TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    affected_row_count BIGINT -- approximate, updated asynchronously
);

CREATE INDEX IF NOT EXISTS legal_holds_tenant_active ON legal_holds(tenant_id, is_active);
CREATE INDEX IF NOT EXISTS legal_holds_endpoint ON legal_holds(endpoint_id) WHERE is_active = true;
