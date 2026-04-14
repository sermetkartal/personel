-- 0035_tickets.up.sql
--
-- Faz 17 item #184 — ticket system integration scaffold.
--
-- Tickets are lightweight metadata rows that track external helpdesk
-- integrations (Jira, Zendesk, Freshdesk). Personel does NOT run a
-- full ticket system internally — instead we forward to the customer's
-- chosen provider and keep a local shadow row for audit + cross-
-- reference. The provider_id column holds the external system's
-- ticket ID so we can correlate webhooks back to our local row.
--
-- State machine:
--   open → in_progress → resolved → closed
--   open → rejected
-- Any transition emits an audit entry via the tickets.Service.
--
-- Tenant isolation: RLS enforced by tenant_id policy matching the
-- session variable app.tenant_id, same pattern as every other tenant-
-- scoped table.

CREATE TABLE IF NOT EXISTS tickets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,          -- 'jira' | 'zendesk' | 'freshdesk' | 'internal'
    provider_id     TEXT,                   -- external system ticket ID
    severity        TEXT NOT NULL,          -- 'P1' | 'P2' | 'P3' | 'P4'
    subject         TEXT NOT NULL,
    body            TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'open',
    assigned_to     UUID REFERENCES users(id),
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    closed_at       TIMESTAMPTZ,

    CONSTRAINT tickets_severity_check CHECK (severity IN ('P1','P2','P3','P4')),
    CONSTRAINT tickets_state_check CHECK (state IN ('open','in_progress','resolved','closed','rejected'))
);

CREATE INDEX IF NOT EXISTS tickets_tenant_state
    ON tickets(tenant_id, state) WHERE state IN ('open','in_progress');

CREATE INDEX IF NOT EXISTS tickets_provider_id
    ON tickets(provider, provider_id) WHERE provider_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS tickets_created_at
    ON tickets(tenant_id, created_at DESC);

-- RLS
ALTER TABLE tickets ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tickets_tenant_isolation ON tickets;
CREATE POLICY tickets_tenant_isolation ON tickets
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

GRANT SELECT, INSERT, UPDATE ON tickets TO app_admin_api;
