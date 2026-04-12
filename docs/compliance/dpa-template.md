# Veri İşleme Sözleşmesi — Personel Platformu Şablonu

> **Hukuki dayanak**: 6698 sayılı KVKK m.12 (veri güvenliği), m.12/2 (yazılı veri işleyen sözleşmesi), m.12/5 (ihlal bildirimi), Kişisel Verilerin Silinmesi/Yok Edilmesi/Anonim Hale Getirilmesi Hakkında Yönetmelik m.7. İleride AB pazarına genişleme için GDPR m.28 (processor obligations) çapraz referansı korunmuştur.
>
> **Kullanım**: İşbu şablon, Personel Platformu'nu **on-prem** olarak müşteri kurum (Veri Sorumlusu) ile **Personel Yazılım** (Veri İşleyen — destek/bakım/güncelleme bağlamında) arasında imzalanacak Veri İşleme Sözleşmesi (DPA) temelidir. **NOT**: `kvkk-framework.md` §3'te de tespit edildiği üzere, on-prem konuşlandırmada Personel firması **kişisel veriye erişmediği** sürece teknik olarak veri işleyen sıfatını taşımaz. Ancak destek ceremony'leri, uzaktan teşhis, log paylaşımı ve `infra/scripts/dlp-enable.sh` gibi opt-in operasyonlar sırasında **istisnai** ve sınırlı erişim olabileceğinden, bu sözleşme **defansif olarak imzalanır** ve müşteriye her zaman yazılı garanti sunar. Phase 2+ yönetilen SaaS veya hibrit modele geçildiğinde aynı şablon **tam ölçekli** veri işleyen sözleşmesi olarak kullanılır.
>
> **Saklama**: İmzalı orijinal müşteri DPO arşivinde KVKK m.12 hesap verebilirlik kanıtı olarak en az **5 yıl** saklanır; sayısal kopyası `/var/lib/personel/legal/dpa-signed.pdf` yoluna yerleştirilir.
>
> Versiyon: 1.0 — Nisan 2026

---

## 1. Taraflar

**1.1. Veri Sorumlusu** (bundan sonra "Müşteri")

| Alan | Değer |
|---|---|
| Tüzel Kişi Tam Unvanı | `[{Müşteri Şirket Tam Unvanı}]` |
| MERSİS No | `[{MERSİS}]` |
| Vergi Dairesi / Vergi No | `[{Vergi Dairesi}] / [{VKN}]` |
| Adres | `[{Tebligat adresi}]` |
| KVKK Veri Sorumlusu Sicil (VERBİS) No | `[{VERBİS}]` |
| Veri Koruma Görevlisi (DPO) | `[{Ad-Soyad}], [{e-posta}]` |
| KVKK İrtibat Kişisi | `[{Ad-Soyad}], [{e-posta}], [{telefon}]` |

**1.2. Veri İşleyen** (bundan sonra "Personel Yazılım" veya "Tedarikçi")

| Alan | Değer |
|---|---|
| Tüzel Kişi Tam Unvanı | `[{Personel Yazılım A.Ş. Tam Unvanı}]` |
| MERSİS No | `[{MERSİS}]` |
| Adres | `[{Tedarikçi tebligat adresi}]` |
| KVKK Sorumlu İrtibat Kişisi | `[{Ad-Soyad}], dpo@personel.com.tr` |
| Acil Güvenlik İrtibatı (7/24) | `[{telefon}]`, security@personel.com.tr |

İşbu sözleşmede taraflar ayrı ayrı "Taraf", birlikte "Taraflar" olarak anılacaktır.

---

## 2. Konu ve Kapsam

İşbu sözleşmenin konusu, Müşteri tarafından Personel Yazılım'dan lisanslı olarak alınan **Personel User Activity Monitoring Platformu**'nun (bundan sonra "Platform") Müşteri'nin kendi bilgi işlem altyapısında konuşlandırılması, işletilmesi, güncellenmesi ve destek hizmetlerinin sağlanması sırasında işlenebilecek her türlü kişisel verinin 6698 sayılı KVKK ve ilgili mevzuata uygun şekilde işlenmesi için Tarafların hak ve yükümlülüklerini düzenlemektir.

