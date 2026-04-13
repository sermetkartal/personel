#!/usr/bin/env bash
# =============================================================================
# dev-seed-showcase.sh — presentation-grade seed for 3 showcase employees.
#
# Creates 3 distinctive personas with 30 days of rich data covering every
# signal the 23-collector agent can produce. Each persona has:
#   - Rich top_apps with per-app file-level breakdowns
#   - rich_signals blob covering browser, email, filesystem, network, usb,
#     bluetooth, mtp, system, device, print, clipboard, keystroke metadata,
#     liveview, policy enforcement, tamper findings
#   - 24 hourly buckets per day
#   - 1-2 assigned endpoints each
#
# Personas:
#   1. Zeynep Kaya — Finans Yöneticisi, yüksek üretken (Excel/Logo Tiger)
#   2. Mert Yılmaz — Kıdemli Yazılım Mühendisi, orta üretken (VS Code/Git)
#   3. Elif Demir  — Satış Temsilcisi, dağılmış profil (Outlook/WhatsApp/USB)
#
# Re-runnable: upserts via ON CONFLICT.
# =============================================================================
set -euo pipefail

PGEXEC='docker exec -i personel-postgres psql -U postgres -d personel -v ON_ERROR_STOP=1'

echo "=== Creating 3 showcase employees ==="

$PGEXEC <<'SQL'
-- Resolve the first active tenant
DO $$
DECLARE
  v_tenant_id UUID;
  v_zeynep_id UUID;
  v_mert_id UUID;
  v_elif_id UUID;
BEGIN
  SELECT id INTO v_tenant_id FROM tenants WHERE is_active = true ORDER BY created_at LIMIT 1;
  IF v_tenant_id IS NULL THEN
    RAISE EXCEPTION 'No active tenant found';
  END IF;

  -- Zeynep Kaya — Finans Yöneticisi
  INSERT INTO users (tenant_id, keycloak_sub, username, email, role, department, job_title, hired_at, locale)
  VALUES (v_tenant_id, 'showcase-zeynep-sub', 'showcase-zeynep', 'zeynep.kaya@personel.demo',
          'manager', 'Finans', 'Finans Müdürü', now() - interval '3 years', 'tr')
  ON CONFLICT (keycloak_sub) DO UPDATE SET
    department = EXCLUDED.department,
    job_title  = EXCLUDED.job_title
  RETURNING id INTO v_zeynep_id;

  -- Mert Yılmaz — Kıdemli Yazılım Mühendisi
  INSERT INTO users (tenant_id, keycloak_sub, username, email, role, department, job_title, hired_at, locale)
  VALUES (v_tenant_id, 'showcase-mert-sub', 'showcase-mert', 'mert.yilmaz@personel.demo',
          'employee', 'Mühendislik', 'Kıdemli Yazılım Mühendisi', now() - interval '18 months', 'tr')
  ON CONFLICT (keycloak_sub) DO UPDATE SET
    department = EXCLUDED.department,
    job_title  = EXCLUDED.job_title
  RETURNING id INTO v_mert_id;

  -- Elif Demir — Satış Temsilcisi
  INSERT INTO users (tenant_id, keycloak_sub, username, email, role, department, job_title, hired_at, locale)
  VALUES (v_tenant_id, 'showcase-elif-sub', 'showcase-elif', 'elif.demir@personel.demo',
          'employee', 'Satış', 'Kıdemli Satış Temsilcisi', now() - interval '8 months', 'tr')
  ON CONFLICT (keycloak_sub) DO UPDATE SET
    department = EXCLUDED.department,
    job_title  = EXCLUDED.job_title
  RETURNING id INTO v_elif_id;

  -- Assigned endpoints (one laptop each; Zeynep has desktop + laptop)
  INSERT INTO endpoints (id, tenant_id, hostname, os, os_version, agent_version,
                         enrolled_at, last_seen_at, is_active, assigned_user_id, status)
  VALUES
    (gen_random_uuid(), v_tenant_id, 'FIN-LAPTOP-ZK01', 'windows', '11 Pro 23H2', '0.9.3',
     now() - interval '3 years', now() - interval '2 minutes', true, v_zeynep_id, 'online'),
    (gen_random_uuid(), v_tenant_id, 'FIN-DESKTOP-ZK02', 'windows', '11 Pro 23H2', '0.9.3',
     now() - interval '2 years', now() - interval '4 hours', true, v_zeynep_id, 'online'),
    (gen_random_uuid(), v_tenant_id, 'DEV-LAPTOP-MY01', 'windows', '11 Pro 23H2', '0.9.3',
     now() - interval '18 months', now() - interval '1 minute', true, v_mert_id, 'online'),
    (gen_random_uuid(), v_tenant_id, 'SAL-LAPTOP-ED01', 'windows', '11 Home 23H2', '0.9.3',
     now() - interval '8 months', now() - interval '25 minutes', true, v_elif_id, 'online')
  ON CONFLICT DO NOTHING;

  RAISE NOTICE 'Zeynep: %, Mert: %, Elif: %', v_zeynep_id, v_mert_id, v_elif_id;
