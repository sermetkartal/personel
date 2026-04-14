-- Rollback migration 0040 — admin bypass column + index.
DROP INDEX IF EXISTS idx_liveview_admin_bypass;
ALTER TABLE live_view_sessions DROP COLUMN IF EXISTS admin_bypass;