İşbu sözleşme, Müşteri ile Personel Yazılım arasında akdedilmiş ana **Yazılım Lisans ve Bakım Sözleşmesi**'nin **ayrılmaz eki** olup ana sözleşme süresince yürürlükte kalır.

### 2.1. İşlemenin Niteliği ve Amacı

- **Niteliği**: Lisanslı yazılımın Müşteri veri merkezinde konuşlanması; periyodik güncelleme; kurulum desteği; olay üzerine teşhis desteği; opt-in DLP aktivasyon ceremony'lerinde teknik refakat.
- **Amacı**: Müşteri'nin User Activity Monitoring, KVKK uyum yükümlülüğü ifası, BT güvenliği ve insider threat tespit ihtiyaçlarının karşılanması.

### 2.2. İşleme Süresi

İşbu sözleşme, ana lisans sözleşmesi süresince yürürlüktedir. Sözleşme sonu sonrası yükümlülükler için bkz. §11.

### 2.3. Veri Sahipleri Kategorileri

- Müşteri'nin çalışanları, stajyerleri, alt yüklenici personeli ve Müşteri yönetiminin Platform'a kaydettiği diğer ilgili kişiler.

### 2.4. Kişisel Veri Kategorileri

Bkz. **Ek-A — İşlenen Veri Kategorileri** (11 kategori).

### 2.5. İşleme Yeri

- **Birincil**: Müşteri'nin kendi veri merkezi (on-prem). Veriler Türkiye sınırlarında, Müşteri'nin fiziksel kontrolü altındaki donanımda kalır.
- **İstisnai**: Destek ceremony'leri sırasında Tedarikçi mühendisi, **yalnızca Müşteri'nin yazılı talimatı ve refakati** ile Müşteri ortamına geçici uzaktan erişim sağlayabilir (bkz. §4.3).

---

## 3. Hukuki Dayanak ve Sıfat Tespiti

3.1. Müşteri, KVKK m.3/1-ı uyarınca **Veri Sorumlusu** sıfatını taşır; işleme amaçlarını ve vasıtalarını belirler.

3.2. Personel Yazılım, on-prem dağıtım modelinde KVKK m.3/1-ğ kapsamında **veri işleyen sıfatını ilke olarak taşımaz** (bkz. `docs/compliance/kvkk-framework.md` §3.1). Ancak bu sözleşme, §2.5'te tanımlanan **istisnai erişim** hâllerinde Tedarikçi'nin **veri işleyen sıfatıyla** hareket ettiğini ve KVKK m.12/2'nin ilgili tüm yükümlülüklerinin uygulanacağını açıkça kabul eder. Bu defansif yaklaşım, Müşteri'ye azami yasal güvence sağlamak amacıyla benimsenmiştir.

3.3. GDPR çapraz referans: AB pazarına genişleme veya AB'de mukim çalışan verisi söz konusu olursa, işbu sözleşme **GDPR m.28** kapsamında "processor agreement" olarak da kabul edilir; m.28/3'te sayılan yedi zorunlu unsur (a–h bentleri) işbu metinde §4–§11 arasında karşılanmıştır.

---

## 4. Veri İşleyen'in Yükümlülükleri

### 4.1. Yalnızca Müşteri Talimatıyla İşleme

Personel Yazılım, kişisel verileri **yalnızca Müşteri'nin yazılı veya yazılı kanıtlanabilir** (e-posta, ticket, signed change request) talimatıyla ve işbu sözleşmede tanımlı amaç dışında işlemez. Talimatın hukuka aykırı olduğunu tespit ederse derhal Müşteri'yi bilgilendirir ve talimatı yerine getirmekten kaçınır.

### 4.2. KVKK m.12 Güvenlik Tedbirleri

