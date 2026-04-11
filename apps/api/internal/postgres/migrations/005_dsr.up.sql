-- 005_dsr.up.sql
-- KVKK m.11 Data Subject Request workflow.

CREATE TABLE IF NOT EXISTS dsr_requests (
    id                      TEXT PRIMARY KEY,
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    employee_user_id        UUID NOT NULL REFERENCES users(id),
    request_type            TEXT NOT NULL CHECK (request_type IN ('access','rectify','erase','object','restrict','portability')),
    scope_json              JSONB NOT NULL DEFAULT '{}',
    justification           TEXT NOT NULL,
    state                   TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','at_risk','overdue','resolved','rejected')),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    sla_deadline            TIMESTAMPTZ NOT NULL,
    assigned_to             UUID REFERENCES users(id),
    response_artifact_ref   TEXT,  -- MinIO path
    audit_chain_ref         TEXT,  -- audit entry id
    extended_at             TIMESTAMPTZ,
    extension_reason        TEXT,
    closed_at               TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS dsr_tenant_state ON dsr_requests(tenant_id, state);
CREATE INDEX IF NOT EXISTS dsr_employee ON dsr_requests(employee_user_id);
CREATE INDEX IF NOT EXISTS dsr_sla_deadline ON dsr_requests(sla_deadline) WHERE state IN ('open','at_risk');
