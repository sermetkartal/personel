-- Migration 0043: tenants.retention_policy — per-tenant KVKK saklama süresi override.
--
-- Wave 9 Sprint 3A settings. The system-wide default is derived from
-- KVKK m.7 + Kurul içtihadı + the data retention matrix at
-- docs/architecture/data-retention-matrix.md. Tenants may override
-- INDIVIDUAL keys but the application layer enforces KVKK minimums —
-- values below the legal minimum are rejected at the service boundary.
--
-- Shape:
--   {
--     "audit_years":     5,     -- KVKK m.12 min 5
--     "event_days":      365,   -- KVKK m.7 guidance (UAM event retention)
--     "screenshot_days": 30,    -- KVKK ölçülülük (proportionality)
--     "keystroke_days":  180,   -- ADR 0013 DLP opt-in ceremony default
--     "live_view_days":  30,    -- Live view recording retention
--     "dsr_days":        3650   -- KVKK m.11 min 10 year DSR record
--   }
--
-- NULL = inherit system default (tenants created before this migration
-- and any tenant that has not explicitly customized).

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS retention_policy JSONB;

COMMENT ON COLUMN tenants.retention_policy IS
    'Tenant-bazlı saklama politikası override. NULL = sistem default. Uygulama katmanı KVKK minimumları enforce eder (audit>=5y, event>=1y, screenshot>=30d, keystroke>=180d, dsr>=10y).';