Personel Yazılım, KVKK m.12/1 uyarınca aşağıdaki idari ve teknik tedbirleri sürekli olarak uygular ve günceller. Ayrıntılı liste **Ek-C**'dedir. Asgari güvenceler:

- **Şifreleme — at rest**: AES-256-GCM zarf şifrelemesi (bkz. `docs/architecture/key-hierarchy.md`); SQLCipher ajan kuyruğu; MinIO server-side encryption; ClickHouse disk şifrelemesi.
- **Şifreleme — in transit**: mTLS 1.3, sertifika pinning, HSTS, gRPC bidi stream üzerinde TLS.
- **Kimlik ve yetki**: OIDC + RBAC (7 rol), zorunlu çok faktörlü kimlik doğrulama, en az ayrıcalık ilkesi.
- **Audit log**: Hash zincirli (SHA-256), append-only, WORM arşivli, günlük checkpoint imzalı.
- **Klavye içeriği gizliliği**: Yöneticiler kriptografik olarak okuyamaz; yalnızca izole DLP servisi (ADR 0013) opt-in ceremony sonrası çözebilir.
- **Anti-tamper**: Watchdog süreci, kod imzalama, çift imzalı update verifikasyonu.
- **Gizlilik testleri**: Yıllık penetrasyon testi, kırmızı takım keystroke admin-blindness testi, üçüncü taraf güvenlik denetimi.

### 4.3. İstisnai Uzaktan Destek Erişimi

Tedarikçi mühendisi, Müşteri ortamına aşağıdaki şartlar **birlikte** karşılanmadıkça erişemez:

1. Müşteri tarafından açılmış destek talebi (ticket).
2. Müşteri'nin **yazılı talimatı** (e-posta veya destek portalı kaydı).
3. Müşteri DPO veya BT Güvenlik Sorumlusu'nun **eş zamanlı refakati** (paylaşılan ekran ile).
4. Erişimin tüm aşamalarının Müşteri tarafında **session recording** ile kaydedilmesi.
5. Erişim sonunda Tedarikçi mühendisinin **yazılı tutanak** ile gerçekleştirilen işlemleri raporlaması.
6. Erişim audit zincirine `vendor.support.session` olayı olarak yazılması.

### 4.4. Çalışanların Gizlilik Taahhüdü

Personel Yazılım, müşteri ortamına erişme yetkisi olan veya kişisel veriye erişme potansiyeli olan her çalışanından/danışmanından **yazılı gizlilik taahhüdü** (NDA + KVKK gizlilik sözleşmesi) alır ve bu kayıtları en az 5 yıl saklar.

### 4.5. Veri İhlali Bildirimi (KVKK m.12/5)

KVKK m.12/5 uyarınca veri ihlali (kişisel verilerin kanuni olmayan yollarla başkaları tarafından elde edilmesi) tespit edildiğinde:

- Personel Yazılım, ihlalin tespitinden itibaren **24 saat içinde** Müşteri'ye yazılı bildirim yapar (e-posta + telefon, ihlalin niteliği + olası etki + alınan tedbirler).
- Müşteri (Veri Sorumlusu) **Kurul'a en geç 72 saat içinde** bildirim yapar; Tedarikçi bu bildirim sürecinde Müşteri'ye gerekli tüm teknik bilgiyi sağlar.
- Tarafların ihlal müdahale planları için bkz. `docs/security/runbooks/incident-response-playbook.md`.

GDPR çapraz referans: GDPR m.33 kapsamında 72 saatlik bildirim süresi karşılanmış olur.

### 4.6. Veri Sahibi Haklarına Destek (KVKK m.11)

Müşteri, kendi çalışanlarından gelen KVKK m.11 talepleri (a–ğ bentleri) için 30 günlük yasal süreyi karşılamakla yükümlüdür. Tedarikçi:

- Şeffaflık Portalı, DSR workflow ve 30 günlük SLA sayacı dahil **teknik altyapıyı sağlar**;
- Müşteri tarafından bildirilen DSR taleplerinde, özel teknik destek (örn. veri çıkarımı, kriptografik silme doğrulaması) için **5 iş günü** içinde teknik destek sunar.
- Doğrudan veri sahibi başvurularını kabul etmez; tüm başvuruları Müşteri DPO'suna yönlendirir.

