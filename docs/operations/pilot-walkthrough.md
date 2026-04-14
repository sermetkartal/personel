# Pilot Müşteri Senaryo Kılavuzu

> **Kapsam**: Faz 17 #188. Personel UAM platformunun gerçek bir pilot
> müşteriye sahne arkasından gösterimi için uçtan uca senaryo kılavuzu.
> Altı senaryo, her biri kendi önkoşul / adım / beklenen sonuç
> bloğuyla. Toplam gösterim süresi ~90 dakika.
>
> **Dil**: Türkçe (operatör / çağrılan destek rolü). Müşteri gözü
> önünde İngilizce kapsam yoktur.
>
> **Araç gereksinimleri**:
> - Laptop + iki sanal masaüstü (biri admin console için, diğeri
>   şeffaflık portalı için)
> - Canlı demo endpoint'leri: `showcase-zeynep`, `showcase-bahadir`,
>   `showcase-elif` — her üçü hazır kurulmuş Windows VM'lerde
> - Operatör JWT'si, ayrı DPO ve HR hesapları
> - Projeksiyon / ekran paylaşımı ile müşteri tarafının
>   görebildiği monitör

---

## Senaryo 1 — Gün 0 Operatör Kurulumu

**Süre bütçesi**: 15 dakika
**Rol dağılımı**: Operatör (siz), BT Müdürü (müşteri), isteğe bağlı DPO (müşteri)

### Önkoşullar

- Müşteri Ubuntu host hazır, SSH erişimi var
- `/opt/personel/infra/compose/.env.example` kopyalanmış `.env` doldurulmuş
- MinIO, Vault, Postgres root şifreleri `pass-keeper`'a eklenmiş
- DNS kayıtları (`personel.customer.local`, `portal.customer.local`) yayında

### Adımlar

1. **SSH bağlantısı** — `ssh ops@personel-host`
2. **Preflight**
   ```bash
   cd /opt/personel
   sudo infra/scripts/preflight-check.sh
   ```
   Beklenen: `PASS` satırlarından oluşan liste, kritik eksik yok.
3. **Install**
   ```bash
   sudo infra/install.sh
   ```
   Bu komut 2 saat hedef süresinde çalışır; demo için "dakikalar içinde"
   anlatın ama izle + çay içme modunda bekleyin. Gerçek demo senaryoları için
   kurulum önceden tamamlanmış olmalıdır.
4. **İlk admin kullanıcısı**
   ```bash
   sudo infra/scripts/create-admin.sh --email admin@customer.local
   ```
   Çıktı: temp parola; müşteri ilk girişte değiştirecek.
5. **İlk 3 endpoint enroll**
   - `showcase-zeynep` VM'inde:
     ```powershell
     msiexec /i personel-agent.msi ENROLL_TOKEN="$token"
     ```
   - Diğer iki endpoint için aynı ceremony
6. **Doğrulama**
   ```bash
   ./infra/scripts/post-install-validate.sh
   ```
   Çıktı JSON'unda `overall: pass`

### Beklenen Sonuçlar

- Console `/tr/endpoints` üç yeni endpoint'i `online` olarak gösterir
- Admin audit log'da `install.complete`, `user.created`, `endpoint.enrolled x3` kayıtları
- MinIO `audit-worm` bucket'ında ilk günlük checkpoint

### Ekran Görüntüsü Yerleşimi

- `[SCR-1A]` preflight çıktısı
- `[SCR-1B]` console endpoint listesi
- `[SCR-1C]` audit log `install.complete` satırı

---

## Senaryo 2 — Çalışan İzleme ve Verimlilik Analizi

**Süre bütçesi**: 15 dakika
**Rol dağılımı**: Operatör + Manager müşteri rolü

### Önkoşullar

- Senaryo 1 bitmiş, 3 showcase endpoint aktif
- En az 30 dakika önce başlatılmış gerçek kullanıcı aktivitesi
  (Outlook, Chrome, Office çalıştırılmış)

### Adımlar

