-- 0033_dsr_fulfillment_and_apikeys.down.sql

DROP INDEX IF EXISTS service_api_keys_tenant;
DROP INDEX IF EXISTS service_api_keys_hash;
DROP TABLE IF EXISTS service_api_keys;

ALTER TABLE dsr_requests
    DROP COLUMN IF EXISTS fulfillment_details,
    DROP COLUMN IF EXISTS response_sha256;

ALTER TABLE users
    DROP COLUMN IF EXISTS terminated_at,
    DROP COLUMN IF EXISTS pii_erased_at,
    DROP COLUMN IF EXISTS pii_erased;
