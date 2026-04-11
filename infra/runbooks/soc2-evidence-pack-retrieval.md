# SOC 2 Type II Evidence Pack — DPO İşletim Runbook'u

> **Hedef kitle**: Personel müşterisinin DPO (Veri Sorumlusu) rolü.
> **Amaç**: 12 aylık SOC 2 Type II gözlem penceresi için denetçiye teslim edilecek
> kanıt paketlerinin düzenli olarak üretilmesi, doğrulanması ve arşivlenmesi.
> **Sıklık**: Ayda bir (önerilir) + dönem sonlarında tam kapsam.
> **Kaynak belgeler**: `docs/adr/0023-soc2-type2-controls.md`,
> `docs/policies/change-management.md`, `docs/security/risk-register.md`.

---

## 1. Kavramsal Özet

Personel platformu her denetlenebilir eylemde (live view oturumu, politika
yayını, KVKK m.11 talebinin kapatılması, vb.) **imzalı kanıt** üretir. Kanıt iki
yere aynı anda yazılır:

1. **Postgres `evidence_items` tablosu** — hızlı sorgulama için metadata.
2. **MinIO `audit-worm` bucket'ı** — Object Lock Compliance mode ile 5 yıl
   retention. Root MinIO hesabı bile retention süresi dolmadan silemez.

Kanıt paketi (pack), bu iki kaynağı birleştirip auditor'a **imzalı bir ZIP**
olarak teslim eder. Auditor ZIP'i açıp hem manifest imzasını hem her item
imzasını bağımsız olarak doğrulayabilir; Personel altyapısına güvenmek
zorunda kalmaz.

---

## 2. Ön Koşullar

- Konsol'a DPO rolüyle giriş yapabilmelisin.
- Web tarayıcı (Edge / Chrome / Firefox son sürüm).
- Denetçinizin PGP anahtarı varsa teslimat için hazır olmalı
  (bkz. §5 "Teslimat").
- **On-prem kurulum için**: `audit-worm` bucket'ının Object Lock Compliance
  mode ile oluşturulduğunu platform ekibinizden teyit edin
  (`infra/compose/minio/worm-bucket-init.sh`).

---

## 3. Aylık Pack Üretimi

### Adım 3.1 — Konsol'a giriş
1. `https://<personel-host>/tr/evidence` adresini aç.
2. Sayfa yüklendiğinde, SOC 2 Kanıt Kasası dashboard'u görünür:
   - **Toplam Kanıt** — dönem için toplam item sayısı
   - **Kapsanan Kontrol** — en az 1 kanıtı olan kontrol sayısı
   - **Eksik Kontrol (Gap)** — sıfır kanıt olan kontroller
   - **Kapsama Matrisi** — her bir TSC kontrolü için ayrıntılı satır

### Adım 3.2 — Dönemi seç
Ay seçici varsayılan olarak içinde bulunulan ayı gösterir. Aylık üretim
yapıyorsan **bir önceki ayı** seç (tam kapanmış dönem).

Örnek: 2026-05-01 tarihinde 2026-04 ayının paketini üretiyorsun.

### Adım 3.3 — Gap kontrolü yap
Sarı "Kanıt eksiği tespit edildi" kutucuğu görünüyorsa, **paketi üretmeden
önce** içindeki kontrol listesine bak:

- **Beklenen gap'ler** (şu anda collector'u olmayan kontroller):
  - `CC6.3`, `CC7.3`, `CC9.1`, `A1.2` — Phase 3.0.3+ ile bağlanacak
- **Beklenmeyen gap'ler** (collector'u olan ama kanıt üretmeyen):
  - `CC6.1` gap görünüyorsa → o ay hiç live view oturumu olmamış. Normal.
  - `CC8.1` gap görünüyorsa → o ay hiç policy push olmamış. Normal.
  - `P7.1` gap görünüyorsa → o ay hiç DSR kapatılmamış. Normal.

**Eğer bu kontrollerde gap var AMA o ay kesin işlem yapıldığını biliyorsan**:
incident! `docs/policies/incident-response.md` §4'e göre Tier 2 olay olarak
bildir. Kayıp evidence = SOC 2 CC7.1 kontrol hatası.

### Adım 3.4 — Paketi indir
**"Paketi İndir (ZIP)"** butonuna bas. Tarayıcı `.zip` dosyasını doğrudan
API'den stream'ler. Dosya adı formatı:

```
personel-evidence-<tenant-uuid>-<YYYY-MM>.zip
```

Tipik boyut: dönem başına 1-10 MB (item sayısına göre).

### Adım 3.5 — İçerik doğrulama
ZIP dosyasını açtıktan sonra aşağıdaki yapıyı görmelisin:

```
manifest.json             # Pack'in içindekiler listesi
manifest.signature        # manifest.json'un Ed25519 imzası
manifest.key_version.txt  # İmzayı atan Vault anahtar sürümü
items/
    01J...01.json         # Her kanıt item'ının yapılandırılmış JSON'u
    01J...01.signature    # Her item'ın bireysel Ed25519 imzası
    01J...02.json
    01J...02.signature
    ...
```

**Hızlı doğrulama** (manifest ItemCount'u ve konsoldaki "Toplam Kanıt"
rakamı aynı olmalı):

```bash
unzip -p personel-evidence-*.zip manifest.json | jq '.item_count'
```

Rakamlar uyuşmuyorsa paketi yeniden indir. Hâlâ uyuşmuyorsa: Tier 2
incident.

---

## 4. İmza Doğrulaması (Opsiyonel — Denetçi Öncesi)