### 4.7. Hesap Verebilirlik ve Kayıt Tutma

Tedarikçi, KVKK m.12 ve GDPR m.30/2 kapsamında işleme faaliyet kayıtlarını (Records of Processing) tutar ve Müşteri talebi üzerine sunar.

---

## 5. Alt Veri İşleyenler (Sub-Processors)

5.1. Personel Yazılım, on-prem MVP konuşlandırmasında **hiçbir alt veri işleyen kullanmaz**. Tüm veri Müşteri lokasyonunda kalır.

5.2. Tedarikçi'nin gelecekte (örn. cloud yan-hizmetler, hata raporlama, e-posta relay) bir alt veri işleyen kullanma ihtiyacı doğarsa:

- Müşteri'ye **en az 30 takvim günü öncesinden** yazılı bildirim yapılır.
- Müşterinin **gerekçeli itiraz hakkı** vardır; itiraz hâlinde Tedarikçi alternatif çözüm sunamazsa Müşteri sözleşmeyi tek taraflı feshetme hakkına sahiptir.
- Onay alınmadan hiçbir alt veri işleyen aktive edilmez.

5.3. Güncel alt veri işleyen listesi **Ek-D** ve canlı `docs/compliance/sub-processor-registry.md` dokümanındadır. Müşteri DPO'su, registry'nin değişikliklerini RSS/e-posta abonesi olarak takip edebilir.

---

## 6. Müşterinin Denetim Hakkı

6.1. Müşteri, KVKK m.12/3 ve GDPR m.28/3-h çerçevesinde Tedarikçi'nin işbu sözleşmeye uyumunu denetleme hakkına sahiptir.

6.2. Denetim modeli:

- **Yıllık standart denetim**: Müşteri yılda **1 (bir) kez** ücretsiz olarak Tedarikçi'nin uyum kanıtlarını talep edebilir. Tedarikçi: SOC 2 Type II raporu, ISO 27001 sertifikası (varsa), penetrasyon testi sonuçları, evidence pack export (`GET /v1/dpo/evidence-packs`) sunar.
- **Olağanüstü denetim**: Bir veri ihlali, Kurul incelemesi veya makul bir şüphe söz konusu ise Müşteri ek denetim talep edebilir; masraflar Müşteri'ye aittir, ancak bulgular Tedarikçi aleyhine ise Tedarikçi karşılar.
- **Yerinde denetim**: Müşteri (kendi DPO + dış denetçi ile birlikte) Tedarikçi ofislerini ziyaret etme hakkına sahiptir; ziyaret en az 10 iş günü önceden bildirilir.

6.3. Tedarikçi denetim sürecine **iyi niyetle** katılır ve makul tüm kayıtlara erişim sağlar.

---

## 7. Kişisel Verinin Aktarımı

7.1. **Yurt içi aktarım**: On-prem kurulumda Tedarikçi'ye kişisel veri aktarımı yapılmaz.

7.2. **Yurt dışı aktarım**: KVKK m.9 kapsamında yurt dışına aktarım söz konusu değildir. Tedarikçi, gelecekte bir yan hizmet için yurt dışı transfer önerirse Müşteri'nin **açık yazılı onayı** ve **uygun korunma araçları** (Kurul'un onayladığı ülke listesi VEYA yeterli koruma taahhütnamesi) zorunludur. GDPR çapraz referans: m.46 (transfer mekanizmaları).

---

## 8. Çalışanların Özel Hassasiyeti — Kriptografik Garanti

8.1. ADR 0013 (DLP Disabled by Default) gereği klavye içeriği:

- Varsayılan olarak **toplanmaz**;
- Toplandığında bile **AES-256-GCM** ile şifrelidir ve TMK Vault transit engine'de `exportable: false` korunur;
- Çözülebilmesi yalnızca izole DLP servisi (seccomp+AppArmor profil) ve **müşteri tarafından imzalı opt-in form** (`docs/compliance/dlp-opt-in-form.md`) ile mümkündür;
- Tedarikçi mühendisleri, hatta Müşteri admin'leri **kriptografik olarak** okuyamaz.

