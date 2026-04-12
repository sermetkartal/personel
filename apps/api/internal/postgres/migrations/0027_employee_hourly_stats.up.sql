-- Migration 0027: employee_hourly_stats — per-hour activity breakdown.
--
-- Companion table to employee_daily_stats. Powers the hour-by-hour
-- active/idle bar chart on the employee detail page (/tr/employees/[id]).
-- In production this is populated by the same ClickHouse roll-up job
-- that writes employee_daily_stats; in dev we seed it directly.
--
-- Partition key is (user_id, day); hour is 0-23 local-server time.

CREATE TABLE IF NOT EXISTS employee_hourly_stats (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day              DATE NOT NULL,
    hour             SMALLINT NOT NULL CHECK (hour BETWEEN 0 AND 23),
    active_minutes   SMALLINT NOT NULL DEFAULT 0 CHECK (active_minutes BETWEEN 0 AND 60),
    idle_minutes     SMALLINT NOT NULL DEFAULT 0 CHECK (idle_minutes BETWEEN 0 AND 60),
    top_app          TEXT,
    screenshot_count SMALLINT NOT NULL DEFAULT 0 CHECK (screenshot_count >= 0),
    PRIMARY KEY (user_id, day, hour)
);

CREATE INDEX IF NOT EXISTS employee_hourly_stats_day
    ON employee_hourly_stats (day DESC, user_id);

COMMENT ON TABLE employee_hourly_stats IS
    'Per-hour activity breakdown. Powers the employee detail page bar chart.';
