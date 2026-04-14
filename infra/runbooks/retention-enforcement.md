# Retention Matrix Enforcement — Operator Runbook

> **KVKK Dayanak**: 6698 sayılı KVKK m.4/2-ç (ölçülülük + öngörülen süre) + m.7 (silme/yok etme) + m.12 (veri güvenliği). Bu runbook, `docs/architecture/data-retention-matrix.md` içindeki süre tablosunun ClickHouse TTL ve MinIO lifecycle tarafından gerçekten uygulandığını periyodik olarak doğrulamak için kullanılır.
>
> **Faz 11 roadmap item #115**. Test harness kodu: `apps/qa/cmd/retention-enforcement-test/main.go`.
>
> Versiyon: 1.0 — Nisan 2026

---

## 1. Amaç

Periyodik (günlük cron + manuel Kurul denetim anı) olarak:

1. Her veri kategorisi için Postgres + ClickHouse + MinIO'da `occurred_at < now - MAX_RETENTION` olan satırları say.
2. Sayı > 0 ise **ihlal**. TTL/lifecycle politikası tetiklenmemiş demektir.
3. CSV raporu üret (`retention-report.csv`).
4. İhlal varsa exit 1, DPO'ya alarm gönder.
5. KVKK Kurul denetimine kanıt olarak aylık attestation arşivi üret.

---

## 2. Çalıştırma

### Elle (ad-hoc denetim için)

```bash
# 1. Script'i build et
cd /home/kartal/personel/apps/qa
go build -o /tmp/retention-enforcement-test ./cmd/retention-enforcement-test

# 2. Çevre değişkenleri
export PERSONEL_PG_DSN="postgres://personel_readonly:***@localhost:5432/personel?sslmode=require"
export PERSONEL_TENANT_ID="be459dac-1a79-4054-b6e1-fa934a927315"

# 3. Çalıştır
/tmp/retention-enforcement-test \
  --report /var/log/personel/compliance/retention-$(date +%Y%m%d).csv \
  --verbose

# Çıktı örneği:
# [INFO] checked category=agent_heartbeat cutoff=2026-03-14T... offending_rows=0 kvkk="m.4 (ölçülülük)"
# [INFO] checked category=keystroke_ciphertext ...
# PASS: tüm retention pencereleri içinde
```

### Systemd timer (nightly cron)

`/etc/systemd/system/personel-retention-check.service`:

```ini
[Unit]
Description=Personel retention matrix enforcement check
After=docker.service

[Service]
Type=oneshot
EnvironmentFile=/etc/personel/retention-check.env
ExecStart=/usr/local/bin/retention-enforcement-test \
    --report /var/log/personel/compliance/retention-%Y%m%d.csv
StandardOutput=append:/var/log/personel/compliance/retention.log
StandardError=append:/var/log/personel/compliance/retention.log
```

`/etc/systemd/system/personel-retention-check.timer`:

```ini
[Unit]
Description=Nightly Personel retention enforcement at 03:15 UTC

[Timer]
OnCalendar=*-*-* 03:15:00 UTC
Persistent=true

[Install]
WantedBy=timers.target
```

Aktive et: `sudo systemctl enable --now personel-retention-check.timer`

---

## 3. İhlal Tespit Edildiğinde Yapılacaklar

**Ciddiyet**: Her ihlal **P1** KVKK olayıdır. DPO 24 saat içinde durumu değerlendirmeli ve (a) veriyi manuel sil, (b) altta yatan TTL/lifecycle bozukluğunu düzelt, (c) Kurul için düzeltici eylem kaydı oluştur.

### Adım 1 — Bağlam topla

```bash
# En son rapordaki ihlali göster
tail -20 /var/log/personel/compliance/retention-$(date +%Y%m%d).csv

# CSV başlıkları:
# generated_at, tenant_id, category, store, cutoff, offending_rows, kvkk_reference, error
```

### Adım 2 — Kategori bazlı tanı

| Kategori | Hangi mekanizma tetiklemeli | Kontrol komutu |
|---|---|---|
| `agent_heartbeat`, `process_events`, `window_title`, `file_events` | ClickHouse `TTL occurred_at + INTERVAL n DAY` | `docker exec personel-clickhouse clickhouse-client --query "SHOW CREATE TABLE personel.events_raw"` — TTL cümlesi var mı? |
| `keystroke_ciphertext` | Postgres scheduled job + Vault key destroy | `psql -c "SELECT COUNT(*) FROM keystroke_keys WHERE created_at < now() - interval '30 days'"` |
| `dlp_matches` | Postgres purge job | `psql -c "SELECT MIN(matched_at), COUNT(*) FROM dlp_matches"` |
| `admin_audit_log` | **SİLİNMEZ** — append-only. 10 yıl dolmuş kayıt = arşivle + tombstone | `psql -c "SELECT MIN(created_at) FROM audit.audit_events"` |
| `identity_sessions` | Postgres scheduled purge | `psql -c "SELECT COUNT(*) FROM sessions WHERE created_at < now() - interval '2 years'"` |

