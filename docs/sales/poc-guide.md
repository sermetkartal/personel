# Personel — POC Kurulum ve Deneme Kılavuzu (TR)

Bu doküman, potansiyel müşterinin 30 günlük ücretsiz POC (Proof of Concept)
ortamını kurmak, çalıştırmak ve sonunda temiz bir şekilde kaldırmak için
adım adım rehberdir.

**Hedef**: 50 endpoint'e kadar gerçek pilot, 2 saatlik kurulum, müşterinin
kendi altyapısında, Personel ekibi uzaktan destekli.

---

## Önkoşullar

### Donanım (1 Ubuntu VM)

| Kaynak | Minimum | Önerilen |
|---|---|---|
| CPU | 4 vCPU | 8 vCPU |
| RAM | 8 GB | 16 GB |
| Disk | 100 GB SSD | 200 GB NVMe SSD |
| Network | 1 Gbps | 1 Gbps |

### Yazılım

- Ubuntu 22.04 LTS veya 24.04 LTS
- Docker Engine 25+ (`curl -fsSL https://get.docker.com | sh`)
- Docker Compose v2 plugin
- `sudo` erişimi
- Dış ağa erişim (Docker imajları için; air-gapped için tarball verilir)

### Ağ

- Endpoint agent'larından sunucuya: TCP 9443 (mTLS gRPC)
- Admin kullanıcılarından sunucuya: TCP 443 (HTTPS Console + Portal)
- Sunucudan dışa: isteğe bağlı (OTA update, license validation için opsiyonel)

### Hazırlanmış veriler

- 3-5 test çalışanı (gerçek değil, POC için synthetic)
- 50 endpoint için Windows 10/11 test makineleri (fiziksel veya sanal)
- Keycloak OIDC için test e-posta adresleri

---

## Kurulum Adımları

### 1. Personel arşivini indir

Personel ekibi size bir tarball göndercek (~500 MB):

```bash
wget https://releases.personel.local/poc/personel-poc-v0.9.tar.gz
tar xzf personel-poc-v0.9.tar.gz
cd personel-poc
```

Air-gapped ortam için USB ile teslim edilen `personel-airgap-v0.9.tar.gz`
(~2 GB — tüm Docker imajlarını içerir) kullanılır.

### 2. POC installer'ı çalıştır

```bash
sudo ./infra/scripts/install-poc.sh --endpoints=50
```

Bu komut:
- Pre-flight check (CPU, RAM, disk, Docker)
- `.env` dosyasını POC default'lar ile oluşturur
- Vault unseal key'lerini generate eder (Shamir 3-of-5)
- Tüm 18 containerı başlatır
- Postgres migration'ları çalıştırır
- Keycloak realm'i import eder (admin/admin123 demo creds)
- `dev-seed-showcase.sh` ile örnek veri yükler (POC-only; üretime taşınmaz)
- Smoke test koşar (healthz + readyz + NATS publish)
- Trial lisans dosyası üretir: `/etc/personel/license.json` (30 gün, 50 endpoint)

**Toplam süre**: 45-90 dakika (Docker imaj indirme dahil).

### 3. Admin Console'a giriş

```
https://<VM_IP>/console/tr/dashboard
Kullanıcı: admin
Şifre: admin123 (POC default — üretime taşıma)
```

İlk girişte size yeni bir şifre sorulur.

### 4. Endpoint enroll

Her Windows test makinesinde:

```powershell
# Sunucudan msi'yi indir
Invoke-WebRequest https://<VM_IP>/downloads/personel-agent.msi -OutFile personel-agent.msi
Start-Process msiexec -ArgumentList "/i personel-agent.msi /quiet" -Wait

# Enroll token al (Console → Endpoints → New Endpoint → "Token Oluştur")
# Token'ı panoya kopyala, sonra:
& "C:\Program Files (x86)\Personel\Agent\enroll.exe" --token "<PASTE_TOKEN>"
```

Agent otomatik olarak service olarak kayıtlanır ve olayları Console'a
göndermeye başlar.

### 5. Doğrulama

Console → Endpoints: 5-10 dakika içinde yeni endpoint'leri görmeye başlamalısınız.

Console → Dashboard: İlk metrikler ~15 dakika sonra görünür.

---

## POC Kapsamı

### Etkin olan özellikler (Faz 1 scope)

- Windows endpoint izleme (process, window, application usage, idle, file, network)
- Ekran görüntüsü (adaptive frequency, primary monitor)
- Admin Console + Employee Portal
- KVKK DSR workflow (m.11)
- Politika editörü + imzalı push
- Hash-zincirli audit log
- Live view (HR dual-control)
- Prometheus metrics + Grafana dashboard

### POC kapsamında OLMAYAN özellikler

