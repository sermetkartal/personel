# Video Script 03 — KVKK DPO Walkthrough (15 dakika)

**Hedef kitle**: Veri Koruma Görevlisi (DPO), Hukuk Müşaviri, Compliance Officer
**Hedef**: KVKK workflow'ları Personel üzerinde uçtan uca göstermek
**Platform**: Internal training + müşteri portal (gated erişim)
**Dil**: Türkçe (hukuki terminoloji korunmuş)

---

## Bölüm 1 — Giriş (0:00-1:00)

**Ses**:
> "Merhaba, ben Personel ekibinin DPO Training Lead'iyim. Bu 15 dakikalık
> videoda, kurumunuzda Personel devreye girdikten sonra DPO olarak yapacağınız
> tüm temel KVKK işlemlerini uçtan uca göstereceğim. Gördüklerinizin hepsi
> pilot ortamında canlı çalışmaktadır."

---

## Bölüm 2 — DPO Dashboard Tanıtımı (1:00-2:30)

**Görsel**:
- DPO olarak Console'a login
- Ana dashboard DPO-özel kartları:
  - Bekleyen DSR'lar (SLA geri sayacı)
  - Aktif legal hold'lar
  - Son 24 saat audit anomalies
  - DLP state (aktif/pasif)
  - SOC 2 evidence coverage matrix

**Ses**:
> "DPO rolü ile login olduğunuzda Admin ile farklı bir dashboard görürsünüz.
> Öne çıkan KPI'lar: bekleyen DSR başvuruları ve SLA geri sayaçları, aktif
> legal hold'lar, son 24 saat audit log anomali uyarıları, DLP etkinleştirme
> durumu ve SOC 2 evidence coverage matrix'i. Bu dashboard sizin KVKK komuta
> merkezinizdir."

---

## Bölüm 3 — DSR Workflow (2:30-6:00)

**Görsel**:
- `/tr/dsr` sayfası
- Bir çalışandan gelen m.11/b "Verilerimin kopyasını istiyorum" başvurusu
- "Bana Ata" → başvuru detay sayfası
- Kimlik doğrulama (Keycloak üzerinden geldi → otomatik onay)
- "Fulfill" → fulfillment service çalışıyor (loading)
- ZIP export hazır → checksumv+ signed
- "Yanıt Gönder"
- Başvuru state "Response Sent" → geri sayaç durur

**Ses**:
> "Çalışan portal üzerinden bir m.11 başvurusu gönderdi. Başvurular listesinde
> onu görüyoruz. 'Bana Ata' diyoruz — ben artık bu başvurunun sorumlusuyum.
> Detayı açıyorum. Başvuru tipi 'veri erişim hakkı' — yani çalışan kendi
> verisinin bir kopyasını istiyor. Kimlik doğrulaması portal üzerinden geldiği
> için Keycloak ile otomatik doğrulandı. Şimdi 'Fulfill' butonuna basıyorum.
>
> Arka planda Fulfillment Service başlatılıyor. Bu servis çalışanın users
> tablosundaki kayıtlarını, endpoint metadata'sını, aktivite özetlerini, ekran
> görüntü referanslarını ve audit log'un ilgili kişi ile ilgili parçasını
> topluyor. Hepsini bir ZIP dosyasına paketliyor, Vault Ed25519 key ile
> imzalıyor ve MinIO'ya yüklüyor. Yaklaşık 10-60 saniye sürer.
>
> İşte. Export hazır. Dosya boyutu 2.3 MB. SHA-256 checksum görünüyor. 'Yanıt
> Gönder' diyorum. Başvuru state 'Response Sent' oldu. SLA geri sayacı durdu.
> Çalışan portal'da dosyayı indirebilir. 30 gün içinde ücretsiz ve tam
> olarak yanıtlanmış oldu."

---

## Bölüm 4 — Legal Hold (6:00-8:00)

**Görsel**:
- `/tr/legal-hold` sayfası
- Yeni legal hold: hukuki dava kapsamında bir çalışanın verilerini koru
- Scope: user_id, tarih aralığı, sebep
- "Place Hold" → audit event
- Hold aktifken saklama matrisi bypass olur
- Hold'u release etmek → ikinci DPO onayı gerekir

**Ses**:
> "Bazen bir hukuki dava, iç soruşturma veya resmi talep nedeniyle belirli
> bir çalışanın verilerinin normal saklama süresi dolsa bile silinmemesi
> gerekir. Bunu 'Legal Hold' ile sağlıyoruz. Sadece DPO rolü bu yetkiye
> sahiptir — Admin bile yerleştiremez.
>
> Yeni bir hold oluşturuyorum. Kapsam olarak belirli bir çalışanı ve tarih
> aralığını seçiyorum. Sebebini yazıyorum — bu alan bir audit gerektiği için
> zorunlu. 'Place Hold' diyorum. Hold artık aktif. Bu çalışanın verileri
> saklama matrisinin yaş-bazlı silme kurallarından bypass olur.
>
> Hold'u kaldırmak da tek tıkla olmaz — ikinci bir DPO onayı gerekir (dual
> control). Bu, hold'un kasıtlı kaldırılmasını engelleyen bir güvencedir."

---

## Bölüm 5 — Destruction Report (8:00-9:30)

**Görsel**:
- `/tr/destruction-reports` sayfası
- Otomatik üretilen 6-ay periyotlu raporlar
- "Generate Now" butonu (manuel trigger)
- Rapor açılır: o dönemde silinen veri özet + hash chain doğrulaması + imzalar

