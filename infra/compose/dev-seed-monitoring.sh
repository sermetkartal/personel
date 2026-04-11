#!/usr/bin/env bash
# =============================================================================
# dev-seed-monitoring.sh — populate the dev stack with realistic-looking
# employee monitoring data so the Personel dashboard, endpoints, live-view,
# audit, and DSR pages render with content instead of empty states.
#
# Inserts directly into Postgres (public.users, public.endpoints,
# public.live_view_sessions, public.dsr_requests, etc.) and into
# audit.audit_log via the append_event stored procedure.
#
# Re-runnable: TRUNCATE-first policy on the seeded rows.
# =============================================================================
set -euo pipefail

TENANT_ID="00000000-0000-0000-0000-000000000001"
PGEXEC='docker exec -i personel-postgres psql -U postgres -d personel -v ON_ERROR_STOP=1'

echo "=== Clearing previous seed data ==="
$PGEXEC <<SQL
TRUNCATE public.live_view_sessions CASCADE;
TRUNCATE public.endpoints CASCADE;
TRUNCATE public.dsr_requests CASCADE;
TRUNCATE public.silence_acknowledgements CASCADE;
-- don't TRUNCATE users — would kill the DPO user used for login
DELETE FROM public.users WHERE username LIKE 'seed-%';
-- audit_log is append-only at the trigger level; accumulated seed entries
-- are left in place and new ones are appended.
SQL

echo "=== Seeding 20 employees + 3 admins/HR ==="
$PGEXEC <<SQL
-- 20 employees with varied departments
WITH depts(d) AS (
  VALUES ('Mühendislik'),('Satış'),('Pazarlama'),('İK'),('Finans'),('Destek'),('Hukuk'),('Operasyon')
),
titles(t) AS (
  VALUES ('Yazılım Geliştirici'),('Senior Engineer'),('Tech Lead'),('Satış Temsilcisi'),('Account Manager'),
         ('Pazarlama Uzmanı'),('İK Uzmanı'),('Muhasebeci'),('Destek Uzmanı'),('Operasyon Uzmanı')
),
names(n) AS (
  VALUES ('ahmet'),('ayse'),('mehmet'),('fatma'),('ali'),('zeynep'),('mustafa'),('elif'),
         ('emre'),('selin'),('burak'),('canan'),('deniz'),('esra'),('furkan'),('gizem'),
         ('hakan'),('irem'),('kaan'),('lale')
)
INSERT INTO public.users(id, tenant_id, keycloak_sub, username, email, role, department, job_title, hired_at, locale)
SELECT
  gen_random_uuid(),
  '$TENANT_ID'::uuid,
  'kc-seed-' || n,
  'seed-' || n,
  n || '@personel.local',
  CASE WHEN row_number() OVER () <= 18 THEN 'employee' ELSE 'manager' END,
  (SELECT d FROM depts ORDER BY random() LIMIT 1),
  (SELECT t FROM titles ORDER BY random() LIMIT 1),
  now() - (random() * 1000 || ' days')::interval,
  'tr'
FROM names;

-- Add one admin, one HR, one investigator (for role-aware pages)
INSERT INTO public.users(id, tenant_id, keycloak_sub, username, email, role, department, job_title, hired_at)
VALUES
  (gen_random_uuid(), '$TENANT_ID', 'kc-seed-admin', 'seed-admin', 'admin@personel.local', 'admin', 'BT', 'Sistem Yöneticisi', now() - interval '1200 days'),
  (gen_random_uuid(), '$TENANT_ID', 'kc-seed-hr1', 'seed-hr-ozgur', 'ozgur@personel.local', 'hr', 'İK', 'İK Müdürü', now() - interval '800 days'),
  (gen_random_uuid(), '$TENANT_ID', 'kc-seed-inv1', 'seed-inv-tolga', 'tolga@personel.local', 'investigator', 'Güvenlik', 'Forensic Analyst', now() - interval '600 days');

SELECT count(*) AS seeded_users FROM public.users WHERE username LIKE 'seed-%';
SQL