8.2. Bu kriptografik garanti, Tedarikçi tarafından **sözleşmesel taahhüt** niteliği taşır; ihlali ana lisans sözleşmesinin esaslı ihlali sayılır.

---

## 9. Veri Güvenliği Olayları — Ortak Müdahale

9.1. Tarafların ortak müdahale prosedürü `docs/security/runbooks/incident-response-playbook.md` ve `infra/runbooks/dr.md` doküman setine atıf yapar.

9.2. Olay sonrası kök neden analizi raporu Müşteri'ye 30 gün içinde sunulur.

---

## 10. Cezai Sorumluluk ve Sorumluluk Sınırları

10.1. Tedarikçi'nin işbu sözleşmenin esaslı ihlalinden doğan sorumluluğu, ana lisans sözleşmesinin sorumluluk sınırları çerçevesinde değerlendirilir; **kasıt veya ağır ihmal hâlleri dışında** sınırlamalar geçerlidir.

10.2. Tedarikçi'nin **kasıt veya ağır ihmali** hâlinde sorumluluk sınırlama hükümleri uygulanmaz; KVKK m.18 idari para cezaları, Kurul talepleri ve veri sahibi tazminat talepleri için tam sorumluluk doğar.

10.3. Müşteri'nin kendi tasarladığı politikalardan (örn. aşırı geniş ekran görüntüsü kapsamı) kaynaklanan ihlallerden Tedarikçi sorumlu değildir.

---

## 11. Sözleşme Sonu — Verilerin İadesi veya İmhası

11.1. Ana lisans sözleşmesi sona erdiğinde Müşteri, **45 takvim günü** içinde aşağıdaki seçeneklerden birini yazılı olarak Tedarikçi'ye bildirir:

- **A) İade**: Tedarikçi tarafından kontrolünde tutulan tüm Müşteri kişisel verisinin (varsa) standart formatta (PostgreSQL dump + MinIO export) iade edilmesi;
- **B) İmha**: Aynı verinin KVKK m.7 ve İmha Yönetmeliği m.10/m.11 uyarınca silinmesi/yok edilmesi.

11.2. Seçim yapılmazsa Tedarikçi varsayılan olarak imha yöntemine geçer ve **tutanak** ile Müşteri'ye bildirir. İmha tutanağı KVKK hesap verebilirlik kanıtı olarak iki Tarafça en az **5 yıl** saklanır.

11.3. Yasal saklama yükümlülüğü olan kayıtlar (audit log, idari para cezası süreçleri) yasal süre dolana kadar saklanır ve ardından imha edilir.

---

## 12. Genel Hükümler

- **Yürürlük**: İşbu sözleşme Tarafların imzalı tarihinde yürürlüğe girer.
- **Değişiklik**: Yalnızca yazılı ve karşılıklı imzalı ek protokol ile değiştirilebilir.
- **Devir yasağı**: Tedarikçi, Müşteri'nin yazılı onayı olmaksızın işbu sözleşmeyi devredemez.
- **Uygulanacak hukuk ve yetki**: Türkiye Cumhuriyeti Hukuku; `[{İstanbul/Ankara}]` Mahkemeleri ve İcra Daireleri yetkilidir.
- **Hükümlerin bağımsızlığı**: Bir hükmün geçersizliği diğer hükümleri etkilemez.

---

## 13. İmzalar

| Taraf | Ad-Soyad | Unvan | Tarih | İmza |
|---|---|---|---|---|
| Müşteri (Veri Sorumlusu) | `[{Ad-Soyad}]` | `[{Yetkili Unvan}]` | `[{YYYY-AA-GG}]` | ____________ |
| Müşteri DPO | `[{Ad-Soyad}]` | DPO | `[{YYYY-AA-GG}]` | ____________ |
| Personel Yazılım (Tedarikçi) | `[{Ad-Soyad}]` | `[{Yetkili Unvan}]` | `[{YYYY-AA-GG}]` | ____________ |
| Tedarikçi KVKK Sorumlusu | `[{Ad-Soyad}]` | DPO/CPO | `[{YYYY-AA-GG}]` | ____________ |

