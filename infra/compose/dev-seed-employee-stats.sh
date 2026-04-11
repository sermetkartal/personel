#!/usr/bin/env bash
# =============================================================================
# dev-seed-employee-stats.sh — populate employee_daily_stats with 7 days
# of realistic Turkish working hours data for the /tr/employees page.
#
# Pattern:
#   - Mon-Fri: 09:00–18:00 (540 min window)
#   - Active: 300-480 min (5-8 hours)
#   - Idle:   60-200 min
#   - Weekend: most employees 0 active, a few outliers
#   - 3 personas per department to spread top_apps realistically
#   - Productivity score 40-95, skewed by role:
#       * engineer: high (60-95)
#       * manager:  med  (50-85)
#       * sales:    med-high (55-90)
#       * support:  med  (50-80)
#       * ops:      med  (45-80)
# Re-runnable: upserts via ON CONFLICT.
# =============================================================================
set -euo pipefail

PGEXEC='docker exec -i personel-postgres psql -U postgres -d personel -v ON_ERROR_STOP=1'

echo "=== Populating employee_daily_stats (23 employees × 7 days) ==="

$PGEXEC <<'SQL'
-- Realistic top-app presets by department. Minutes are randomized per row
-- but the app mix per department stays consistent so the UI looks natural.
WITH app_presets(dept, apps) AS (
  VALUES
    ('Mühendislik', '[
        {"name":"Visual Studio Code","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Slack","category":"neutral"},
        {"name":"Terminal","category":"productive"},
        {"name":"Docker Desktop","category":"productive"},
        {"name":"YouTube","category":"distracting"},
        {"name":"GitHub","category":"productive"}
     ]'::jsonb),
    ('Satış', '[
        {"name":"Outlook","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Excel","category":"productive"},
        {"name":"Zoom","category":"productive"},
        {"name":"Salesforce","category":"productive"},
        {"name":"WhatsApp Web","category":"distracting"}
     ]'::jsonb),
    ('Pazarlama', '[
        {"name":"Chrome","category":"neutral"},
        {"name":"Canva","category":"productive"},
        {"name":"Figma","category":"productive"},
        {"name":"LinkedIn","category":"neutral"},
        {"name":"Instagram","category":"distracting"},
        {"name":"Google Analytics","category":"productive"}
     ]'::jsonb),
    ('İK', '[
        {"name":"Outlook","category":"productive"},
        {"name":"BordroPlus","category":"productive"},
        {"name":"Word","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"LinkedIn","category":"neutral"},
        {"name":"Teams","category":"productive"}
     ]'::jsonb),
    ('Finans', '[
        {"name":"Excel","category":"productive"},
        {"name":"Logo Tiger","category":"productive"},
        {"name":"Outlook","category":"productive"},
        {"name":"Mikro","category":"productive"},
        {"name":"Chrome","category":"neutral"}
     ]'::jsonb),
    ('Destek', '[
        {"name":"Zendesk","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Outlook","category":"productive"},
        {"name":"Slack","category":"neutral"},
        {"name":"YouTube","category":"distracting"}
     ]'::jsonb),
    ('Hukuk', '[
        {"name":"Word","category":"productive"},
        {"name":"Outlook","category":"productive"},
        {"name":"Adobe Acrobat","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Teams","category":"productive"}
     ]'::jsonb),
    ('Operasyon', '[
        {"name":"Excel","category":"productive"},
        {"name":"Outlook","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Jira","category":"productive"},
        {"name":"Slack","category":"neutral"}
     ]'::jsonb),
    ('BT', '[
        {"name":"Terminal","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Docker","category":"productive"},
        {"name":"Slack","category":"neutral"},
        {"name":"Remote Desktop","category":"productive"}
     ]'::jsonb),
    ('Güvenlik', '[
        {"name":"Terminal","category":"productive"},
        {"name":"Wireshark","category":"productive"},
        {"name":"Chrome","category":"neutral"},
        {"name":"Splunk","category":"productive"}
     ]'::jsonb)
),
-- All seeded employees (dpo-test excluded, agents skipped)
emps AS (
  SELECT u.id, u.username, u.department, u.role
  FROM public.users u
  WHERE u.username LIKE 'seed-%'
    AND u.role IN ('employee','manager','hr','investigator')
),
-- Build 7 calendar days ending today
days AS (
  SELECT (current_date - (n || ' days')::interval)::date AS day,
         EXTRACT(ISODOW FROM (current_date - (n || ' days')::interval)) AS dow
  FROM generate_series(0, 6) AS n
),
-- Cartesian product, then compute per-row stats
raw AS (
  SELECT
    e.id AS user_id,
    e.username,
    e.department,
    e.role,
    d.day,
    d.dow,
    -- Weekend: 10% chance someone worked
    CASE
      WHEN d.dow >= 6 AND random() > 0.10 THEN 0
      WHEN d.dow >= 6 THEN (60 + (random() * 120))::int
      ELSE (300 + (random() * 180))::int
    END AS active_min,
    CASE
      WHEN d.dow >= 6 THEN (random() * 30)::int
      ELSE (40 + (random() * 140))::int
    END AS idle_min,
    CASE
      WHEN d.dow >= 6 THEN 0
      ELSE (20 + (random() * 50))::int
    END AS screens,
    CASE
      WHEN d.dow >= 6 THEN 0
      ELSE (2000 + (random() * 8000))::int
    END AS keys,
    -- Base score by role, then jitter
    CASE
      WHEN d.dow >= 6 THEN (30 + (random() * 40))::int
      WHEN e.role = 'manager' THEN (55 + (random() * 30))::int
      WHEN e.department IN ('Mühendislik','BT','Güvenlik') THEN (60 + (random() * 35))::int
      WHEN e.department IN ('Finans','Hukuk') THEN (58 + (random() * 30))::int
      ELSE (48 + (random() * 35))::int
    END AS score,
    ap.apps AS apps_pool
  FROM emps e
  CROSS JOIN days d
  LEFT JOIN app_presets ap ON ap.dept = e.department
),
-- For each (user, day) pick top 5 apps with randomized minutes that sum close to active_min
with_top AS (
  SELECT
    r.*,
    (
      SELECT jsonb_agg(
        jsonb_build_object(
          'name', a->>'name',
          'category', a->>'category',
          'minutes', GREATEST(5, (r.active_min * (0.35 - ordinality * 0.05 + random() * 0.05))::int)
        )
        ORDER BY ordinality
      )
      FROM jsonb_array_elements(COALESCE(r.apps_pool, '[]'::jsonb)) WITH ORDINALITY a(a, ordinality)
      WHERE ordinality <= 5
    ) AS top_apps_json
  FROM raw r
)
INSERT INTO employee_daily_stats(
  user_id, day, active_minutes, idle_minutes, screenshot_count, keystroke_count,
  productivity_score, top_apps, first_activity_at, last_activity_at, updated_at
)
SELECT
  user_id,
  day,
  active_min,
  idle_min,
  screens,
  keys,
  LEAST(100, GREATEST(0, score)),
  COALESCE(top_apps_json, '[]'::jsonb),
  CASE WHEN dow < 6 AND active_min > 0
       THEN day::timestamptz + interval '9 hours' + (random() * interval '30 minutes')
       ELSE NULL END,
  -- Last activity: for today's row and active employees, "now minus a few minutes"
  -- so the currently-active indicator lights up on a handful of rows
  CASE
    WHEN day = current_date AND random() > 0.6 THEN now() - (random() * interval '3 minutes')
    WHEN dow < 6 AND active_min > 0
         THEN day::timestamptz + interval '17 hours' + (random() * interval '90 minutes')
    ELSE NULL
  END,
  now()
FROM with_top
ON CONFLICT (user_id, day) DO UPDATE SET
  active_minutes    = EXCLUDED.active_minutes,
  idle_minutes      = EXCLUDED.idle_minutes,
  screenshot_count  = EXCLUDED.screenshot_count,
  keystroke_count   = EXCLUDED.keystroke_count,
  productivity_score = EXCLUDED.productivity_score,
  top_apps          = EXCLUDED.top_apps,
  first_activity_at = EXCLUDED.first_activity_at,
  last_activity_at  = EXCLUDED.last_activity_at,
  updated_at        = now();
SQL

echo
echo "=== Summary ==="
$PGEXEC <<SQL
SELECT day, count(*) AS employees,
       ROUND(AVG(active_minutes)) AS avg_active_min,
       ROUND(AVG(productivity_score)) AS avg_score
FROM employee_daily_stats
GROUP BY day ORDER BY day DESC;

SELECT 'currently_active_now', count(*) FROM employee_daily_stats
WHERE day = current_date AND last_activity_at > now() - interval '5 minutes';
SQL
