-- 0032_endpoint_commands.up.sql
-- Remote command audit + state tracking for endpoint deactivate/wipe/revoke.
--
-- Faz 6 items #64 #65. The admin API issues remote commands to an agent
-- (via NATS). Each command gets a row here so the console can show the
-- lifecycle (pending → acknowledged → completed/failed/timeout) and so
-- an auditor can prove that a given wipe was authorised by a specific
-- admin with a specific reason.
--
-- Rows are NEVER deleted in normal operation. The table is effectively
-- append-plus-state-transition.

CREATE TABLE IF NOT EXISTS endpoint_commands (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    endpoint_id     UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    issued_by       UUID NOT NULL REFERENCES users(id),
    kind            TEXT NOT NULL CHECK (kind IN ('deactivate','wipe','revoke')),
    reason          TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'pending'
                    CHECK (state IN ('pending','acknowledged','completed','failed','timeout')),
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error_message   TEXT,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS endpoint_commands_tenant_state
    ON endpoint_commands(tenant_id, state, issued_at DESC);

CREATE INDEX IF NOT EXISTS endpoint_commands_endpoint
    ON endpoint_commands(endpoint_id, issued_at DESC);
