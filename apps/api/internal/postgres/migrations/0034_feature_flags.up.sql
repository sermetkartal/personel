-- 0034_feature_flags.up.sql
--
-- Faz 16 #173 — feature flags table.
--
-- A pure-Go evaluator (apps/api/internal/featureflags) reads this table
-- on a ~30s in-process cache. Every admin-initiated flip writes an
-- audit_log entry BEFORE the row update, per recorder.go contract.
--
-- The table is intentionally flat: no versioning, no history. The audit
-- log IS the history; reconstructing a past flag state means reading
-- the audit entry's "new" details field.

CREATE TABLE IF NOT EXISTS feature_flags (
    key                 TEXT        PRIMARY KEY CHECK (char_length(key) <= 128),
    description         TEXT        NOT NULL DEFAULT '',
    enabled             BOOLEAN     NOT NULL DEFAULT false,
    default_value       BOOLEAN     NOT NULL DEFAULT false,
    rollout_percentage  INTEGER     NOT NULL DEFAULT 0
                         CHECK (rollout_percentage BETWEEN 0 AND 100),

    -- Override maps. JSONB instead of separate tables for simplicity —
    -- the admin UI writes the whole blob, the evaluator reads the whole
    -- blob, no query patterns need an index on individual override keys.
    tenant_overrides    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    role_overrides      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    user_overrides      JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by          TEXT        NOT NULL DEFAULT '',

    -- Metadata is free-form; the UI uses it for e.g. linked ADR, JIRA
    -- ticket, owner contact. Not evaluated, never returned to
    -- non-admins.
    metadata            JSONB       NOT NULL DEFAULT '{}'::jsonb
);

-- Faster "list all ordered by key" — the only query shape the admin
-- console needs. No other index is justified at this scale (flags are
-- in the dozens, not millions).
CREATE INDEX IF NOT EXISTS idx_feature_flags_updated_at
    ON feature_flags (updated_at DESC);

-- Seed a few default-off canary flags so the console has something to
-- show on first deploy.
INSERT INTO feature_flags (key, description, enabled, default_value, rollout_percentage)
VALUES
    ('new_dashboard',         'Next-gen analytics dashboard (Faz 8)',   false, false, 0),
    ('uba_v2',                'UBA detector v2 (isolation forest + LLM)', false, false, 0),
    ('mobile_live_view',      'Live view viewer in the mobile admin app', false, false, 0),
    ('canary_release_routing','Route /v1/* through canary tenant cohort',   false, false, 0)
ON CONFLICT (key) DO NOTHING;