1. Manager hesabıyla console'a giriş (`manager@customer.local`)
2. **Dashboard** — Flow 7 silence panel, DSR kuyruğu, live view
   onaylarını göster; şu an hepsi boş
3. **Employees** → `Zeynep Demir`
4. **Günlük istatistik şeridi** — aktif dakikalar, idle dakikalar,
   odak segmentleri
5. **Saatlik aktivite grafiği** — 24 saatlik sütun grafik
6. **Top app drill-down** — `chrome.exe`, `OUTLOOK.EXE`, `Teams.exe`
   satırlarını tıkla, süreç sayacı + pencere başlığı örnekleri
7. **Kategori dağılımı** — Faz 8 #82 ML classifier ya da Go regex
   fallback (hangisi aktifse) sonucunu göster: "Bankacılık / üretkenlik
   / kişisel" pie chart
8. **Çalışan profili kartı** — HRIS'den senkronize edilmiş (mock değer
   göster: departman, yönetici, telefon sadece Admin rolüne)

### Beklenen Sonuçlar

- Grafikler boş değil, en az 30 dakikalık veri görünür
- "Özel nitelikli" bayrak ile işaretlenmiş hiçbir olay yok
  (özellikle göster — KVKK uyum noktası)

### Ekran Görüntüsü Yerleşimi

- `[SCR-2A]` employee detail top-of-page
- `[SCR-2B]` saatlik bar chart
- `[SCR-2C]` top apps drill-down tablosu

---

## Senaryo 3 — KVKK DSR Yaşam Döngüsü

**Süre bütçesi**: 20 dakika
**Rol dağılımı**: Operatör + Çalışan (ikinci laptop/tablet) + DPO (müşteri)

### Önkoşullar

- Employee user `zeynep.demir@customer.local` Keycloak'ta var
- DPO kullanıcısı farklı tarayıcıda giriş yapmış

### Adımlar

**Çalışan tarafı** (şeffaflık portalı):

1. `https://portal.customer.local/tr/haklar` — m.11 hak listesi
2. "Erişim hakkımı kullanmak istiyorum" butonu
3. Form: konu = "son 30 gündeki tüm aktivite kayıtlarım", gerekçe metni
4. "Gönder" → başvuru ID, SLA timer (30 gün)

**DPO tarafı** (admin console):

