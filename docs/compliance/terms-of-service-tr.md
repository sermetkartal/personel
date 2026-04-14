# Personel — Hizmet Koşulları (Terms of Service)

> Dil: Türkçe. Hedef okuyucu: Personel UAM platformunu kullanan kurumsal müşteri yöneticileri (**operatörler**). Bu doküman template'dir; kurumlar kendi hukuki danışmanları ile inceleyip yayımlar.
>
> **Bu doküman çalışanlar için değil — operatörler içindir**. Çalışanlar için: `docs/user-manuals/employee-manual-tr.md` ve `docs/compliance/privacy-policy-tr.md`.

---

## 1. Kabul

Bu Hizmet Koşulları (bundan böyle "Koşullar"), Personel UAM (User Activity Monitoring) platformunu (bundan böyle "Hizmet") kullanan müşteri kurum yöneticilerinin ("Operatör") Hizmet'i nasıl kullanacağını düzenler.

Hizmet'i kullanarak, Operatör bu Koşulları **okuduğunu, anladığını ve kabul ettiğini** beyan eder. Koşulları kabul etmeyen kişiler Hizmet'i kullanamaz.

Operatör aynı zamanda:

- İşlendiği kurumun **Veri Sorumlusu** olduğunu kabul eder
- KVKK m.12 kapsamında **teknik ve idari tedbirler**den sorumlu olduğunu bilir
- Çalışan haklarının korunmasının kendi yetkisinde olduğunu kabul eder

---

## 2. Hizmet Tanımı

Personel UAM platformu, **on-prem** (müşteri kurum sunucusunda) çalışan bir çalışan aktivite izleme ve performans analitiği sistemidir. Platform:

- Windows uç noktalarından aktivite verisi toplar
- Verileri merkezi sunucuda saklar ve analiz eder
- Yöneticiye rapor, alarm ve denetim akışı sunar
- Çalışana **Şeffaflık Portalı** ile görünürlük ve KVKK m.11 başvuru arayüzü sağlar
- Canlı ekran izleme (sıkı dual-control kapısı altında) sunar
- KVKK uyumu için denetim, DSR, saklama ve imha özellikleri içerir

**Faz 1 MVP kapsamı**: Windows uç noktaları, single-tenant, on-prem. macOS/Linux/SaaS Faz 2+.

Kapsam dışı: Personel **bir güvenlik ürünü değildir**; bir SIEM, bir DLP motorunun tek parçası olarak konumlandırılabilir ancak bütünsel güvenlik için ek çözümler (AV, firewall, IDS, SOC) gerekir.

---

## 3. Sorumluluk Sınırlaması

### 3.1 İşveren Meşru Menfaati ve Çalışan Korumaları Dengesi

Personel platformu **KVKK m.5/2-f meşru menfaat** temelinde çalışır. Operatör:

- Her izleme faaliyetinin **ölçülü** olduğunu (m.4/2-ç) kabul eder
- Özel nitelikli verileri (m.6) **minimum düzeyde** işlemeyi taahhüt eder
- Çalışanın **onurunu ve mahremiyetini** ihlal edecek kullanımlardan kaçınır
- Sistem kayıtlarının **ispat için** kullanılacağını, ceza için değil, kabul eder
- Aydınlatma yükümlülüğünü (m.10) **eksiksiz** yerine getirir
- DSR (m.11) başvurularını **30 gün içinde** yanıtlar

### 3.2 Platform Sorumluluğu

Personel sağlayıcı (bundan böyle "Sağlayıcı"):

- Platformun **dökümante edilen** özelliklerini sağlar
- Kritik güvenlik açıklarını **zamanında** yamalamaktan sorumludur (SLA tanımlı)
- Müşteri verilerine erişim **yalnızca** yazılı müşteri onayı ile yapılır (remote support)
- Hizmet'i iyi niyet ile çalıştırır; veri üzerinde analiz, kişiselleştirme, hedefleme yapmaz

### 3.3 Sorumluluk Dışı

