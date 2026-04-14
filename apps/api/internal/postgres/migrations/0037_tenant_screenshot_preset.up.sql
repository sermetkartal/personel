-- Migration 0037: tenants.screenshot_preset — per-tenant screenshot capture preset.
--
-- Nullable TEXT column used by the new /v1/tenants/me/screenshot-preset
-- endpoint and the Settings/General dropdown in the admin console. Valid
-- values: 'minimal' | 'low' | 'medium' | 'high' (default) | 'max'.
-- Enum validation lives in application code (apps/api/internal/tenant/
-- screenshot_handler.go) rather than as a DB CHECK constraint — matches
-- existing project conventions (role validation, etc.).
--
-- The agent reads this preset via the tenant preference API and stamps it
-- into the boot-time env var PERSONEL_SCREENSHOT_PRESET which
-- PolicyView::default() consumes. Once PolicyPush apply lands, the same
-- value also flows through the live PolicyBundle path.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS screenshot_preset TEXT;

COMMENT ON COLUMN tenants.screenshot_preset IS
    'Screenshot capture preset: minimal|low|medium|high|max. NULL = default (high). Validated in Go handler.';
