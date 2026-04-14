# Personel — Admin Eğitimi (4 saat, TR)

Bu doküman, Personel Platform'un yönetici rolünü devralan IT personeli için
4 saatlik hızlandırılmış eğitim programının içeriğini tanımlar.

**Hedef kitle**: IT Müdürü, IT Operator, Admin rolü alacak kullanıcı
**Ön koşul**: Personel kurulumu tamamlanmış ve çalışıyor
**Format**: 1 saat teori + 2 saat canlı uygulama + 1 saat quiz + Q&A

---

## Gün Planı

### 0:00-0:15 — Giriş ve Tanışma

- Eğitmen tanıtımı
- Katılımcı tanışma: şu anki rol, Personel ile neden çalışıyor, beklentiler
- Eğitim yapısı ve hedeflerin gözden geçirilmesi
- Ön test (5 dakika, 10 soru — bilgi seviyesi ölçümü için)

---

### 0:15-1:15 — Bölüm 1: Personel Mimarisi ve Temel Kavramlar (60 dk)

**Slayt + Q&A**

#### 0:15-0:35 — Personel nedir ve neden?

- UAM pazarının kısa özeti
- Personel'in 5 değer önerisi (tekrar)
- KVKK-native mimari (ADR 0013 + anahtar hiyerarşisi özet)
- On-prem vs SaaS tartışması
- Rakip ürünlerden temel farklılıklar (1 slayt)

#### 0:35-0:55 — Mimari tur

- 18 container diyagramı (`docs/architecture/c4-container.md`)
- Her bileşenin sorumluluğu (1-2 cümle):
  - Vault — PKI + secret store
  - PostgreSQL — metadata + audit
  - ClickHouse — time-series event store
  - NATS — event bus
  - MinIO — screenshot + blob
  - Keycloak — auth
  - API — admin iş mantığı
  - Gateway — agent ingest
  - Enricher — event zenginleştirme
  - Console — admin UI
  - Portal — çalışan UI

#### 0:55-1:15 — Rol modeli (RBAC)

- 7 rol: Admin, DPO, HR Manager, Manager, IT Manager, IT Operator, Investigator, Auditor
- Her rolün sorumluluk + sınırı
- Rol çakışması (requester ≠ approver dual-control kuralı)
- KVKK m.5 orantılılık prensibinin RBAC'a yansıması

**Break 15 dk**

---

### 1:30-3:30 — Bölüm 2: Canlı Uygulama (120 dk)

**Lab ortamı**: Eğitmen bir pilot VM'ye canlı bağlanır, her katılımcı kendi VM'sinde takip eder.

#### 1:30-1:50 — Lab 1: Console'a Giriş ve Navigasyon

- https://<VM>/console/tr/dashboard
- İlk giriş modalı (KVKK aydınlatma metni onayı)
- Dashboard metrikleri gezintisi
- Ana menü: Endpoints / Policies / DSR / Audit / Live View / Reports / Settings

**Alıştırma**: Her katılımcı kendi admin hesabı ile login olur, dashboard'dan 3 metrik değeri not alır.

#### 1:50-2:20 — Lab 2: Endpoint Yönetimi

- Endpoint listesi + filtreler
- Yeni endpoint enroll token oluşturma
- Windows agent MSI kurulumu + enroll.exe
- Endpoint detay sayfası: daily stats, hourly stats, uygulama listesi, dosya aktivitesi
- Endpoint revoke / deactivate / wipe

**Alıştırma**: Eğitmen'in Windows VM'sine yeni bir endpoint enroll et, 5 dakika sonra dashboard'da görünmesini bekle.

#### 2:20-2:45 — Lab 3: Policy Editörü

- Mevcut politikaların listesi
- Yeni politika oluşturma (hassas dosya dışlama örneği)
- Policy signing (control-plane key ile)
- Policy push (broadcast veya belirli endpoint)
- Policy history + rollback

**Alıştırma**: "banking sitelerini ekran yakalamaktan dışla" politikası oluştur ve push et.

**Break 15 dk**

#### 3:00-3:30 — Lab 4: Audit Log + Raporlar

- Audit log arama (filtre + tam metin)
- Hash-chain doğrulama (DPO rolü ile)
- Rapor sayfaları: Productivity, Top Apps, Idle/Active, Endpoint Activity
- Raporu CSV olarak export
- Trend analizi (hafta/ay)

**Alıştırma**: Son 7 günde en çok kullanılan 5 uygulamayı bul, PDF export al.

---

### 3:30-4:30 — Bölüm 3: Quiz + Q&A (60 dk)

#### 3:30-4:00 — Admin Certification Quiz

20 çoktan seçmeli soru (`docs/training/certification-exam-tr.md`).
Geçme kriteri: 15/20 (75%).

Başaramayanlar için: 1 hafta içinde retake hakkı.

#### 4:00-4:30 — Açık Oturum

- Katılımcıların sorularının yanıtlanması
- Gerçek kullanım senaryolarının tartışılması
- Sonraki adımlar:
  - Admin user manual dağıtımı (`docs/user-manuals/admin-tr.md`)
  - Destek kanalı bilgileri
  - İlk sertifikasyon başarı sertifikası (dijital, imzalı PDF)

---

## Eğitim Öncesi Hazırlık (Eğitmen için)

- [ ] Pilot VM hazır (50 endpoint seed data ile)
- [ ] Her katılımcıya bireysel Keycloak admin user
- [ ] Laptop / projektor / ses sistemi
- [ ] Slayt deck (`docs/training/slides/admin-training.pptx` — TODO)
- [ ] Quiz printouts (20 soru × katılımcı sayısı)
- [ ] Sertifika template'i (imzalanacak)
- [ ] Coffee break + kahve

## Eğitim Sonrası Takip

- 1 hafta sonra follow-up email
- 30 gün sonra kısa anket (eğitimin günlük işlerinde ne kadar yardımcı olduğu)
- 90 gün sonra advanced eğitim daveti (opsiyonel)

## Advanced Admin Training (Ayrı Kurs)

Sonrasında isteyenler için 1 günlük ileri düzey eğitim:
- Vault PKI rotation
- ClickHouse query optimization
- Custom Prometheus alert tanımı
- Policy version control iyi pratikleri
- Troubleshooting runbook'ları

---

## Öğrenme Çıktıları

Katılımcı eğitim sonunda şunları yapabilmeli:

1. ✅ Console'a login olup dashboard'u yorumlayabilir
2. ✅ Yeni endpoint enroll edebilir
3. ✅ Temel bir politika oluşturup push edebilir
4. ✅ Audit log'da arama yapabilir
5. ✅ Standart rapor sayfalarından veri export edebilir
6. ✅ Destek kanalını kullanarak ticket açabilir
7. ✅ 7 rolün RBAC matrisini özetleyebilir
8. ✅ Acil durumda (agent'lar kopmuş) ilk troubleshooting adımlarını uygulayabilir

---

*Güncelleme: eğitim her Personel major release'inden sonra (2 ayda 1) revize edilir.*
