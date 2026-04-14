-- Migration 0045: backup_runs — per-target backup run history.
--
-- Wave 9 Sprint 3A backend. Every backup-cron invocation creates a row
-- at start and updates status+completed_at+size+sha256 on finish. The
-- existing Phase 3.0.3 SOC 2 backup.Service.RecordRun evidence path
-- writes an evidence item AFTER this row is marked success — the row
-- itself is the operator-facing history and is cheap to query.
--
-- running rows with stale started_at > N hours are surfaced in the
-- settings UI as failed-with-no-report (the cron crashed mid-run).
-- Users trigger retries from the UI which calls POST .../run which
-- inserts a new row.

CREATE TABLE IF NOT EXISTS backup_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id     UUID NOT NULL REFERENCES backup_targets(id) ON DELETE CASCADE,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'running',
    size_bytes    BIGINT,
    sha256        TEXT,
    error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_target
    ON backup_runs (target_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_backup_runs_tenant
    ON backup_runs (tenant_id, started_at DESC);

-- Allowed kind values (full|incremental) + allowed status values
-- (running|success|failed) are enforced at the application layer.

ALTER TABLE backup_runs ENABLE ROW LEVEL SECURITY;

CREATE POLICY backup_runs_tenant_isolation ON backup_runs
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

COMMENT ON TABLE backup_runs IS
    'Backup çalıştırma geçmişi — her target × her çalıştırma bir satır';
COMMENT ON COLUMN backup_runs.status IS
    'running | success | failed — stale running satırları crashed-mid-run olarak gösterilir';
