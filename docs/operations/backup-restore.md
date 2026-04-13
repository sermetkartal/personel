# Yedekleme ve Geri Yükleme Operasyonel Runbook

> Roadmap #58 — Üretim seviyesi yedekleme otomasyonu. Bu doküman
> `infra/runbooks/backup-restore.md` (özet) ile birlikte okunmalıdır.
> Burada operatör ve DPO için uçtan uca prosedür yer alır.

## Mimari Özet

| Katman | Yedekleme Stratejisi | Sıklık | Retention | Script |
|---|---|---|---|---|
| Postgres | `pg_basebackup` (PITR base) + WAL archive | Nightly base, continuous WAL | 7 base + 7 gün WAL | `backup-orchestrator.sh` |
| ClickHouse | `BACKUP DATABASE TO Disk('backups')` | Nightly | 14 backup | `backup-orchestrator.sh` |
| Vault | `operator raft snapshot save` | Nightly | 14 snapshot | `backup-orchestrator.sh` |
| Keycloak | `kc.sh export --realm` | Nightly | 14 export | `backup-orchestrator.sh` |
| MinIO (audit-worm) | Object Lock Compliance Mode (5 yıl) | Continuous | 5 yıl (immutable) | Otomatik (ADR 0014) |
| MinIO (diğer bucket) | Bucket versioning | Continuous | Tenant lifecycle policy | `backup-orchestrator.sh` --component minio |

> **audit-worm bucket** SOC 2 + KVKK m.12 için authoritative immutable
> kopya görevini görür. Backup script onu MIRROR ETMEZ — ikinci bir kopya
> Object Lock state'ini kaybettirebilirdi.

## Aktivasyon

Pilot sonrası canlı operasyon için:

```bash
# Nightly full backup (02:00 UTC + 5dk jitter)
sudo systemctl enable --now personel-backup.timer

# Hourly incremental (WAL validate + ClickHouse parts merge + metric emit)
sudo systemctl enable --now personel-backup-incremental.timer

# Doğrulama
systemctl list-timers --all | grep personel-backup
journalctl -u personel-backup.service -n 50
journalctl -u personel-backup-incremental.service -n 50
```

Manuel tetikleme:

```bash
sudo systemctl start personel-backup.service               # full
sudo systemctl start personel-backup-incremental.service   # incremental
```

veya doğrudan script:

```bash
sudo /opt/personel/infra/scripts/backup-orchestrator.sh                 # all
sudo /opt/personel/infra/scripts/backup-orchestrator.sh --component pg  # postgres only
sudo /opt/personel/infra/scripts/backup-orchestrator.sh --dry-run       # plan
```

## Yedekleme Doğrulama (Haftalık DPO Görevi)

DPO her Pazartesi sabahı şu kontrolü yapar:

### 1. Son backup başarılı mı?

```bash
# systemd journal
journalctl -u personel-backup.service --since '7 days ago' | grep -E '(Started|Done|FATAL)'

# Prometheus metric
curl -s http://localhost:9090/api/v1/query?query=personel_backup_last_success_ts | jq
```

`personel_backup_last_success_ts` 24 saatten eski ise alarm.

### 2. Backup boyutu makul mü?

```bash
du -sh /var/backups/personel/pg/base/*
du -sh /var/backups/personel/clickhouse/*.zip
du -sh /var/backups/personel/vault/*.snap
```