### Adım 3 — TTL politikası tetiklenmiyorsa

```bash
# ClickHouse'da TTL zorlamak için:
docker exec personel-clickhouse clickhouse-client --query "
  ALTER TABLE personel.events_raw MATERIALIZE TTL
"

# Postgres'te manuel silme (KVKK m.7 — tek yönlü, geri alınamaz):
docker exec personel-postgres psql -U postgres -d personel -c "
  BEGIN;
  DELETE FROM keystroke_keys WHERE created_at < now() - interval '30 days';
  -- Verify count matches expectation before COMMIT
  COMMIT;
"
```

### Adım 4 — Manuel silme audit trail'i

Her manuel silme **hash-zincirli audit log'a** yazılmalıdır. DPO Console'dan:

```
DPO Console → Audit Log → Add Manual Entry
  Action:  retention.manual_purge
  Target:  table=<name> category=<name>
  Details: rows_deleted=<n> reason="nightly TTL did not fire, manual enforcement"
```

### Adım 5 — Kök neden analizi

TTL tetiklenmeme nedenleri:

1. **ClickHouse merge thread'i pasif**: `SELECT * FROM system.merges` — hiç merge yok mu?
2. **ClickHouse disk doluluğu**: `system.disks` — %95 üzeri mi? Merge bloklanmış olabilir.
3. **Postgres bgworker cron job durmuş**: `pg_cron` extension kontrol.
4. **MinIO lifecycle manager disabled**: `mc admin config get <alias> notify`

### Adım 6 — Düzeltici eylem kaydı

`docs/compliance/hukuki-riskler-ve-azaltimlar.md` ilgili risk (R12 veya R2) altına yeni giriş:

```
| Tarih | Olay | Sebep | Düzeltme | DPO imzası |
|---|---|---|---|---|
| 2026-04-13 | keystroke_ciphertext 7 satır 30 gün aştı | Postgres cron disabled | Manuel DELETE + cron re-enable | <ad soyad> |
```

---

## 4. Yanlış Pozitifler

- **Legal hold aktif**: `legal_hold = TRUE` olan satırlar TTL'den muaftır. Test harness legal_hold filtresini **hesaba katmıyor** — bu durum özellikle kasıtlı. KVKK denetiminde "legal hold var ama TTL'den muaf" kanıtı olarak hash-zincirli `legal_hold.placed` audit kaydı gösterilmelidir.
- **ClickHouse eventual consistency**: `ALTER TABLE ... DELETE` mutasyonları asenkron. `system.mutations` tablosunda `is_done=0` ise çalışıyor demektir.
- **Replication lag**: İkinci ClickHouse replica'sı daha yeni silinmemiş kayıtları gösterebilir. Her iki node'u da test edin.

---

## 5. Kurul Denetimine Hazırlık

Kurul müfettişi istediğinde şunlar hazır olmalı:

- [ ] Son 12 ayın aylık CSV raporları (arşiv: `/var/log/personel/compliance/retention-YYYYMM*.csv`)
- [ ] `docs/architecture/data-retention-matrix.md` versiyon geçmişi (git log)
- [ ] Her ihlal için düzeltici eylem kaydı (yukarıdaki Adım 6)
- [ ] `docs/compliance/iltica-silme-politikasi.md` güncel kopyası
- [ ] TTL politikalarının CREATE TABLE çıktıları (canlı deployment kanıtı)

Detay: `docs/compliance/kurul-denetim-runbook.md` §3.

---

## 6. İlgili Dokümanlar

- `docs/architecture/data-retention-matrix.md` — authoritative süre tablosu
- `docs/compliance/iltica-silme-politikasi.md` — KVKK silme/imha politikası
- `docs/compliance/hukuki-riskler-ve-azaltimlar.md` — risk register (R2, R12)
- `docs/compliance/kurul-denetim-runbook.md` — Kurul müfettiş prep checklist
- `apps/qa/cmd/retention-enforcement-test/main.go` — bu runbook'un koşturduğu binary
- `infra/scripts/verify-audit-chain.sh` — paralel audit integrity check

---

*Versiyon 1.0 — Nisan 2026. Bu runbook her manuel silme sonrasında güncellenmeli; manuel silme bir zincirleme etkiye girerse (başka kategori de ihlal eder), DPO değerlendirmesi gerekir.*