- Klavye içeriği (DLP) — ADR 0013 ile varsayılan KAPALI
- macOS / Linux agent (Faz 2 scaffold)
- UBA insider threat detection (eklenti, POC'da isteğe bağlı etkinleştirilebilir)
- OCR içerik analizi (eklenti)
- HRIS entegrasyonu (eklenti)
- Mobile admin app (Faz 2 scaffold)
- HA cluster (Faz 5 — pilot sonrası)

---

## 30-Günlük Deneme Planı

### Hafta 1: Kurulum ve Temel Konfigürasyon

- **Gün 1-2**: POC ortamı kurulumu, ilk endpoint enroll
- **Gün 3-4**: Keycloak AD/LDAP bağlantısı (opsiyonel), kullanıcı rolleri atama
- **Gün 5-7**: Politika tanımı, hassas veri dışlama kuralları, saklama matrisi review

### Hafta 2: Genişletme ve Eğitim

- **Gün 8-10**: 50 endpoint tam enrollment tamamlanır
- **Gün 11-12**: DPO eğitimi (4 saat — `docs/training/dpo-training-tr.md`)
- **Gün 13-14**: Admin eğitimi (4 saat — `docs/training/admin-training-tr.md`)

### Hafta 3: Gerçek Kullanım

- **Gün 15-17**: İlk gerçek raporlar, Dashboard inceleme, çalışan feedback toplama
- **Gün 18-19**: KVKK DSR test (gerçek çalışan bir başvuru yapar, DPO yanıtlar)
- **Gün 20-21**: Canlı izleme simülasyonu (ikili onay akışı test)

### Hafta 4: Değerlendirme ve Karar

- **Gün 22-25**: POC success metrics analizi (ayak izi, throughput, DSR SLA, uptime)
- **Gün 26-28**: Teknik detay toplantısı, teklif revizyonu
- **Gün 29-30**: Karar: satın al / uzat / kaldır

### POC Success Metrics

- [ ] Endpoint ayak izi <%2 CPU, <150 MB RAM (10 örnek endpoint ölçüm)
- [ ] 50 endpoint 30 gün boyunca %99+ uptime
- [ ] Dashboard p95 query süresi <2 saniye
- [ ] İlk DSR başvurusu 30 gün SLA içinde yanıtlandı
- [ ] Çalışan Şeffaflık Portal kullanım oranı >%50

---

## POC Sonrası Seçenekler

### Seçenek A — Satın al

1. Personel satış ekibi teklif gönderir
2. Sözleşme imzalanır, lisans anahtarı verilir
3. POC ortamı → Production'a dönüştürülür (veri kaybı yok)
4. Onboarding playbook'u devreye girer (`docs/customer-success/playbook-tr.md`)

### Seçenek B — 30 gün uzatma

1. Personel ekibinin onayı ile ek 30 gün trial lisans verilir
2. Ek kullanım ücretsizdir (en fazla 1 uzatma)

### Seçenek C — Kaldır (temiz iade)

```bash
sudo ./infra/scripts/poc-teardown.sh
```

Bu komut:
- Tüm Docker containerı durdurur ve siler
- Volume'ları siler (`docker volume prune`)
- `/var/lib/personel/` dizinini temizler
- `/etc/personel/` config dosyalarını siler
- **Audit export**: tüm audit log'u `/tmp/personel-poc-audit-$(date +%F).tar.gz`
  olarak dışa aktarır (müşteride kanıt kalması için)
- KVKK m.7 kapsamında tüm kişisel veriyi siler
- Imha raporu üretir: `/tmp/personel-poc-destruction-report-$(date +%F).pdf`

Teardown sonrası Personel ekibine raporlar teslim edilir, POC süreci
kapanır.

---

## Destek ve İletişim

POC süresince **Personel DevOps ekibi Priority tier SLA** verir (ücretsiz):

- Mesai içi (09-18) ilk yanıt: 4 saat
- Kritik arıza (P1): 1 saat
- Haftalık 30 dakika durum toplantısı
- WhatsApp/Slack destek kanalı

**İletişim**:
- POC lead: poc-lead@personel.local
- Teknik destek: destek@personel.local
- Acil P1: +90 5xx xxx xx xx (7×24 sadece POC süresi)

---

## Sorun Giderme (Hızlı)

| Sorun | Çözüm |
|---|---|
| `install-poc.sh` fail | `cat /var/log/personel/install.log`, Personel destek'e gönder |
| Endpoint enroll `ERROR: cert signing failed` | `docker logs personel-vault` → sealed mi kontrol et |
| Console `502 Bad Gateway` | `docker ps` → `personel-api` container çalışıyor mu, restart: `docker compose restart api` |
| Agent Console'da görünmüyor | Windows'ta `sc query PersonelAgent` → service çalışıyor mu, `Get-EventLog -LogName Application -Source PersonelAgent -Newest 20` |
| Disk dolu | `df -h /var/lib/personel` → ClickHouse logs en çok yer kaplar, retention policy azalt |
| `license.json expired` | Personel'e ulaş, uzatma talep et |

Detaylı troubleshooting: `docs/operations/troubleshooting.md`

---

*Bu kılavuz POC ekibi için yazıldı — production kurulumu farklıdır, bkz.
`infra/runbooks/install.md`. POC lisansı 30 gün sonunda otomatik expiracy'ye
girer, grace period 7 gün sonra read-only mode.*
