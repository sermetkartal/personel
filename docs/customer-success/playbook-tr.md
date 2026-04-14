# Personel — Customer Success Playbook (TR)

Bu doküman, Personel müşteri başarı ekibinin her müşteriye sunacağı deneyimi
adım adım tanımlar. Pre-sales'ten steady state'e, genişlemeden churn riskine
kadar tüm aşamaları kapsar.

**Hedef kitle**: Customer Success Manager (CSM), Satış, Destek ekibi

---

## 1. Pre-Sales Aşaması

### 1.1 Requirements Discovery (1 toplantı, 60 dk)

Görüşme öncesi müşteriden istenen bilgiler:

- [ ] Çalışan sayısı (toplam + izlenecek alt küme)
- [ ] Sektör (finans, sağlık, imalat, teknoloji, hizmet)
- [ ] Mevcut UAM / DLP çözümü var mı? Neden değiştiriliyor?
- [ ] DPO / KVKK sorumlu kişi atanmış mı?
- [ ] Hedef OS dağılımı (Windows / macOS / Linux oranı)
- [ ] On-prem mi SaaS mi tercih? (Personel sadece on-prem Faz 1)
- [ ] Mevcut IT altyapısı (Windows AD, Linux, Docker, Kubernetes var mı)
- [ ] KVKK denetim geçmişi (Kurul'dan soru/uyarı/ceza oldu mu)
- [ ] Bütçe aralığı (nitel; $/endpoint/yıl hissi)
- [ ] Karar alma sürecinde kimler var? (IT Müdürü, DPO, CISO, İK, CFO)

### 1.2 Architect Review (1 toplantı, 90 dk)

Personel tarafı: Satış + Architect. Müşteri tarafı: IT Müdürü + CISO.

Kapsanacak konular:

- Müşterinin mevcut mimari diyagramı
- Personel mimari özet sunumu (`docs/architecture/c4-container.md`)
- On-prem kurulum senaryosu (1 VM veya 3-VM HA)
- Network segmentasyonu planı
- Vault unseal ceremony sorumlulukları (kimler keyholder)
- Data flow + KVKK veri envanteri review

### 1.3 Proposal (yazılı teklif)

Teklif iskeleti:

1. Executive summary (1 sayfa)
2. Scope (endpoint sayısı, modüller, lokasyon)
3. Teknik mimari (1 sayfa C4 görseli + kısa açıklama)
4. Implementation plan (4 hafta POC + 4 hafta rollout)
5. Pricing (Yıllık + tek seferlik + eklenti)
6. Support SLA (`docs/customer-success/support-sla.md` referans)
7. KVKK uyum kanıtları (DPIA + DPA + audit export)
8. Yol haritası (Faz 2-3 preview)
9. Referanslar (varsa)
10. Sözleşme template'i

Teklif **14 gün geçerli**. Personel tarafı revize teklif isteklerini 48 saat içinde yanıtlar.

---

## 2. POC Aşaması (30 gün)

Detay: `docs/sales/poc-guide.md`

### 2.1 Success Criteria tanımı (POC başlangıcı)

Müşteri ve Personel CSM ortak olarak POC başarı kriterlerini yazılı olarak
anlaşır:

**Teknik kriterler**:
- [ ] X endpoint 30 gün boyunca %99+ uptime
- [ ] Endpoint ayak izi <%2 CPU, <150 MB RAM
- [ ] Dashboard p95 query süresi <2 saniye
- [ ] İlk DSR başvurusu SLA içinde yanıtlandı

**Ticari kriterler**:
- [ ] Minimum 2 stakeholder eğitim tamamladı
- [ ] İlk haftalık rapor yayınlandı
- [ ] Çalışan Şeffaflık Portal kullanım >%50

### 2.2 Kick-off toplantısı (Gün 0, 60 dk)

- Sunum: Personel ekibi, CSM, destek kanalları
- Gözden geçirme: POC scope, kurulum planı, haftalık sync takvimi
- Sözleşme: POC NDA + başarı kriterleri imzalı

### 2.3 Haftalık sync (30 dk × 4)

Her sync'te:
- Geçen hafta neler yapıldı
- Karşılaşılan sorunlar + çözümler
- Bu hafta yapılacaklar
- Yardıma ihtiyaç duyulan konular
- Toplantı notları müşteriye 24 saat içinde gönderilir

### 2.4 POC Exit Review (Gün 28-30, 60 dk)

- Başarı kriterlerinin madde madde değerlendirilmesi
- Müşterinin geri bildirimleri
- 3 yol: Satın al / Uzat / Kaldır
- Karar 3 iş günü içinde yazılı olarak teyit

---

## 3. Onboarding Aşaması (90 gün)

Satın alma sonrası 90 günlük yoğun destek dönemi. CSM bu süreçte haftada en
az 1 kez müşteri ile temasta olur.

### Gün 0-7: Kurulum

- [ ] Production license dosyası teslim edilir
- [ ] Production VM üzerinde `install.sh` çalıştırılır
- [ ] Vault Shamir ceremony (5 keyholder)
- [ ] Keycloak AD/LDAP federasyonu kurulur
- [ ] Backup otomatik cron devreye girer
- [ ] Monitoring (Prometheus + Grafana) onaylanır

### Hafta 2: İlk Eğitimler

- [ ] Admin eğitimi (4 saat) — `docs/training/admin-training-tr.md`
- [ ] DPO eğitimi (2 saat) — `docs/training/dpo-training-tr.md`
- [ ] İK eğitimi (1 saat) — canlı izleme onay akışı
- [ ] Kullanıcı manuelleri dağıtılır

### Hafta 3-4: Pilot Rollout

- [ ] Pilot bölüm (10-20 çalışan) rollout
- [ ] Çalışanlara portal tanıtımı
- [ ] İlk DSR işlenir (test veya gerçek)
- [ ] İlk ekran görüntüsü incelemesi (Investigator onayı ile)

### Hafta 5-8: Full Rollout

- [ ] Tüm çalışanlara rollout (bölüm bölüm)
- [ ] İlk politika imzalı push (hassas veri dışlama kuralı)
- [ ] İlk aylık rapor üretilir ve DPO'ya sunulur

### Hafta 9-12: Health Review

- [ ] 90. gün sağlık kontrolü toplantısı
- [ ] Uptime, ayak izi, DSR SLA compliance raporu
- [ ] Çalışan portal kullanım analizi
- [ ] Churn riski göstergeleri (bkz §7) taraması
- [ ] Quarterly Business Review (QBR) takvimi belirlenir

**Milestone gate'ler**:
| Gate | Kriter | Geçmezse |
|---|---|---|
| Hafta 2 | Kurulum tam, smoke test geçti | Personel engineer 24h SLA ile müdahale |
| Hafta 4 | İlk DSR yanıtlandı | CSM + DPO ile özel sync |
| Hafta 8 | Full rollout tamam | Full-rollout engelleri değerlendirilir |
| Hafta 12 | Health review başarılı | Red flag → escalation (bkz §6) |

---

## 4. Steady State

### 4.1 Quarterly Business Review (QBR)

Her 3 ayda bir CSM + müşteri IT + DPO + yönetim bir araya gelir.

Kapsam:

- Son 3 ayın metrikleri (uptime, event throughput, DSR sayısı, SLA compliance)
- Yeni özellik istekleri (roadmap inputs)
- Yol haritası preview (gelecek 3 ay)
- Lisans kullanım durumu (kapasite yaklaşıyor mu)
- Upsell fırsatları (bkz §5)
- Churn risk tespiti (bkz §7)

### 4.2 Aylık metrik raporu (otomatik)

Personel, her ayın 1'inde müşteriye yazılı rapor gönderir:

- Endpoint online/offline istatistiği
- DSR başvuru sayısı + SLA compliance
- Audit log büyüme
- Politika değişikliği sayısı
- En çok kullanılan 10 uygulama
- En aktif bölüm
- Uptime % (SLA hedef karşılaştırması)

Otomasyon: `infra/scripts/monthly-customer-report.sh` (cron'da yazılacak — Faz 15).

### 4.3 On-call SLA response

Bkz `docs/customer-success/support-sla.md`

---

## 5. Expansion — Upsell Fırsatları

### 5.1 Trigger'lar

- **Yeni bölüm/departman ekleniyor** → endpoint kapasitesi artışı
- **Yeni iştirak/alt şirket** → multi-tenant etkinleştirme
- **M&A** → birleşme sonrası tek stack kurulumu
- **KVKK denetimi geldi** → SOC 2 evidence modülü upsell
- **Insider threat olayı yaşadı** → UBA modülü upsell
- **Sertifikasyon süreci** (ISO 27001, PCI-DSS) → SIEM export modülü

### 5.2 Upsell yaklaşımı

- Satış agresifliği DEĞİL, problem-çözme yaklaşımı
- "Bu durumda size yardımcı olabilecek ek modülümüz var" çerçevesi
- 14 günlük ücretsiz modül deneme hakkı

---

## 6. Escalation Matrix

| Seviye | Sebep | Aksiyon | Yanıt Süresi |
|---|---|---|---|
| L1 | Routine ticket | Support ticket | SLA tier gore |
| L2 | Hafta 4/8/12 gate fail | CSM + Engineering daily standup | 24 saat |
| L3 | Üretim P1 down | CSM + CTO + VP Sales call | 1 saat |
| L4 | Müşteri sözleşme iptal tehdidi | CSM + CEO call | 4 saat |

---

## 7. Churn Risk İndikatörleri

### 7.1 Erken Uyarı

- 🟡 Aylık rapor 2 ay arka arkaya okunmadı (email tracking)
- 🟡 DPO kişisi işten ayrıldı (yeni kişiyle re-onboarding gerekli)
- 🟡 Endpoint online sayısı %20+ düştü
- 🟡 3 hafta sync toplantısı iptal edildi
- 🟡 Destek ticket'ında olumsuz ton

### 7.2 Kritik

- 🔴 Production VM kapatıldı (yeniden kurulum sürüyor mu?)
- 🔴 Agent'lar 7 gündür güncel veri yollamıyor
- 🔴 Sözleşme yenileme süresi yaklaşıyor (<60 gün) ve QBR yapılmadı
- 🔴 Müşteri rakip ürün POC yapıyor (haber geldi)
- 🔴 Ödeme gecikmesi

### 7.3 Retention Playbook

1. **Tespit** (otomatik alarm veya CSM manuel fark)
2. **Teyit** (CSM müşteri IT ile kısa call)
3. **Root cause analizi**
4. **Retention offer** (indirim, eklenti modül ücretsiz, ek destek, özel eğitim)
5. **Eskalasyon** (gerekirse CEO seviyesi)
6. **Post-mortem** (kaybedilse bile öğren)

---

## 8. Knowledge Base ve Self-Service

Müşterilerin bağımsız kalabilmesi için sürekli güncellenen KB:

- `docs/operations/troubleshooting.md`
- `docs/user-manuals/` (admin, DPO, IT operator)
- `docs/training/` (eğitim materyalleri)
- Internal: `docs/customer-success/known-issues.md` (yeni issue'lar eklenecek)

---

## 9. Customer Health Score

Her müşteriye her hafta 0-100 health score atanır:

| Faktör | Ağırlık | Ölçüm |
|---|---|---|
| Uptime son 30 gün | 25% | %99.5+ → tam puan |
| SLA compliance | 20% | DSR yanıt süreleri |
| Platform kullanımı | 15% | Aktif admin + DPO kullanıcı sayısı |
| Çalışan portal kullanımı | 10% | >%50 → tam puan |
| Ticket sayısı | 10% | Az ticket = sağlıklı |
| Upsell gerçekleşme | 10% | Eklenti satın alma |
| Sözleşme yaş | 10% | İlk yıl içindeyse yüksek risk |

- 80-100: Healthy 🟢
- 60-79: At risk 🟡
- <60: Churn risk 🔴

---

## 10. Dokümantasyonun Güncelleme Sorumluluğu

Bu playbook canlı bir dokümandır:

- **Aylık revize**: CSM ekibi içgörüleri ekler
- **Yılda 1 kez büyük revizyon**: Yeni Faz'lar (Faz 2, Faz 3) geldiğinde
- **Her müşteri kaybı**: Post-mortem ile iyileştirilir

Son güncelleme: 2026-04-13