END $$;
SQL

echo
echo "=== Seeding employee_daily_stats (30 days x 3 showcase employees) ==="

$PGEXEC <<'SQL'
-- Per-persona rich data profiles. Each profile emits a per-day row with
-- top_apps (including files arrays) and a rich_signals blob.

WITH showcase AS (
  SELECT id, username, department, role
  FROM users
  WHERE username IN ('showcase-zeynep','showcase-mert','showcase-elif')
),
days AS (
  SELECT (current_date - (n || ' days')::interval)::date AS day,
         n,
         EXTRACT(ISODOW FROM (current_date - (n || ' days')::interval)) AS dow
  FROM generate_series(0, 29) AS n
),
base AS (
  SELECT
    s.id AS user_id,
    s.username,
    s.department,
    d.day,
    d.n AS days_ago,
    d.dow,
    -- Is today? Strong numbers. Weekend? Usually zero.
    CASE
      WHEN d.day = current_date THEN true
      ELSE false
    END AS is_today,
    (d.dow >= 6) AS is_weekend
  FROM showcase s CROSS JOIN days d
),
shaped AS (
  SELECT
    b.*,
    -- Zeynep: high producer, 420-500 min active on weekdays
    -- Mert:   medium,       360-460 min active on weekdays
    -- Elif:   distracted,   260-380 min active on weekdays
    CASE
      WHEN b.is_weekend AND b.username = 'showcase-zeynep' AND random() > 0.85 THEN (60 + random()*90)::int
      WHEN b.is_weekend THEN 0
      WHEN b.username = 'showcase-zeynep' THEN (420 + random()*80)::int
      WHEN b.username = 'showcase-mert'   THEN (360 + random()*100)::int
      WHEN b.username = 'showcase-elif'   THEN (260 + random()*120)::int
    END AS active_min,
    CASE
      WHEN b.is_weekend THEN (random()*30)::int
      WHEN b.username = 'showcase-zeynep' THEN (40 + random()*70)::int
      WHEN b.username = 'showcase-mert'   THEN (60 + random()*90)::int
      WHEN b.username = 'showcase-elif'   THEN (90 + random()*130)::int
    END AS idle_min,
    CASE
      WHEN b.is_weekend THEN 0
      WHEN b.username = 'showcase-zeynep' THEN (45 + random()*30)::int
      WHEN b.username = 'showcase-mert'   THEN (30 + random()*20)::int
      WHEN b.username = 'showcase-elif'   THEN (55 + random()*40)::int
    END AS screens,
    CASE
      WHEN b.is_weekend THEN 0
      WHEN b.username = 'showcase-zeynep' THEN (6000 + random()*4000)::int
      WHEN b.username = 'showcase-mert'   THEN (12000 + random()*6000)::int
      WHEN b.username = 'showcase-elif'   THEN (3000 + random()*3000)::int
    END AS keys,
    CASE
      WHEN b.is_weekend THEN (35 + random()*25)::int
      WHEN b.username = 'showcase-zeynep' THEN (78 + random()*17)::int
      WHEN b.username = 'showcase-mert'   THEN (72 + random()*20)::int
      WHEN b.username = 'showcase-elif'   THEN (52 + random()*25)::int
    END AS score
  FROM base b
)
INSERT INTO employee_daily_stats(
  user_id, day, active_minutes, idle_minutes, screenshot_count, keystroke_count,
  productivity_score, top_apps, rich_signals, first_activity_at, last_activity_at, updated_at
)
SELECT
  user_id,
  day,
  active_min,
  idle_min,
  screens,
  keys,
  LEAST(100, GREATEST(0, score)),
  -- top_apps by persona with per-app file breakdowns
  CASE username
    WHEN 'showcase-zeynep' THEN jsonb_build_array(
      jsonb_build_object(
        'name','Microsoft Excel','category','productive',
        'minutes', (active_min * 0.42)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','C:\Users\zeynep\Documents\Finans\2026_Q1_Butce_Konsolide.xlsx','minutes', (active_min*0.18)::int),
          jsonb_build_object('path','C:\Users\zeynep\Documents\Finans\Nakit_Akis_Mart.xlsx','minutes', (active_min*0.11)::int),
          jsonb_build_object('path','C:\Users\zeynep\Documents\Finans\Bordro_Uzlasma_Nisan.xlsx','minutes', (active_min*0.08)::int),
          jsonb_build_object('path','C:\Users\zeynep\OneDrive\Paylasilanlar\KPI_Raporu_2026Q1.xlsx','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','Logo Tiger 3','category','productive',
        'minutes', (active_min * 0.24)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Muhasebe / Yevmiye Fişi Girişi','minutes', (active_min*0.12)::int),
          jsonb_build_object('path','Muhasebe / Mizan Raporu','minutes', (active_min*0.07)::int),
          jsonb_build_object('path','Finans / Banka Hareketleri','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','Microsoft Outlook','category','productive',
        'minutes', (active_min * 0.14)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Gelen Kutusu / Mart Kapanış','minutes', (active_min*0.06)::int),
          jsonb_build_object('path','Gelen Kutusu / Denetim Yazışmaları','minutes', (active_min*0.04)::int),
          jsonb_build_object('path','Gönderilenler / CFO Raporlaması','minutes', (active_min*0.04)::int)
        )
      ),
      jsonb_build_object(
        'name','Microsoft Teams','category','productive',
        'minutes', (active_min * 0.10)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Finans Ekibi / Haftalık Stand-up','minutes', (active_min*0.05)::int),
          jsonb_build_object('path','CFO Sync','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','Denetçi Görüşmesi (PwC)','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','Google Chrome','category','neutral',
        'minutes', (active_min * 0.07)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','interaktif.gib.gov.tr','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','ebirlik.com.tr','minutes', (active_min*0.02)::int),
          jsonb_build_object('path','tcmb.gov.tr','minutes', (active_min*0.02)::int)
        )
      )
    )
    WHEN 'showcase-mert' THEN jsonb_build_array(
      jsonb_build_object(
        'name','Visual Studio Code','category','productive',
        'minutes', (active_min * 0.48)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','C:\personel\apps\api\internal\user\employee_detail.go','minutes', (active_min*0.14)::int),
          jsonb_build_object('path','C:\personel\apps\console\src\app\[locale]\(app)\employees\[id]\detail-client.tsx','minutes', (active_min*0.12)::int),
          jsonb_build_object('path','C:\personel\apps\api\internal\postgres\migrations\0030_employee_daily_stats_rich_signals.up.sql','minutes', (active_min*0.08)::int),
          jsonb_build_object('path','C:\personel\apps\agent\crates\personel-collectors\src\network.rs','minutes', (active_min*0.07)::int),
          jsonb_build_object('path','C:\personel\apps\gateway\internal\enricher\classifier.go','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','Google Chrome','category','neutral',
        'minutes', (active_min * 0.18)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','github.com/sermetkartal/personel','minutes', (active_min*0.08)::int),
          jsonb_build_object('path','stackoverflow.com','minutes', (active_min*0.04)::int),
          jsonb_build_object('path','doc.rust-lang.org','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','nextjs.org/docs','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','Windows Terminal','category','productive',
        'minutes', (active_min * 0.14)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','cargo build --release','minutes', (active_min*0.05)::int),
          jsonb_build_object('path','docker compose logs -f api','minutes', (active_min*0.04)::int),
          jsonb_build_object('path','git push origin main','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','psql -U postgres -d personel','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','Slack','category','neutral',
        'minutes', (active_min * 0.09)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','#dev-backend','minutes', (active_min*0.04)::int),
          jsonb_build_object('path','#general','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','DM: Ahmet','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','YouTube','category','distracting',
        'minutes', (active_min * 0.06)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Rust Async Deep Dive — Jon Gjengset','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','Primeagen: VS Code vs Neovim','minutes', (active_min*0.02)::int),
          jsonb_build_object('path','Lofi Hip Hop Radio','minutes', (active_min*0.01)::int)
        )
      ),
      jsonb_build_object(
        'name','Docker Desktop','category','productive',
        'minutes', (active_min * 0.05)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','personel-postgres','minutes', (active_min*0.02)::int),
          jsonb_build_object('path','personel-api','minutes', (active_min*0.02)::int),
          jsonb_build_object('path','personel-console','minutes', (active_min*0.01)::int)
        )
      )
    )
    ELSE jsonb_build_array(
      jsonb_build_object(
        'name','Microsoft Outlook','category','productive',
        'minutes', (active_min * 0.30)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Gelen Kutusu / Teklif Yazışmaları','minutes', (active_min*0.11)::int),
          jsonb_build_object('path','Gelen Kutusu / Müşteri Takip','minutes', (active_min*0.08)::int),
          jsonb_build_object('path','Gönderilenler / Sözleşme Dosyaları','minutes', (active_min*0.06)::int),
          jsonb_build_object('path','Takvim / Müşteri Toplantıları','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','Salesforce','category','productive',
        'minutes', (active_min * 0.20)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Opportunities / Q2 Pipeline','minutes', (active_min*0.09)::int),
          jsonb_build_object('path','Leads / Soğuk Arama Listesi','minutes', (active_min*0.06)::int),
          jsonb_build_object('path','Contacts / Müşteri 360','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','WhatsApp Web','category','distracting',
        'minutes', (active_min * 0.22)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Kişisel Sohbetler','minutes', (active_min*0.10)::int),
          jsonb_build_object('path','Aile Grubu','minutes', (active_min*0.07)::int),
          jsonb_build_object('path','Müşteri WhatsApp Kanalı','minutes', (active_min*0.05)::int)
        )
      ),
      jsonb_build_object(
        'name','Instagram','category','distracting',
        'minutes', (active_min * 0.12)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Ana Akış','minutes', (active_min*0.06)::int),
          jsonb_build_object('path','Reels','minutes', (active_min*0.04)::int),
          jsonb_build_object('path','DM','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','Microsoft Excel','category','productive',
        'minutes', (active_min * 0.10)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','C:\Users\elif\Documents\Satis\Teklif_Sablonu.xlsx','minutes', (active_min*0.05)::int),
          jsonb_build_object('path','C:\Users\elif\Documents\Satis\Musteri_Listesi.xlsx','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','C:\Users\elif\Downloads\Rakip_Analizi.xlsx','minutes', (active_min*0.02)::int)
        )
      ),
      jsonb_build_object(
        'name','Zoom','category','productive',
        'minutes', (active_min * 0.06)::int,
        'files', jsonb_build_array(
          jsonb_build_object('path','Müşteri Demo / ABC Ltd','minutes', (active_min*0.03)::int),
          jsonb_build_object('path','Pipeline Review','minutes', (active_min*0.02)::int),
          jsonb_build_object('path','Eğitim: Satış Teknikleri','minutes', (active_min*0.01)::int)
        )
      )
    )
  END AS top_apps,
  -- rich_signals by persona
  CASE username
    WHEN 'showcase-zeynep' THEN jsonb_build_object(
      'browser', jsonb_build_object(
        'top_domains', jsonb_build_array(
          jsonb_build_object('domain','interaktif.gib.gov.tr','visits', (12 + random()*8)::int, 'minutes', (15 + random()*10)::int),
          jsonb_build_object('domain','ebirlik.com.tr','visits', (8 + random()*5)::int, 'minutes', (10 + random()*8)::int),
          jsonb_build_object('domain','tcmb.gov.tr','visits', (6 + random()*4)::int, 'minutes', (8 + random()*5)::int),
          jsonb_build_object('domain','kap.org.tr','visits', (4 + random()*3)::int, 'minutes', (5 + random()*4)::int),
          jsonb_build_object('domain','linkedin.com','visits', (3 + random()*2)::int, 'minutes', (4 + random()*3)::int)
        ),
        'incognito_blocked', 0
      ),
      'email', jsonb_build_object(
        'sent', (22 + random()*18)::int,
        'received', (68 + random()*40)::int,
        'top_correspondents', jsonb_build_array(
          jsonb_build_object('address','cfo@personel.demo','count', (12 + random()*8)::int),
          jsonb_build_object('address','denetim@pwc.com','count', (8 + random()*5)::int),
          jsonb_build_object('address','muhasebe@personel.demo','count', (15 + random()*10)::int),
          jsonb_build_object('address','tedarik@abcltd.com.tr','count', (6 + random()*4)::int)
        ),
        'redacted_subjects', (2 + random()*3)::int
      ),
      'filesystem', jsonb_build_object(
        'created', (18 + random()*12)::int,
        'written', (142 + random()*80)::int,
        'deleted', (8 + random()*6)::int,
        'sensitive_hashed', (6 + random()*4)::int,
        'top_paths', jsonb_build_array(
          jsonb_build_object('path','C:\Users\zeynep\Documents\Finans','events', (85 + random()*40)::int),
          jsonb_build_object('path','C:\Users\zeynep\OneDrive\Paylasilanlar','events', (42 + random()*20)::int),
          jsonb_build_object('path','C:\Users\zeynep\Downloads','events', (18 + random()*10)::int)
        )
      ),
      'network', jsonb_build_object(
        'flows', (4200 + random()*1800)::int,
        'dns_queries', (1850 + random()*600)::int,
        'top_hosts', jsonb_build_array(
          jsonb_build_object('host','office365.com','bytes', (85000000 + random()*30000000)::bigint),
          jsonb_build_object('host','interaktif.gib.gov.tr','bytes', (12000000 + random()*5000000)::bigint),
          jsonb_build_object('host','onedrive.live.com','bytes', (45000000 + random()*20000000)::bigint),
          jsonb_build_object('host','logo-tiger-server.local','bytes', (8000000 + random()*3000000)::bigint)
        ),
        'geoip', jsonb_build_array(
          jsonb_build_object('country','TR','ip_count',142),
          jsonb_build_object('country','US','ip_count',38),
          jsonb_build_object('country','IE','ip_count',12),
          jsonb_build_object('country','DE','ip_count',6)
        )
      ),
      'usb', jsonb_build_object(
        'attached', 1, 'removed', 1,
        'timeline', jsonb_build_array(
          jsonb_build_object('ts', (day::timestamptz + interval '10 hours 24 minutes')::text,
                              'event','attached','vendor','Kingston','product','DataTraveler 64GB'),
          jsonb_build_object('ts', (day::timestamptz + interval '11 hours 02 minutes')::text,
                              'event','removed','vendor','Kingston','product','DataTraveler 64GB')
        )
      ),
      'bluetooth', jsonb_build_object(
        'paired_devices', jsonb_build_array(
          jsonb_build_object('name','Logitech MX Master 3','class','Mouse'),
          jsonb_build_object('name','Sony WH-1000XM5','class','Headphones'),
          jsonb_build_object('name','Zeynep iPhone','class','Phone')
        )
      ),
      'mtp', jsonb_build_object(
        'devices', jsonb_build_array(
          jsonb_build_object('friendly_name','Zeynep iPhone','manufacturer','Apple Inc.')
        )
      ),
      'system', jsonb_build_object(
        'locks', (8 + random()*4)::int, 'unlocks', (8 + random()*4)::int,
        'sleeps', 1, 'wakes', 1, 'av_deactivated', 0
      ),
      'device', jsonb_build_object(
        'cpu_avg_percent', 18.4 + random()*8, 'rss_avg_mb', (118 + random()*20)::int,
        'battery_percent', (65 + random()*30)::int, 'battery_charging', true,
        'uptime_hours', 8.5 + random()*1.5
      ),
      'print', jsonb_build_object(
        'jobs', (3 + random()*4)::int, 'pages', (18 + random()*20)::int,
        'top_printers', jsonb_build_array(
          jsonb_build_object('printer','Finans-HP-LaserJet','jobs',(4 + random()*3)::int),
          jsonb_build_object('printer','Genel-Xerox-WorkCentre','jobs',(1 + random()*2)::int)
        )
      ),
      'clipboard', jsonb_build_object(
        'metadata_events', (42 + random()*30)::int,
        'redaction_hits', jsonb_build_array(
          jsonb_build_object('rule','IBAN','count',(2 + random()*2)::int),
          jsonb_build_object('rule','TCKN','count',(1 + random()*2)::int)
        )
      ),
      'keystroke', jsonb_build_object(
        'total_events', keys, 'encrypted_blobs', (keys / 500)::int, 'dlp_enabled', false
      ),
      'liveview', jsonb_build_object(
        'sessions', 0
      ),
      'policy', jsonb_build_object(
        'blocked_app_attempts', 0, 'blocked_web_attempts', 0
      ),
      'tamper', jsonb_build_object(
        'findings', 0, 'last_check', (now() - interval '12 minutes')::text
      )
    )
    WHEN 'showcase-mert' THEN jsonb_build_object(
      'browser', jsonb_build_object(
        'top_domains', jsonb_build_array(
          jsonb_build_object('domain','github.com','visits',(42 + random()*20)::int,'minutes',(48 + random()*20)::int),
          jsonb_build_object('domain','stackoverflow.com','visits',(28 + random()*15)::int,'minutes',(22 + random()*10)::int),
          jsonb_build_object('domain','doc.rust-lang.org','visits',(18 + random()*10)::int,'minutes',(15 + random()*8)::int),
          jsonb_build_object('domain','nextjs.org','visits',(12 + random()*8)::int,'minutes',(10 + random()*6)::int),
          jsonb_build_object('domain','pkg.go.dev','visits',(8 + random()*5)::int,'minutes',(6 + random()*4)::int),
          jsonb_build_object('domain','youtube.com','visits',(6 + random()*4)::int,'minutes',(18 + random()*10)::int)
        ),
        'incognito_blocked', (random()*2)::int
      ),
      'email', jsonb_build_object(
        'sent', (8 + random()*6)::int,
        'received', (32 + random()*20)::int,
        'top_correspondents', jsonb_build_array(
          jsonb_build_object('address','ci-cd@personel.demo','count',(18 + random()*10)::int),
          jsonb_build_object('address','ahmet.ozkan@personel.demo','count',(6 + random()*4)::int),
          jsonb_build_object('address','github-notifications@github.com','count',(42 + random()*20)::int),
          jsonb_build_object('address','sentry@personel.demo','count',(5 + random()*3)::int)
        ),
        'redacted_subjects', 0
      ),
      'filesystem', jsonb_build_object(
        'created', (58 + random()*30)::int,
        'written', (384 + random()*150)::int,
        'deleted', (22 + random()*15)::int,
        'sensitive_hashed', 0,
        'top_paths', jsonb_build_array(
          jsonb_build_object('path','C:\personel\apps\api','events',(180 + random()*60)::int),
          jsonb_build_object('path','C:\personel\apps\console\src','events',(120 + random()*40)::int),
          jsonb_build_object('path','C:\personel\apps\agent\crates','events',(75 + random()*30)::int)
        )
      ),
      'network', jsonb_build_object(
        'flows', (8500 + random()*3500)::int,
        'dns_queries', (3200 + random()*1200)::int,
        'top_hosts', jsonb_build_array(
          jsonb_build_object('host','github.com','bytes',(180000000 + random()*80000000)::bigint),
          jsonb_build_object('host','registry.npmjs.org','bytes',(240000000 + random()*100000000)::bigint),
          jsonb_build_object('host','crates.io','bytes',(95000000 + random()*40000000)::bigint),
          jsonb_build_object('host','docker.io','bytes',(620000000 + random()*200000000)::bigint),
          jsonb_build_object('host','proxy.golang.org','bytes',(45000000 + random()*20000000)::bigint)
        ),
        'geoip', jsonb_build_array(
          jsonb_build_object('country','TR','ip_count',85),
          jsonb_build_object('country','US','ip_count',124),
          jsonb_build_object('country','DE','ip_count',28),
          jsonb_build_object('country','NL','ip_count',16),
          jsonb_build_object('country','FR','ip_count',9)
        )
      ),
      'usb', jsonb_build_object(
        'attached', (random()*2)::int, 'removed', (random()*2)::int,
        'timeline', CASE WHEN random() > 0.5 THEN jsonb_build_array(
          jsonb_build_object('ts',(day::timestamptz + interval '14 hours 12 minutes')::text,
                              'event','attached','vendor','SanDisk','product','Extreme Pro 1TB'),
          jsonb_build_object('ts',(day::timestamptz + interval '16 hours 45 minutes')::text,
                              'event','removed','vendor','SanDisk','product','Extreme Pro 1TB')
        ) ELSE '[]'::jsonb END
      ),
      'bluetooth', jsonb_build_object(
        'paired_devices', jsonb_build_array(
          jsonb_build_object('name','Keychron K8','class','Keyboard'),
          jsonb_build_object('name','Logitech MX Master 3S','class','Mouse'),
          jsonb_build_object('name','AirPods Pro','class','Headphones'),
          jsonb_build_object('name','Mert Pixel','class','Phone')
        )
      ),
      'mtp', jsonb_build_object(
        'devices', jsonb_build_array(
          jsonb_build_object('friendly_name','Pixel 8 Pro','manufacturer','Google Inc.')
        )
      ),
      'system', jsonb_build_object(
        'locks', (4 + random()*3)::int, 'unlocks', (4 + random()*3)::int,
        'sleeps', 0, 'wakes', 0, 'av_deactivated', 0
      ),
      'device', jsonb_build_object(
        'cpu_avg_percent', 42.8 + random()*15, 'rss_avg_mb', (145 + random()*25)::int,
        'battery_percent', (72 + random()*25)::int, 'battery_charging', false,
        'uptime_hours', 11.2 + random()*3
      ),
      'print', jsonb_build_object('jobs', 0, 'pages', 0, 'top_printers', '[]'::jsonb),
      'clipboard', jsonb_build_object(
        'metadata_events', (128 + random()*80)::int,
        'redaction_hits', '[]'::jsonb
      ),
      'keystroke', jsonb_build_object(
        'total_events', keys, 'encrypted_blobs', (keys / 450)::int, 'dlp_enabled', false
      ),
      'liveview', jsonb_build_object('sessions', 0),
      'policy', jsonb_build_object('blocked_app_attempts', 0, 'blocked_web_attempts', (random()*3)::int),
      'tamper', jsonb_build_object('findings', 0, 'last_check',(now() - interval '8 minutes')::text)
    )
    ELSE jsonb_build_object(
      'browser', jsonb_build_object(
        'top_domains', jsonb_build_array(
          jsonb_build_object('domain','web.whatsapp.com','visits',(68 + random()*30)::int,'minutes',(85 + random()*30)::int),
          jsonb_build_object('domain','instagram.com','visits',(45 + random()*20)::int,'minutes',(52 + random()*20)::int),
          jsonb_build_object('domain','salesforce.com','visits',(38 + random()*15)::int,'minutes',(62 + random()*20)::int),
          jsonb_build_object('domain','linkedin.com','visits',(28 + random()*12)::int,'minutes',(18 + random()*10)::int),
          jsonb_build_object('domain','youtube.com','visits',(22 + random()*10)::int,'minutes',(38 + random()*15)::int),
          jsonb_build_object('domain','hepsiburada.com','visits',(12 + random()*8)::int,'minutes',(15 + random()*10)::int),
          jsonb_build_object('domain','trendyol.com','visits',(10 + random()*6)::int,'minutes',(12 + random()*8)::int)
        ),
        'incognito_blocked', (1 + random()*3)::int
      ),
      'email', jsonb_build_object(
        'sent', (18 + random()*12)::int,
        'received', (48 + random()*25)::int,
        'top_correspondents', jsonb_build_array(
          jsonb_build_object('address','satis-mudur@personel.demo','count',(8 + random()*5)::int),
          jsonb_build_object('address','info@abcltd.com.tr','count',(5 + random()*4)::int),
          jsonb_build_object('address','potansiyel@kozmetiksa.com','count',(6 + random()*4)::int),
          jsonb_build_object('address','kisisel@gmail.com','count',(12 + random()*8)::int)
        ),
        'redacted_subjects', (1 + random()*2)::int
      ),
      'filesystem', jsonb_build_object(
        'created', (12 + random()*8)::int,
        'written', (48 + random()*30)::int,
        'deleted', (5 + random()*4)::int,
        'sensitive_hashed', (1 + random()*2)::int,
        'top_paths', jsonb_build_array(
          jsonb_build_object('path','C:\Users\elif\Downloads','events',(62 + random()*25)::int),
          jsonb_build_object('path','C:\Users\elif\Documents\Satis','events',(35 + random()*15)::int),
          jsonb_build_object('path','C:\Users\elif\Desktop','events',(22 + random()*10)::int)
        )
      ),
      'network', jsonb_build_object(
        'flows', (3800 + random()*1200)::int,
        'dns_queries', (2100 + random()*800)::int,
        'top_hosts', jsonb_build_array(
          jsonb_build_object('host','web.whatsapp.com','bytes',(220000000 + random()*80000000)::bigint),
          jsonb_build_object('host','instagram.com','bytes',(480000000 + random()*200000000)::bigint),
          jsonb_build_object('host','salesforce.com','bytes',(65000000 + random()*20000000)::bigint),
          jsonb_build_object('host','youtube.com','bytes',(320000000 + random()*150000000)::bigint),
          jsonb_build_object('host','hepsiburada.com','bytes',(35000000 + random()*15000000)::bigint)
        ),
        'geoip', jsonb_build_array(
          jsonb_build_object('country','TR','ip_count',96),
          jsonb_build_object('country','US','ip_count',58),
          jsonb_build_object('country','IE','ip_count',22)
        )
      ),
      'usb', jsonb_build_object(
        'attached', CASE WHEN is_today THEN 2 ELSE (random()*2)::int END,
        'removed',  CASE WHEN is_today THEN 1 ELSE (random()*2)::int END,
        'timeline', CASE WHEN is_today THEN jsonb_build_array(
          jsonb_build_object('ts',(day::timestamptz + interval '09 hours 42 minutes')::text,
                              'event','attached','vendor','Samsung','product','T7 Portable SSD 2TB'),
          jsonb_build_object('ts',(day::timestamptz + interval '11 hours 15 minutes')::text,
                              'event','attached','vendor','Unknown','product','USB Mass Storage 8GB'),
          jsonb_build_object('ts',(day::timestamptz + interval '11 hours 28 minutes')::text,
                              'event','removed','vendor','Unknown','product','USB Mass Storage 8GB')
        ) ELSE '[]'::jsonb END
      ),
      'bluetooth', jsonb_build_object(
        'paired_devices', jsonb_build_array(
          jsonb_build_object('name','Elif iPhone 15','class','Phone'),
          jsonb_build_object('name','AirPods','class','Headphones'),
          jsonb_build_object('name','JBL Flip 5','class','Speaker')
        )
      ),
      'mtp', jsonb_build_object(
        'devices', jsonb_build_array(
          jsonb_build_object('friendly_name','iPhone 15','manufacturer','Apple Inc.'),
          jsonb_build_object('friendly_name','Canon EOS R50','manufacturer','Canon Inc.')
        )
      ),
      'system', jsonb_build_object(
        'locks', (14 + random()*8)::int, 'unlocks', (14 + random()*8)::int,
        'sleeps', (2 + random()*2)::int, 'wakes', (2 + random()*2)::int,
        'av_deactivated', CASE WHEN is_today THEN 1 ELSE 0 END
      ),
      'device', jsonb_build_object(
        'cpu_avg_percent', 22.1 + random()*10, 'rss_avg_mb', (108 + random()*25)::int,
        'battery_percent', (35 + random()*40)::int, 'battery_charging', false,
        'uptime_hours', 6.8 + random()*2
      ),
      'print', jsonb_build_object(
        'jobs', (2 + random()*3)::int, 'pages', (12 + random()*15)::int,
        'top_printers', jsonb_build_array(
          jsonb_build_object('printer','Satis-Ofisi-Canon','jobs',(2 + random()*3)::int)
        )
      ),
      'clipboard', jsonb_build_object(
        'metadata_events', (85 + random()*40)::int,
        'redaction_hits', jsonb_build_array(
          jsonb_build_object('rule','KrediKarti','count',(1 + random()*2)::int),
          jsonb_build_object('rule','TCKN','count',(2 + random()*3)::int),
          jsonb_build_object('rule','Email','count',(5 + random()*3)::int)
        )
      ),
      'keystroke', jsonb_build_object(
        'total_events', keys, 'encrypted_blobs', (keys / 400)::int, 'dlp_enabled', false
      ),
      'liveview', jsonb_build_object(
        'sessions', CASE WHEN is_today THEN 1 ELSE 0 END,
        'last_request_at', CASE WHEN is_today THEN (now() - interval '4 hours')::text ELSE NULL END,
        'last_requested_by', CASE WHEN is_today THEN 'hr-manager@personel.demo' ELSE NULL END
      ),
      'policy', jsonb_build_object(
        'blocked_app_attempts', (1 + random()*3)::int,
        'blocked_web_attempts', (3 + random()*5)::int
      ),
      'tamper', jsonb_build_object(
        'findings', CASE WHEN is_today THEN 1 ELSE 0 END,
        'last_check', (now() - interval '22 minutes')::text
      )
    )
  END AS rich_signals,
  CASE
    WHEN active_min > 0 THEN day::timestamptz + interval '9 hours' + (random() * interval '25 minutes')
    ELSE NULL
  END,
  CASE
    WHEN is_today THEN now() - (random() * interval '3 minutes')
    WHEN active_min > 0 THEN day::timestamptz + interval '17 hours' + (random() * interval '90 minutes')
    ELSE NULL
  END,
  now()
FROM shaped
ON CONFLICT (user_id, day) DO UPDATE SET
  active_minutes    = EXCLUDED.active_minutes,
  idle_minutes      = EXCLUDED.idle_minutes,
  screenshot_count  = EXCLUDED.screenshot_count,
  keystroke_count   = EXCLUDED.keystroke_count,
  productivity_score = EXCLUDED.productivity_score,
  top_apps          = EXCLUDED.top_apps,
  rich_signals      = EXCLUDED.rich_signals,
  first_activity_at = EXCLUDED.first_activity_at,
  last_activity_at  = EXCLUDED.last_activity_at,
  updated_at        = now();
SQL

echo
echo "=== Seeding employee_hourly_stats (today x 3 showcase employees x 24 hours) ==="

$PGEXEC <<'SQL'
WITH showcase AS (
  SELECT s.user_id, s.active_minutes AS day_active, s.idle_minutes AS day_idle,
         s.screenshot_count AS day_screens, s.top_apps AS apps, u.username
  FROM employee_daily_stats s
  JOIN users u ON u.id = s.user_id
  WHERE s.day = current_date AND u.username IN ('showcase-zeynep','showcase-mert','showcase-elif')
),
hours AS (SELECT generate_series(0, 23) AS hour),
buckets AS (
  SELECT
    sc.user_id, sc.username, h.hour,
    sc.day_active, sc.day_idle, sc.day_screens, sc.apps,
    -- Per-persona hour distribution
    CASE
      -- Zeynep: strict 09-17 with 12-13 lunch dip
      WHEN sc.username = 'showcase-zeynep' AND h.hour BETWEEN 9 AND 17 THEN
        CASE
          WHEN h.hour IN (12,13) THEN 0.06
          WHEN h.hour IN (9,17)  THEN 0.09
          ELSE 0.14
        END
      -- Mert: later start, long tail into evening
      WHEN sc.username = 'showcase-mert' AND h.hour BETWEEN 10 AND 19 THEN
        CASE
          WHEN h.hour = 13 THEN 0.05
          WHEN h.hour IN (10,19) THEN 0.07
          WHEN h.hour IN (14,15,16) THEN 0.14
          ELSE 0.11
        END
      -- Elif: scattered, early start + late-evening tail
      WHEN sc.username = 'showcase-elif' AND h.hour BETWEEN 9 AND 21 THEN
        CASE
          WHEN h.hour IN (12,13) THEN 0.04
          WHEN h.hour IN (9,21) THEN 0.05
          WHEN h.hour IN (19,20) THEN 0.10
          ELSE 0.09
        END
      ELSE 0
    END AS weight
  FROM showcase sc CROSS JOIN hours h
),
normalized AS (
  SELECT b.*, SUM(weight) OVER (PARTITION BY b.user_id) AS total_weight FROM buckets b
)
INSERT INTO employee_hourly_stats(user_id, day, hour, active_minutes, idle_minutes, top_app, screenshot_count)
SELECT
  n.user_id, current_date, n.hour,
  CASE WHEN n.total_weight > 0 AND n.weight > 0
    THEN LEAST(60, GREATEST(0, (n.day_active * n.weight / n.total_weight + (random()*4 - 2))::int))
    ELSE 0 END,
  CASE WHEN n.total_weight > 0 AND n.weight > 0
    THEN LEAST(60, GREATEST(0, (n.day_idle * n.weight / n.total_weight + (random()*3 - 1))::int))
    ELSE 0 END,
  CASE WHEN n.weight > 0 AND jsonb_array_length(COALESCE(n.apps,'[]'::jsonb)) > 0
    THEN (n.apps -> ((abs(hashtext(n.user_id::text || n.hour::text)) % jsonb_array_length(n.apps)))) ->> 'name'
    ELSE NULL END,
  CASE WHEN n.weight > 0
    THEN GREATEST(0, (n.day_screens * n.weight / NULLIF(n.total_weight,0))::int)
    ELSE 0 END
FROM normalized n
ON CONFLICT (user_id, day, hour) DO UPDATE SET
  active_minutes   = EXCLUDED.active_minutes,
  idle_minutes     = EXCLUDED.idle_minutes,
  top_app          = EXCLUDED.top_app,
  screenshot_count = EXCLUDED.screenshot_count;
SQL

echo
echo "=== Summary ==="
$PGEXEC <<'SQL'
SELECT u.username, u.department, u.job_title,
       s.active_minutes, s.productivity_score,
       jsonb_array_length(s.top_apps) AS top_app_count,
       jsonb_array_length(s.top_apps->0->'files') AS first_app_files,
       jsonb_object_keys(s.rich_signals) IS NOT NULL AS has_rich_signals
FROM employee_daily_stats s
JOIN users u ON u.id = s.user_id
WHERE u.username IN ('showcase-zeynep','showcase-mert','showcase-elif')
  AND s.day = current_date
ORDER BY u.username;
SQL

echo
echo "Showcase seed complete."
