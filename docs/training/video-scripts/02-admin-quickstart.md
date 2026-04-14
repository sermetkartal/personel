# Video Script 02 — Admin Quickstart (10 dakika)

**Hedef kitle**: Yeni Personel Admin kullanıcısı
**Hedef**: Console'a ilk girişten ilk rapora kadar adım adım rehber
**Platform**: YouTube (unlisted) + internal training portal
**Dil**: Türkçe anlatım + Türkçe hard-coded altyazı
**Şekil**: Screen-capture dominant, minimum talking head (sadece intro/outro)

---

## Sahne 1 — Açılış (0:00-0:30)

**Görsel**: Personel logo + "Admin Quickstart"

**Ses**:
> "Merhaba, bu video Personel Platform admin rolüne yeni atanan kullanıcılar için
> hazırlandı. 10 dakika içinde Console'a ilk girişten, yeni bir endpoint enroll
> etmeye, ilk politikayı push etmeye ve ilk raporu almaya kadar tüm temel
> işlemleri yapacaksınız. Hazırsanız başlayalım."

---

## Sahne 2 — Console'a Giriş (0:30-1:30)

**Görsel**:
- Tarayıcı: https://vm.pilot/console
- Keycloak login ekranı
- admin / ilk şifre gir → zorunlu şifre değiştirme → yeni şifre → dashboard

**Ses**:
> "Personel Console'a URL'den erişiyoruz. Keycloak üzerinden SSO ile giriş
> yapılır. İlk girişte zorunlu şifre değişimi yapılır — lütfen güçlü bir şifre
> seçin. Ardından KVKK aydınlatma metni modal'ı karşınıza çıkar. Okuduğunuzu
> onaylamadan devam edemezsiniz. Bu bir KVKK m.10 yükümlülüğüdür."

---

## Sahne 3 — Dashboard Navigasyonu (1:30-2:30)

**Görsel**:
- Dashboard ana kartları (endpoint count, event rate, DSR pending, silence count)
- Ana menü tanıtımı: Endpoints / Policies / DSR / Audit / Live View / Reports / Settings

**Ses**:
> "Dashboard sizi dört ana KPI ile karşılıyor: online endpoint sayısı, olay akış
> hızı, bekleyen DSR sayısı ve son 24 saat sessiz endpoint uyarıları. Sol menüden
> 7 ana modüle erişebilirsiniz."

---

## Sahne 4 — Endpoint Enroll (2:30-4:30)

**Görsel**:
- Endpoints sayfası → "Yeni Endpoint" → "Token Oluştur"
- Token kopyalanır
- Ekran geçişi: Windows VM
- PowerShell: `enroll.exe --token "..."`
- 5 dakika bekleme → Console'a geri dön → endpoint listede görünür

**Ses**:
> "Yeni bir Windows cihazını Personel'e kaydetmek için önce Console'dan bir
> enroll token üretiyoruz. Bu token, tek kullanımlık ve belirli bir süre
> geçerlidir. Token'ı kopyalayıp, Windows agent'ın kurulu olduğu cihaza
> geçiyoruz. PowerShell'i yönetici modunda açıp enroll.exe'yi token ile
> çalıştırıyoruz. Agent otomatik olarak service olarak register olur,
> Vault PKI'dan client cert alır ve gateway'e bağlanır. Yaklaşık 1-2 dakika
> sonra Console'da endpoint'i görmeye başlarsınız."

---

## Sahne 5 — Endpoint Detay (4:30-5:30)

**Görsel**:
- Endpoint listesinde yeni endpoint'e tıkla
- Detay sayfası: günlük metrikler, saatlik bar chart, top apps, recent files
- Agent version + last seen + policy version

**Ses**:
> "Endpoint detay sayfası, seçilen cihazın tüm metriklerini tek bakışta
> gösterir. Günlük aktif süre, idle yüzdesi, en çok kullanılan 10 uygulama,
> son erişilen dosyalar, agent versiyonu, uygulanan politika. Bu sayfa
> managerlar için performans değerlendirme, IT için troubleshooting noktası."