**Ses**:
> "KVKK saklama matrisine göre, süresi dolan verileri otomatik olarak silen
> bir iç işlemimiz var. Her 6 ayda bir bu işlemler için bir 'Destruction
> Report' üretilir. Bu rapor: hangi veri kategorisinin silindiğini, kaç kayıt
> etkilendiğini, hangi dönem kapsamında olduğunu ve bunların kriptografik
> kanıtını içerir.
>
> Rapor Vault ile imzalanır ve MinIO WORM bucket'ına yazılır. KVKK denetimi
> geldiğinde bu raporlar 'biz saklama matrisine uyuyoruz' kanıtınızdır.
> Kurul müfettiş bu signed PDF'i alıp kendi imza doğrulama araçlarıyla
> kontrol edebilir."

---

## Bölüm 6 — Canlı İzleme DPO Override (9:30-11:00)

**Görsel**:
- `/tr/live-view/sessions` → 1 aktif oturum
- Oturum detay: kim izliyor, kimi, neden, süre
- "Terminate (KVKK Override)" butonu
- Sebep kodu seçimi: scope_violation
- Oturum anında sonlanır

**Ses**:
> "Canlı izleme Personel'de IT Operator'ın talep ettiği, IT Manager'ın onayladığı
> bir dual-control işlemdir. DPO normal akışın bir parçası değildir. Ancak
> istisnai bir yetkiniz var: KVKK kapsamını aşan bir canlı izleme gördüğünüzde,
> oturumu anında sonlandırabilirsiniz.
>
> Örneğin bir IT Operator'ın 'sistem bakımı' bahanesiyle HR verilerine bakan
> bir canlı izleme oturumunu durdurmak için. 'Terminate (KVKK Override)' butonuna
> basıyorum. Sebep kodu seçiyorum: 'scope_violation'. Oturum anında sonlanır.
> Requester ve approver'a email gider. Audit log'da bu override kaydedilir.
> Bu sizin DPO olarak en güçlü araçlarınızdan biridir — kötüye kullanıma karşı
> son savunma."

---

## Bölüm 7 — VERBİS Export (11:00-12:00)

**Görsel**:
- Settings → KVKK → VERBİS Export
- Tek butonla JSON + PDF üretim
- VERBİS portalına yüklemek için hazır format

**Ses**:
> "VERBİS'e yılda 2 kez güncelleme göndermek zorundayız. Personel bunu
> otomatik hale getirdi. Settings → KVKK → VERBİS Export butonuna basıyorum.
> Sistem otomatik olarak veri işleme envanterini, veri kategorilerini, saklama
> sürelerini, işleme amaçlarını ve alıcı gruplarını çıkarır. Hem JSON hem PDF
> formatında. VERBİS portalına yüklemek için hazır."

---

## Bölüm 8 — Inspection Ready (12:00-13:30)

**Görsel**:
- "Inspection Pack" butonu (hipotetik — Faz 15)
- Hazırlanan dosyalar listesi:
  - Audit log export (son 6 ay)
  - Retention enforcement raporu
  - DSR işlem kayıtları (son 12 ay)
  - Policy imza zinciri
  - Aydınlatma onay kanıtları
  - DPIA güncel
  - Son 3 destruction report

**Ses**:
> "KVKK Kurulu'ndan müfettiş geldiği senaryoyu düşünün. Personel size bir
> 'Inspection Ready Pack' hazırlar. Tek tıkla, denetim için gerekli tüm
> belgeleri bir ZIP'e toplar: son 6 ay audit log'u, retention enforcement
> raporu, tüm DSR işlem kayıtları, politika imza zinciri, çalışan aydınlatma
> kanıtları, DPIA dokümanı, son 3 destruction raporu. Hepsi imzalı ve
> tamper-proof. Müfettişi 30 dakika içinde karşılamaya hazırsınız."

---

## Bölüm 9 — Kapanış (13:30-15:00)

**Görsel**:
- Özet slayt:
  - DSR workflow ✓
  - Legal hold ✓
  - Destruction report ✓
  - Canlı izleme DPO override ✓
  - VERBİS export ✓
  - Inspection ready ✓
- İletişim: dpo@kurum.local (kurum DPO), destek@personel.local (Personel destek)

**Ses**:
> "Bu videoda DPO olarak yapacağınız 6 ana işlemi gördük: DSR workflow, legal
> hold, destruction report, canlı izleme override, VERBİS export ve inspection
> ready. Bu beceriler ile KVKK yükümlülüklerinizi karşılayabilir, Kurul denetimine
> hazır olabilir ve çalışanlarınızın haklarını güvence altına alabilirsiniz.
>
> Personel sizin DPO işinizi kolaylaştırmak için tasarlandı. Sorularınız olursa
> destek kanalımızdan ulaşın. Kurumsal DPO eğitim programımıza da katılmanızı
> öneririz — 4 saatlik derinlemesine workshop. Teşekkürler, başarılar dilerim."

---

## Prodüksiyon Notları

- **Süre**: 15:00 dakika (biraz esneklik var, 14-16 arası OK)
- **Ton**: Ciddi ve kurumsal (KVKK hukuki içerik)
- **Tempo**: Orta (bilgi yoğunluğu yüksek, yavaş yerler gerekiyor)
- **Ekran kaydı**: Her buton tıklaması zoom + highlight
- **Disclaimers**:
  - "Bu video yasal tavsiye değildir. Kendi DPO'nuzla özel durumları değerlendirin."
  - "Personel pilot ortamı kullanıldı; production'da detaylar farklılık gösterebilir."