Sağlayıcı aşağıdakilerden **sorumlu değildir**:

- Operatörün Hizmet'i KVKK, iş hukuku veya başka mevzuata **aykırı** kullanması
- Operatörün yetersiz **DPIA** yapması veya **aydınlatma** yerine getirmemesi
- Operatörün çalışana ayrımcılık, psikolojik taciz gibi kötü niyetli kullanımı
- Kurum içi yanlış yapılandırma kaynaklı veri kaybı
- Mücbir sebep (doğal afet, savaş, DDoS, ulusal network outage)
- Müşteri ekipmanı arızalarından kaynaklı veri kayıpları (backup müşteri sorumluluğunda)

Sorumluluk tutarı, **sözleşme yıllık ücretinin 2 katını** aşamaz. Sağlayıcı dolaylı, özel, arızi veya sonuçsal zararlardan (kar kaybı, müşteri kaybı, itibar zararı) sorumlu **değildir**.

---

## 4. Kullanım Koşulları

### 4.1 Lisans

Operatör Hizmet'e **sözleşme süresi** boyunca, **kurum içi kullanım** için, **devredilemez** ve **münhasır olmayan** bir lisans alır. Lisans:

- Yeniden satılamaz
- Üçüncü taraf danışmanlık şirketine kiralanamaz
- Reverse engineering yapılamaz
- Benzer bir ürün geliştirmek için kullanılamaz

### 4.2 Destek

Destek seviyeleri sözleşmede tanımlıdır:

| Tier | İçerik | SLA |
|---|---|---|
| Basic | Dokümantasyon + community forum | Best effort |
| Standard | E-posta destek | 48 saat |
| Professional | Öncelikli destek + 1 senior mühendis | 4 saat |
| Enterprise | 7/24 + on-call | 15 dakika P1 |

### 4.3 Güncelleme Yükümlülüğü

Operatör:

- Sağlayıcı tarafından yayımlanan **güvenlik yamalarını** 30 gün içinde uygulamakla yükümlüdür
- Major release'lere (yıllık) 6 ay içinde geçmelidir
- Ajan sürümlerini uyumlu aralıkta tutmalıdır

Güncelleme reddedilmesi halinde destek garantisi askıya alınabilir.

---

## 5. Yasak Kullanım

Operatör Hizmet'i aşağıdaki amaçlarla kullanamaz:

### 5.1 Çalışan Haklarının İhlali

- Çalışanın onay vermediği özel nitelikli veri toplama (ADR 0013 DLP opt-in dışı)
- Özel yaşama müdahale (mola, tuvalet, sağlık durumu takibi)
- **Açık iletişimin** olmadığı (aydınlatma yapılmadan) izleme
- Sendika, siyasi görüş, din, cinsel yönelim gibi özel nitelikli verileri ayrımcılık için kullanma

### 5.2 Ayrımcılık

- Cinsiyet, yaş, ırk, din, engellilik, hamilelik, etnik köken, sendika üyeliği bazlı hedeflemeli izleme
- Verilerin ayrımcı performans değerlendirmede tek başına kullanılması (m.11/g itiraz hakkı)
- Gerçek performans göstergeleri olmayan mikromanagement

### 5.3 Psikolojik Taciz (Mobbing)

- Bir çalışana yönelik sistematik canlı izleme
- Tek bir çalışana özel sıkılaştırılmış kurallar
- Psikolojik baskı kurmak için rapor paylaşımı

### 5.4 Yasa Dışı Kullanım

- Yasa dışı gözetim
- Kamu düzenine aykırı amaçlar
- Yargı emrine uygun olmayan veri paylaşımı
- KVKK m.9 yurt dışı aktarım kurallarını ihlal

Yasak kullanım tespit edilirse Sağlayıcı sözleşmeyi **tek taraflı** feshedebilir.

---

## 6. Yetkili Mahkeme ve Hukuk

Bu Koşullar **Türkiye Cumhuriyeti** hukukuna tabidir.

Bu Koşullar'dan doğacak uyuşmazlıklarda **İstanbul Mahkemeleri** ve **İcra Daireleri** yetkilidir.

