# Personel Çalışan Kılavuzu (Şeffaflık Portalı)

> Dil: Türkçe. Hedef okuyucu: Personel kullanan kurumun çalışanları. Bu kılavuz, sizin olarak çalışanın Personel Şeffaflık Portalı'nı nasıl kullanacağını, haklarınızı ve verilerinizin nasıl işlendiğini anlatır.
>
> İlgili yasal çerçeve: 6698 sayılı **Kişisel Verilerin Korunması Kanunu (KVKK)**.

## İçindekiler

1. [Şeffaflık Portalı Nedir?](#1-şeffaflık-portalı-nedir)
2. [Portala Giriş](#2-portala-giriş)
3. [Ne İzleniyor?](#3-ne-izleniyor)
4. [Neler İzlenmiyor?](#4-neler-izlenmiyor)
5. [KVKK Haklarım](#5-kvkk-haklarım-m11)
6. [DSR Başvurusu](#6-dsr-veri-sahibi-başvurusu)
7. [Aydınlatma Metni](#7-aydınlatma-metni-m10)
8. [Canlı İzleme Haklarım](#8-canlı-izleme-haklarım)
9. [DLP Durumu](#9-dlp-durumu-klavye-içeriği)
10. [SSS](#10-sık-sorulan-sorular)

---

## 1. Şeffaflık Portalı Nedir?

Şeffaflık Portalı, Personel platformunun size **tam görünürlük** sağladığı bölümdür. Türkiye Cumhuriyeti 6698 sayılı KVKK'nın **m.10 Aydınlatma Yükümlülüğü** ve **m.11 İlgili Kişinin Hakları** gereği, çalışan olarak hangi verilerinizin işlendiğini, neden işlendiğini, ne kadar saklandığını ve haklarınızı nasıl kullanabileceğinizi bilmek hakkınızdır.

Portal adresi genellikle: `https://portal.personel.musteri.local`

Personel şirketiniz tarafından **işyerinde performans ve güvenlik** amaçlı kullanılmaktadır. Bu kılavuz sizin için hazırlanmıştır — dürüst, açık ve anlaşılır.

---

## 2. Portala Giriş

1. Kurumsal e-posta ile: şirket SSO (Single Sign-On) üzerinden giriş yaparsınız
2. İlk girişinizde **onboarding ekranı** çıkar — bu ekran ilk kez:
   - Aydınlatma metnini özetler
   - Canlı izlemenin mümkün olduğunu bir defa bildirir
   - Haklarınızı gösterir
   - Kabul ettiğinizi işaretletir (audit log'a yazılır, zorunlu)

![Onboarding modal](figure: first-login — placeholder)

Bir kez kabul ettikten sonra giriş ekranı bu bildirimi tekrar göstermez; ancak metinler **her zaman** sol menüden erişilebilir.

---

## 3. Ne İzleniyor?

Portal'ın **Verilerim** sayfası (`/tr/verilerim`), şirketinizin hangi veri kategorilerini topladığını **11 başlık** altında açıklar:

| # | Kategori | Ne içerir? | Ne işe yarar? |
|---|---|---|---|
| 1 | Süreç (process) olayları | Hangi uygulamaları açıp kapattığınız | Yazılım lisans kullanımı, prodüktivite |
| 2 | Ön plan pencere | Hangi pencerede ne kadar süre kaldığınız | Çalışma süresi analizi |
| 3 | Pencere başlıkları | Pencere başlık metni (ör. "Rapor.xlsx") | İş hattı analizi; hassas başlıklar otomatik maskelenir |
| 4 | Ekran görüntüsü | 1-5 dakika aralıkla anlık ekran | Compliance, olay kanıtı |
| 5 | Boşta / aktif zaman | Klavye + fare hareketi var/yok | Mola / aktif çalışma farkı |
| 6 | Dosya olayları | Dosya oluşturma, okuma, yazma, silme | Veri sızıntısı tespiti |
| 7 | USB olayları | USB bellek takma/çıkarma | Veri transferi kontrolü |
| 8 | Yazıcı olayları | Yazdırma işi meta verisi (belge adı, sayfa) | Yasal metadata |
| 9 | Ağ akış özeti | Hangi siteye ne kadar trafik | Güvenlik |
| 10 | Klavye istatistikleri | Tuş SAYISI, content **değil** | Aktivite ölçümü |
| 11 | Pano (clipboard) meta | Ne zaman kopyalanmış (içerik değil) | Veri sızıntısı tespiti |

> **Önemli**: Her kategori için saklama süresi **KVKK Saklama Matrisi**'nde tanımlıdır. Örneğin ekran görüntüleri en fazla 30 gün saklanır; klavye istatistikleri 90 gün; USB olayları 365 gün.

Detay: `docs/architecture/data-retention-matrix.md` (operatörünüze sorun).

---

## 4. Neler İzlenmiyor?

**Güven, şeffaflıktan gelir.** Portal'ın **Neler İzlenmiyor?** sayfası (`/tr/neler-izlenmiyor`), Personel'in **ASLA toplamadığı** 10 maddeyi listeler:

1. **Klavye içerikleri (varsayılan)** — Şifreleriniz, özel mesajlarınız, yazdığınız içerik. Kurumunuz bunu **ayrı bir karar** ile (DLP opt-in) etkinleştirebilir; o durumda bile sistem yöneticisi **kriptografik olarak** içeriği okuyamaz (ADR 0013).
2. **E-posta içeriği** — Outlook/Thunderbird mesaj gövdesi. Sadece alıcı/gönderen/konu/zaman metadata.
3. **Web tarayıcı içeriği** — Ziyaret ettiğiniz sayfaların HTML'i. Sadece URL'ler (hassas alanlar maskelenir).
4. **Çerezler ve şifreler** — Tarayıcı password manager içeriğine asla dokunulmaz.
5. **Mikrofon / kamera** — Kapalıdır, aktivasyon yok.
6. **GPS / konum** — Yapılmaz.
7. **Kişisel bulut dosyaları** — Dropbox/Drive sync klasörü yalnızca sync event meta olarak izlenir, dosya içeriği okunmaz.
8. **Sağlık, sendika, din, siyasi görüş, ırk, etnisite** — KVKK m.6 özel nitelikli veriler; bu kategoriler **otomatik tespit** edilince kısa TTL bucket'a alınır ve erişimi DPO ile sınırlıdır.
9. **İş dışı saatler** — Varsayılan politika yalnızca mesai saatlerinde izler (kurumunuz farklı yapılandırmış olabilir — aydınlatma metninize bakın).
10. **Çocuklarınızın veya ailenizin verileri** — Ev kullanımı için değildir; eğer kurumsal laptop'u evde kullanıyorsanız, bilmelisiniz ki izleme devam eder. Kişisel kullanımı minimize etmeniz önerilir.

---

## 5. KVKK Haklarım (m.11)

KVKK m.11 size **yedi hak** tanır:

| # | Hak | Ne anlama gelir? |
|---|---|---|
| a | Kişisel verinizin işlenip işlenmediğini öğrenme | "Bu şirket benim verimi işliyor mu?" |
| b | İşlenmişse bilgi talep etme | "Hangi verimi işliyorsunuz?" |
| c | İşlenme amacını öğrenme | "Neden işliyorsunuz?" |
| d | Aktarıldığı kişileri bilme | "Kimlerle paylaştınız?" |
| e | Eksik/yanlış işlenmişse düzeltme | "Yanlış olanları düzeltin" |
| f | Silme / yok etme | "Silin (KVKK m.7 koşullarına göre)" |
| g | İtiraz etme | "Otomatik işleme sonucuna itiraz ediyorum" |

> **Cevap süresi**: KVKK m.13/2 gereği kurumunuz **30 gün** içinde size yazılı olarak cevap vermek zorundadır. Personel platformu bu süreyi otomatik takip eder; DPO panelinde süresi yaklaşan başvurular uyarı verir.

---

## 6. DSR (Veri Sahibi Başvurusu)

### 6.1 Başvuru Nasıl Yapılır?

**Başvurularım** (`/tr/basvurularim`) → **Yeni Başvuru**:

1. Başvuru türünü seçin (erişim / silme / düzeltme / itiraz / taşınabilirlik)
2. Gerekçenizi yazın (kısa açıklama yeterli; detay yazma zorunda değilsiniz)
3. Gönder → başvuru **yeni** durumunda kaydedilir
4. DPO'ya otomatik bildirim gider

![DSR başvuru formu](figure: dsr-form — placeholder)

### 6.2 Başvurumu Takip Etme

**Başvurularım** sayfasında tüm başvurularınız listelenir:

- Tarih
- Tür
- Durum (yeni / işlemde / tamamlandı / reddedildi)
- SLA kalan gün

Tamamlandığında e-posta bildirimi alırsınız.

### 6.3 Erişim Talebinden Ne Alırım?

"Erişim" talebinde kurumunuz size **kişisel verilerinizin kopyasını** sağlamak zorundadır. Personel bu kopyayı otomatik üretir:

- **PDF**: okunabilir özet (kategoriler, toplam sayılar, tarih aralıkları)
- **CSV** / **JSON**: detaylı export
- **Presigned URL**: 24 saat geçerli indirme linki

### 6.4 Silme Talebi (m.11/e-f, m.7)

Silme talebinde:
- Normal kayıtlar silinir (şifreli blob'lar için anahtar imhası = kripto-shred)
- **İstisna**: Yasal saklama yükümlülüğü olan kayıtlar (ör. 5 yıllık maaş bordrosu, audit log). Bu istisnalar DPO tarafından gerekçelendirilir.

### 6.5 Ret Durumu

Nadiren ret — ret gerekçesi yazılı olarak size iletilir ve Kurul'a (KVKK Kişisel Verileri Koruma Kurulu) **başvurma hakkınız** vardır.

---

## 7. Aydınlatma Metni (m.10)

**Aydınlatma** sayfası (`/tr/aydinlatma`) kurumunuzun hazırladığı tam aydınlatma metnini gösterir. Template: `docs/compliance/aydinlatma-metni-template.md`.

Aydınlatma metninde yer alan tipik başlıklar:

1. **Veri sorumlusunun kimliği** (kurumunuz)
2. **Kişisel verilerin hangi amaçla işleneceği**
3. **İşlenen kişisel verilerin kimlere ve hangi amaçla aktarılabileceği**
4. **Kişisel veri toplamanın yöntemi ve hukuki sebebi**
5. **KVKK m.11'de sayılan haklarınız**

> **Önemli**: Aydınlatma metnini **kabul etmediğiniz bildiğiniz** ama işe başladığınız varsayılır. İşveren meşru menfaati KVKK m.5/2-f uyarınca bir hukuki sebeptir — açık rıza her zaman gerekmez.

---

## 8. Canlı İzleme Haklarım

### 8.1 Canlı İzleme Nedir?

Yetkili bir yönetici (ör. İK soruşturması), sınırlı şartlar altında ekranınızı **canlı** olarak izleyebilir:

- **Sebep gerekli**: Bir olay, politika ihlali veya yasal zorunluluk olmalıdır
- **Çift onay**: İsteği yapan kişi **ASLA** onaylayamaz; farklı bir İK çalışanı ayrıca onaylamalıdır (kriptografik kural)
- **Süre sınırlı**: Maksimum 60 dakika
- **Kayıt yok**: Canlı oturum video olarak saklanmaz (Faz 1)
- **Audit**: Tüm oturumlar hash-zincirli değiştirilemez log'a yazılır

### 8.2 Geriye Dönük Görünürlük

**Canlı İzleme** (`/tr/canli-izleme`) sayfasında, size yönelik **tamamlanmış** canlı oturumların listesi 7 gün gecikme ile görünür:

- Tarih / süre
- Sebep kodu
- Talep eden kişi
- Onay veren kişi
- Ticket / olay numarası

> **Neden 7 gün gecikme?** Soruşturmanın sonuçlanmasına fırsat vermek için.

### 8.3 İtirazım Varsa?

Canlı izleme KVKK m.4 ölçülülük ilkesi çerçevesinde yapılmalıdır. Eğer bir oturumun sebepsiz olduğunu düşünüyorsanız:

1. DPO'ya yazılı itiraz yapın (DSR başvurusu ile)
2. Gerekirse Kurul'a başvurun

---

## 9. DLP Durumu (Klavye İçeriği)

**DLP Durumu** sayfası (`/tr/dlp-durumu`) size klavye içerik izlemesinin durumunu gösterir:

### Varsayılan: KAPALI

Yeni kurulumlarda klavye içerik DLP **varsayılan olarak kapalıdır** (ADR 0013). Kurumunuz etkinleştirmedikçe yazdığınız hiçbir şey toplanmaz.

### Etkinleştirme (Opt-in)

Kurumunuz DLP'yi etkinleştirmek isterse, bunu yapmadan önce:

1. DPO DPIA güncellemesi yapar
2. İmzalı opt-in formu tamamlanır (hukuk + güvenlik + DPO)
3. Şeffaflık Portalı banner'ı size **etkinleştirilme zamanını** ve **gerekçesini** bildirir
4. Ancak o zaman klavye içeriği toplanmaya başlar

### Etkinleşse Bile

Eğer etkinse bile:
- Sistem yöneticisi içeriği **kriptografik olarak okuyamaz** — sadece izole DLP motoru, önceden tanımlı DLP kurallarıyla eşleşme aramak için kısa süreliğine çözer
- Çözme işlemi her seferinde **audit log'a** yazılır
- "Eşleşme" olmayan içerik asla insan gözüne ulaşmaz

> **Pazarlama cümlesi**: "Personel, varsayılan olarak keystroke-blind olan tek KVKK-uyumlu UAM'dir." Bu bir söz değil — matematikselolarak ispatlanan bir durumdur.

---

## 10. Sık Sorulan Sorular

### 10.1 Evde kullandığım şirket laptop'unda da izleniyor muyum?

Evet — ajanlar cihaz tabanlıdır. Eğer kurumsal cihazı özel işleriniz için kullanıyorsanız, bilmelisiniz ki izleme aynı devam eder. **Kişisel kullanım için özel cihaz** önerilir.

### 10.2 Şifrelerim görüntüleniyor mu?

Hayır. Klavye içerik DLP kapalıyken yazdığınız içerik hiç toplanmaz. Açıksa bile içeriğe insan erişimi yoktur.

### 10.3 Facebook, X gibi sosyal medya sitelerini ziyaret ediyorum. Ne görülür?

Yalnızca **domain** ve ziyaret zamanı kaydedilir. İçerik, mesajlarınız, beğendikleriniz görülmez. İçerik olarak yalnızca pencere başlığı görülebilir (ör. "(15) Messenger" — profil adı değil).

### 10.4 Kişisel e-postalarım görülüyor mu?

Hayır. Outlook kurumsal e-posta için gönderen/alıcı/konu/zaman metadata toplanır. Gmail / Outlook.com gibi personal webmail yalnızca domain/URL olarak kaydedilir.

### 10.5 Ekran görüntüsünde ne kadar net görünüyorum?

Ekran görüntüsü WebP formatında, genelde 60-100 KB. Ekran çözünürlüğünün 2/3'ü kalitede. Metinler okunabilir; ince detaylar yumuşamış olabilir.

### 10.6 Kişisel bir mesaj yazarken ekran görüntüsü alınırsa?

Politika kurallarına bağlıdır. Çoğu kurumsal politika 1-5 dakika aralıkla yakalar; saniye bazında değil. Ayrıca **hassas pencere tespiti** (ör. 1Password, banka uygulaması başlığı) otomatik dışlama yapar.

### 10.7 "Boşta" sürem nasıl hesaplanıyor?

Klavye veya fare hareketi olmadığı sürece. Boşta süresi kaydedilir ama bu **ceza değildir** — mola, toplantı, telefon görüşmesi normaldir. Yalnızca istatistiktir.

### 10.8 Verilerimi Türkiye dışına aktarılıyor mu?

Personel **on-prem** (şirketinizin kendi sunucusunda) çalışır. Verileriniz yurt dışına **gönderilmez**. Eğer kurumunuz cloud kurulumuna geçmeyi düşünüyorsa, bu kararı ayrıca bildirir ve KVKK m.9 yurt dışı aktarım kurallarına göre işler.

### 10.9 Bir meslektaşım işten ayrıldı. Onun verileri ne kadar saklanıyor?

Her kategori için saklama matrisine göre. Tipik: süreç/pencere 90 gün, ekran görüntüsü 30 gün, USB 365 gün, audit log 5 yıl. Ayrıldıktan sonra erişim yetkisi kalkar ama veri kendi TTL süresine göre silinir.

### 10.10 Bu platformun bir hatası ya da kötüye kullanımından şüphelenirsem ne yapmalıyım?

Sırayla:

1. Kurumsal DPO'ya yazılı başvuru (Portal → Başvurularım → Yeni Başvuru → "İtiraz")
2. Tatmin olmadıysanız KVKK Kurulu'na başvuru
3. Acil güvenlik sorunu varsa (veri sızıntısı, kötüye kullanım) DPO acil kanalına bildirin

---

## Ek: İletişim

- Kurum DPO e-posta: *(aydınlatma metninizde yazılı)*
- KVKK Kurulu: [www.kvkk.gov.tr](https://www.kvkk.gov.tr)

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #162 — İlk sürüm (TR) |