---

## Ek-A — İşlenen Veri Kategorileri (11 Kategori)

Aşağıdaki kategoriler `apps/portal/src/app/[locale]/verilerim/page.tsx` Şeffaflık Portalı sayfası ile birebir tutarlıdır.

| # | Kategori | İçerik | Hassasiyet | Hukuki Sebep |
|---|---|---|---|---|
| 1 | **Kimlik ve Oturum** (`identity`) | Kullanıcı adı, endpoint hostname, oturum lock/unlock | Düşük | m.5/2-c, m.5/2-f |
| 2 | **Süreç ve Pencere** (`process`) | Çalışan uygulamalar, foreground değişimi, pencere başlığı | Orta | m.5/2-c, m.5/2-f |
| 3 | **Ekran Görüntüsü/Video** (`screenshot`) | Periyodik ekran capture, video klip | **Yüksek (m.6 kazara riski)** | m.5/2-f |
| 4 | **Dosya Sistemi** (`file`) | Dosya oluşturma, okuma, yazma, silme, yeniden adlandırma, kopyalama | Orta | m.5/2-f |
| 5 | **Pano (Clipboard)** (`clipboard`) | Pano metadata + opt-in ile şifreli içerik | **Yüksek** | m.5/2-f |
| 6 | **Klavye** (`keystroke`) | Pencere bazlı tuş istatistikleri + opt-in ile şifreli içerik | **Çok yüksek (kriptografik izolasyon)** | m.5/2-c, m.5/2-f |
| 7 | **Yazıcı** (`print`) | Yazdırma işi metadata | Orta | m.5/2-f |
| 8 | **USB Donanım** (`usb`) | USB takma/çıkarma, kütle depolama politika ihlali | Düşük | m.5/2-f |
| 9 | **Ağ Akışları** (`network`) | Akış özeti, DNS sorgu, TLS SNI | Orta | m.5/2-f, m.5/2-ç |
| 10 | **Canlı İzleme Denetim** (`liveView`) | HR onaylı canlı izleme oturum kayıtları | Denetim (m.12) | m.5/2-f, m.12 |
| 11 | **Politika Uygulama** (`policy`) | App/web bloklama, ajan tamper, politika versiyon kayıtları | Düşük | m.5/2-f |

Saklama süreleri matrisi için bkz. **Ek-E** ve `docs/compliance/iltica-silme-politikasi.md` §3.

---

## Ek-B — İşleme Amaçları

1. **User Activity Monitoring (UAM)**: çalışan verimliliği analizi, çalışma süresi takibi, performans göstergeleri.
2. **KVKK uyum**: Şeffaflık Portalı, DSR yönetimi, periyodik imha, VERBİS export, hash-zincirli denetim.
3. **BT Güvenliği**: insider threat tespiti, veri sızıntı önleme (DLP — opt-in), tampering tespiti, USB politika zorlama.
4. **Hesap verebilirlik (KVKK m.12)**: 5 yıllık canlı izleme audit zinciri, yönetici işlem denetimi.
5. **Olay incelemesi**: meşru menfaat ve hukuki yükümlülük çerçevesinde delil toplama.

---

## Ek-C — Teknik ve İdari Tedbirler

### Teknik Tedbirler