Alternatif olarak, Kişisel Veriler ile ilgili uyuşmazlıklarda **Kişisel Verileri Koruma Kurulu** (Ankara) yetkilidir.

---

## 7. Değiştirme Hakkı

Sağlayıcı, bu Koşulları gerekçe göstererek değiştirebilir:

- Kritik değişiklikler (fiyat, yasak kullanım, sorumluluk sınırı): **60 gün önceden** e-posta bildirimi
- Küçük değişiklikler (typo düzeltme, link güncelleme): bildirim gerekmez
- Yasal zorunluluk (KVKK, iş kanunu değişikliği): **30 gün** önceden bildirim

Operatör değişiklikleri kabul etmezse sözleşmeyi **cezasız** feshedebilir.

---

## 8. Fesih

### 8.1 Normal Fesih

- **Operatör**: 90 gün önceden yazılı bildirim
- **Sağlayıcı**: Normal koşullarda 180 gün önceden bildirim

### 8.2 Haklı Sebep ile Fesih

Aşağıdaki durumlarda **derhal** fesih mümkündür:

- Ücret ödemesinin 30 gün gecikmesi
- Yasak kullanım tespiti
- KVKK ihlali kanıtı
- İflas veya konkordato
- Force majeure > 90 gün

### 8.3 Fesih Sonrası

Fesih durumunda:

1. Operatör **90 gün** içinde müşteri verilerini export edebilir (KVKK uyumlu format)
2. 90 gün sonrasında Sağlayıcı tüm müşteri verilerini **kripto-shred** eder
3. Backup'lar 180 gün boyunca off-site tutulur, sonra imha
4. Fesih raporu (imzalı PDF) DPO'ya teslim
5. İmza tarihinden itibaren 5 yıl sonra tüm arşiv kayıtları imha

Audit log hakları fesih sonrası da devam eder (5-10 yıl saklama).

---

## 9. Diğer Hükümler

### 9.1 Gizlilik

Operatör ve Sağlayıcı arasındaki sözleşme dokümanları, fiyat bilgileri ve teknik mimari **gizli** bilgidir. Karşılıklı yazılı onay olmadan üçüncü şahıslara ifşa edilemez.

### 9.2 Referans

Sağlayıcı, Operatörün **kurum adını** referans olarak listeleyebilir (logo, web sitesi). Operatör yazılı olarak reddetmedikçe bu hak geçerlidir.

### 9.3 Bölünebilirlik

Bu Koşullar'ın herhangi bir maddesi geçersiz sayılırsa, diğer maddeler geçerli kalır.

### 9.4 Sözleşme Bütünlüğü

Bu Hizmet Koşulları, birlikte şunları oluşturur:
- Asıl hizmet sözleşmesi
- **Data Processing Agreement (DPA)** — `docs/compliance/dpa-template.md`
- **Service Level Agreement (SLA)**
- **Sub-processor Registry** — `docs/compliance/sub-processor-registry.md`

Tüm bu dokümanlar tek bir bütün olarak yorumlanır. Çelişki durumunda öncelik sırası: sözleşme → DPA → SLA → bu Koşullar → diğer ekler.

---

## 10. İmza ve Yürürlük

Bu Koşullar, **[TARİH]** itibarıyla yürürlüğe girer ve sözleşme süresi boyunca veya bir tarafça feshedilene kadar geçerli kalır.

### İmzalar

**Sağlayıcı**:
[Sağlayıcı firma unvanı]
[Yetkili isim]
[İmza + tarih]

**Operatör / Müşteri**:
[Kurum unvanı]
[Yetkili isim — genel müdür / CTO / CISO]
[İmza + tarih]

**Operatör DPO**:
[DPO adı]
[İmza + tarih]

---

## İletişim

Bu Koşullar hakkında sorular için:
- Sağlayıcı hukuk: legal@personel.local
- Müşteri DPO: dpo@[kurum].com.tr

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #167 — İlk template sürümü (TR) |
