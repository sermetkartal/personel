# Rollback Runbook

Faz 16 #176 — Personel sürüm geri alma prosedürü.

## Ne zaman rollback?

| Senaryo | Rollback |
|---|---|
| Yeni sürüm %5 canary'de hata patlatıyor | ✅ evet — canary abort (#175) |
| Yeni sürüm üretim %100'de hata | ✅ evet — bu runbook |
| Müşteri talebi: "eskiye dön" | ⚠️ önce root cause analizi |
| Veri bozulması şüphesi | ❌ HAYIR — rollback durumu kötüleştirebilir, incident response |
| KVKK DSR SLA tehlikede | ❌ HAYIR — DSR döngüsü bitirilmeden rollback yasak |
| 6 aylık imha raporu sonrası | ❌ HAYIR — imha edilmiş veri geri gelmez |

## Kategoriler

### Seviye 1: Yalnızca image rollback (güvenli)

Yeni sürüm imajları geri çekilir, **şema değişmez**. Hedef sürüm
forward-compatible bir versiyondaysa bu tek başına yeterlidir.

```bash
sudo infra/scripts/rollback.sh v1.4.2 --reason "p95 latency regression"
```

Script:
1. DSR SLA güvenlik kapısını kontrol eder
2. 6 aylık imha penceresini kontrol eder
3. İnteraktif `ROLLBACK v1.4.2` teyidi ister
4. Image'ları `:v1.4.2` → `:latest` yeniden etiketler
5. Stateless container'ları `--force-recreate` ile değiştirir
6. Cache'leri ısıtır, smoke test koşar
7. `/v1/system/rollback-report` audit endpoint'ine post eder

### Seviye 2: Image + şema rollback (TEHLİKELİ)

Yeni sürüm breaking migration çaldıysa şemayı da geri almak gerekir.
`--include-schema` bayrağı **migration down bir adım** koşar. Bu:

- **Veri kaybına yol açabilir** (yeni kolonlar silinir, yeni tablolar drop edilir)
- İkinci bir interaktif teyit gerektirir (`DROP SCHEMA <version>`)
- ASLA birden fazla adım geri gitmez — her ek adım ayrı çalıştırma

```bash
sudo infra/scripts/rollback.sh v1.4.2 --include-schema --reason "breaking migration 0035 dropped field"
```

**Alternatif**: çoğu durumda rollback yerine **ileri forward-fix** daha
güvenlidir. Yeni bir `v1.4.3` patch çıkarıp bug'ı düzeltin, rollback
yerine onu deploy edin.

### Seviye 3: Tam recovery (backup restore)

Image + şema yeterli değilse — veri kaybı veya tampering — rollback
aracı kullanılmaz. `infra/runbooks/backup-restore.md` (Faz 13 #138)
prosedürü izlenir.

## Last-known-good marker

`infra/compose/.last-known-good` dosyası son başarılı yayının tag'ini
tutar. `release.yml` workflow'u başarılı bir yayın sonrası bu dosyayı
günceller. Operatör "hangi versiyona dönmeliyim" sorusunu sormak
zorunda kalmaz:

```bash
sudo infra/scripts/rollback.sh last-known-good
```

Dosya el ile de güncellenebilir (pilot ortamda sürüm değişikliği
manuel olduğu için):

```bash
echo "v1.4.2" > infra/compose/.last-known-good
```

## Güvenlik kapıları

Script kendi başına şu korumaları sağlar — bunları atlatmak için
`--force` bayrağı gerekir ve bu bayrak kullanıldığında rollback log'a
`FORCE` flag'i de işlenir:

### 1. DSR SLA penceresi

30 günlük KVKK m.11 SLA'sına 24 saatten az kalmış aktif bir DSR varsa
rollback bloke. Gerekçe: yeni sürümün yarattığı durumu geri alsan bile
DSR SLA saati durmaz, ikinci kez eskilikten dolayı overdue olamaz.

### 2. 6 aylık imha penceresi

Son destruction report'tan önceki bir versiyona dönmek, yasal olarak
imha edilmiş kanıtların sisteme geri dönmesi demek. Rollback aracı
son imha tarihini gösterir, operatörden `yes` bekler.

### 3. Teyit tokeni

Her rollback interaktif `ROLLBACK <version>` tokeni ister; `--force`
ile atlatılabilir, ama hem log'a hem audit_log'a `force=true` olarak
yazılır.

### 4. Schema rollback ikinci kapı

`--include-schema` bayrağı ikinci bir `DROP SCHEMA <version>` tokeni
ister. Bu, migration down'un birden fazla kez çalıştırılmasını önlemek
için değildir — script zaten tek adım geri alır — niyet onayı içindir.

## Rollback sonrası

1. **Ürünsel gözlem**: 15 dakika boyunca `/healthz`, `/readyz`, p95 latency
2. **Müşteri iletişimi**: status sayfası güncellemesi, mail
3. **Incident postmortem**: `docs/operations/incident-response.md` akışı
4. **Root cause**: yeni sürümün neden başarısız olduğunu belge altına al
5. **Forward fix**: düzeltilmiş patch versiyon (`v1.4.3`) planlama

## Operator log formatı

`/var/log/personel/rollbacks.log`:

```
2026-04-13T14:23:11+03:00  kartal  INFO   v1.4.2  rollback start — actor=kartal dry-run=false
2026-04-13T14:23:12+03:00  kartal  INFO   v1.4.2  resolved last-known-good → v1.4.2
2026-04-13T14:23:15+03:00  kartal  INFO   v1.4.2  retagging images to v1.4.2
2026-04-13T14:23:42+03:00  kartal  INFO   v1.4.2  recreating stateless containers
2026-04-13T14:23:58+03:00  kartal  INFO   v1.4.2  smoke ok
2026-04-13T14:23:59+03:00  kartal  INFO   v1.4.2  rollback complete — active version is now v1.4.2
```

Log dosyası append-only'dir, `chmod 644` ve `chattr +a` önerilir
(pilot'ta chattr kullanılmıyor — production harden'da).

## Test

Staging'de ayda bir rollback tatbikatı:

1. Eski bir tag seç (örn. `v1.3.5`)
2. `sudo infra/scripts/rollback.sh v1.3.5 --dry-run`
3. Planı doğrula
4. Onay: pilot ortamda gerçek koş
5. Smoke: tüm endpoint'ler 200, audit_log'da `rollback.report` entry'si var
6. İleri: son tag'e geri git (`rollback.sh <current>`)

Bu tatbikat `docs/operations/phase-1-exit-criteria.md` #RollbackDrill
maddesine bağlanır.

## Değişiklik kaydı

| Tarih | Değişiklik | Yazan |
|---|---|---|
| 2026-04-13 | İlk versiyon — Faz 16 #176 | devops-engineer |