- **Şifreleme Hiyerarşisi**: TMK → PE-DEK → DEK; AES-256-GCM zarf şifrelemesi (`docs/architecture/key-hierarchy.md`).
- **Vault Transit**: TMK `exportable:false`, `derived:true`, Shamir 3-of-5 unseal.
- **mTLS PKI**: Sertifika pinning, kısa TTL, otomatik rotasyon (`docs/architecture/mtls-pki.md`).
- **Audit Zinciri**: SHA-256 hash zinciri, append-only Postgres + WORM mirror (MinIO Object Lock Compliance mode).
- **HR-Gated Live View**: Çift onay (requester ≠ approver), zaman sınırı (15/60 dk), reason code zorunlu.
- **DLP Default OFF (ADR 0013)**: Distroless container, seccomp + AppArmor profil, AppRole izolasyonu.
- **Anti-Tamper**: Watchdog süreci, çift imzalı update, ETW-based collector, kod imzalama.
- **Hassasiyet Filtresi**: `screenshot_exclude_apps`, `window_title_sensitive_regex`, k-anonimlik (k≥5).
- **Network**: TLS 1.3 minimum, NATS JetStream at-rest encryption, Postgres RLS.

### İdari Tedbirler

- **Personel**: Yıllık KVKK eğitimi, gizlilik taahhütnamesi, en az ayrıcalık ilkesi.
- **Erişim Yönetimi**: 7 RBAC rolü, OIDC + zorunlu MFA, periyodik erişim incelemesi (CC6.3).
- **Politika**: ISO 27001/SOC 2 uyumlu doküman seti (`docs/compliance/iso27001-soc2-*.md`).
- **Olay Müdahalesi**: 24/7 IRT, 24h Müşteri bildirim SLA, 72h Kurul bildirim hazırlığı.
- **İş Sürekliliği**: Yıllık BCDR drill (CC9.1), günlük yedekleme (A1.2), DR planı.
- **Tedarikçi Yönetimi**: Sub-processor onay süreci, yıllık vendor review.
- **Eğitim**: Yıllık KVKK + güvenlik tazeleme, phishing simulation.

---

## Ek-D — Alt Veri İşleyenler Listesi

İşbu ek, `docs/compliance/sub-processor-registry.md` dokümanındaki güncel listeye atıf yapar. **İşbu sözleşmenin imza tarihinde aktif alt veri işleyen sayısı: 0 (sıfır)**. Phase 1 on-prem MVP'de Personel Yazılım hiçbir alt veri işleyen kullanmaz.

---

## Ek-E — Saklama Süreleri Matrisi

`docs/compliance/iltica-silme-politikasi.md` §3 dokümanına atıf yapar. Özet:

- En kısa: Klavye içeriği (şifreli) — 14 gün
- En uzun: Canlı izleme denetim kaydı — 5 yıl (append-only)
- Periyodik imha: Günlük TTL + 6 aylık formal rapor (Yönetmelik m.11)

---

## English Mirror — Summary

> This is a high-fidelity English summary of the Turkish DPA above. The Turkish version is the legally binding document under KVKK 6698. The English mirror is provided for cross-border legal review (anticipated EU expansion in Phase 3+) and for non-Turkish-speaking auditors.

### 1. Parties

**Data Controller (Customer)**: `[{Customer Legal Entity}]`, MERSIS `[{...}]`, VERBIS `[{...}]`, DPO `[{...}]`.

**Data Processor (Vendor)**: Personel Yazılım, MERSIS `[{...}]`, KVKK Officer dpo@personel.com.tr, 24/7 security@personel.com.tr.

### 2. Subject Matter, Nature, Purpose

This DPA governs the processing of personal data that may occur during deployment, operation, update and support of the Personel User Activity Monitoring Platform on Customer's on-premise infrastructure. It is an integral annex to the Software Licence and Maintenance Agreement.

### 3. Role Determination

Customer is the **Data Controller** under KVKK Art. 3/1-ı. In the on-prem deployment Vendor does not, as a matter of principle, qualify as a Data Processor under KVKK Art. 3/1-ğ because Vendor does not access personal data. However, this DPA is signed defensively to govern **exceptional remote support sessions** during which Vendor may be granted limited access to the Customer environment. In those exceptional sessions Vendor acts as a Data Processor and the full KVKK Art. 12/2 obligations apply. GDPR Art. 28 is cross-referenced for forward compatibility with EU expansion.

### 4. Processor Obligations

