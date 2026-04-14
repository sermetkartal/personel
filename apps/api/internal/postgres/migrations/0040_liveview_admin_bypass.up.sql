-- Migration 0040: live_view_sessions.admin_bypass
--
-- ADR 0026 — Admin role bypasses the HR/IT dual-control approval gate for
-- live view sessions. The session is created directly in APPROVED state and
-- auto-provisioned. Every such row carries admin_bypass=true so that
-- compliance auditors can exclude bypass rows from the dual-control drill
-- report (which only counts ceremony-path sessions as qualifying evidence
-- for the CC6.3 control).

ALTER TABLE live_view_sessions
    ADD COLUMN IF NOT EXISTS admin_bypass BOOLEAN NOT NULL DEFAULT false;

-- Partial index: only bypass rows are interesting to query directly
-- (e.g. "how many admin bypass sessions were created this month?").
-- Keeping the index partial keeps it tiny and cheap to maintain.
CREATE INDEX IF NOT EXISTS idx_liveview_admin_bypass
    ON live_view_sessions (tenant_id, created_at DESC)
    WHERE admin_bypass = true;
