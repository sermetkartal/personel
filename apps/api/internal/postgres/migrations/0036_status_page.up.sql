-- 0036_status_page.up.sql
--
-- Faz 17 item #185 — status page scaffold.
--
-- status_incidents tracks active + historical service incidents so
-- the public status page endpoint /public/status.json can return a
-- 7-day history + current state without re-querying the audit log.
--
-- maintenance_windows lets operators schedule planned downtime so
-- the public status page can pre-announce it and the alerting layer
-- can suppress noise during the window.
--
-- Neither table is tenant-scoped — status applies to the whole
-- installation. This also means NO RLS (handled by authorization
-- at the handler layer: public endpoint is read-only, writes require
-- admin role).

CREATE TABLE IF NOT EXISTS status_incidents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    severity        TEXT NOT NULL,          -- 'P1' | 'P2' | 'P3' | 'P4'
    component       TEXT NOT NULL,          -- 'api' | 'gateway' | 'postgres' | ...
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'investigating',  -- investigating | identified | monitoring | resolved
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    last_update_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      UUID REFERENCES users(id),

    CONSTRAINT status_incidents_severity_check CHECK (severity IN ('P1','P2','P3','P4')),
    CONSTRAINT status_incidents_state_check CHECK (state IN ('investigating','identified','monitoring','resolved'))
);

CREATE INDEX IF NOT EXISTS status_incidents_active
    ON status_incidents(state, started_at DESC) WHERE state <> 'resolved';

CREATE INDEX IF NOT EXISTS status_incidents_history
    ON status_incidents(started_at DESC);

CREATE TABLE IF NOT EXISTS maintenance_windows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    scheduled_start TIMESTAMPTZ NOT NULL,
    scheduled_end   TIMESTAMPTZ NOT NULL,
    affected_components TEXT[] NOT NULL DEFAULT '{}',
    state           TEXT NOT NULL DEFAULT 'scheduled', -- scheduled | in_progress | completed | cancelled
    created_by      UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT mw_time_order CHECK (scheduled_end > scheduled_start),
    CONSTRAINT mw_state_check CHECK (state IN ('scheduled','in_progress','completed','cancelled'))
);

CREATE INDEX IF NOT EXISTS maintenance_windows_upcoming
    ON maintenance_windows(scheduled_start)
    WHERE state IN ('scheduled','in_progress');

GRANT SELECT, INSERT, UPDATE ON status_incidents TO app_admin_api;
GRANT SELECT, INSERT, UPDATE ON maintenance_windows TO app_admin_api;