5. `/tr/dsr` — yeni başvuru kuyruğun başında
6. Başvuru detayı — SLA geri sayımı, çalışan profili, açıklama
7. "Veri topla" butonu — arka planda DSR fulfillment job tetiklenir
   (ClickHouse + Postgres + MinIO'dan PII export)
8. Job tamamlanınca: imzalı ZIP dosyası + SHA-256 + Vault imzası
9. "Çalışana teslim et" — MinIO presigned URL portal'a push edilir
10. Audit log'a `dsr.collected`, `dsr.fulfilled` satırları eklendi

**Çalışan tarafı**:

11. Portal bildirimi "başvurunuz yanıtlandı"
12. İndir butonu → imzalı ZIP

**DPO tarafı — denetim izi**:

13. `/tr/audit?entity=dsr.{id}` — tüm durum geçişleri hash-zincirli
    olarak listelenir, her satırın sha256'sı bir önceki satıra zincirli

### Beklenen Sonuçlar

- SLA geri sayımı dashboard'da yeşil (30 gün içinde tamamlandı)
- Evidence locker'a `P7.1` kontrolü için bir `KindComplianceAttestation`
  öğesi eklendi (Faz 3.0.2 collector A)
- Audit zinciri kesintisiz

### Ekran Görüntüsü Yerleşimi

- `[SCR-3A]` portal başvuru formu
- `[SCR-3B]` DPO dashboard SLA timer
- `[SCR-3C]` imzalı ZIP iniş sayfası

---

## Senaryo 4 — Güvenlik Olay Araştırması (HR-Gated Live View)

**Süre bütçesi**: 20 dakika
**Rol dağılımı**: Operatör + Investigator + HR (üç ayrı laptop veya
tarayıcı profili)

### Önkoşullar

- Investigator ve HR kullanıcıları Keycloak'ta ayrı rollerde
- Showcase-zeynep endpoint'inde anomali oluşturmak için önceden USB
  mass storage takılmış (UBA detektör bunu off-hours sinyali ile
  eşleştirecek şekilde kurgulanmış)

### Adımlar

**Investigator tarafı**:

1. `/tr/dashboard` — UBA risk skor kartı, Zeynep "orta risk (62/100)"
2. "Olayı incele" — zaman çizelgesi, tetikleyici olay: USB + yüksek
   dosya okuma oranı
3. "Canlı izleme iste" butonu
4. Form: gerekçe kodu `investigation.insider_suspect_001`, süre 15 dk,
   açıklama metni (≥ 200 karakter zorunlu)
5. "Onay bekliyor" durumuna geçti

**HR tarafı**:

6. `/tr/live-view/approvals` — bekleyen talep
7. Onay formunda requester ID ve metadata
8. **Önemli**: HR kullanıcısı `approver_id != requester_id` kontrolü
   sunucu tarafında; aynı kullanıcı onaylayamaz
9. "Onayla" — audit log'a `liveview.approved` satırı

**Investigator tarafı**:

10. Oturum başladı, LiveKit viewer embed'i açılır, 15 dakika geri sayım
11. Gerçek demo: 2-3 dakika izledikten sonra "Oturumu sonlandır"
12. Sonlandırma modal'ında `termination_reason` (ör. "yeterli delil
    toplandı")

**DPO tarafı**:

13. `/tr/audit?type=liveview` — oturumun tüm geçişleri (requested →
    approved → started → terminated)
14. Evidence locker `CC6.1` için yeni kayıt (Faz 3.0.1 liveview
    collector)

### Beklenen Sonuçlar

- Oturum süresi audit'te "requested 15m, actual 02:47"
- Video kaydı YOK (Faz 1 pilot, recording kapalı)
- Audit zinciri üst üste 4 satır, her biri önceki sha256'ya bağlı
- Çalışan portal'da "geçmiş canlı izleme oturumları" listesinde bu
  oturum görünür (varsayılan açık şeffaflık)

### Ekran Görüntüsü Yerleşimi

- `[SCR-4A]` investigator isteme formu
- `[SCR-4B]` HR onay ekranı
- `[SCR-4C]` LiveKit viewer geri sayım
- `[SCR-4D]` audit log zinciri

---

## Senaryo 5 — KVKK Denetim Hazırlığı (Kurul Prep)

**Süre bütçesi**: 10 dakika
**Rol dağılımı**: Operatör + DPO

### Önkoşullar

- En az 30 gün geçmiş olay verisi (demo için "son 30 gün" filtresi
  canlı kabul edilir)
- `/tr/evidence` sayfası DPO tarafından erişilebilir

### Adımlar

1. **Evidence Locker** → `/tr/evidence`
2. Kontrol kapsama matrisi: 9 SOC 2 TSC kontrolü (CC6.1, CC6.3, CC7.1,
   CC7.3, CC8.1, CC9.1, A1.2, P5.1, P7.1) × son 12 ay heatmap
3. Boşluk uyarı kartı (eğer varsa): gri hücreler "Yetersiz kanıt" olarak
   işaretli
4. "Pakedi İndir (ZIP)" — DPO-only butonu
5. İmzalı ZIP şunları içerir:
   - `manifest.json` (kontrol listesi + item özeti)
   - `manifest.signature` (Vault transit key imzası)
   - `manifest.key_version.txt`
   - Her item için `<id>.json` + `<id>.signature`
6. **Chain verify**
   ```bash
   ./infra/scripts/verify-audit-chain.sh --date 2026-03-01..2026-03-31
   ```
   Çıktı: "chain OK, 12847 rows, 0 tampered"
7. **Retention proof**
   - `/tr/retention` sayfası
   - 36 event türü × retention süresi × imha yöntemi tablosu
   - Son 6 aylık otomatik imha raporu indir (imzalı PDF)

### Beklenen Sonuçlar

- Evidence pack imzaları doğrulanabilir (operator `openssl` +
  Vault public key ile ispatlar)
- Chain verify sıfır tampered satır
- Retention policy tablosu KVKK matris şablonuyla birebir eşleşiyor

### Ekran Görüntüsü Yerleşimi

- `[SCR-5A]` evidence coverage heatmap
- `[SCR-5B]` imzalı pack indirme diyaloğu
- `[SCR-5C]` retention raporu PDF ilk sayfa

---

## Senaryo 6 — Yükseltme Tatbikatı (Rolling Upgrade + Rollback Kararı)

**Süre bütçesi**: 10 dakika
**Rol dağılımı**: Operatör + BT Müdürü (gözlemci)

### Önkoşullar

- Mevcut sürüm: 0.1.0
- Yeni sürüm imajları staging registry'de: 0.1.1-rc1
- Son tam backup bugün 02:00'de alınmış

### Adımlar

1. **Backup doğrulama**
   ```bash
   ls -lh /var/lib/personel/backups/$(date +%Y-%m-%d)
   ./infra/scripts/backup-orchestrator.sh --verify-only
   ```
   Çıktı: `verified`, hash eşleşti
2. **Rolling upgrade başlat**
   ```bash
   cd /opt/personel
   git fetch origin && git checkout v0.1.1-rc1
   PERSONEL_VERSION=0.1.1-rc1 docker compose up -d \
     api gateway enricher console portal
   ```
3. **Sağlık kontrolü**
   ```bash
   ./infra/scripts/post-install-validate.sh --report=/tmp/upgrade.json
   ```
4. **Canary smoke**
   ```bash
   ./infra/scripts/final-smoke-test.sh --skip-phase1 --budget=180
   ```
5. **Karar noktası**: rapor yeşilse `PERSONEL_VERSION=0.1.1-rc1` olarak
   `.env`'a kilitle ve commit at. Kırmızıysa rollback:
   ```bash
   git checkout v0.1.0
   PERSONEL_VERSION=0.1.0 docker compose up -d
   ```
6. **Upgrade audit log** — `system.upgrade.start/success/rollback`

### Beklenen Sonuçlar

- Uygulamalar 2 dakikadan kısa sürede kubişi (roll) yapıldı
- Endpoint agent bağlantıları kopmadı (mTLS gateway downtime < 15s)
- Smoke rapor `overall: pass`
- Tam rollback senaryosunda sıfır veri kaybı

### Ekran Görüntüsü Yerleşimi

- `[SCR-6A]` git checkout + compose up çıktısı
- `[SCR-6B]` post-install rapor JSON'u
- `[SCR-6C]` audit log upgrade satırı

---

## Demo Formatı Özeti

| Senaryo | Süre | Rol(ler) | Göstergeler |
|---|---|---|---|
| 1 — Kurulum | 15dk | Ops + BT | install.sh, enroll ceremony |
| 2 — İzleme | 15dk | Manager | employee detail, ML kategori |
| 3 — DSR | 20dk | Employee + DPO | KVKK m.11 lifecycle |
| 4 — Güvenlik | 20dk | Inv + HR + DPO | dual-control live view |
| 5 — Denetim | 10dk | DPO | evidence pack + chain verify |
| 6 — Upgrade | 10dk | Ops | rolling upgrade + rollback |
| **Toplam** | **90dk** | | 6 senaryo, ~15 ekran |

## Demo Sonrası Teslimat

1. Senaryo 5'in imzalı evidence pack'i
2. Senaryo 3'ün DSR yanıt ZIP'i (hijyen — customer tarafında siler)
3. Senaryo 6'nın upgrade audit log'u
4. `final-smoke-test.md` rapor çıktısı
5. Bu kılavuz dokümanı (`docs/operations/pilot-walkthrough.md`)

---

*Son güncelleme*: 2026-04-14 — Faz 17 #188 closeout.
