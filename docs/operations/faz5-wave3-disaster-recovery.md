# Faz 5 Wave 3 — Disaster Recovery Orchestrator Runbook

> **Kapsam**: Faz 5 Wave 3 DR maddeleri (#59 restore drill, #60 off-site
> backup mirror, #61 PITR) için üç bölümlü çalıştırma kılavuzu.
> **Sahip**: SRE / DevOps — DPO sign-off zorunlu (KVKK m.12 veri
> güvenliği kanıtı)
> **Süre**: Restore drill ~60 dk, PITR kurulum ~30 dk, off-site mirror
> kurulum ~45 dk — ayrı ayrı koşulabilir.
> **Önkoşul**: Wave 1 `backup-restore.md` nightly backup aktif + Wave 2
> cluster scaffold tamamlanmış (postgres replica en azından) olmalı.

---

## 0. Amaç ve İki Blocker

Bu runbook üç maddeyi kapsar:

1. **#59 Restore drill** — gerçek RTO (Recovery Time Objective) ve RPO
   (Recovery Point Objective) ölçümü. Şu an hedef RTO < 1 saat, RPO <
   15 dakika; ama ölçüm yapılmadı.
2. **#60 Off-site backup mirror** — MinIO replikasyonu üçüncü bir
   makinaya (veya müşteri Nas'ına) — AWAITING operator action.
3. **#61 PITR (Point-in-Time Recovery)** — Postgres WAL archiving +
   pg_rewind + ClickHouse incremental backup + journal replay.

**Blocker #1**: #60 için CLAUDE.md §0 3-makine limitine bağlı olarak
üçüncü bir on-prem veya müşteri kontrolünde bir makine gerekli. Bu
runbook backend kısmını ve Settings UI'dan target ekleme akışını
açıklar (Wave 9 Sprint 3'te gelecek UI öncesi hazır).

**Blocker #2**: #59 canlı bir stack gerektirir. vm3 + vm5 production
data ile doluyken dikkatli koşulmalı; staging klonunda test edilmesi
zorunlu (aşağıda staging oluşturma adımları).

## 1. Önkoşul Zinciri

| Girdi | Durum gerekli |
|---|---|
| `backup-restore.md` | nightly backup çalışıyor, son 3 başarılı |
| `postgres-replication.md` | vm5 replica streaming |
| `clickhouse-cluster.md` | ReplicatedMergeTree aktif |
| `minio-worm-migration.md` | audit-worm + evidence-worm COMPLIANCE modda |
| Vault | unseal + transit key `kv/personel/backup/encryption-key` |

## 2. Bölüm A — Restore Drill (#59)

### 2.1 Staging klon oluşturma

**YASAK**: Production vm3'e `DROP DATABASE` çekmeyin.

Güvenli prosedür:

1. vm3'te backup script'in son çıktısını onayla:
   ```bash
   ls -lt /var/backups/personel/postgres/ | head -5
   # Beklenen: son 24 saat içinde .dump dosyası
   ```
2. Staging compose dosyası hazırla — aynı repo içinde
   `infra/compose/docker-compose.staging.yaml` (ayrı port prefix,
   ayrı volume). Staging'i **vm3 içinde ayrı container'lar** olarak
   çalıştır — bu CLAUDE.md §0 üç-makine kuralını ihlal etmez.
3. Staging postgres'a son nightly backup'ı restore et:
   ```bash
   docker exec -i personel-postgres-staging pg_restore \
     -U postgres -d personel_staging --clean --if-exists \
     < /var/backups/personel/postgres/latest.dump
   ```

### 2.2 Staging'i kasten boz

Restore drill'in amacı "tamamen kaybetmiş olsaydık ne kadar sürerdi"
sorusuna cevap vermek. Senaryolar:

| Senaryo | Test ettiği şey | Komut |
|---|---|---|
| A1 Tek tablo drop | Postgres partial restore | `DROP TABLE personel.audit_log CASCADE` |
| A2 Tüm DB drop | Postgres full restore | `DROP DATABASE personel_staging` |
| A3 ClickHouse veri dizini wipe | CH restore | `rm -rf /var/lib/personel/clickhouse-staging/data` |
| A4 MinIO bucket delete (WORM hariç) | Object restore | `mc rb --force staging/personel-screenshots` |
| A5 Vault seal | Vault recovery | `vault operator seal` → unseal ceremony yeniden |

Her senaryo için ayrı drill koş, RTO ölç.

### 2.3 Restore + RTO ölçümü

```bash
# Senaryo A2 örneği — RTO stopwatch
SCENARIO=A2
START=$(date +%s)

# Adım 1: backup indir
mc cp off-site/backups/postgres/latest.dump /tmp/restore.dump

# Adım 2: staging'i sıfırla
docker exec personel-postgres-staging psql -U postgres -c \
  "DROP DATABASE IF EXISTS personel_staging"
docker exec personel-postgres-staging psql -U postgres -c \
  "CREATE DATABASE personel_staging"

# Adım 3: restore
docker exec -i personel-postgres-staging pg_restore \
  -U postgres -d personel_staging --clean --if-exists \
  < /tmp/restore.dump

# Adım 4: smoke test
docker exec personel-postgres-staging psql -U postgres -d personel_staging \
  -c "SELECT count(*) FROM personel.users"

END=$(date +%s)
RTO_SEC=$((END - START))
echo "Scenario $SCENARIO RTO: ${RTO_SEC}s"
```

### 2.4 RPO hesaplama

RPO = (backup tamamlanma zamanı) − (son işlem zamanı geri yüklenen set
içinde).

```bash
# Son işlem zamanı (restore sonrası)
docker exec personel-postgres-staging psql -U postgres -d personel_staging -c "
  SELECT max(created_at) FROM personel.audit_log
"
# Backup tamamlanma zamanı
ls -l /var/backups/personel/postgres/latest.dump | awk '{print $6" "$7" "$8}'
# Fark = RPO
```

### 2.5 Sonuç kayıt

```bash
curl -X POST http://vm3:8000/v1/system/bcp-drills \
  -H "Authorization: Bearer ${DPO_JWT}" \
  -d '{
    "drill_type": "live",
    "scenario": "A2_full_postgres_restore",
    "duration_seconds": '"${RTO_SEC}"',
    "rto_target_seconds": 3600,
    "rto_actual_seconds": '"${RTO_SEC}"',
    "met_rto": '"$([ $RTO_SEC -lt 3600 ] && echo true || echo false)"',
    "rpo_target_seconds": 900,
    "rpo_actual_seconds": <hesaplanan>,
    "facilitator": "<kartal>",
    "lessons_learned": "<drill sonrası notlar>"
  }'
```

Bu `bcp.Service.RecordDrill` → SOC 2 CC9.1 evidence kaydı atar
(Phase 3.0.4 collector).

### 2.6 Hedefler

| Metrik | Hedef | Wave 9 Sprint 5 ölçümü |
|---|---|---|
| Postgres RTO | < 60 dk | AWAITING (drill koşulmadı) |
| Postgres RPO | < 15 dk | AWAITING |
| ClickHouse RTO | < 120 dk | AWAITING |
| ClickHouse RPO | < 60 dk (günlük snapshot) | AWAITING |
| MinIO (non-WORM) RTO | < 30 dk | AWAITING |
| MinIO (WORM) RTO | N/A — değiştirilemez | — |
| Vault unseal RTO | < 10 dk | AWAITING |

Her AWAITING satırı ilk drill sonrası doldurulur ve bu runbook güncellenir.

## 3. Bölüm B — Off-site Backup Mirror (#60)

### 3.1 Mimari

```
vm3 /var/backups/personel/               [on-site primary]
    ├── postgres/<date>.dump
    ├── clickhouse/<date>.tar
    └── minio/<bucket>/...

   rclone/mc sync (nightly 03:00 UTC)
        │
        ▼
Off-site target                           [AWAITING — 3. node TBD]
    ├── S3-compatible bucket (MinIO cluster, müşteri NAS)
    ├── SFTP ssh target
    └── WebDAV/S3 gateway
```

### 3.2 Backend — Settings UI backing table

Wave 9 Sprint 3'te gelecek Settings UI için backend hazır olmalı.
Tablo:

```sql
-- Migration 0038 (Wave 3 backlog)
CREATE TABLE IF NOT EXISTS backup_targets (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES tenants(id),
  name          TEXT NOT NULL,
  kind          TEXT NOT NULL CHECK (kind IN ('s3','sftp','webdav','minio')),
  endpoint      TEXT NOT NULL,
  bucket        TEXT,
  access_key_id TEXT,
  -- secret_key gizli; Vault kv/personel/backup-targets/<id>/secret_key
  region        TEXT,
  prefix        TEXT DEFAULT '',
  enabled       BOOLEAN NOT NULL DEFAULT false,
  last_run_at   TIMESTAMPTZ,
  last_run_ok   BOOLEAN,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE backup_targets ENABLE ROW LEVEL SECURITY;

CREATE POLICY backup_targets_tenant_isolation ON backup_targets
  USING (tenant_id = current_setting('app.tenant_id')::uuid);
```

API endpoint'leri (Sprint 3 tarafından oluşturulacak):

```
GET    /v1/system/backup-targets        (admin, it_manager)
POST   /v1/system/backup-targets        (admin)
PATCH  /v1/system/backup-targets/{id}   (admin)
DELETE /v1/system/backup-targets/{id}   (admin)
POST   /v1/system/backup-targets/{id}/test   (admin) — bağlantı testi
```

### 3.3 Sync script iskelet

`infra/scripts/backup-offsite-mirror.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

TARGET=${1:?target-id required}

# API'dan target config'i al
TARGET_JSON=$(curl -sf "http://vm3:8000/v1/system/backup-targets/${TARGET}" \
  -H "Authorization: Bearer ${SERVICE_JWT}")

KIND=$(jq -r .kind <<<"${TARGET_JSON}")
ENDPOINT=$(jq -r .endpoint <<<"${TARGET_JSON}")
BUCKET=$(jq -r .bucket <<<"${TARGET_JSON}")
ACCESS=$(jq -r .access_key_id <<<"${TARGET_JSON}")

# Secret Vault'tan
SECRET=$(vault kv get -field=secret_key \
  "kv/personel/backup-targets/${TARGET}/secret_key")

case "${KIND}" in
  s3|minio)
    mc alias set offsite "${ENDPOINT}" "${ACCESS}" "${SECRET}"
    mc mirror --remove --preserve \
      /var/backups/personel/ "offsite/${BUCKET}/"
    ;;
  sftp)
    rsync -avz --delete \
      -e "ssh -i /etc/personel/backup-target.key" \
      /var/backups/personel/ \
      "${ACCESS}@${ENDPOINT}:${BUCKET}/"
    ;;
  *)
    echo "unsupported target kind: ${KIND}" >&2
    exit 1
    ;;
esac

# Sonuç API'a bildir
curl -X PATCH "http://vm3:8000/v1/system/backup-targets/${TARGET}" \
  -H "Authorization: Bearer ${SERVICE_JWT}" \
  -d '{"last_run_at":"'"$(date -Iseconds)"'","last_run_ok":true}'
```

### 3.4 Systemd timer

`/etc/systemd/system/personel-backup-offsite.timer`:

```
[Unit]
Description=Personel off-site backup mirror

[Timer]
OnCalendar=*-*-* 04:00:00
Persistent=true
Unit=personel-backup-offsite.service

[Install]
WantedBy=timers.target
```

### 3.5 Doğrulama

```bash
# Target eklendikten sonra elle koş
sudo systemctl start personel-backup-offsite.service
sudo journalctl -u personel-backup-offsite.service --since '5 min ago'

# Offsite tarafında dosya sayısı
mc ls offsite/personel-backup/postgres/ | wc -l
# On-site tarafında aynı sayı
ls /var/backups/personel/postgres/ | wc -l
# Fark 0 olmalı (veya 1 — en yeni henüz sync edilmemiş olabilir)
```

## 4. Bölüm C — PITR (#61)

### 4.1 Postgres WAL archiving

`postgresql.conf` (primary):

```
wal_level = replica
archive_mode = on
archive_command = '/opt/personel/bin/wal-archive.sh %p %f'
archive_timeout = 300
```

`/opt/personel/bin/wal-archive.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
WAL_PATH=$1
WAL_NAME=$2
DEST=/var/backups/personel/postgres/wal
install -m 0600 "${WAL_PATH}" "${DEST}/${WAL_NAME}"
# Opsiyonel: gzip + offsite mirror
gzip -c "${DEST}/${WAL_NAME}" > "${DEST}/${WAL_NAME}.gz"
rm "${DEST}/${WAL_NAME}"
```

### 4.2 PITR recovery prosedürü

Hedef: "X zamanına kadar her şeyi geri al".

```bash
TARGET_TIME="2026-04-14 12:00:00+03"

# 1. Base backup'ı restore et (en son pg_basebackup)
docker stop personel-postgres-staging
rm -rf /var/lib/personel/postgres-staging/*
tar -xzf /var/backups/personel/postgres/base-latest.tar.gz \
  -C /var/lib/personel/postgres-staging/

# 2. recovery.conf yaz
cat > /var/lib/personel/postgres-staging/recovery.signal
cat > /var/lib/personel/postgres-staging/postgresql.auto.conf <<EOF
restore_command = 'gunzip -c /var/backups/personel/postgres/wal/%f.gz > %p'
recovery_target_time = '${TARGET_TIME}'
recovery_target_action = 'promote'
EOF

# 3. Başlat, replay'in tamamlanmasını bekle
docker start personel-postgres-staging
# pg_is_in_recovery() false olana kadar poll
while docker exec personel-postgres-staging \
  psql -U postgres -t -c "SELECT pg_is_in_recovery()" | grep -q t; do
  sleep 5
done
echo "PITR complete — staging at ${TARGET_TIME}"
```

### 4.3 ClickHouse incremental backup

ClickHouse native `BACKUP TO Disk('backups', ...)` komutuyla incremental:

```sql
-- Haftalık full
BACKUP DATABASE personel TO Disk('backups', 'week-2026-15-full.zip');

-- Günlük incremental (base: son full)
BACKUP DATABASE personel TO Disk('backups', 'week-2026-15-day-1.zip')
  SETTINGS base_backup = Disk('backups', 'week-2026-15-full.zip');
```

Restore:

```sql
RESTORE DATABASE personel FROM Disk('backups', 'week-2026-15-day-1.zip');
```

### 4.4 Journal replay (audit zinciri bütünlüğü)

Audit `audit_log` tablosu WORM'a checkpoint atıyor (Phase 3.0.5). PITR
sonrası verify script'i koş:

```bash
infra/scripts/verify-audit-chain.sh --tenant=<id> --since=2026-04-01
# Beklenen: "chain OK N entries verified"
```

Fail ise PITR zamanının checkpoint aralığına denk düşmediği anlamına
gelir — bir önceki checkpoint zamanına target'ı geri al.

## 5. DR Exercise Sağlığı

Aylık olarak tam bir drill:

1. Bölüm A senaryo A2 (full postgres restore) — zorunlu
2. Bölüm A senaryo A3 (ClickHouse wipe + restore) — zorunlu
3. Bölüm C PITR testi (30 dk öncesine geri al) — çeyrek dönemlik
4. Her drill sonrası `bcp.Service.RecordDrill` → CC9.1 evidence

## 6. KVKK + SOC 2

- KVKK m.12 veri güvenliği → DR planı zorunlu, DPIA'ya eklenmeli
- SOC 2 A1.2 availability → RTO + RPO ölçümü periyodik, trend analizi
- SOC 2 CC9.1 risk mitigation → drill sonucu kanıt olarak aylık pack'e

## 7. AWAITING

- [ ] **Üçüncü makine / off-site target seçimi** — 3-makine limitine
      bağlı pilot müşteri kontrolünde bir NAS ya da client-managed MinIO
- [ ] **İlk canlı restore drill** — bu runbook'un Bölüm A'sını takip
      ederek RTO ölçümü (CLAUDE.md §0 AWAITING #59)
- [ ] **PITR production bring-up** — WAL archive script'ı yerinde
      ancak systemd timer henüz aktif değil
- [ ] **backup_targets tablosu ve Settings UI** — Wave 9 Sprint 3
      kapsamında; bu runbook backend hazırlığının kaynağıdır
- [ ] **Aylık DR drill cadence kararı** — DPO ile sign-off

---

*Versiyon 1.0 — 2026-04-14 — Wave 9 Sprint 5 teslimatı.*
*Bağlı runbook'lar: `backup-restore.md`, `postgres-replication.md`,
`clickhouse-cluster.md`, `minio-worm-migration.md`.*
