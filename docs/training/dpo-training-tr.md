# Personel — DPO Eğitimi (KVKK-Özel, 4 saat, TR)

Bu doküman, Personel Platform'u kullanacak müşteri kurumun Veri Koruma
Görevlisi (DPO / KVKK Sorumlusu) için 4 saatlik özel eğitim programını
tanımlar.

**Hedef kitle**: DPO, KVKK Sorumlusu, Hukuk Müşaviri, Compliance Officer
**Ön koşul**: KVKK 6698 temel bilgisi
**Format**: 2 saat teori + 1 saat canlı uygulama + 1 saat Q&A + senaryolar

---

## Program

### 0:00-0:15 — Giriş

- Tanışma, DPO rolünün Personel'deki özel yeri
- Neden ayrı DPO eğitimi? (Admin'den farklı yetkiler + KVKK'ya özel workflow'lar)
- Gündem

---

### 0:15-1:15 — Bölüm 1: Personel'in KVKK Framework'ü (60 dk)

#### 0:15-0:40 — KVKK Framework Recap

- KVKK 6698 temel maddeler:
  - m.5 — Genel ilkeler (meşru amaç, orantılılık, gereklilik)
  - m.6 — Özel nitelikli kişisel veri
  - m.10 — Aydınlatma yükümlülüğü
  - m.11 — İlgili kişi hakları (DSR)
  - m.12 — Güvenlik tedbirleri
  - m.13-16 — Veri sorumlusuna başvuru
- UAM aracının DPO sorumluluğuna getirdiği ek yükümlülükler
- 2019/144 Kurul kararı (DPIA zorunluluğu)
- VERBİS kayıt süreci
- Cezai düzenlemeler (m.18 idari para cezaları)

#### 0:40-1:00 — Personel'in mimari yaklaşımı

- **Veri minimizasyonu**: Varsayılan ayarların ne topladığı ve ne toplamadığı
- **ADR 0013**: Klavye içeriği kapalı, opt-in töreni zorunlu
- **Saklama matrisi**: Her veri kategorisi × süre × silme mekanizması
- **Hash-zincirli audit**: Tamper-proof kanıt
- **Şeffaflık Portalı**: Çalışan self-service KVKK m.11
- **DSR SLA**: 30 gün otomatik takip

#### 1:00-1:15 — DPO'nun Personel üzerindeki yetkileri

- DPO rolü RBAC matrisinde ne yapabilir:
  - Tüm audit log'u okuyabilir
  - Legal hold yerleştirebilir (tek başına yetkili)
  - DSR başvurularını yönetebilir (assign, respond, reject, extend)
  - Destruction raporu üretebilir
  - Live view'ı sonlandırabilir (KVKK kapsam ihlali şüphesi)
  - Screenshot'ları görebilir (Investigator ile aynı)
  - Keystroke içeriğini **GÖREMEZ** (ADR 0013 ile kimse göremez)

**Break 15 dk**

---

### 1:30-2:30 — Bölüm 2: DSR Workflow Eğitimi (60 dk)

#### 1:30-1:50 — DSR Nedir, Nasıl Çalışır?

- KVKK m.11 hakları:
  - m.11/a — Bilgi alma hakkı (info request)
  - m.11/b — Veri erişim hakkı (data access — en yaygın)
  - m.11/c — Düzeltme hakkı (rectification)
  - m.11/d — Silme hakkı (erasure)
  - m.11/e — Yok etme hakkı
  - m.11/f — Kopya alma hakkı (data portability)
  - m.11/g — Otomatik karar itiraz hakkı
  - m.11/h — Zarar tazmin hakkı
- 30 gün SLA kuralı
- Ücretsizlik prensibi (m.13)

#### 1:50-2:30 — Canlı Lab: Pilot sisteminde DSR akışı

Eğitmen canlı, katılımcı kendi ekranında takip eder.

**Senaryo**: Bir çalışan portal üzerinden "benimle ilgili tüm verilerin
kopyasını istiyorum" başvurusunda bulundu.

1. DPO console'a login
2. `/tr/dsr` sayfasına git
3. Bekleyen başvurular listesi
4. Yeni DSR'yi "Bana Ata" (assign self)
5. Başvuru detayı inceleme
6. Kimlik doğrulama (portal'den geldi, Keycloak ile onaylandı → otomatik geçer)
7. "Fulfill" butonu ile başlat
8. Fulfillment service otomatik:
   - User verilerini toplar (users tablosu, endpoint metadata, aktivite özetleri)
   - Ekran görüntülerini signed URL listesi olarak hazırlar
   - Audit log'un ilgili kişi parçasını extract eder
   - ZIP'e imzalar (Vault Ed25519)
   - MinIO'ya yükler ve başvuruya ekler
9. "Response Sent" state'ine geçer
10. Çalışan portal'da dosyayı indirebilir
11. 30 gün SLA sayacı kapanır

**Alıştırma**: Her katılımcı kendi test DSR'sini işler.

---

### 2:30-3:30 — Bölüm 3: Özel Senaryolar (60 dk)

#### 2:30-2:45 — Senaryo 1: KVKK Kurulu denetimi geldi

- Kurul müfettiş çağrısı geldi, 30 dakika sonra gelecek
- Sıfırdan inspection-ready runbook'u (`infra/runbooks/inspection-ready.md`)
- Hazırlanacak kanıt dosyaları:
  1. Audit log export (son 6 ay)
  2. Saklama matrisi enforcement raporu
  3. DSR işlem kayıtları
  4. Politika imza zinciri
  5. Aydınlatma metni onay kanıtları
  6. DPIA güncel versiyonu
  7. Son 3 destruction raporu (signed PDF)
- Konsol üzerinde tek tıkla "Inspection Pack" üretme (Faz 15 roadmap)

#### 2:45-3:00 — Senaryo 2: Çalışan sildirme talebi

- Kullanıcı m.11/d ile "verilerimi silin" diyor
- Hukuki değerlendirme: Silme hakkı mı yoksa işlem kısıtlama mı?
- Crypto-erase akışı:
  1. DSR "erasure" tipinde açılır
  2. Fulfillment service user PE-DEK'ini Vault'tan siler
  3. ClickHouse / MinIO / Postgres'teki tüm user verisi ciphertext olarak kalır, ama matematiksel olarak kurtarılamaz
  4. users tablosundaki row `pii_erased=true` bayrağı konur
  5. Audit log'da FK referansları korunur
  6. Destruction report signed PDF olarak üretilir
- Geri alınamaz — uyarılır

#### 3:00-3:15 — Senaryo 3: Canlı izleme talebi reddi

- IT Operator bir çalışanı canlı izlemek istiyor
- IT Manager onayı gerekli, ama çalışanın işlevi KVKK kapsamında olabilir
- DPO'nun rolü:
  - Canlı izleme geçmişini denetlemek
  - Kapsam ihlali şüphesi varsa oturumu sonlandırmak
  - Sonlandırma audit event'ini değerlendirmek
- DPO override akışı:
  1. `/tr/live-view/sessions` → aktif oturum
  2. "Terminate (KVKK Override)" butonu
  3. Sebep kodu: "scope_violation"
  4. Sessiyon anında sonlanır, requester + approver'a otomatik email

#### 3:15-3:30 — Senaryo 4: VERBİS güncellemesi

- VERBİS yılda 2 kez güncelleme yükümlülüğü
- Personel /v1/kvkk/verbis-export endpoint'i ile otomatik export
- DPO export'u alır, VERBİS portal'ına yükler
- Demo: Console → Settings → KVKK → VERBİS Export butonu

---

### 3:30-4:00 — Q&A (30 dk)

Açık oturum — katılımcıların kendi kurumsal senaryoları tartışılır.

---

## Öğrenme Çıktıları

DPO eğitim sonunda:

1. ✅ Personel'in KVKK framework'ünü mimari düzeyde açıklayabilir
2. ✅ DSR başvurusunu uçtan uca işleyebilir
3. ✅ Legal hold yerleştirebilir ve kaldırabilir
4. ✅ Destruction raporu üretebilir ve imzalı PDF'i anlayabilir
5. ✅ KVKK Kurulu denetimine hazırlanabilir (runbook'u takip ederek)
6. ✅ Canlı izleme DPO override yetkisini kullanabilir
7. ✅ VERBİS güncellemesi için export alabilir
8. ✅ ADR 0013 DLP off-by-default garantisini çalışanlara + yönetime açıklayabilir
9. ✅ Hash-zincirli audit'in tamper-proof özelliğini doğrulayabilir
10. ✅ Çalışan Şeffaflık Portalı'nın içeriğini (hangi veri, hangi amaç) bilir

---

## Eğitim Materyalleri

- Slayt deck (TODO: `docs/training/slides/dpo-training.pptx`)
- KVKK Framework referansı (`docs/compliance/kvkk-framework.md`)
- Aydınlatma metni template (`docs/compliance/aydinlatma-metni-template.md`)
- DPIA şablonu (`docs/compliance/dpia-sablonu.md`)
- VERBİS rehberi (`docs/compliance/verbis-kayit-rehberi.md`)
- Hukuki risk register (`docs/compliance/hukuki-riskler-ve-azaltimlar.md`)
- Inspection runbook (`infra/runbooks/inspection-ready.md`)

---

## Sertifikasyon

Eğitim sonunda DPO'ya **"Personel Certified DPO Operator"** sertifikası verilir.
Dijital imzalı PDF (yıllık yenileme).

---

*Güncelleme: KVKK mevzuatı veya Kurul kararları değiştiğinde revize edilir.*
