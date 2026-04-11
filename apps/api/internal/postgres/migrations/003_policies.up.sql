-- 003_policies.up.sql
-- Agent policies and endpoint policy assignments.

CREATE TABLE IF NOT EXISTS policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rules       JSONB NOT NULL DEFAULT '{}',
    version     BIGINT NOT NULL DEFAULT 1,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_default  BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS policies_tenant_id ON policies(tenant_id);

-- Explicit policy → endpoint assignment (optional; if absent, default policy applies).
CREATE TABLE IF NOT EXISTS policy_assignments (
    policy_id   UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    assigned_by UUID NOT NULL REFERENCES users(id),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (policy_id, endpoint_id)
);
