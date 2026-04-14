-- Migration 0041: tenants_integrations — per-tenant third-party credential vault.
--
-- Wave 9 Sprint 3A backend for the settings console section. Stores
-- credentials for optional integrations that the platform can dial out
-- to (MaxMind GeoIP2, Cloudflare WAF, PagerDuty / Slack alerting, Sentry
-- error tracking). Every secret field lives in Vault transit — this
-- table stores ONLY the ciphertext blob plus the Vault key version so
-- crypto-erase and key rotation can be audited.
--
-- RLS enforces per-tenant isolation. Uniqueness on (tenant_id,
-- service_name) means exactly one configuration per integration per
-- tenant; re-upserting replaces the row. The audit log carries the
-- history of who changed what when.
--
-- The list of allowed service_name values is enforced at the
-- application layer (integrations.AllowedServices) — a typo here would
-- silently create a dead row that no code path reads.

CREATE TABLE IF NOT EXISTS tenants_integrations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    service_name       TEXT NOT NULL,
    config_encrypted   BYTEA NOT NULL,
    config_key_version INT  NOT NULL,
    enabled            BOOLEAN NOT NULL DEFAULT false,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    audit_actor_id     UUID,
    UNIQUE (tenant_id, service_name)
);

CREATE INDEX IF NOT EXISTS idx_integrations_tenant
    ON tenants_integrations (tenant_id);

ALTER TABLE tenants_integrations ENABLE ROW LEVEL SECURITY;

CREATE POLICY integrations_tenant_isolation ON tenants_integrations
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

COMMENT ON TABLE tenants_integrations IS
    '3. parti servis entegrasyon credentialları — Vault transit ile şifrelenmiş';
COMMENT ON COLUMN tenants_integrations.service_name IS
    'Uygulama katmanında allowlisted: maxmind | cloudflare | pagerduty | slack | sentry';
COMMENT ON COLUMN tenants_integrations.config_encrypted IS
    'Vault transit/encrypt/integrations çıktısı (vault:vN:<base64>) BYTEA olarak';
COMMENT ON COLUMN tenants_integrations.config_key_version IS
    'Vault transit key version — rotation sonrası eski satırları decrypt etmek için';
