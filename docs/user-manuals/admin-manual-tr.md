# Personel Yönetici Kılavuzu

> Dil: Türkçe. Hedef okuyucu: kurum BT/İK yöneticisi, Personel Admin Console kullanıcısı. Faz 1 MVP kapsamı.
>
> Önemli ön bilgi: `docs/compliance/kvkk-framework.md`, `docs/compliance/aydinlatma-metni-template.md`.

## İçindekiler

1. [İlk Giriş](#1-ilk-giriş)
2. [Ana Panel Turu](#2-ana-panel-turu)
3. [Cihaz Yönetimi](#3-cihaz-yönetimi)
4. [Politika Yönetimi](#4-politika-yönetimi)
5. [Canlı İzleme](#5-canlı-izleme)
6. [DSR İşleme](#6-dsr-kvkk-m11-işleme)
7. [Kullanıcı Yönetimi](#7-kullanıcı-yönetimi)
8. [Ayarlar](#8-ayarlar)
9. [Raporlar](#9-raporlar)
10. [Sorun Giderme](#10-sorun-giderme)

---

## 1. İlk Giriş

1. Kurumunuzun Personel adresine tarayıcıdan gidin (ör. `https://personel.musteri.local`).
2. Karşınıza **Keycloak** kimlik doğrulama sayfası çıkar. Kurum e-posta + şifrenizi girin.
3. İlk giriş yaparken **şifre değiştirme** ekranı gelir. En az 12 karakter, harf + rakam + özel karakter içermelidir.
4. Giriş başarılı olduğunda otomatik olarak **Ana Panel**'e yönlendirilirsiniz (`/tr/dashboard`).

![İlk giriş ekranı](figure: keycloak login — placeholder)

> **Güvenlik notu**: İlk girişinizde KVKK kapsamında **aydınlatma metni** ve kullanım şartlarını kabul etmeniz istenebilir. Bu kabul audit log'a işlenir.

### Rolünüz nedir?

Personel'de 7 rol vardır; size atanan rol, neyi görebileceğinizi belirler:

| Rol | Yetki Özeti |
|---|---|
| **admin** | Tam yönetim — cihaz, politika, kullanıcı, ayar |
| **dpo** | KVKK Veri Sorumlusu — DSR, silme, legal hold, evidence |
| **hr** | İK — canlı izleme onayı, çalışan bilgisi |
| **manager** | Takım yöneticisi — kendi ekibinin raporları |
| **investigator** | Soruşturmacı — canlı izleme talebi, audit log arama |
| **auditor** | Denetçi — read-only audit + evidence |
| **employee** | Çalışan — sadece Şeffaflık Portalı (`/tr/*`) |

Sol menüde göreceğiniz öğeler rolünüze bağlıdır.

---

## 2. Ana Panel Turu

Ana panel (`/tr/dashboard`) sizi aşağıdaki bilgilerle karşılar:

1. **Aktif cihaz sayısı** — son 5 dakikada heartbeat gönderen uç noktalar
2. **Toplam enrolled** — kayıtlı cihaz sayısı
3. **Açık DSR sayısı** — 30 günlük KVKK süresi yaklaşanlar kırmızı
4. **Son 24 saat event hacmi** — ClickHouse'tan anlık
5. **DLP durumu rozeti** — başlıkta her zaman görünür (ADR 0013 uyarınca)
6. **Aktif canlı izleme oturumu** — varsa sayaç
7. **Alert akışı** — son 10 sistem uyarısı

![Dashboard](figure: dashboard-overview — placeholder)

### Sol menü öğeleri

- **Cihazlar** — uç nokta yönetimi
- **Politikalar** — izleme kuralları
- **Canlı İzleme** — HR-onaylı canlı oturumlar
- **DSR / Başvurular** — KVKK m.11 talepleri
- **Denetim Logu** — hash-zincirli kayıt
- **Legal Hold** — soruşturma için veri saklama (DPO)
- **SOC 2 Kanıt Kasası** — evidence coverage (DPO/auditor)
- **Raporlar** — prodüktivite, trend, risk
- **Kullanıcılar** — Keycloak entegrasyonu
- **Ayarlar** — DLP, bildirim, gelişmiş

---

## 3. Cihaz Yönetimi

### 3.1 Cihaz Listesi

**Cihazlar** menüsünde şirketinizdeki tüm enrolled uç noktalar listelenir.

Filtreler:
- Durum (aktif / pasif / revoked / wiped)
- Grup
- Kullanıcı
- Son görülme tarihi

![Cihaz listesi](figure: endpoints-list — placeholder)

### 3.2 Yeni Cihaz Ekleme (Enroll)

1. **Yeni Cihaz Ekle** düğmesine tıklayın.
2. Form doldurun:
   - Asset Tag (şirket envanter numarası, opsiyonel)
   - Kullanıcı (Keycloak'tan seçim)
   - Grup (opsiyonel)
3. **Token Üret** → ekranda bir kez opaque token görünür (24 saat geçerli, tek kullanım).
4. Token'ı kopyalayın ve çalışana güvenli kanalla iletin (Teams/Outlook ile değil — kurum parola yöneticisi ideal).
5. Çalışan laptop'unda `enroll.exe --token <TOKEN>` çalıştırır.
6. Enroll tamamlanınca cihaz listede "Aktif" olarak görünür.

### 3.3 Cihaz Detay Sayfası

Cihaza tıklayınca detay sayfası açılır:

- **Genel bilgi**: hostname, OS, ajan versiyonu, enrollment tarihi, son aktivite
- **Son aktivite grafiği**: 24h event akışı
- **Politika**: atanan politika + son push zamanı
- **Ekran görüntüleri**: son 10 (yetkiliyse; saklama 30 gün)
- **Uyarılar**: tamper tespiti, politika ihlali
- **Komutlar**: wipe, deactivate, cert refresh

### 3.4 Uzaktan Wipe (KVKK m.7)

Wipe komutu, uç noktadaki ajan kuyruğunu ve config'i **siler**. Cihaz üzerindeki kullanıcı verilerine dokunmaz (Faz 1 kapsamında sadece Personel ajanını kaldırır).

Kullanım senaryoları:
- Çalışan ayrıldı
- Cihaz çalındı / kayboldu
- DSR erasure talebi onaylandı
- Cihaz tehlikeye girdi (compromised)

Wipe için:

1. Cihaz detay → **Komutlar** → **Wipe**
2. Gerekçe formunu doldurun:
   - Sebep kodu (seçimli: çalındı / kaybedildi / ayrılma / tehlike / DSR silme)
   - Ticket ID
   - Açıklama (min. 20 karakter)
3. Onayla → komut kuyruğa atılır, ajan bir sonraki heartbeat'te alır
4. **Audit log'a** işlenir; DPO bildirim alır.

![Wipe komutu](figure: wipe-dialog — placeholder)

### 3.5 Toplu İşlem (Bulk)

Çoklu seçim ile 500 uç noktaya kadar aynı anda işlem:
- Bulk enroll (csv import)
- Bulk deactivate
- Bulk policy re-push

**Cihazlar** → liste başı **Toplu İşlem** düğmesi.

---

## 4. Politika Yönetimi

### 4.1 Politika Listesi

**Politikalar** menüsünde tenant'ınıza ait politikalar listelenir. Her politika versiyonludur (append-only).

### 4.2 Visual Policy Editor

**Yeni Politika** → adım adım sihirbaz:

1. **Genel**: ad, açıklama, atama kapsamı (tüm cihazlar / grup / etiket)
2. **Ekran görüntüsü**:
   - Yakalama sıklığı (30 sn - 10 dk)
   - Dışlama listesi (özel bankacılık uygulamaları, 1Password vb.)
   - Çoklu monitör davranışı
3. **Klavye**:
   - İstatistik toplama (açık / kapalı)
   - İçerik toplama — **UYARI**: ADR 0013 uyarınca sadece DLP opt-in seremonisi tamamlandıysa bu seçenek aktiftir
4. **USB**:
   - Kitle depolama bloklaması (allow / block / read-only)
   - VID/PID allow-list
5. **Uygulama bloklaması**:
   - Process ad desenleri (regex veya glob)
6. **Web bloklaması**:
   - Domain listesi (tam metin veya regex)
7. **Hassas pencere tespiti (SensitivityGuard)**:
   - KVKK m.6 özel nitelikli alanlar (sağlık, sendika, siyasi)
   - Otomatik DLP match → kısa TTL bucket

![Politika editörü](figure: policy-editor — placeholder)

### 4.3 Politika İmzalama ve Push

**Kaydet** → politika imzalanır (control-plane Ed25519 key) → NATS üstünden atanmış cihazlara dağıtılır. Cihazlar imzayı doğrulamadan yeni politikayı kabul etmez.

Her push işlemi **SOC 2 CC8.1** evidence olarak kaydedilir.

### 4.4 Politika Versiyonlama

Yeni push, eski politika versiyonunu geçersiz kılar ama audit log'da tüm eski sürümler erişilebilirdir.

---

## 5. Canlı İzleme

Canlı izleme, kurumsal güven açısından en hassas özelliktir. Personel bunu **üç kapı** ile korur:

### 5.1 Talep Akışı (Requester — genelde investigator)

1. **Canlı İzleme** → **Yeni Talep**
2. Form:
   - Hedef uç nokta
   - Sebep kodu (dropdown: incident, policy violation, DSR, legal hold)
   - Gerekçe (min 30 karakter — audit log'a girer)
   - Süre (60 sn - 60 dk)
3. **Gönder** → talep **beklemede** durumuna girer, İK rolündeki bir kişinin onayını bekler.

### 5.2 Onay Akışı (HR)

**Canlı İzleme** → **Beklemedeki Talepler** → talep seç → incele → onayla / reddet.

> **Çift onay kuralı (kriptografik)**: Onaylayan kişi talep sahibi ile aynı olamaz. Sistem bunu **kriptografik olarak** enforce eder (409 Conflict).

Onaylanan talep için LiveKit token üretilir (kısa süreli) ve requester'a bildirim gider.

![Onay bekliyor](figure: liveview-approve — placeholder)

### 5.3 Aktif Oturum

Requester, onay aldıktan sonra **Başlat** → LiveKit odasına girer → canlı ekran izlenir.

- Süre sayacı görünür; süre dolduğunda oturum otomatik sonlanır
- Her ekran kare'i **audit log**'a meta olarak yazılır
- Oturum kaydı **yoktur** (Faz 1 — recording ADR 0019 Faz 2)
- Sonlandırma → **evidence locker** kaydı (CC6.1)

### 5.4 Çalışan Bilgilendirmesi

Çalışan, kendi Şeffaflık Portalı'nda **tamamlanan** canlı oturumları geriye dönük görebilir (7 gün gecikme ile). KVKK m.10 aydınlatma kapsamında ilk kurulumda genel bilgilendirme yapılmıştır.

---

## 6. DSR (KVKK m.11) İşleme

### 6.1 DSR Kuyruğu

**DSR / Başvurular** → tüm açık talepler listelenir. Kırmızı rozet: 30 günlük yasal süresi < 3 gün.

Her DSR:
- Başvuru tipi (erişim, silme, düzeltme, itiraz, taşınabilirlik)
- Başvuru sahibi (email)
- Durum (yeni / işlemde / tamamlandı / reddedildi / süresi geçti)
- SLA deadline

### 6.2 Erişim Talebini Karşılama (m.11/b-c-d)

1. DSR detay → **Erişim Sağla**
2. Sistem otomatik olarak şu kategorilerde export hazırlar:
   - Çalışana ait event kategorileri
   - Son 6 ay ekran görüntüsü listesi (gerçek görüntüler değil — meta)
   - Politika uygulama kayıtları
3. PDF + JSON + CSV export üretilir
4. **İmzalı presigned URL** (24 saat geçerli) çalışana e-posta ile gönderilir
5. Audit log'a tam rapor — SLA içinde mi? Süre kaldı mı? Evidence locker'a P7.1 kaydı

![DSR erişim](figure: dsr-access-fulfill — placeholder)

### 6.3 Silme Talebini Karşılama (m.11/e-f, m.7)

1. DSR detay → **Silme Uygula**
2. Kapsam seç:
   - **Tam silme**: tüm kişisel veriler
   - **Kısmi silme**: legal hold veya yasal saklama muafiyeti uygulanan veriler hariç
3. Sistem **kripto-shred** uygular:
   - Şifreli blob'ların DEK'leri Vault'tan iptal edilir
   - Postgres kayıtları soft-delete + 30 gün sonra hard-delete
   - ClickHouse TTL force mutation
   - OpenSearch indices delete-by-query
4. DPO onay altında tamamlanır; çalışana tamamlandı bildirimi

### 6.4 DSR Ret

Nadiren meşru menfaat veya yasal saklama gerekçesi ile ret. Gerekçe **detaylı** yazılır (≥50 karakter), audit log'a girer, çalışan itiraz hakkını Kurul'a taşıyabilir.

---

## 7. Kullanıcı Yönetimi

**Kullanıcılar** ekranı Keycloak'tan live veri çeker.

- Kullanıcı ekle / düzenle / pasifleştir
- Rol atama (admin/dpo/hr/manager/investigator/auditor/employee)
- Tenant attribute set (multi-tenant için)
- LDAP federation durumu (varsa)

Kullanıcı oluşturma / silme → otomatik audit log + SOC 2 CC6.3 (access review) için evidence.

> **KVKK notu**: Admin rolü **ikili** olmamalıdır — her adminin yaptığı iş audit log'a düşer; dolayısıyla kişisel hesap ile giriş şart. Ortak "admin" hesabı KVKK m.12 ihlalidir.

---

## 8. Ayarlar

### 8.1 DLP Durum

**Ayarlar → DLP Durumu** sayfası, klavye içerik DLP'sinin **aktif/pasif** olduğunu gösterir.

- **Varsayılan**: KAPALI (ADR 0013)
- **Etkinleştirme**: Bu sayfada **düğme yoktur**. Etkinleştirme **opt-in seremonisi** gerektirir:
  1. DPO tarafından DPIA güncellemesi
  2. İmzalı opt-in formu (DPO + IT güvenlik + Hukuk)
  3. Operatör tarafından `infra/scripts/dlp-enable.sh` çalıştırılır
  4. Vault Secret ID issue edilir
  5. DLP servisi başlatılır
  6. Şeffaflık Portalı'na banner eklenir

Detay: `docs/compliance/dlp-opt-in-form.md`.

### 8.2 Diğer Ayarlar

- **Bildirim**: e-posta / Slack webhook / Teams webhook
- **Retention politikası**: varsayılan TTL'ler (salt-okunur; değişiklik DPO onayı ister)
- **Saat dilimi**: TRT UTC+3
- **Dil**: TR / EN

---

## 9. Raporlar

### 9.1 Hazır Raporlar

**Raporlar** menüsü altında:

1. **En Çok Kullanılan Uygulamalar** — 30 günlük top 20
2. **Boşta / Aktif Zaman** — çalışan bazlı grafik
3. **Prodüktivite Skoru** — kategorize uygulamalara göre
4. **USB / Yazıcı İstatistikleri**
5. **Politika İhlali Trendi** — haftalık
6. **Risk Skoru (UBA)** — insider threat tespit (Faz 2)

### 9.2 Özel Rapor

**Raporlar** → **Özel Sorgu** — tarih aralığı + filtre + metrik seçimi. Sonuç PDF veya Excel export'lanır.

### 9.3 Rapor Export'u

Her rapor **PDF** veya **Excel** olarak export edilebilir. Export işlemi audit log'a düşer.

---

## 10. Sorun Giderme

### 10.1 Giriş yapamıyorum

- Şifrenizi sıfırlatmak için DPO'ya başvurun
- Keycloak cert'i tarayıcınızda güvenilir mi?
- Cookie blocker eklentisini devre dışı bırakın

### 10.2 Cihaz enroll olmuyor

- Token 24 saat içinde kullanılmadıysa süresi geçmiştir
- Ajan log'una bakın: `C:\ProgramData\Personel\agent\logs\agent.log`
- `docs/operations/troubleshooting.md` §13

### 10.3 Canlı izleme onay düğmesi aktif değil

- Rolünüz `hr` mi? Kendi talep ettiğiniz kayıtları onaylayamazsınız (409 Conflict — bu kasıtlı kuraldır)

### 10.4 DSR export 413 hatası

- Çok büyük dataset → export_format=chunked kullanın, veya DPO'ya başvurun

### 10.5 "Insufficient role" uyarısı

- Rolünüzü admin panelinden kontrol ettirin
- Keycloak oturumu eski olabilir → çıkış yap, yeniden giriş yap

### 10.6 Rapor açılmıyor

- Tarih aralığı çok geniş olabilir (> 90 gün) — daralt
- Stack overload → birkaç dakika bekle, tekrar dene

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #161 — İlk sürüm (TR) |
