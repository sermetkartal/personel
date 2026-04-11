-- 008_destruction_reports.up.sql
-- 6-month periodic destruction report storage.

CREATE TABLE IF NOT EXISTS destruction_reports (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    period          TEXT NOT NULL,     -- e.g. "2026-H1"
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    minio_path      TEXT NOT NULL,
    manifest        JSONB NOT NULL DEFAULT '{}',
    signing_key_id  TEXT NOT NULL,
    signature       BYTEA NOT NULL,
    UNIQUE (tenant_id, period)
);

CREATE INDEX IF NOT EXISTS destruction_reports_tenant ON destruction_reports(tenant_id);

-- Silence acknowledgements (supporting agent silence Flow 7 dashboard).
CREATE TABLE IF NOT EXISTS silence_acknowledgements (
    endpoint_id         UUID NOT NULL REFERENCES endpoints(id),
    tenant_id           UUID NOT NULL REFERENCES tenants(id),
    silence_at          TIMESTAMPTZ NOT NULL,
    acknowledged_by     UUID NOT NULL REFERENCES users(id),
    acknowledged_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (endpoint_id, silence_at)
);
