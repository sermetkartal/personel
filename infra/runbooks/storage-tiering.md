# Depolama Katmanlama Runbook'u (Storage Tiering)

**Faz 7 — Roadmap maddesi #76**
**Kapsam**: ClickHouse tiered storage (hot/warm) + MinIO cold export
**Hedef kitle**: DBA / Ops / SRE
**KVKK/Retention referansı**: `docs/architecture/data-retention-matrix.md`

---

## 1. Genel bakış

Personel olay tabloları üç katmanlı bir saklama ladderine yerleştirilmiştir:

| Katman | Yaş (gün) | Konum | Performans |
|---|---|---|---|
| **Hot** | 0–7 | ClickHouse `hot` volume (NVMe/SSD) | Tüm dashboard sorguları buradan çalışır. |
| **Warm** | 8–90 | ClickHouse `warm` volume (büyük HDD/ucuz SSD) | Occasional raporlama + DSR arama. |
| **Cold** | ≥91 | MinIO `backups/cold/<table>/<partition>.parquet` | Sorgulanmaz — yalnızca denetim/DSR için manuel re-import. |

**ClickHouse TTL clause'ları** (migration `schemas_tiered.go` ile uygulanır):

```sql
TTL
  toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
  toDateTime(occurred_at) + INTERVAL 90 DAY DELETE WHERE legal_hold = FALSE
```

Cold tier ClickHouse'un DIŞINDADIR. 90 günden sonra `cold-export` işi
her partition'ı Parquet olarak MinIO'ya yazar, sonra
`ALTER TABLE ... DROP PARTITION` ile ClickHouse'tan düşürür.

---

## 2. İlk kurulum (bir defa)

### 2.1 Disk hazırlığı

```bash
sudo mkdir -p /var/lib/clickhouse/hot /var/lib/clickhouse/warm
sudo chown -R 101:101 /var/lib/clickhouse/hot /var/lib/clickhouse/warm
# Varsa ayrı mount noktası: fstab'a ekleyip /var/lib/clickhouse/warm'a mount et.
```

**Dikkat**: `hot` ve `warm` aynı fiziksel disk üzerinde ise tiering bir
fayda sağlamaz; bütün kazanç `warm`'ın ayrı (ve daha ucuz) bir volume'a
mount edilmesinden gelir.

### 2.2 ClickHouse config

```bash
sudo cp infra/compose/clickhouse/storage-config.xml \
        /etc/clickhouse-server/config.d/storage-config.xml
sudo systemctl restart clickhouse-server
# VEYA docker compose deployment için:
docker compose restart clickhouse
```

**Doğrulama**:

```sql
SELECT policy_name, volume_name, disks
  FROM system.storage_policies
 WHERE policy_name = 'tiered';
```

İki satır dönmeli (hot + warm).

### 2.3 Migration uygulama

```bash
# Gateway repo'da:
go run ./apps/gateway/cmd/apply-migrations \
  --clickhouse "$PERSONEL_CLICKHOUSE_DSN" \
  --set tiered
```

VEYA manuel:

```bash
docker exec -i personel-clickhouse clickhouse-client --multiquery <<'SQL'
ALTER TABLE events_raw MODIFY SETTING storage_policy = 'tiered';
ALTER TABLE events_raw MODIFY TTL
  toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
  toDateTime(occurred_at) + INTERVAL 90 DAY DELETE WHERE legal_hold = FALSE;
-- (diğer tablolar için schemas_tiered.go'daki listeyi tekrarla)
SQL
```

**Doğrulama**:

```sql
SELECT table, disk_name, sum(rows) AS rows, sum(bytes_on_disk) AS bytes
  FROM system.parts
 WHERE database = currentDatabase()
   AND active = 1
 GROUP BY table, disk_name
 ORDER BY table, disk_name;
```

İlk birkaç saat içinde tüm yeni partlar `hot` diskinde görünmelidir.

