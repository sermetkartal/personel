-- Rollback Phase 2.0 HRIS field migration.
-- WARNING: in production, rolling back destroys synced HRIS metadata.

DROP INDEX IF EXISTS users_active_employees;
DROP INDEX IF EXISTS users_manager_id;
DROP INDEX IF EXISTS users_hris_lookup;

ALTER TABLE users
    DROP COLUMN IF EXISTS custom_fields,
    DROP COLUMN IF EXISTS hris_synced_at,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS terminated_at,
    DROP COLUMN IF EXISTS hired_at,
    DROP COLUMN IF EXISTS manager_user_id,
    DROP COLUMN IF EXISTS job_title,
    DROP COLUMN IF EXISTS department,
    DROP COLUMN IF EXISTS hris_source,
    DROP COLUMN IF EXISTS hris_id;
