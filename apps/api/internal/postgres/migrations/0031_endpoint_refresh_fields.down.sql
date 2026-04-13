-- 0031_endpoint_refresh_fields.down.sql
ALTER TABLE enrollment_tokens
    DROP COLUMN IF EXISTS issued_for_tenant;

ALTER TABLE endpoints
    DROP COLUMN IF EXISTS refresh_count,
    DROP COLUMN IF EXISTS last_refresh_at;