echo "=== Seeding 30 endpoints (25 active + 5 revoked) ==="
$PGEXEC <<SQL
-- Create 25 active endpoints, each randomly assigned to a seeded employee.
WITH oses(v) AS (
  VALUES ('Windows 11 Pro 23H2'),('Windows 11 Enterprise 23H2'),('Windows 10 Pro 22H2'),('Windows 11 Pro 24H2')
),
numbered_employees AS (
  SELECT id, row_number() OVER () - 1 AS idx
  FROM public.users
  WHERE username LIKE 'seed-%' AND role IN ('employee','manager')
),
emp_count AS (SELECT count(*) AS c FROM numbered_employees),
ep_seq AS (SELECT generate_series(1, 25) AS n)
INSERT INTO public.endpoints(id, tenant_id, hostname, os_version, agent_version, assigned_user_id, cert_serial, enrolled_at, last_seen_at, is_active)
SELECT
  gen_random_uuid(),
  '$TENANT_ID'::uuid,
  'DESKTOP-' || upper(substr(md5(random()::text || ep_seq.n::text), 1, 8)),
  (SELECT v FROM oses ORDER BY random() LIMIT 1),
  '0.1.0-dev',
  (SELECT id FROM numbered_employees WHERE idx = (ep_seq.n - 1) % (SELECT c FROM emp_count)),
  'CN=endpoint-' || ep_seq.n,
  now() - (random() * 400 || ' days')::interval,
  now() - (random() * 300 || ' seconds')::interval,
  true
FROM ep_seq;

INSERT INTO public.endpoints(id, tenant_id, hostname, os_version, agent_version, enrolled_at, last_seen_at, is_active, revoked_at, revoke_reason)
SELECT
  gen_random_uuid(),
  '$TENANT_ID'::uuid,
  'DESKTOP-OLD-' || upper(substr(md5(random()::text), 1, 6)),
  'Windows 10 Pro 21H2',
  '0.0.9',
  now() - interval '400 days',
  now() - (random() * 30 || ' days')::interval,
  false,
  now() - (random() * 15 || ' days')::interval,
  (ARRAY['çalışan ayrıldı','cihaz değişimi','hardware arıza','güvenlik ihlali','ad değişikliği'])[1 + (random()*4)::int]
FROM generate_series(1, 5);

SELECT is_active, count(*) FROM public.endpoints GROUP BY is_active;
SQL

echo "=== Seeding live view sessions (15 × mixed states across last 90 days) ==="
$PGEXEC <<SQL
WITH hr_user AS (SELECT id FROM public.users WHERE role='hr' AND username LIKE 'seed-%' LIMIT 1),
     inv_user AS (SELECT id FROM public.users WHERE role='investigator' AND username LIKE 'seed-%' LIMIT 1),
     random_endpoints AS (SELECT id FROM public.endpoints WHERE is_active=true ORDER BY random() LIMIT 15),
     seq AS (SELECT generate_series(1, 15) AS n)
INSERT INTO public.live_view_sessions(
  id, tenant_id, endpoint_id, requester_id, approver_id, reason_code, justification,
  requested_duration_seconds, state, created_at, approved_at, started_at, ended_at
)
SELECT
  '01J' || upper(substr(md5(random()::text || seq.n::text), 1, 23)),
  '$TENANT_ID'::uuid,
  (SELECT id FROM random_endpoints ORDER BY random() LIMIT 1),
  (SELECT id FROM inv_user),
  CASE WHEN seq.n > 3 THEN (SELECT id FROM hr_user) ELSE NULL END,
  (ARRAY['security_incident','policy_violation','hr_request','forensic_investigation','dlp_followup'])[1 + (seq.n % 5)],
  (ARRAY[
    'KVKK m.6 kapsamında olay inceleme',
    'Şüpheli veri aktarım denemesi izlendi',
    'Disiplin soruşturması — İK talebi',
    'Forensic inceleme: belirlenen zaman aralığı',
    'DLP eşleşmesi sonrası kısa gözlem'
  ])[1 + (seq.n % 5)],
  900 + (seq.n * 60),
  CASE
    WHEN seq.n <= 3 THEN 'REQUESTED'
    WHEN seq.n <= 5 THEN 'APPROVED'
    WHEN seq.n = 6  THEN 'DENIED'
    WHEN seq.n = 7  THEN 'EXPIRED'
    WHEN seq.n = 8  THEN 'TERMINATED_BY_HR'
    ELSE 'ENDED'
  END,
  now() - ((90 - seq.n * 5) || ' days')::interval - (random() * 12 || ' hours')::interval,
  CASE WHEN seq.n > 3 THEN now() - ((90 - seq.n * 5) || ' days')::interval - interval '5 minutes' ELSE NULL END,
  CASE WHEN seq.n > 5 AND seq.n != 6 THEN now() - ((90 - seq.n * 5) || ' days')::interval - interval '4 minutes' ELSE NULL END,
  CASE WHEN seq.n >= 7 THEN now() - ((90 - seq.n * 5) || ' days')::interval + interval '12 minutes' ELSE NULL END
FROM seq;

SELECT state, count(*) FROM public.live_view_sessions GROUP BY state ORDER BY state;
SQL