---

## Sahne 6 — Policy Editör (5:30-7:00)

**Görsel**:
- Policies sayfası → "Yeni Politika"
- SensitivityGuard editörü: hassas dosya dışlama kuralı ekle
- "Banking applications" → screen capture exclude
- Kaydet → "Signed Push" butonu → broadcast

**Ses**:
> "Şimdi ilk politikamızı yazalım. Policies sayfasında 'Yeni Politika' diyoruz.
> SensitivityGuard editör açılıyor. Burası çok önemli — Personel'in KVKK orantılılık
> prensibine uyması için, hassas uygulamaların ekran görüntüleri asla alınmaz.
> Banka uygulamaları, parola yöneticileri, private browsing modu — hepsi default
> olarak dışlanır. Kendi kurumunuzun ek kurallarını da burada tanımlayabilirsiniz.
> Politika kaydedildiğinde control-plane Ed25519 key ile imzalanır ve tüm
> endpoint'lere broadcast edilir. Agent'lar politikayı aldıktan sonra anında
> uygular."

---

## Sahne 7 — Audit Log (7:00-8:00)

**Görsel**:
- Audit sayfası → "Son 24 saat" filtresi
- policy.push event görünür
- Entry detayı: actor, timestamp, policy_version, hash_chain

**Ses**:
> "Az önce push ettiğimiz politika audit log'a yazıldı. Audit log hash-zincirli
> bir yapıdır — her entry önceki entry'nin hash'iyle bağlanır. Bu da tampering'i
> matematiksel olarak tespit edilebilir kılar. Günlük checkpoint'ler Vault
> Ed25519 key ile imzalanır ve MinIO Object Lock WORM bucket'a yazılır.
> Superuser DBA bile trigger kapatsa bile bir sonraki günlük checkpoint'te
> tespit edilir."

---

## Sahne 8 — Reports (8:00-9:00)

**Görsel**:
- Reports sayfası → Productivity raporu
- Hafta/ay filtresi
- Grafik: line chart + top apps bar chart
- Export CSV / PDF

**Ses**:
> "Reports menüsünden standart raporlara erişebilirsiniz. Productivity, Top Apps,
> Idle/Active, Endpoint Activity, App Blocks. Her raporu CSV veya PDF olarak
> export edebilirsiniz. ClickHouse destekli olduğu için büyük veri kümelerinde
> bile p95 sorgu süresi 1 saniyenin altındadır."

---

## Sahne 9 — Kapanış (9:00-10:00)

**Görsel**:
- Personel logo
- "Ne öğrendiniz" özeti (bullet list)
- "Sonraki videolar" QR
- iletisim: destek@personel.local

**Ses**:
> "Bu 10 dakikalık videoda Personel Console'a giriş yapmayı, yeni endpoint
> enroll etmeyi, policy yazıp push etmeyi, audit log'u okumaya ve rapor
> almayı öğrendiniz. Bu 5 temel beceri ile günlük admin işlerinizin büyük
> çoğunluğunu yapabilirsiniz. Daha derinlemesine konular için DPO eğitim
> videomuza ve operatör eğitim videomuza göz atabilirsiniz. Sorularınız için
> destek@personel.local adresine yazın. Teşekkürler ve iyi kullanımlar."

---

## Prodüksiyon Notları

- **Toplam süre**: 10:00 dakika
- **Ekran kaydı**: 1920x1080, 30fps, cursor visible + click highlight
- **Zoom efekti**: Kritik anlarda (buton tıklaması) 1.2x zoom
- **Tempo**: Orta, kullanıcının takip edebileceği hızda
- **Ses**: Rehberlik eden, dostça, jargon minimum
- **Müzik**: Hafif ambient (yalnızca açılış/kapanış)
