-- Migration 0023: add HRIS-related nullable columns to users table.
--
-- Phase 2.0 preparation: the Phase 2 HRIS connector framework (ADR 0018) will
-- sync employment metadata from BambooHR / Logo Tiger / Workday / Personio.
-- Adding these columns as nullable NOW avoids a big-bang schema change during
-- Phase 2.3 when the connectors land. Phase 1 code ignores them; Phase 2 code
-- backfills them.
--
-- All columns are nullable and default-null. Existing rows are unaffected.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS hris_id            TEXT,           -- external HRIS primary key
    ADD COLUMN IF NOT EXISTS hris_source        TEXT            -- which HRIS this came from
        CHECK (hris_source IS NULL OR hris_source IN (
            'bamboohr','workday','personio','logo_tiger','mikro','netsis','manual'
        )),
    ADD COLUMN IF NOT EXISTS department         TEXT,
    ADD COLUMN IF NOT EXISTS job_title          TEXT,
    ADD COLUMN IF NOT EXISTS manager_user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS hired_at           TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS terminated_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS locale             TEXT NOT NULL DEFAULT 'tr'
        CHECK (locale IN ('tr','en')),
    ADD COLUMN IF NOT EXISTS hris_synced_at     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS custom_fields      JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Index for HRIS sync lookup (idempotent upserts by external id).
CREATE INDEX IF NOT EXISTS users_hris_lookup
    ON users (hris_source, hris_id)
    WHERE hris_id IS NOT NULL;

-- Index for manager-reports queries (e.g. "my direct reports" view).
CREATE INDEX IF NOT EXISTS users_manager_id
    ON users (manager_user_id)
    WHERE manager_user_id IS NOT NULL;

-- Partial index for active employees (terminated_at IS NULL) — the most
-- common filter for admin console queries.
CREATE INDEX IF NOT EXISTS users_active_employees
    ON users (tenant_id, department)
    WHERE terminated_at IS NULL AND is_active = TRUE;

COMMENT ON COLUMN users.hris_id IS 'External HRIS primary key; stable across syncs';
COMMENT ON COLUMN users.hris_source IS 'Which HRIS owns this record; NULL = locally managed';
COMMENT ON COLUMN users.department IS 'Org chart department; synced from HRIS';
COMMENT ON COLUMN users.manager_user_id IS 'Direct manager reference; synced from HRIS';
COMMENT ON COLUMN users.hired_at IS 'Employment start date; drives retention clock for ex-employee data';
COMMENT ON COLUMN users.terminated_at IS 'Employment end date; triggers KVKK retention countdown';
COMMENT ON COLUMN users.locale IS 'UI language preference (tr/en); default tr per locked decision #1';
COMMENT ON COLUMN users.hris_synced_at IS 'Last successful HRIS sync timestamp';
COMMENT ON COLUMN users.custom_fields IS 'Arbitrary HRIS-specific fields; Phase 2 connectors map these';