Beklenen: postgres base ~500MB-2GB, clickhouse 5-50GB (event volume'a göre),
vault snapshot ~10MB, keycloak realm ~500KB.

### 3. WAL chain bütünlüğü

```bash
cat /var/backups/personel/pg/wal/.index.sha256 | head
ls -la /var/backups/personel/pg/wal/ | wc -l
```

WAL dosya sayısı son 7 günde sürekli artmalı, ardından sabitlenmeli (eski
dosyalar pruning ile silinir).

### 4. Restore drill (ayda bir)

Side container içinde test geri yükleme:

```bash
# Geçici test dizini
mkdir -p /tmp/restore-drill
docker run --rm -v /tmp/restore-drill:/data -v /var/backups/personel/pg/base:/backup:ro \
  postgres:16 bash -c '
    cd /data &&
    tar -xzf $(ls -t /backup/*/base.tar.gz | head -1) &&
    pg_ctl -D . start &&
    psql -h /tmp -U postgres -c "SELECT count(*) FROM information_schema.tables"
  '
```

Sonuç: tablo sayısı yaklaşık eşit olmalı (üretim postgres ile karşılaştır).

## Geri Yükleme Senaryoları

### Senaryo A — Postgres bozuldu, dünkü base'e dönmek gerekiyor

```bash
# 1. List
sudo /opt/personel/infra/scripts/restore-orchestrator.sh --list pg

# 2. Postgres'i durdur
docker compose -f /opt/personel/infra/compose/docker-compose.yaml stop postgres

# 3. Restore
sudo /opt/personel/infra/scripts/restore-orchestrator.sh \
  --component pg \
  --backup-id 2026-04-12T02-00-00Z

# 4. Postgres'i başlat
docker compose -f /opt/personel/infra/compose/docker-compose.yaml start postgres

# 5. Doğrula
docker compose logs --tail=20 postgres
docker compose exec postgres psql -U postgres -d personel -c '\dt'
```

### Senaryo B — Postgres PITR (tam zaman noktası)

Operatör 14:30 UTC'de yanlışlıkla bir tablo drop ettiyse:

```bash
# 1. En son base'i bul (drop'tan ÖNCEKİ)
sudo /opt/personel/infra/scripts/restore-orchestrator.sh --list pg

# 2. Postgres durdur, restore (drop'tan 1 dakika öncesini hedefle)
docker compose stop postgres
sudo /opt/personel/infra/scripts/restore-orchestrator.sh \
  --component pg \
  --backup-id 2026-04-13T02-00-00Z \
  --pitr-target '2026-04-13 14:29:00 UTC'

# 3. Postgres'i başlat. Hedef zamana ulaşıldığında DURACAK (pause)
docker compose start postgres
docker compose logs -f postgres | grep 'recovery'

# 4. Veriyi doğrula, sonra promote et
docker compose exec postgres psql -U postgres -d personel -c \
  "SELECT pg_wal_replay_resume();"
```

### Senaryo C — Vault tamamen kayboldu (worst case)

```bash
# Pre-req: VAULT_TOKEN ortamda
export VAULT_TOKEN=<root token from initial unseal ceremony>

# Vault container çalışıyor olmalı (UNSEALED)
docker compose start vault
sudo /opt/personel/infra/scripts/vault-unseal.sh

sudo /opt/personel/infra/scripts/restore-orchestrator.sh \
  --component vault \
  --backup-id 2026-04-13T02-00-00Z

# Force restore Shamir state'i sıfırlayabilir; gerekirse yeniden unseal
sudo /opt/personel/infra/scripts/vault-unseal.sh
```

### Senaryo D — ClickHouse event store

```bash
sudo /opt/personel/infra/scripts/restore-orchestrator.sh --list clickhouse
sudo /opt/personel/infra/scripts/restore-orchestrator.sh \
  --component clickhouse \
  --backup-id 2026-04-13T02-00-00Z
```

## Off-site Mirroring (Roadmap #60 — Gelecek)

Bu sprintte sadece local backup hedefi devreye alındı. Off-site mirror için:

1. İkincil bir MinIO veya S3-uyumlu hedef (müşteri felaket kurtarma sözleşmesi)
2. `mc mirror --watch /var/backups/personel s3-remote/personel-backup`
3. systemd `personel-backup-mirror.service` + watchdog
4. Hash chain verifier off-site'ı da kapsamalı

Roadmap #60 paralel agent'ı bu adımı çözecek. Şu an sadece local backup
otomasyonu canlıdır.

## Retention Policy DPO Checklist

Yıllık DPO denetiminde gözden geçirilmesi gereken sorular:

- [ ] `BACKUP_RETENTION_DAILY=7` müşteri sözleşmesindeki RPO ile uyumlu mu?
      (Standart RPO: 24 saat — 7 gün fazlasıyla yeterli)
- [ ] `BACKUP_RETENTION_WAL=7` PITR pencere uzunluğunu kapsıyor mu?
- [ ] `BACKUP_RETENTION_VAULT=14` PKI rotation cadence'i ile uyumlu mu?
- [ ] audit-worm bucket retention 5 yıl (ADR 0014) — DBA tarafından
      değiştirilmediğini doğrula:
      ```bash
      mc retention info personel-backup/audit-worm
      ```
- [ ] KVKK m.7 (silme hakkı) ile çakışma yok — backup'ta DSR fulfilled
      kayıtlarının da silinmesi gerekiyor mu? (Cevap: Hayır, DSR sonrası
      tombstone bırakılır, kişisel veri zaten silinir; backup metadata
      kayıtları PII içermez)

## Backup Failure Alerting

Prometheus alert kuralları (`infra/compose/prometheus/alerts.yml`):

```yaml
- alert: PersonelBackupStale
  expr: time() - personel_backup_last_success_ts > 86400
  for: 1h
  labels: { severity: critical }
  annotations:
    summary: "Personel backup has not succeeded in 24h"
    runbook: "docs/operations/backup-restore.md#yedekleme-doğrulama"

- alert: PersonelBackupIncrementalStale
  expr: time() - personel_backup_incremental_last_success_ts > 7200
  for: 30m
  labels: { severity: warning }
```

Alert tetiklendiğinde DPO + IT operatörü bilgilendirilir.

## Önemli Uyarılar

1. **Backup script root olarak koşmalı** — pg_basebackup container exec
   gerektirir, /var/backups/personel/ izinleri için chown gerek var.
2. **Yedekleme alırken DLP container'ı durmaz** — vault snapshot
   `disabled-blackout-window` ihtiyacını ortadan kaldırır.
3. **Restore destructive** — orchestrator script `YES` onayı bekler,
   otomasyona koymak tehlikeli.
4. **Backup hedef diski doluysa** script fail eder ve evidence kaydı
   atmaz. node_exporter `node_filesystem_avail_bytes{mountpoint="/var/backups/personel"}`
   metriğine alert kurulmalı.

## Eski Backup Script'leriyle İlişki

Repo'da iki eski backup script'i var:
- `infra/backup.sh` (eski monolitik)
- `infra/scripts/backup.sh` (orta sürüm)

Yeni `backup-orchestrator.sh` her ikisini de geçersiz kılıyor ama eski
script'ler operator review tamamlanana kadar yerinde bırakıldı. Operator
review sonrası iki eski dosya `.deprecated` ek'iyle yeniden adlandırılacak,
sonraki sprintte silinecek.

systemd `personel-backup.service` unit'i ŞU AN `infra/backup.sh` çalıştırıyor.
Geçiş için unit dosyasının `ExecStart` satırını
`/opt/personel/infra/scripts/backup-orchestrator.sh` olarak güncelle ve
`systemctl daemon-reload && systemctl restart personel-backup.timer` koş.
