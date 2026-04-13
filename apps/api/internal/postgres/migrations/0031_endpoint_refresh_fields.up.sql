-- 0031_endpoint_refresh_fields.up.sql
-- Faz 6 #63 — endpoint token refresh support.
--
-- last_refresh_at + refresh_count power the per-endpoint rate limit
-- (one refresh per ten minutes) and give operators a feel for how often
-- a given endpoint is cycling its certificate. They are intentionally
-- NOT part of the audit log — audit.append_event is still the source of
-- truth for the full lifecycle. These columns are a fast-path to answer
-- "when was the last rotation?" without a join to the hash chain.
--
-- issued_for_tenant mirrors enrollment_tokens.tenant_id but makes the
-- binding explicit in the schema — the column name makes it obvious to
-- an auditor that the tenant on the row is the tenant the ISSUING
-- principal belonged to, not anything the client could influence.
-- Existing rows are backfilled from tenant_id in the same transaction.

ALTER TABLE endpoints
    ADD COLUMN IF NOT EXISTS last_refresh_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS refresh_count   INT NOT NULL DEFAULT 0;

ALTER TABLE enrollment_tokens
    ADD COLUMN IF NOT EXISTS issued_for_tenant UUID REFERENCES tenants(id) ON DELETE CASCADE;

-- Backfill existing rows. The column will always be set going forward
-- because service.Enroll writes it alongside tenant_id.
UPDATE enrollment_tokens
   SET issued_for_tenant = tenant_id
 WHERE issued_for_tenant IS NULL;