- **Process only on Customer's documented instructions** (Sec. 4.1).
- **Implement KVKK Art. 12 security measures**: AES-256-GCM at rest, mTLS 1.3 in transit, OIDC+MFA+RBAC, hash-chained audit log, cryptographic isolation of keystroke content (ADR 0013), anti-tamper, annual penetration test, third-party security audit (Sec. 4.2 and Annex C).
- **Exceptional remote support gating**: written ticket + Customer DPO/Security accompaniment + session recording + audit chain `vendor.support.session` event (Sec. 4.3).
- **Confidentiality commitments** from all Vendor staff with potential data access (Sec. 4.4).
- **Personal data breach notification**: Vendor notifies Customer in writing **within 24 hours** of detection. Customer has **72 hours** to notify the Authority under KVKK Art. 12/5 / GDPR Art. 33 (Sec. 4.5).
- **Support data subject rights**: technical assistance to Customer for KVKK Art. 11 / GDPR Art. 15-22 requests within 5 business days; Vendor never accepts data subject requests directly (Sec. 4.6).
- **Records of processing** maintained per KVKK Art. 12 / GDPR Art. 30/2 (Sec. 4.7).

### 5. Sub-Processors

In the Phase 1 on-prem MVP **no sub-processors are used**. Future sub-processor additions require **30-day prior written notice** and Customer's right to object; objection without remediation is grounds for termination. Live registry: `docs/compliance/sub-processor-registry.md`.

### 6. Audit Right

Customer may conduct one free annual standard audit, plus extraordinary audits in case of breach or Authority inquiry, plus on-site audits with 10 business days' notice (Sec. 6).

### 7. Data Transfers

No transfer of personal data to Vendor in the on-prem model. No cross-border transfers. Future cross-border transfers require Customer's explicit written consent and adequate safeguards under KVKK Art. 9 / GDPR Art. 46.

### 8. Cryptographic Privacy Guarantee

Per ADR 0013, keystroke content is collected only after a customer-signed opt-in ceremony, encrypted with AES-256-GCM, and decryptable only by an isolated DLP service. Vendor engineers and Customer admins are **cryptographically unable** to read keystroke content. Breach of this guarantee is a material breach of the licence agreement (Sec. 8).

### 9. Security Incidents

Joint incident response per `docs/security/runbooks/incident-response-playbook.md`. Root cause analysis report delivered to Customer within 30 days (Sec. 9).

### 10. Liability

Standard liability caps apply except in cases of **wilful misconduct or gross negligence**, where caps are removed and full liability arises for KVKK Art. 18 administrative fines, Authority orders and data subject damages (Sec. 10).

### 11. End of Contract

Within **45 calendar days** of contract termination, Customer chooses (A) return of any personal data Vendor controls or (B) destruction per KVKK Art. 7 / Destruction Regulation Art. 10-11. Default is destruction. Destruction certificates retained by both parties for at least 5 years (Sec. 11).

### Annexes

- **Annex A**: 11 categories of processed data — consistent with `apps/portal/src/app/[locale]/verilerim/page.tsx`.
- **Annex B**: Processing purposes (UAM, KVKK compliance, IT security, accountability, incident investigation).
- **Annex C**: Technical and organisational measures (key hierarchy, Vault, mTLS, audit chain, HR-gated live view, ADR 0013 DLP isolation, RBAC, training, BCDR).
- **Annex D**: Sub-processor list — currently **zero**, references live registry.
- **Annex E**: Retention matrix — references `docs/compliance/iltica-silme-politikasi.md` Sec. 3.

---

*Şablon versiyonu: 1.0 — Nisan 2026. Müşteri hukuk müşavirinin onayı olmadan tam olarak aynı metin kullanılmamalı; her şirkete özel uyarlama gerekir. KVKK madde numaraları 6698 sayılı Kanun ve ilgili yönetmeliklerin 2026 yılı yürürlükteki metnine göre kontrol edilmiştir; mevzuat değişikliği hâlinde güncelleme zorunludur.*