Denetçi imzaları kendi tarafında doğrulayacak olsa da, teslim etmeden önce
kendin doğrulamak iyi bir pratiktir.

### 4.1 — Manifest imzasını doğrula

Vault'taki control-plane anahtarının public key'ini bir kerelik dışa aktar
(platform ekibi sana verir veya `vault read -format=json transit/keys/control-plane-signing`
çıktısından alırsın):

```bash
# control-plane-signing-v1.pem gibi bir dosya elde edersin
openssl pkeyutl -verify \
    -in manifest.json \
    -sigfile manifest.signature \
    -pubin -inkey control-plane-signing-v1.pem
```

`Signature Verified Successfully` dönmeli. Dönmezse paketi teslim etme.

### 4.2 — Bir item'ın WORM bucket karşılığını doğrula (spot check)

Manifest'teki bir satırın `worm_object_key` değerini al:

```bash
unzip -p personel-evidence-*.zip manifest.json | jq '.items[0].worm_object_key'
# → "evidence/tenant-a/2026-04/01J...01.bin"
```

Platform ekibinize bu object key için WORM bucket'tan canonical bytes'ı
çekmelerini isteyin. Canonical bytes + item'ın imzası aynı public key ile
doğrulanmalı. Bu bağımsız doğrulama yolu, pack içi ile WORM içi kanıtın
tutarlılığını kanıtlar.

---

## 5. Teslimat

### 5.1 — Auditor kanalı
Denetçiye paketi teslim etmeden önce:

1. Auditor'un resmi PGP public key'ini al (anlaşma ekinde).
2. ZIP'i PGP ile şifrele:
   ```bash
   gpg --encrypt --recipient auditor@example.com \
       --output personel-evidence-2026-04.zip.gpg \
       personel-evidence-2026-04.zip
   ```
3. Şifreli dosyayı mutabık kalınan kanaldan ilet (SFTP, secure email,
   auditor portalı). **Asla plain ZIP'i internete bırakma**.

### 5.2 — İç arşiv
Paketi MinIO'daki DPO özel bucket'ına da yükle:

- Bucket: `dpo-archive`
- Path: `evidence-packs/<YYYY>/<YYYY-MM>.zip`

Bu, 12 aylık SOC 2 gözlem penceresi boyunca DPO'nun elinde tam bir kopyanın
durmasını sağlar. Audit findings başlangıcında DPO arşivi referans alınır.

### 5.3 — Tutanak
Her paket teslimatında aşağıdaki tutanağı tut (`docs/compliance/evidence-pack-log.md`
dosyasına ekle):

| Dönem | Paketi üreten (DPO) | ItemCount | Gap kontrol sayısı | Teslim tarihi | Denetçi |
|---|---|---|---|---|---|
| 2026-04 | _______ | _______ | _______ | 2026-05-03 | _______ |

---

## 6. Acil Durum Senaryoları

### 6.1 — Paket indirme başarısız
- **Semptom**: Tarayıcı ZIP'i indirmeye başlıyor ama yarıda kesiliyor.
- **Sebep**: API request timeout'u (nginx proxy default 60s) veya API
  server restart.
- **Çözüm**: Yeniden dene. İkinci deneme de başarısız olursa, Tier 2
  incident olarak bildir. WORM'daki kanıt etkilenmez — sadece pack
  oluşturma başarısızdır.

### 6.2 — İmza doğrulama başarısız
- **Semptom**: `openssl pkeyutl -verify` "Signature Verification Failure"
  diyor.
- **Sebep A**: Yanlış public key kullanıyorsun (anahtar rotasyonu sonrası
  eski key ile yeni imzayı doğrulamaya çalışıyorsan). `manifest.key_version.txt`
  dosyasındaki sürümle Vault'taki mevcut sürümü karşılaştır.
- **Sebep B**: ZIP içeriği bozulmuş. Yeniden indir.
- **Sebep C**: **Hakiki tampering**. Eğer A ve B elendiyse Tier 1 incident.
  Vault signer key'i rotasyonu ve forensic inceleme başlat.

### 6.3 — Aynı dönemi yeniden üretmeye ihtiyaç
Pack builder idempotent değildir — her çağrı yeni bir `generated_at`
timestamp'i ile yeni bir ZIP üretir. Aynı dönem için iki kopya varsa, her
ikisi de geçerlidir; `generated_at` alanı farklı olduğu için tutanakta
ayırt edilebilirler. **Auditor'a yeni kopya gönderirken "replaces pack
generated at ___" notunu ekle**.

---

## 7. Yıllık Gözlem Penceresi Kapanışı

Observation window kapanışında (Type II denetiminin başladığı an):

1. Son 12 ayın her biri için ayrı ayrı paket üret.
2. 12 paketi tek bir üst-ZIP'e arşivle: `personel-evidence-2026-04_2027-03.zip`.
3. §4'teki imza doğrulamasını 12 paket için de tekrarla.
4. Manifest satırlarından toplam item sayısını topla — denetçiye özet olarak
   sun.
5. CC6.1, CC8.1, P7.1 için tüm dönemlerde sıfır olmayan kapsama olduğunu
   raporla. Gap varsa §3.3'teki incident sürecini geriye dönük açıkla.

---

## İlgili Dokümanlar

- `docs/adr/0013-dlp-disabled-by-default.md`
- `docs/adr/0014-worm-audit-sink.md`
- `docs/adr/0023-soc2-type2-controls.md`
- `docs/policies/change-management.md`
- `docs/policies/incident-response.md`
- `docs/security/risk-register.md` §R-API-001
- `infra/runbooks/backup-restore.md`

---

*Versiyon 1.0 — Phase 3.0.3 — 2026-04-11*
