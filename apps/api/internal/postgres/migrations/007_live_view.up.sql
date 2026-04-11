-- 007_live_view.up.sql
-- Live view session state machine persistence.

CREATE TABLE IF NOT EXISTS live_view_sessions (
    id                          TEXT PRIMARY KEY,
    tenant_id                   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    endpoint_id                 UUID NOT NULL REFERENCES endpoints(id),
    requester_id                UUID NOT NULL REFERENCES users(id),
    approver_id                 UUID REFERENCES users(id),
    approval_notes              TEXT,
    reason_code                 TEXT NOT NULL,
    justification               TEXT NOT NULL DEFAULT '',
    requested_duration_seconds  BIGINT NOT NULL DEFAULT 900,
    state                       TEXT NOT NULL DEFAULT 'REQUESTED'
        CHECK (state IN ('REQUESTED','APPROVED','ACTIVE','ENDED','DENIED','EXPIRED','FAILED','TERMINATED_BY_HR','TERMINATED_BY_DPO')),
    livekit_room                TEXT,
    admin_token                 TEXT,
    agent_token                 TEXT,
    signing_key_id              TEXT,
    failure_reason              TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at                 TIMESTAMPTZ,
    started_at                  TIMESTAMPTZ,
    ended_at                    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS lv_tenant_state ON live_view_sessions(tenant_id, state);
CREATE INDEX IF NOT EXISTS lv_endpoint ON live_view_sessions(endpoint_id);
CREATE INDEX IF NOT EXISTS lv_requester ON live_view_sessions(requester_id);
