# Personel — 15 Slide Demo Deck Outline (TR)

Bu doküman, Personel UAM platformunu potansiyel müşteriye canlı sunmak için
kullanılacak 15-slaytlık demo deck'in yapısını tanımlar. Her slayt için:
konuşma süresi, kapsanacak noktalar, kullanılacak ekran görüntüleri ve
demo komutları listelenir.

**Toplam süre**: 30-45 dakika (15 slayt × 2-3 dakika) + 10 dakika soru-cevap

**Hedef kitle**: IT Müdürü, CISO, DPO, İK Müdürü, CFO (karar komitesi)

**Pre-demo hazırlık**: vm3 pilot ortamında 3 senaryo hazır (aşağıda #6-8)

---

## Slayt 1 — Kapak

- **Başlık**: "Personel: KVKK-Native Kurumsal Çalışan İzleme Platformu"
- **Alt başlık**: "On-Prem. Türkçe. Mimariden Uyumlu."
- **Konuşmacı**: Ad + unvan
- **Tarih + şirket logosu**

**Süre**: 30 sn

---

## Slayt 2 — Pazar Problemi

- Türkiye'de 50-500 çalışanlı kurumlar çalışan verimliliğini ölçmek ve veri sızıntısı
  risklerini yönetmek için SaaS UAM araçlarına yöneliyor
- Ancak bu araçlar:
  - **Veri yurt dışına çıkıyor** (KVKK m.9 risk)
  - **KVKK m.11 hakları sonradan yamalı** (DSR workflow yok)
  - **Yönetici klavye içeriğine sınırsız erişim** (KVKK m.5 orantılılık ihlali)
  - **Türkçe değil** (DPO ve İK yabancı arayüzle baş etmek zorunda)
- Sonuç: pilot projeler başlıyor, KVKK denetimine hazırlık aşamasında iptal oluyor

**Süre**: 2 dk

---

## Slayt 3 — UAM Pazarı (Türkiye) + Rakipler

- Global pazar: Teramind, ActivTrak, Veriato, Insightful, Safetica
- Türkiye'ye özgü oyuncu: **yok** (Personel pazardaki ilk yerli-mimari çözüm)
- Rekabet matrisi (detay: `docs/product/competitive-analysis.md`):
  - Teramind: Özellik açısından zengin ama KVKK için tasarlanmamış, pahalı
  - ActivTrak: Cloud-only, klavye içeriği yok, verimlilik analytics odaklı
  - Veriato: Insider threat güçlü, on-prem var ama TR desteği yok
- **Personel'in yeri**: KVKK-native + on-prem + Türkçe + kriptografik gizlilik

**Süre**: 2 dk

---

## Slayt 4 — Personel Farkı

Üç çivi:

1. **ADR 0013 — Klavye içeriği varsayılan KAPALI**: DPO + IT Security + Hukuk
   imzalı tören olmadan aktifleşmez. Admin, etkinleştirilse bile içeriği
   **kriptografik olarak** okuyamaz — sadece izole DLP motoru kurallarla
   eşleşme arayabilir.
2. **On-prem ilk gün**: Docker Compose + systemd, 2 saatlik kurulum, air-gapped
   destek. Kubernetes zorunlu değil. Cloud seçeneği Faz 3.
3. **KVKK mimariden içerilmiş**: VERBİS export, saklama matrisi, DSR 30 gün SLA,
   hash-zincirli audit, Şeffaflık Portalı — hepsi çekirdekte.

**Süre**: 3 dk

---

## Slayt 5 — Mimari Özet

- Tek görsel: C4 Container diyagramı (`docs/architecture/c4-container.md`)
- Renkli katmanlar:
  - **Endpoint** (Rust agent) — kırmızı
  - **Gateway + Enricher** (Go) — turuncu
  - **Storage Tier** (Postgres, ClickHouse, MinIO, OpenSearch, Vault, Keycloak) — yeşil
  - **Admin API + Console + Portal** (Go + Next.js) — mavi
- "Tüm stack tek VM'e sığar, 18 container, `infra/install.sh` ile 2 saatte ayağa"

**Süre**: 3 dk

---

## Slayt 6 — Demo Senaryo 1: Enterprise Admin Workflow

**Canlı demo** (pilot vm3 üzerinden):

1. Admin Console'a OIDC ile giriş (`https://vm3.pilot/console`)
2. Endpoint listesi → 50 çalışan ekran görüntüsü
3. Bir endpoint detayına tıkla → günlük/saatlik istatistik, en çok kullanılan uygulamalar
4. Politika editörü → "hassas veri dışlama kuralı" ekle, imzala, push et
5. Audit log'da "policy.push" entry'sini göster

**Komut**: `./infra/scripts/dev-seed-showcase.sh` önceden çalıştırılır

**Süre**: 4 dk

---

## Slayt 7 — Demo Senaryo 2: KVKK DSR Fulfillment

**Canlı demo**:

1. Çalışan portalı üzerinden "Verilerim" (`/tr/verilerim`) sayfası
2. Çalışan DSR başvurusu: "Benimle ilgili tüm verilerin kopyasını istiyorum" (m.11/b)
3. DPO dashboard'a geç → bekleyen DSR listesi → başvuruyu al
4. DPO "Yanıtla" → Fulfillment service ZIP export üretir (imzalı PDF + veri JSON)
5. 30 gün SLA geri sayacı göster

**Vurgu**: "Rakip hiçbir üründe bu workflow çekirdekte yok."

**Süre**: 4 dk

---

## Slayt 8 — Demo Senaryo 3: Insider Threat Detection

**Canlı demo** (veya video, gerçek tehdit simülasyonu zor):

1. UBA detector dashboard (`apps/uba-detector`) → risk skoru yüksek çalışan
2. Detayda: mesai dışı dosya erişimi + yeni host bağlantıları + politika ihlalleri
3. Investigator rolü ile canlı izleme talebi
4. IT Manager onay ekranı → ikili onay (requester ≠ approver)
5. 15 dakika canlı oturum → otomatik kapanış + audit kaydı

**Önemli not**: "Bu oturum hash-zincirli audit'e kaydedildi, değiştirilemez."

**Süre**: 4 dk

---

## Slayt 9 — Güvenlik (Threat Model + Defense-in-Depth)

- Threat model: `docs/security/threat-model.md` (STRIDE kapsamlı)
- 5 savunma katmanı:
  1. **Endpoint**: Rust bellek-güvenli + DPAPI config + anti-tamper watchdog
  2. **Transport**: mTLS + Vault PKI + cert pinning + key version handshake
  3. **Backend**: Postgres RLS + ClickHouse role-based + NATS JetStream at-rest encryption
  4. **Secrets**: Vault Shamir 3-of-5 + transit engine + `exportable: false`
  5. **Audit**: Hash-chained append-only + MinIO Object Lock WORM + günlük checkpoint imzası
- SOC 2 Type II observation window aktif (9 kontrol collector)

**Süre**: 3 dk

---

## Slayt 10 — KVKK Uyum Kanıtları

- **VERBİS**: Otomatik envanter export (`/v1/kvkk/verbis-export`)
- **DPIA**: Template dolduruldu (`docs/compliance/dpia-sablonu.md`)
- **Aydınlatma metni**: Çalışan ilk girişte onaylamak zorunda (first-login modal)
- **Açık rıza**: Sadece DLP opt-in için (ADR 0013), geri çekilebilir
- **Saklama matrisi**: 12 veri kategorisi × 12 süre × silme mekanizması
- **Hash-zincirli audit**: 55 kanonik aksiyon, her mutasyon imzalı
- **Kurul denetim runbook'u**: Hazır — gelen müfettişe sunulacak dosya seti

Tek slayt: görsel matris + 6 kanıt dosyasına referans

**Süre**: 3 dk

---

## Slayt 11 — Performans + Ölçeklenebilirlik

- Endpoint ayak izi: **<%2 CPU, <150 MB RAM** (Rust, `qa/footprint-bench`)
- Backend throughput: **500 endpoint / 1 VM** (Faz 1 exit kriteri)
- Event throughput: **10.000+ event/sn** gateway ingest
- Dashboard p95 query: **<1 saniye** (ClickHouse)
- Uptime hedef: **%99.5** (30 günlük pilot)
- Genişleme: Faz 5 cluster mode ile 2+ node Postgres + ClickHouse + NATS

**Süre**: 2 dk

---

## Slayt 12 — Kurulum + Operasyon

- **Kurulum**: `sudo infra/install.sh` → idempotent, 2 saat
- **Prerequisites**: 1 Ubuntu 22.04 VM, 8 core, 16 GB RAM, 200 GB disk, Docker 25+
- **Bootstrap ceremony**: Vault Shamir 3-of-5 unseal key split (5 keyholder, 3 gerekli)
- **Backup**: Nightly Postgres dump + ClickHouse snapshot + MinIO mirror
- **Upgrade**: Zero-downtime blue-green (Faz 13 #136)
- **Monitoring**: Prometheus + Grafana + AlertManager dahil
- **Destek**: 3 tier SLA (`docs/customer-success/support-sla.md`)

**Süre**: 2 dk

---

## Slayt 13 — Fiyatlandırma Modeli

- **Endpoint başına yıllık** (Pazar standardı)
- 3 tier:
  - **Starter** (50-100 endpoint): Temel özellikler, 8×5 destek
  - **Business** (100-300 endpoint): + UBA + OCR + Mobile admin app, 24×5 destek
  - **Enterprise** (300+ endpoint): + HRIS + SIEM + HA cluster + özel özellik, 24×7 destek
- **Eklenti modüller**: UBA (%20), OCR (%15), HRIS integration (%15), Mobile app (%10)
- **POC**: 30 gün ücretsiz, 50 endpoint'e kadar
- **Lisans mekanizması**: Online veya offline (air-gapped) validasyon (`apps/api/internal/license/`)

**Gerçek rakamlar müşteriye özel teklifte** — genel fiyat listesi public değil

**Süre**: 3 dk

---

## Slayt 14 — Yol Haritası (Faz 2-3 Preview)

**Faz 2 (aktif)**:
- macOS + Linux agent scaffolds (ilk release sonrası çalışabilir)
- ML kategori sınıflandırıcı (Llama 3.2 3B + regex fallback)
- UBA insider threat detection (isolation forest)
- Mobile admin app (Expo React Native, 5 ekran)
- HRIS connector framework (BambooHR + Logo Tiger adapter)
- SIEM exporter (Splunk HEC + Sentinel DCR)
- SOC 2 Type II evidence locker (aktif)

**Faz 3 (planlama)**:
- Multi-tenant SaaS deployment (K8s)
- ISO 27001 + ISO 27701 sertifikasyonu
- GDPR genişleme (AB pazarı)
- Windows minifilter driver (forensic DLP)

**Süre**: 2 dk

---

## Slayt 15 — Sorular + İletişim

- **Teşekkür ederiz**
- **Bugünkü eylem planı**:
  1. POC talebi gönder → 48 saat içinde kurulum
  2. DPO ile 1 saatlik KVKK risk review
  3. Teknik derin dalış: ikinci toplantı (CISO + IT mimarı)
- **İletişim**:
  - Satış: satis@personel.local
  - Teknik: destek@personel.local
  - DPO: dpo@personel.local
- **Materyal**: `docs/sales/sales-faq-tr.md` + `docs/sales/roi-calculator-tr.md`

**Süre**: Soru-cevap + 5 dk

---

## Demo Hazırlık Checklist (Sunum Günü)

- [ ] vm3 pilot stack çalışıyor (`sudo systemctl status personel-*`)
- [ ] `dev-seed-showcase.sh` çalıştırıldı (son 24 saatte)
- [ ] Admin console erişilebilir (`https://vm3.pilot/console/tr/dashboard`)
- [ ] Portal erişilebilir (`https://vm3.pilot/portal/tr/verilerim`)
- [ ] UBA dashboard'da en az 1 yüksek-risk çalışan hazır (veya seed data ile)
- [ ] İnternet bağlantısı yedek (demo fail-safe)
- [ ] Slayt deck hem TR hem EN versiyonu (karar komitesi karışık olabilir)
- [ ] Rekabet matrisi yazdırılmış el ilanı (1 sayfa)