---

## 3. Günlük operasyon

### 3.1 Warm volume kontrolü

```bash
# Haftalık olarak:
df -h /var/lib/clickhouse/hot /var/lib/clickhouse/warm
```

Warm <%20 boşluk kaldığında alert tetiklenir (`DiskUsageHigh` kuralı).

### 3.2 Cold export cron

```cron
# /etc/cron.d/personel-cold-export
30 3 * * 0  kartal  PERSONEL_COLD_EXPORT_DRY_RUN=false \
                    PERSONEL_COLD_EXPORT_DAYS=91 \
                    PERSONEL_COLD_EXPORT_TABLES=events_raw \
                    PERSONEL_CLICKHOUSE_DSN=clickhouse://... \
                    /usr/local/bin/personel-cold-export \
                    >> /var/log/personel/cold-export.log 2>&1
```

**İlk çalıştırma MUTLAKA `DRY_RUN=true` ile yapılmalıdır**. Dry run,
düşürülecek partitions'ları listeler ama DROP etmez. Kontrol + arşiv
yedek aldıktan sonra `DRY_RUN=false` geçilir.

### 3.3 Cold verisinin geri çağrılması (DSR / denetim)

1. MinIO'dan Parquet dosyasını indir:
   ```bash
   mc cp minio/backups/cold/events_raw/202512.parquet /tmp/
   ```
2. Geçici bir ClickHouse tablosuna yükle:
   ```sql
   CREATE TABLE events_raw_cold_202512 AS events_raw ENGINE = MergeTree
     ORDER BY (tenant_id, endpoint_id, occurred_at, event_type);
   INSERT INTO events_raw_cold_202512
     FROM INFILE '/tmp/202512.parquet' FORMAT Parquet;
   ```
3. Sorguyu koş, sonra tabloyu DROP et.

---

## 4. Sorun giderme

### Partlar warm'a geçmiyor

- `system.parts` içinde TTL kolonunu kontrol et — boşsa `MODIFY TTL`
  uygulanmamıştır, migration'ı tekrar koş.
- `system.merges` içinde `move` tipi var mı? Aktif bir merge varsa
  bekle.
- `SYSTEM START MOVES events_raw;` komutunu çalıştır — bazen moves
  pause edilmiş olabilir.

### Cold export DRY RUN hiçbir şey bulmuyor

- `PERSONEL_COLD_EXPORT_DAYS` değerini düşür (ör. 30) ve tekrar koş;
  eğer hâlâ boşsa TTL DELETE clause zaten daha erken tetikleniyordur
  ve cold tier'a hiç veri varmıyordur. Bu durumda cold export'a gerek
  yoktur.

### DROP PARTITION çalışmadı

- `system.replicas` içinde `is_readonly=1` olan node var mı?
  (Replicated cluster kurulumunda keeper problemi.)
- Audit entry yazıldı mı? Çalışmadıysa cold veriyi geri yükle ve
  incident-response runbook'unu takip et.

---

## 5. KVKK uyum notları

- **Cold Parquet dosyaları şifrelidir**: MinIO bucket `backups`
  SSE-S3 ile şifrelidir (PE-DEK altında değil). Cross-region
  replication kapalıdır.
- **Legal hold**: `legal_hold = TRUE` olan rows TTL DELETE clause'u
  tarafından atlanır, ama cold export onları da export eder. Legal
  hold'daki satırlar cold'a gitmemesi gerekiyorsa partition bazlı
  koruma kullanın (`SELECT count() WHERE partition = ... AND
  legal_hold`).
- **DSR m.11 silme hakkı**: Cold Parquet dosyaları KVKK m.11/ç (silme
  hakkı) geldiğinde ilgili tenant-id satırları indirilmeli, filtrelenmeli,
  yeniden yazılmalı. Şu an manuel bir süreçtir (Phase 3.1'de
  otomatikleşecek).
