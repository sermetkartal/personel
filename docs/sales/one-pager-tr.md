# Personel — KVKK-Native Kurumsal Çalışan Aktivite İzleme ve Verimlilik Platformu

**Türkiye'nin ilk kriptografik çalışan gizliliği garantisi sunan on-prem UAM platformu.**

---

## Neden Personel?

Türkiye'de 50-500 çalışanlı kurumlar, çalışan verimliliğini ölçmek ve veri sızıntısı
risklerini yönetmek için yabancı SaaS araçlarına (Teramind, ActivTrak, Veriato)
mecbur kalıyor. Bu araçlar:

- **KVKK uyumu için tasarlanmamış** — VERBİS, aydınlatma metni, m.11 hakları sonradan yamalanmış
- **Verileri yurt dışına taşıyor** — KVKK m.9 transfer yasağı risk altında
- **Yönetici klavye içeriğine sınırsız erişim veriyor** — KVKK m.5 orantılılık ihlali
- **Cloud-only** — finansal kurumlar, savunma sanayi, sağlık için on-prem şart

**Personel, on-prem-first, KVKK-native bir alternatif olarak bu beş boşluğu mimari
düzeyde kapatır.**

---

## 5 Temel Değer Önerisi

### 1. KVKK-Native Uyum — Yeniden Yamalı Değil, Mimariden İçerilmiş

- **VERBİS uyumlu veri envanteri** otomatik üretilir (`docs/compliance/verbis-kayit-rehberi.md`)
- **Aydınlatma metni template'i** ilk-giriş modalında zorunlu onay (`apps/portal/src/components/onboarding/first-login-modal.tsx`)
- **Saklama matrisi** (`docs/architecture/data-retention-matrix.md`) — her veri türü için kategori × süre × silme mekanizması
- **Şeffaflık Portalı** — çalışanın hangi verisinin ne için toplandığını kendisinin görebildiği Türkçe web arayüzü
- **DSR (m.11) 30 gün SLA** — otomatik takip, gecikme alarmı, signed PDF yanıt artefaktı
- **Kurul denetim runbook'u** — `infra/runbooks/inspection-ready.md` (hazır)

### 2. Kriptografik Çalışan Gizliliği — Yönetici "Kör" Tutulmuştur

- Klavye içeriği toplanır ancak **admin rolü tarafından dekripte edilemez** — ADR 0013
- Sadece izole DLP motoru, önceden imzalanmış kurallarla eşleşme arayabilir
- **DLP varsayılan KAPALI** — etkinleştirmek için DPO + IT Security + Hukuk imzalı tören gerekir
- Keystroke red team test suite Faz 1 çıkış kriterleri #9'da doğrulanır
- Canlı ekran izleme: **ikili onay** (requester ≠ approver) + süre sınırı + hash-zincirli audit

### 3. Düşük Endpoint Ayak İzi — Rust Agent

- **<%2 CPU, <150 MB RAM** hedef; `qa/footprint-bench` ile ölçülür
- Tek binary (Rust), kütüphane bağımlılığı yok
- Anti-tamper: mutual watchdog + DPAPI-sealed config + PE self-hash
- Offline SQLCipher queue → bağlantı kesintilerinde veri kaybı yok
- WebP delta-encoding ekran görüntüsü → %50 boyut tasarrufu

### 4. On-Prem Modern Stack — 2 Saatlik Kurulum

- Docker Compose + systemd; Kubernetes zorunlu değil
- Tek komut: `sudo infra/install.sh` — idempotent
- 500 endpoint'e kadar tek VM (8 core / 16 GB / 200 GB)
- Vault PKI + Keycloak OIDC + ClickHouse + NATS JetStream + MinIO WORM
- Air-gapped kurulum destekli (offline lisans validasyonu)

### 5. Türkçe-First UX

- Admin console + Employee portal **Türkçe** (İngilizce opsiyonel)
- Bildirimler, raporlar, hata mesajları yerli mevzuat terminolojisi ile
- DPO dashboard KVKK m.11 workflow'u üzerine inşa edilmiş

---

## Rekabetçi Farklılaştırıcılar

| Özellik | Personel | Teramind | ActivTrak | Veriato |
|---|---|---|---|---|
| On-prem yerli deployment | ✅ | ⚠️ (pahalı, sınırlı) | ❌ (cloud-only) | ⚠️ |
| KVKK-native framework | ✅ | ❌ | ❌ | ❌ |
| Kriptografik admin kör-izleme | ✅ (ADR 0013) | ❌ | ❌ | ❌ |
| Türkçe UI + Türkçe destek | ✅ | ❌ | ❌ | ❌ |
| Hash-zincirli tamper-proof audit | ✅ (SOC 2 CC8.1) | ⚠️ | ❌ | ⚠️ |
| Rust agent (bellek-güvenli) | ✅ | ❌ (C++) | ❌ | ❌ |
| Şeffaflık Portalı (çalışan self-service) | ✅ | ❌ | ❌ | ❌ |

Detaylı teardown: `docs/product/competitive-analysis.md`

---

## Fiyatlandırma Göstergesi

- **Başlangıç**: Endpoint başına yıllık (50-100 endpoint tier)
- **Kurumsal**: Özel tier'lar (200+ endpoint, HA cluster, 7×24 destek)
- **Eklenebilir modüller**: UBA insider threat detection, OCR içerik analizi, ML sınıflandırma, HRIS entegrasyonu
- **POC**: 30 günlük ücretsiz deneme, 50 endpoint'e kadar

Destek SLA tier'ları: `docs/customer-success/support-sla.md`

---

## Hemen Başlamak İçin

1. **POC talep et**: `docs/sales/poc-guide.md` — tek VM, 2 saatlik kurulum
2. **Demo izle**: 15 dakika canlı gösteri (`docs/sales/demo-deck-outline.md`)
3. **KVKK risk değerlendirmesi**: DPO'nuzla 1 saatlik review — DPIA şablonumuz hazır
4. **İletişim**: satis@personel.local — cevap süresi 24 saat

---

*Personel, Türkiye pazarına özel tasarlanmış ve KVKK 6698'in her maddesi mimari
kararlara gömülü bir üründür. Yabancı UAM araçlarının eklentisi değil, yerli
mevzuat için ilk günden inşa edilmiş bir çözümdür.*
