ALTER TABLE tenants
    DROP CONSTRAINT IF EXISTS tenants_ca_mode_check;

ALTER TABLE tenants
    DROP COLUMN IF EXISTS ca_mode,
    DROP COLUMN IF EXISTS ca_config;
