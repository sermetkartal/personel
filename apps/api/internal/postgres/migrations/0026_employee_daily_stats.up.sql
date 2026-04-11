-- Migration 0026: employee_daily_stats — rolled-up activity per user per day.
--
-- Phase 2 preview: this table is populated by a hypothetical roll-up job
-- that consumes ClickHouse time-series events (app focus, keystrokes,
-- screenshots, idle detection) and writes a single row per (user, day).
-- For the dev/demo path we seed it directly via dev-seed-employee-stats.sh.
--
-- The console's /tr/employees page reads from this table to show
-- "what was each person doing today" without joining to ClickHouse.

CREATE TABLE IF NOT EXISTS employee_daily_stats (
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day                 DATE NOT NULL,
    active_minutes      INT NOT NULL DEFAULT 0 CHECK (active_minutes >= 0),
    idle_minutes        INT NOT NULL DEFAULT 0 CHECK (idle_minutes >= 0),
    screenshot_count    INT NOT NULL DEFAULT 0 CHECK (screenshot_count >= 0),
    keystroke_count     INT NOT NULL DEFAULT 0 CHECK (keystroke_count >= 0),
    productivity_score  SMALLINT NOT NULL DEFAULT 0 CHECK (productivity_score BETWEEN 0 AND 100),
    top_apps            JSONB NOT NULL DEFAULT '[]'::jsonb,  -- [{"name": "...", "minutes": N, "category": "productive|neutral|distracting"}]
    first_activity_at   TIMESTAMPTZ,
    last_activity_at    TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day)
);

CREATE INDEX IF NOT EXISTS employee_daily_stats_day
    ON employee_daily_stats (day DESC);

CREATE INDEX IF NOT EXISTS employee_daily_stats_user_day
    ON employee_daily_stats (user_id, day DESC);

COMMENT ON TABLE employee_daily_stats IS
    'Daily activity roll-up per employee. Dev seed writes directly; production writes from a ClickHouse roll-up job (Phase 2).';
COMMENT ON COLUMN employee_daily_stats.top_apps IS
    'Top 10 applications by active minutes for the day, as a JSON array.';
COMMENT ON COLUMN employee_daily_stats.productivity_score IS
    '0-100 weighted score: productive app minutes / (productive + distracting + idle/2).';
