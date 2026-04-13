# MinIO Object Lock WORM Geçişi — Operasyonel Runbook

> **Hedef kitle**: Personel platform DPO + DevOps  
> **Süre**: ~20 dakika  
> **Kapsam**: `audit-worm` ve `evidence-worm` bucket'larının COMPLIANCE modunda 5 yıllık retention ile devreye alınması  
> **Roadmap madde**: #50  
> **Yasal dayanak**: KVKK m.7 (silme/imha hakkına rağmen audit yükümlülüğü), ADR 0014 (audit immutability)

Bu runbook, audit zincir checkpoint'lerinin ve SOC 2 Type II evidence locker item'larının kriptografik olarak değiştirilemez (immutable) bir şekilde saklanmasını sağlayan MinIO Object Lock yapılandırmasını anlatır.

---

## 1. Neden COMPLIANCE modu ve neden 5 yıl?

### Object Lock modları

MinIO (S3 spec'i ile uyumlu olarak) iki Object Lock modu sunar:

| Mod | Bypass | Kullanım |
|---|---|---|
| **GOVERNANCE** | `s3:BypassGovernanceRetention` izniyle bypass edilebilir | Operasyonel "soft" lock |
| **COMPLIANCE** | **Hiçbir kullanıcı tarafından bypass edilemez** — root dahil | Düzenleyici uyumluluk |

Personel platformunda audit checkpoint'leri ve SOC 2 evidence item'ları **hiçbir koşulda** geçmişe dönük değiştirilmemelidir. Bu, KVKK Kurul denetiminin temel beklentisi ve SOC 2 Type II observation window'un çalışabilmesi için gereklidir. Dolayısıyla **COMPLIANCE modu zorunludur**.

### Neden 5 yıl (1826 gün)?

| Gereksinim | Süre | Dayanak |
|---|---|---|
| KVKK m.7 — saklama zorunluluğu | 5 yıl | İlgili Kişi başvuruları ve Kurul denetimi geriye dönük 5 yıl içinde olabilir |
| SOC 2 Type II observation window | 12 ay | İlk audit; sonraki yıllar +12 ay |
| ADR 0014 — audit immutability | "düzenleyici minimumdan az olamaz" | Değer 5 yıl olarak sabitlendi |
| Vergi mevzuatı (defter ve belgeler) | 5 yıl | VUK m.253 |

5 yıl tüm bu gereksinimleri karşılayan en küçük ortak süredir. Daha uzun bir retention seçmek (örneğin 10 yıl) MinIO depolama maliyetini ikiye katlardı ve KVKK m.7 "gerekli olandan fazla saklama yasağı" ile çelişme riski yaratırdı.

> 5 yıllık retention `infra/scripts/minio-worm-bootstrap.sh` içinde `RETENTION_DAYS=1826` olarak sabit kodlanmıştır (bir artık yıl hesaba katılarak). Bu değer DPO onayı olmadan değiştirilmemelidir.

---

## 2. Bir audit checkpoint'in yaşam döngüsü

```
                  ┌──────────────────────────────────────┐
                  │ apps/api/internal/audit/recorder.go  │
                  │ Her admin mutasyonu hash zinciri     │
                  │ sonraki node'unu üretir              │
                  └──────────────┬───────────────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────────────┐
                  │ Günlük checkpoint job (UTC 00:05)    │
                  │ apps/api/internal/audit/worm.go      │
                  │ • Postgres'ten son 24h zincir oku    │
                  │ • Vault transit ile imzala (Ed25519) │
                  │ • CheckpointRecord JSON üret         │
                  └──────────────┬───────────────────────┘
                                 │ PUT (Compliance Lock + RetainUntilDate)
                                 ▼
                  ┌──────────────────────────────────────┐
                  │ MinIO bucket: audit-worm             │
                  │ Object Lock: COMPLIANCE 5 yıl        │
                  │ Versioning: enabled                  │
                  │ DELETE: imkansız (root dahil)        │
                  └──────────────┬───────────────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────────────┐
                  │ apps/api/internal/audit/verifier.go  │
                  │ Periyodik olarak okur, signature +   │
                  │ hash chain doğrular. Tutarsızlık     │
                  │ Prometheus alert'i tetikler.         │
                  └──────────────────────────────────────┘
```

Aynı akış `evidence-worm` bucket'ı için de geçerlidir; sadece üretici `apps/api/internal/evidence/store.go` ve içerik SOC 2 control evidence item'larıdır.

---

## 3. Bootstrap

### 3.1 Ön gereksinimler

- [ ] MinIO çalışıyor ve sağlıklı (`docker compose ps minio` → healthy)
- [ ] `/etc/personel/secrets/minio-root.env` mevcut ve şu anahtarları içeriyor:
  ```
  MINIO_ROOT_USER=...
  MINIO_ROOT_PASSWORD=...
  ```
- [ ] `docker` kullanılabilir (script `minio/mc:latest` image'ını çeker)
- [ ] Bucket'lar **henüz oluşturulmamış** olmalı. Eğer `audit-worm` veya `evidence-worm` Object Lock olmadan zaten varsa §6'ya bakın.

### 3.2 Çalıştır

```bash
sudo /home/kartal/personel/infra/scripts/minio-worm-bootstrap.sh
```

Beklenen çıktı (kısaltılmış):

```
[minio-worm-bootstrap] Waiting for MinIO at http://minio:9000...
[minio-worm-bootstrap]   MinIO ready
[minio-worm-bootstrap] Provisioning bucket 'audit-worm'...
[minio-worm-bootstrap]   creating bucket 'audit-worm' with Object Lock...
[minio-worm-bootstrap]   bucket created
[minio-worm-bootstrap]   setting default retention: COMPLIANCE 1826d
[minio-worm-bootstrap] Provisioning bucket 'evidence-worm'...
[minio-worm-bootstrap]   creating bucket 'evidence-worm' with Object Lock...
[minio-worm-bootstrap]   bucket created
[minio-worm-bootstrap]   setting default retention: COMPLIANCE 1826d
[minio-worm-bootstrap] Locking down bucket policy on 'audit-worm'...
[minio-worm-bootstrap] Locking down bucket policy on 'evidence-worm'...
[minio-worm-bootstrap] Provisioning service account 'personel-audit-writer'...
[minio-worm-bootstrap] writer creds written to /etc/personel/secrets/audit-writer.creds
[minio-worm-bootstrap] Bootstrap complete.
```

### 3.3 Doğrulama

```bash
# Bucket varlığı
docker run --rm --network personel_default \
  -e MC_HOST_personel="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@minio:9000" \
  minio/mc:latest \
  ls personel/

# Beklenen: audit-worm/, evidence-worm/, ... (diğer bucket'lar)

# Object Lock + retention
docker run --rm --network personel_default \
  -e MC_HOST_personel="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@minio:9000" \
  minio/mc:latest \
  retention info --default personel/audit-worm

# Beklenen: Default retention configuration: COMPLIANCE 1826d

# Versioning
docker run --rm --network personel_default \
  -e MC_HOST_personel="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@minio:9000" \
  minio/mc:latest \
  version info personel/audit-worm

# Beklenen: Suspended/Enabled = Enabled
```

> COMPLIANCE retention'ın gerçekten **bypass edilemez** olduğunu doğrulamak için bir test object'i yükleyip silmeyi deneyin:
>
> ```bash
> echo test > /tmp/x
> mc cp /tmp/x personel/audit-worm/test.txt
> mc rm personel/audit-worm/test.txt
> # Beklenen: mc: <ERROR> Failed to remove ... Object is WORM protected and cannot be overwritten
> ```
>
> **DİKKAT**: Bu test object'i 5 yıl boyunca silinemeyecek. Sadece root değil, hiç kimse silemez. Test için ayrı bir disposable bucket kullanmayı tercih edebilirsiniz.

---

## 4. API servisini bağlama

### 4.1 Config güncelleme

`apps/api/configs/api.yaml.minio-worm-snippet` dosyasındaki `minio:` bölümünü mevcut `apps/api/configs/api.yaml` dosyasına işleyin (override, eklemez).

### 4.2 Compose mount

API servisi creds dosyasını okumalı. `infra/compose/docker-compose.yaml` içinde `api` servisine ekleyin:

```yaml
  api:
    volumes:
      - /etc/personel/secrets/audit-writer.creds:/etc/personel/secrets/audit-writer.creds:ro
```

### 4.3 Servis yeniden başlatma

```bash
docker compose restart api
```

API loglarında ilk checkpoint job'unun WORM yazma işlemini izleyin:

```bash
docker compose logs -f api | grep -i "worm"
```

Beklenen log: `WORM put OK key=audit/<tenant>/2026-04-13.json`.

---

## 5. KVKK uyum etkisi

Bu bootstrap'in tamamlanmasıyla aşağıdaki KVKK m.5/m.6/m.7 yükümlülükleri **kriptografik olarak garanti altına alınır**:

| Madde | Yükümlülük | WORM ile sağlanması |
|---|---|---|
| KVKK m.7 | İmha yasağı / saklama zorunluluğu | 5 yıl COMPLIANCE → bypass edilemez |
| KVKK m.10 | Aydınlatma yükümlülüğünün ispatı | Audit log'da kayıtlı, audit checkpoint WORM'a aktarılmış |
| KVKK m.11 | İlgili kişi başvurularına 30 gün içinde cevap | DSR fulfillment audit checkpoint'inde, geriye dönük inceleme imkanı |
| KVKK m.12 | Veri güvenliği — kayıt bütünlüğü | Hash chain + Vault Ed25519 imza + WORM = üçlü tamper evidence |
| ADR 0014 | Audit immutability | Native S3 Object Lock COMPLIANCE |

SOC 2 Type II observation window'da `evidence-worm` bucket'ı şu kontrolleri besler:

- **CC6.1** liveview privileged access sessions
- **CC6.3** access reviews
- **CC7.3** incident closures
- **CC8.1** policy push change authorization
- **CC9.1** BCP drills
- **A1.2** backup runs
- **P5.1 / P7.1** DSR fulfillment

Coverage matrix `GET /v1/system/evidence-coverage` endpoint'inden okunabilir.

---

## 6. Recovery — Object Lock olmadan oluşturulmuş bucket

Eğer `audit-worm` veya `evidence-worm` bucket'ı **Object Lock olmadan** zaten oluşturulmuşsa, Object Lock geriye dönük olarak eklenemez. S3 spec'i bunu yasaklar.

### 6.1 Tehlike değerlendirmesi

Mevcut bucket'taki object'lerin değiştirilebilir olduğunu varsayın. Bu durumda yapılması gerekenler:

1. **DPO bilgilendirin** — bu bir compliance hadisesi olabilir.
2. Mevcut bucket'taki tüm object'leri **sayın ve hash'leyin**:
   ```bash
   docker run --rm --network personel_default \
     -e MC_HOST_personel="..." \
     minio/mc:latest \
     find personel/audit-worm --print '{base} {size}' > /tmp/old-audit-inventory.txt
   sha256sum /tmp/old-audit-inventory.txt > /tmp/old-audit-inventory.sha256
   ```
3. Inventory dosyalarını incident response evidence olarak `evidence-worm` bucket'ına PUT edin (yeni bucket oluşturulduktan sonra).

### 6.2 Yeniden adlandırma stratejisi

Eski bucket'ı silmeyin (içinde audit kanıt olabilir). Bunun yerine yeni bucket adı kullanın:

```
audit-worm  →  audit-worm-legacy   (yeniden adlandırılamaz, ama referans için)
              + audit-worm-v2  (yeni, Object Lock'lı)
```

`apps/api/internal/audit/worm.go` içindeki `WORMBucket` sabiti bir code change ile güncellenmelidir. Bu bir hot-fix release'dir. Eski bucket'a yazma artık olmaz, ama eski checkpoint'ler geçmiş audit doğrulamaları için okunabilir kalır.

### 6.3 Önerilen yaklaşım

Eğer henüz prodüksiyon trafiği yoksa (Phase 1 pilot), eski bucket'ı `mc rb --force` ile silmek **kabul edilebilir**:

```bash
docker run --rm --network personel_default \
  -e MC_HOST_personel="..." \
  minio/mc:latest \
  rb --force personel/audit-worm
```

Sonra `minio-worm-bootstrap.sh` script'ini tekrar çalıştırın.

> **DPO onayı zorunludur**. Bu işlem geri alınamaz ve audit log içerebilir. Karar verilmeden önce `iltica-silme-politikasi.md` §"İhlal müdahale" bölümüne bakın.

---

## 7. Anahtar rotasyonu

`personel-audit-writer` service account secret key'i yıllık olarak rotate edilmelidir:

```bash
sudo /home/kartal/personel/infra/scripts/minio-worm-bootstrap.sh --force
```

Bu komut bucket'lara veya retention politikasına dokunmaz; sadece writer secret key'ini rotate eder. Yeni secret key dosyası `/etc/personel/secrets/audit-writer.creds` üzerine yazılır. Sonrasında api servisini yeniden başlatın.

> Object Lock ve retention politikaları **rotate edilemez**. Bunlar bucket'ın yaşam süresi boyunca sabittir.

---

## 8. Audit izi

Bootstrap'in tamamlandığını audit log'a kaydedin:

```bash
PGPASSWORD="$POSTGRES_PASSWORD" psql -h localhost -U app_admin_api -d personel -c "
INSERT INTO audit.audit_log (tenant_id, actor, action, target, payload, created_at)
VALUES (
  (SELECT id FROM tenants LIMIT 1),
  'devops:$USER',
  'system.minio_worm_provisioned',
  'minio',
  jsonb_build_object(
    'phase', '5',
    'item', 50,
    'buckets', ARRAY['audit-worm','evidence-worm'],
    'mode', 'COMPLIANCE',
    'retention_days', 1826,
    'runbook', 'docs/operations/minio-worm-migration.md'
  ),
  now()
);
"
```

Bu kayıt SOC 2 Type II observation window'unun başlangıç tarihinin ispatıdır.

---

*Versiyon 1.0 — 2026-04-13 — Faz 5 madde #50 scaffold*