echo "=== Seeding DSR requests (8 × mixed states) ==="
$PGEXEC <<SQL
WITH employees AS (SELECT id FROM public.users WHERE role='employee' AND username LIKE 'seed-%' ORDER BY random() LIMIT 8),
     seq AS (SELECT generate_series(1, 8) AS n),
     emp_with_seq AS (SELECT id, row_number() OVER () AS n FROM employees)
INSERT INTO public.dsr_requests(
  id, tenant_id, employee_user_id, request_type, scope_json, justification, state,
  created_at, sla_deadline
)
SELECT
  gen_random_uuid(),
  '$TENANT_ID'::uuid,
  e.id,
  (ARRAY['access','erase','rectify','restrict','portability','object'])[1 + (e.n % 6)],
  jsonb_build_object('categories', array['app_usage','screenshots']),
  'Demo amaçlı veri sahibi talebi — ' || e.n,
  CASE
    WHEN e.n <= 3 THEN 'open'
    WHEN e.n <= 5 THEN 'at_risk'
    WHEN e.n = 6  THEN 'overdue'
    WHEN e.n = 7  THEN 'resolved'
    ELSE 'rejected'
  END,
  now() - ((35 - e.n * 4) || ' days')::interval,
  now() + ((30 - (35 - e.n * 4)) || ' days')::interval
FROM emp_with_seq e;

SELECT state, count(*) FROM public.dsr_requests GROUP BY state ORDER BY state;
SQL

echo "=== Seeding audit.audit_log via append_event ==="
$PGEXEC <<'SQL'
DO $$
DECLARE
  tenant UUID := '00000000-0000-0000-0000-000000000001';
  actor TEXT;
  actions TEXT[] := ARRAY[
    'policy.pushed','policy.created','policy.updated',
    'live_view.requested','live_view.approved','live_view.started','live_view.stopped',
    'dsr.submitted','dsr.assigned','dsr.responded',
    'endpoint.enrolled','endpoint.revoked',
    'screenshot.viewed','file_event.viewed',
    'audit.chain_verified','backup.run','agent.silence.ack'
  ];
  action TEXT;
  i INT;
BEGIN
  FOR i IN 1..40 LOOP
    actor := (ARRAY['seed-admin','seed-hr-ozgur','seed-inv-tolga','dpo-test','system'])[1 + (i % 5)];
    action := actions[1 + (i % array_length(actions, 1))];
    PERFORM audit.append_event(
      actor,
      NULL::inet,
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) demo-seed',
      tenant,
      action,
      CASE
        WHEN action LIKE 'policy.%' THEN 'policy:' || i
        WHEN action LIKE 'live_view.%' THEN 'session:' || i
        WHEN action LIKE 'dsr.%' THEN 'dsr:' || i
        WHEN action LIKE 'endpoint.%' THEN 'endpoint:' || i
        ELSE 'resource:' || i
      END,
      jsonb_build_object('seed_iteration', i, 'note', 'seed-evidence-' || i)
    );
  END LOOP;
END;
$$;
SELECT count(*) AS audit_rows FROM audit.audit_log;
SQL

echo "=== Seeding silence_acknowledgements (5 gaps) ==="
$PGEXEC <<SQL
WITH endpoints_sub AS (SELECT id FROM public.endpoints WHERE is_active=true ORDER BY random() LIMIT 5),
     endpoint_seq AS (SELECT id, row_number() OVER () AS n FROM endpoints_sub)
INSERT INTO public.silence_acknowledgements(tenant_id, endpoint_id, silence_at, acknowledged_by, acknowledged_at)
SELECT
  '$TENANT_ID'::uuid,
  e.id,
  now() - ((10 - e.n) || ' days')::interval,
  (SELECT id FROM public.users WHERE role='hr' LIMIT 1),
  now() - ((10 - e.n) || ' days')::interval + interval '2 hours'
FROM endpoint_seq e;
SELECT count(*) FROM public.silence_acknowledgements;
SQL

echo
echo "=== SEED COMPLETE ==="
$PGEXEC <<SQL
SELECT 'users' AS what, count(*) FROM public.users WHERE username LIKE 'seed-%'
UNION ALL SELECT 'endpoints', count(*) FROM public.endpoints
UNION ALL SELECT 'live_view_sessions', count(*) FROM public.live_view_sessions
UNION ALL SELECT 'dsr_requests', count(*) FROM public.dsr_requests
UNION ALL SELECT 'audit_log', count(*) FROM audit.audit_log
UNION ALL SELECT 'silence_gaps', count(*) FROM public.silence_acknowledgements;
SQL
