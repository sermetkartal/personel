# KVKK Uyum Çerçevesi — Personel Platformu

> Hukuki çerçeve: 6698 sayılı Kişisel Verilerin Korunması Kanunu (KVKK), Kişisel Verileri Koruma Kurulu (Kurul) ilke ve kararları, Kişisel Veri Saklama ve İmha Yönetmeliği, 4857 sayılı İş Kanunu, Anayasa m.20.
> Hedef okuyucu: Müşteri kurumun hukuk müşaviri, Veri Koruma Görevlisi (DPO), İK direktörü ve BT güvenlik yöneticisi.
> Versiyon: 1.0 — Nisan 2026

## 1. Yönetici Özeti

Personel; Windows uç noktalarında çalışan bir UAM (User Activity Monitoring) platformudur. Müşteri kurumun **kendi veri merkezinde (on-prem)** konuşlanır; kişisel veri hiçbir koşulda Personel firmasının altyapısına aktarılmaz. Bu mimari tercih, KVKK kapsamında **veri sorumlusu / veri işleyen** ayrımını temelden şekillendirmektedir: müşteri kurum **veri sorumlusu** (m.3/1-ı), Personel firması ise yalnızca **yazılım sağlayıcıdır** ve KVKK anlamında **veri işleyen sıfatını taşımaz**.

Bu çerçeve, Personel'in müşteri tarafından KVKK'ya uygun şekilde konuşlandırılması için gerekli tüm hukuki analizi, şablonları ve süreçleri sağlar.

## 2. Hukuki Dayanak Analizi

### 2.1. Temel Hukuki Sebep Seçimi: Meşru Menfaat vs. Açık Rıza

**Karar: Personel kapsamındaki veri işleme faaliyetlerinin büyük çoğunluğu, KVKK m.5/2-f uyarınca "ilgili kişinin temel hak ve özgürlüklerine zarar vermemek kaydıyla, veri sorumlusunun meşru menfaatleri için veri işlenmesinin zorunlu olması" hükmüne dayandırılmalıdır.** Açık rıza (m.5/1) işveren–çalışan ilişkisinde **hukuken zayıf bir temeldir** ve aşağıdaki nedenlerle ana dayanak olarak kullanılmamalıdır:

1. **Güç asimetrisi doktrini**: Kurul, istikrarlı şekilde, işveren–çalışan ilişkisinde çalışanın "özgür iradesiyle" rıza verdiğinin kabul edilemeyeceğini vurgulamaktadır. Çalışanın işini kaybetme korkusuyla verilen rıza, KVKK m.3/1-a'daki "özgür iradeyle açıklanan" rıza tanımını karşılamaz. [DOĞRULANMALI — Kurul'un bu yöndeki yerleşik ilkesi kararlarla somutlaşmıştır; güncel karar numarası için hukuk müşaviri teyidi alınmalıdır.]
2. **Geri alınabilirlik riski**: Açık rıza her an geri alınabilir (m.5/1 yorum). Bu, kurumsal izleme sistemi için operasyonel olarak sürdürülemezdir: rızasını geri alan her çalışan için agent'ın anında devre dışı bırakılması, bir çalışanın denetim kapsamı dışında bırakılması sonucunu doğurur ve BT güvenlik amacını boşa çıkarır.
3. **Amaç bölünmesi**: Aynı veri kategorisinin hem meşru menfaat hem açık rıza ile işlendiği karma model, KVKK m.4/2 şeffaflık ilkesine aykırıdır ve denetimde zayıf pozisyondur.

**Meşru menfaat testi (üç aşamalı denge testi — Kurul Rehberi):**

| Aşama | Analiz |
|---|---|
| **1. Meşru menfaatin varlığı** | İşveren; (a) bilgi güvenliği ve siber tehditlerin önlenmesi, (b) şirket varlıklarının (donanım, veri, ticari sır) korunması, (c) iş sözleşmesinin ifasının denetlenmesi (İş K. m.25, m.399), (d) KVKK m.12 veri güvenliği yükümlülüğünün yerine getirilmesi amaçlarıyla meşru menfaate sahiptir. |
| **2. İşlemenin zorunluluğu** | Bu amaçlara, uç nokta izleme olmadan ulaşmak teknik olarak mümkün değildir. Ağ katmanı denetimi uzaktan çalışan ve şifreli trafik için yetersizdir; DLP ihlalleri ve iç tehditler uç noktada doğar. |
| **3. Denge (temel hak ve özgürlüklere zarar vermeme)** | Personel; (a) klavye içeriğini kriptografik olarak yöneticilerden izole eder, (b) ekran görüntüsü/video için en kısa saklama sürelerini uygular (14–90 gün), (c) canlı izlemeyi çift onay kapısına bağlar, (d) aydınlatma ve şeffaflık portalı ile öngörülebilirliği sağlar. Bu tedbirler, m.4 ölçülülük ilkesini somutlaştırır ve dengeyi meşru menfaat lehine kurar. |

### 2.2. İş Sözleşmesinin İfası (m.5/2-c) — İkincil Dayanak

Süreç, pencere, dosya, oturum aktif/boşta gibi **performans ve devam denetimi** niteliği taşıyan olaylar için m.5/2-c ("sözleşmenin kurulması veya ifasıyla doğrudan doğruya ilgili olması kaydıyla, sözleşmenin taraflarına ait kişisel verilerin işlenmesinin gerekli olması") ek dayanak olarak kullanılabilir. İş Kanunu m.399 ve Borçlar Kanunu m.399, işçinin işini özenle ifa yükümlülüğü ile birlikte okunarak, çalışma saatlerinin ve iş sözleşmesi kapsamındaki faaliyetin denetlenmesini hukuki altyapıya bağlar.

### 2.3. Hukuki Yükümlülük (m.5/2-ç)

- Vergi/SGK: bordro zaman takip yükümlülükleri (dolaylı ilgi).
- MASAK, 5651 sayılı Kanun (log tutma yükümlülüğü — iç ağ kullanıcı faaliyetleri için sınırlı ama ağ akış özetleri bakımından bağlantılıdır).
- Sektörel yükümlülükler: bankacılık (BDDK), elektronik haberleşme, sağlık (ek yükümlülükler).

### 2.4. Özel Nitelikli Kişisel Veri (m.6)

**Kritik tespit**: Personel'in asıl amacı özel nitelikli veri işlemek değildir; ancak **ekran görüntüsü, ekran video klibi, pano içeriği ve klavye içeriği** kanalları, çalışanın dolaylı olarak özel nitelikli veri (sağlık bilgisi, din/inanç, cinsel hayat, sendikal üyelik, biyometrik/genetik veri) üretmesi hâlinde bu verileri **kazara** içerebilir.

KVKK m.6 altında özel nitelikli veri işleme için gereken hukuki sebepler sınırlıdır:
- Sağlık ve cinsel hayat dışındakiler: kanunlarda öngörülme **veya** açık rıza.
- Sağlık ve cinsel hayat: kamu sağlığı, koruyucu hekimlik vb. amaçlarla sır saklama yükümlülüğü altındaki kişiler **veya** açık rıza.

**Sonuç ve ürün gereksinimi**: Personel, özel nitelikli veriyi "işleme amacı" olarak hiçbir zaman seçmemelidir. Özel nitelikli verinin **kazara kaydı** durumunda:

1. **Otomatik redaksiyon / maskeleme filtreleri** — ekran görüntüsü alındığında sağlık uygulamaları (whitelisted paket adları: e-Nabız, HES, sağlık portalları) foreground'da iken ekran görüntüsü alınmamalıdır. Bu, ürün tarafında `screenshot_exclude_apps` politikası ile zorlanmalıdır (bkz. `docs/architecture/mvp-scope.md` — politika motoru).
2. **En kısa saklama süresi** — m.6 verisi yakalanması muhtemel kanallarda (ekran görüntüsü 30 gün, ekran video 14 gün, klavye içeriği 14 gün). Bkz. §7.
3. **Yükseltilmiş erişim kontrolü** — klavye içeriğinin yöneticilerce okunması kriptografik olarak imkânsızdır (bkz. §10). Ekran görüntüsü erişimi RBAC ile "investigator" rolüne kısıtlanmalıdır.
4. **DLP kural setinde m.6 sinyal tespiti** — sağlık, din, sendika ile ilgili anahtar kelimeler tespit edildiğinde ilgili kayıt **DLP incidents** yerine **sensitive-data-flag** bayrağı ile ayrı bir işleme akışına alınmalı ve saklama süresi 7 güne düşürülmelidir. **[ÜRÜN BOŞLUĞU — bkz. §13 Gap Listesi.]**

**Açık rıza yokluğu hali**: Kazara yakalanan özel nitelikli veri için açık rıza alınmamışsa, bu veri hukuka aykırı işlenmiş sayılır. Bu nedenle **filtreleme + erken imha** stratejisi, m.6 ihlalini önlemenin asıl yoludur.

## 3. Rol Ayrımı: Veri Sorumlusu vs. Veri İşleyen

### 3.1. On-Prem Kurulumda Temel Konumlandırma

| Taraf | KVKK Sıfatı | Gerekçe |
|---|---|---|
| **Müşteri kurum** (Personel'i konuşlandıran şirket) | **Veri sorumlusu** (m.3/1-ı) | İşleme amaçlarını ve vasıtalarını belirler; veriyi kendi altyapısında tutar; çalışanlarıyla iş ilişkisi kurmuştur. |
| **Personel firması** (yazılım sağlayıcı) | **Yazılım sağlayıcı** — veri işleyen değil | Kişisel veri, Personel firmasının altyapısına hiçbir koşulda akmaz. Firma sadece lisanslı yazılım, destek ve güncelleme sağlar. Kişisel veriye erişmez, işleme amacını ve vasıtasını belirlemez. |

**Hukuki argüman**: KVKK m.3/1-ğ uyarınca "veri işleyen", "veri sorumlusunun verdiği yetkiye dayanarak onun adına kişisel verileri işleyen gerçek veya tüzel kişidir". Personel firması, müşteri verisine erişmediğinden "onun adına işleme" unsuru oluşmaz. Bu, SaaS ve on-prem arasındaki en önemli hukuki ayrımdır ve lisans sözleşmesinde açıkça belirtilmelidir.

### 3.2. Lisans Sözleşmesine Dahil Edilecek Madde (Örnek Metin)

> "Personel firmasının işbu sözleşme kapsamında sağladığı yazılım, Müşteri'nin kendi bilgi işlem altyapısında (on-prem) çalışır. Yazılımın toplayıp işlediği tüm kişisel veriler, başlangıcından itibaren Müşteri'nin kontrolündedir. Personel firması, söz konusu kişisel verilere erişmez, transferini almaz, işleme amacını veya vasıtasını belirlemez. Bu itibarla 6698 sayılı KVKK kapsamında Müşteri **veri sorumlusu**, Personel firması ise yalnızca yazılım sağlayıcı konumunda olup **veri işleyen sıfatını taşımaz**. Destek ve bakım hizmetlerinde Personel firma personelinin müşteri ortamına erişimi istisnai, yazılı ve denetime tabi olup bu durumda dahi kişisel verilere erişim teknik olarak engellenmiştir (bkz. Ek-A Teknik Tedbirler)."

### 3.3. Gelecek SaaS Geçişi İçin Uyarı

Faz 3+'ta yönetilen SaaS sunulduğunda bu konumlandırma değişir: Personel firması o aşamada **veri işleyen** sıfatı kazanır ve KVKK m.12/2 uyarınca yazılı **veri işleyen sözleşmesi** imzalanması zorunlu hâle gelir. Sözleşme unsurları: işleme amacı, süresi, kategorileri, veri sorumlusunun talimat hakkı, alt-işleyen izni, güvenlik tedbirleri, denetim hakkı, iade/imha yükümlülüğü, gizlilik.

## 4. Aydınlatma Yükümlülüğü (m.10)

Tam metin şablonu için bkz. `docs/compliance/aydinlatma-metni-template.md`.

Şablon aşağıdaki m.10 zorunlu unsurlarının tamamını içerir:
1. Veri sorumlusunun ve varsa temsilcisinin kimliği
2. Kişisel verilerin hangi amaçla işleneceği (spesifik — "performans analizi" gibi muğlak ifade kullanılmamalıdır)
3. İşlenen kişisel verilerin kimlere ve hangi amaçla aktarılabileceği (on-prem'de: "aktarılmamaktadır")
4. Kişisel veri toplamanın yöntemi ve hukuki sebebi
5. M.11'de sayılan haklar

**Teslim yöntemi** (mimari ile uyumlu):
- Ajan kurulum anında zorunlu tam ekran bildirim (bkz. `docs/architecture/live-view-protocol.md` — "Employee Notification Semantics").
- Şeffaflık Portalı üzerinden kalıcı erişim.
- Personel özlük dosyasına imzalı kopyanın eklenmesi (İK süreci).

## 5. İşleme Amacı ve Hukuki Sebep Matrisi

Aşağıdaki matris, `docs/architecture/event-taxonomy.md` içindeki 36 olay türünün tamamını amaç, hukuki sebep, saklama süresi ve silme yöntemi açısından haritalar.

| # | Olay Türü | Kategori | İşleme Amacı | Hukuki Sebep (m.5/6) | Saklama | Silme Yöntemi |
|---|---|---|---|---|---|---|
| 1 | `process.start` | BEHAVIORAL | BT güvenlik + performans denetimi | m.5/2-f, m.5/2-c | 90 gün | ClickHouse TTL |
| 2 | `process.stop` | BEHAVIORAL | BT güvenlik + performans denetimi | m.5/2-f, m.5/2-c | 90 gün | ClickHouse TTL |
| 3 | `process.foreground_change` | BEHAVIORAL | Performans denetimi | m.5/2-f, m.5/2-c | 90 gün | ClickHouse TTL |
| 4 | `window.title_changed` | CONTENT | Performans denetimi + DLP | m.5/2-f | 90 gün | ClickHouse TTL + OpenSearch rollover |
| 5 | `window.focus_lost` | BEHAVIORAL | Aktiflik hesabı | m.5/2-c | 30 gün | TTL |
| 6 | `session.idle_start` | BEHAVIORAL | Çalışma süresi hesabı | m.5/2-c | 90 gün | TTL |
| 7 | `session.idle_end` | BEHAVIORAL | Çalışma süresi hesabı | m.5/2-c | 90 gün | TTL |
| 8 | `session.lock` | IDENTIFIER | Güvenlik + devam denetimi | m.5/2-f, m.5/2-c | 90 gün | TTL |
| 9 | `session.unlock` | IDENTIFIER | Güvenlik + devam denetimi | m.5/2-f, m.5/2-c | 90 gün | TTL |
| 10 | `screenshot.captured` | SENSITIVE | Delil toplama + DLP | m.5/2-f (m.6 riski filtre ile azaltılır) | 30 gün | MinIO lifecycle |
| 11 | `screenclip.captured` | SENSITIVE | Olay incelemesi | m.5/2-f | 14 gün | MinIO lifecycle |
| 12 | `file.created` | CONTENT | Veri sızıntısı önleme | m.5/2-f | 180 gün | TTL |
| 13 | `file.read` | CONTENT | Erişim denetimi | m.5/2-f | 30 gün | TTL |
| 14 | `file.written` | CONTENT | Veri sızıntısı önleme | m.5/2-f | 180 gün | TTL |
| 15 | `file.deleted` | CONTENT | Veri kaybı önleme | m.5/2-f | 180 gün | TTL |
| 16 | `file.renamed` | CONTENT | DLP | m.5/2-f | 180 gün | TTL |
| 17 | `file.copied` | CONTENT | DLP | m.5/2-f | 180 gün | TTL |
| 18 | `clipboard.metadata` | BEHAVIORAL | DLP | m.5/2-f | 90 gün | TTL |
| 19 | `clipboard.content_encrypted` | SENSITIVE | DLP desen eşleme | m.5/2-f | 30 gün | Anahtar iptali + blob silme |
| 20 | `print.job_submitted` | CONTENT | Veri sızıntısı önleme | m.5/2-f | 180 gün | TTL |
| 21 | `usb.device_attached` | IDENTIFIER | BT güvenlik (donanım kontrolü) | m.5/2-f | 365 gün | TTL |
| 22 | `usb.device_removed` | IDENTIFIER | BT güvenlik | m.5/2-f | 365 gün | TTL |
| 23 | `usb.mass_storage_policy_block` | IDENTIFIER | Politika zorlaması delili | m.5/2-f | 365 gün | TTL |
| 24 | `network.flow_summary` | CONTENT | BT güvenlik + 5651 kısmi | m.5/2-f, m.5/2-ç | 60 gün | TTL |
| 25 | `network.dns_query` | CONTENT | BT güvenlik | m.5/2-f | 30 gün | TTL |
| 26 | `network.tls_sni` | CONTENT | BT güvenlik | m.5/2-f | 30 gün | TTL |
| 27 | `keystroke.window_stats` | BEHAVIORAL | Performans göstergesi | m.5/2-c, m.5/2-f | 90 gün | TTL |
| 28 | `keystroke.content_encrypted` | SENSITIVE | Yalnızca DLP (idari kontrol dışı) | m.5/2-f (teknik tedbirle kriptografik izolasyon) | 14 gün | Anahtar imhası (bkz. §10) |
| 29 | `app.blocked_by_policy` | BEHAVIORAL | Politika uygulama delili | m.5/2-f | 365 gün | TTL |
| 30 | `web.blocked_by_policy` | CONTENT | Politika uygulama delili | m.5/2-f | 365 gün | TTL |
| 31 | `agent.health_heartbeat` | NONE | Sistem sağlığı | — (kişisel veri değil) | 30 gün | TTL |
| 32 | `agent.policy_applied` | NONE | Uyum kaydı | — | 2 yıl | TTL |
| 33 | `agent.update_installed` | NONE | Versiyon takibi | — | 2 yıl | TTL |
| 34 | `agent.tamper_detected` | IDENTIFIER | Güvenlik olayı | m.5/2-f | 3 yıl | Scheduled purge |
| 35 | `live_view.started` | IDENTIFIER (denetim) | Hesap verebilirlik (m.12) | m.5/2-f, m.12 | 5 yıl | Append-only; arşivleme |
| 36 | `live_view.stopped` | IDENTIFIER (denetim) | Hesap verebilirlik (m.12) | m.5/2-f, m.12 | 5 yıl | Append-only; arşivleme |

**Matrisin KVKK kriterleri açısından doğrulanması**: m.4/2-ç uyarınca "işlendikleri amaçla bağlantılı, sınırlı ve ölçülü olma" ilkesi; yukarıdaki saklama sürelerinin seçiminde temel kriter olarak uygulanmıştır. En hassas üç kategori (klavye içeriği, ekran video, ekran görüntüsü) en kısa sürelerdedir.

## 6. Özel Nitelikli Kişisel Veri (m.6) Uygulama Kuralları

1. **İşleme amacı seti**: m.6 verisi Personel'de hiçbir zaman "hedeflenen" veri kategorisi olmayacaktır. VERBİS kaydında "özel nitelikli veri" kategorisi **işaretlenmemelidir**; müşteri DPO'su m.6 verisinin kazara toplanma riskini "kabul edilen kalıntı risk" olarak DPIA'da belgelemelidir.
2. **Filtreleme (preventive)**:
   - Ekran görüntüsü/video için **işlem bazlı istisnalar** (sağlık uygulamaları, banka uygulamaları, HR self-service portalları — varsayılan exclusion list).
   - Pencere başlığı filtresi: düzenli ifade tabanlı (`(?i)(sağlık|hastane|e-nabız|reçete|HIV|gebelik|...)`) tetiklemesinde olay "özel_nitelikli_potansiyel" bayrağı ile işaretlenir.
3. **Erken imha (reactive)**: özel_nitelikli_potansiyel bayraklı kayıtlar varsayılandan %50 daha kısa tutulur (ekran görüntüsü için 15 gün, klavye içeriği için 7 gün).
4. **Yükseltilmiş erişim**: m.6 olası kayıtlara erişim yalnızca "DPO" ve "Legal Investigator" rollerine açıktır; her erişim ikinci bir onay gerektirir (dört göz ilkesi) ve denetim zincirine hash'li olarak yazılır.
5. **Klavye içeriği istisna**: klavye içeriği zaten kriptografik olarak yöneticilerin erişimine kapalıdır (§10). Burada m.6 riski, DLP kural seti tarafından özel nitelikli desen yakalanırsa ortaya çıkar; bu durumda DLP `dlp.match` metadata'sında yalnızca rule_id ve redakte edilmiş snippet görünür, ham metin asla görülmez.
6. **Açık rıza alma**: meşru menfaat m.6 için yetmez; dolayısıyla m.6 olasılıklı kanalları tamamen devre dışı bırakma seçeneği politika motorunda sunulmalıdır. **[ÜRÜN BOŞLUĞU — §13]**

## 7. Veri Sahibi Hakları (m.11) Uygulaması

KVKK m.11 kapsamındaki her hakkın ürün üzerinde nasıl karşılandığı aşağıda haritalanmıştır. Birincil arayüz **Şeffaflık Portalı**'dır.

| # | m.11 Hakkı | Ürün Özelliği | Kaynak |
|---|---|---|---|
| a | Kişisel verinin işlenip işlenmediğini öğrenme | Portalda "Verilerim" sayfası; her çalışan kendi endpoint kayıtlarının varlığını görür | Portal |
| b | İşlenmişse buna ilişkin bilgi talep etme | "İşleme Bilgileri" ekranı: amaç, hukuki sebep, kategori, süre | Portal |
| c | İşleme amacına uygun kullanılıp kullanılmadığını öğrenme | Audit log türetilmiş "amaç dışı kullanım yoktur" raporu (DPO imzalı) | Portal + DPO |
| ç | Yurt içi/yurt dışı aktarılan üçüncü kişileri bilme | Portal: "Aktarım yapılmamaktadır — veriler kurumunuzun kendi altyapısında tutulmaktadır" | Portal |
| d | Eksik/yanlış işlenmiş olması halinde düzeltme | Portal: düzeltme talebi formu → DPO workflow | Portal + Admin Console |
| e | M.7'de öngörülen şartlar çerçevesinde silme/yok etme | Portal: silme talebi formu → meşru menfaat dengesi incelemesi → silme iş emri | Portal + Policy Engine |
| f | (d) ve (e) bentleri kapsamında yapılan işlemlerin aktarıldığı kişilere bildirilmesi | On-prem aktarım olmadığından uygulanabilir değildir; durum portalda belirtilir | Portal |
| g | İşlenen verinin münhasıran otomatik sistemlerce analiz edilmesi suretiyle aleyhine bir sonucun ortaya çıkması hâlinde buna itiraz etme | Portal: itiraz formu. Faz 1'de otomatik karar mekanizması yoktur (Phase 2 anomaly detection geldiğinde bu hak aktifleşir). | Portal |
| ğ | Kanuna aykırı işleme nedeniyle uğradığı zararın giderilmesini talep etme | Portal: şikayet formu → hukuk departmanına yönlendirilir | Portal + harici süreç |

**m.13 süre kuralı**: Başvuru tarihinden itibaren en geç 30 gün içinde yanıt. Portal, başvuru açıldığında müşteri DPO'sunun e-postasına eşzamanlı bildirim gönderir ve SLA sayacını başlatır. DPO 30 gün sonunda yanıtlamamışsa dashboard'da kırmızı uyarı üretilir.

## 8. Saklama ve İmha (m.7)

### 8.1. KVKK m.7 Üç Yöntem Ayrımı

| Yöntem | Tanım (Yönetmelik) | Personel'de Uygulama |
|---|---|---|
| **Silme** | Verinin ilgili kullanıcılar için hiçbir şekilde erişilemez ve tekrar kullanılamaz hale getirilmesi | ClickHouse TTL, PostgreSQL scheduled DELETE |
| **Yok etme** | Verinin hiç kimse tarafından hiçbir şekilde erişilemez ve tekrar kullanılamaz hale getirilmesi | Kriptografik anahtar imhası + ciphertext blob silme (klavye içeriği, pano içeriği) |
| **Anonim hale getirme** | Veriye geri döndürülemez biçimde kimlik bilgileri çıkarılması | Rapor agregasyonları (k≥5 eşiği) |

### 8.2. Kriptografik İmha = "Yok Etme" Savunması

Personel'in klavye/pano içeriği blob'ları için uyguladığı imha yöntemi şudur (bkz. `docs/architecture/key-hierarchy.md`, "Destruction = Key Destruction"):

1. TMK versiyonu Vault transit üzerinde planlı imhaya alınır.
2. PE-DEK wrap kaydı PostgreSQL'den silinir.
3. MinIO lifecycle ciphertext blob'unu siler.

**Bu yöntem KVKK m.7 ve Yönetmelik m.10 anlamında "yok etme" olarak savunulabilir mi?**

Evet. Argüman: Kişisel Veri Saklama ve İmha Yönetmeliği m.10/2-a, "kişisel verilerin depolandığı tüm kopyaların tespit edilmesi" ve m.11'de imha yönteminin "kişisel veriye yeniden erişilebilirliği imkânsız kılacak" nitelikte olmasını ister. AES-256-GCM ile şifrelenmiş ve anahtarı yok edilmiş bir ciphertext, mevcut kriptografik varsayımlar altında **pratik olarak geri getirilemez**; Kurul'un Güvenlik Rehberi de güçlü kriptografi ile anahtar imhasını geçerli bir tahrip yöntemi olarak kabul eder. **[DOĞRULANMALI — Kurul'un "Kişisel Veri Saklama ve İmha Yönetmeliği" rehberinde kriptografik imha ifadesinin spesifik paragrafı hukuk müşavirince doğrulanmalıdır.]**

**Çift katmanlı savunma**: Personel hem anahtarı imha eder hem de ciphertext blob'u siler. Bu, "yedek medyada bile geri getirilemez" şartını karşılar.

### 8.3. Periyodik İmha Takvimi

Yönetmelik m.11 uyarınca periyodik imha **6 ayı geçmemek kaydıyla** yapılmalıdır. Personel otomatik TTL ile günlük çalışır, ancak uyum belgelenmesi için **6 aylık formal imha raporu** DPO tarafından üretilir (Admin Console → "İmha Raporları" ekranı). Rapor içeriği: silinen kayıt sayısı, anahtar imha olayları, manuel istisnalar, legal hold durumları.

### 8.4. Legal Hold

Devam eden bir disiplin soruşturması, iş mahkemesi davası veya Kurul/Savcılık talebinde ilgili kayıtlara "legal hold" bayrağı konulur, TTL atlanır, durum hash-zincirli audit log'a yazılır. Legal hold kaldırıldığında normal imha sürecine geri döner.

## 9. Teknik ve İdari Tedbirler (m.12)

Aşağıdaki tablo, Kurul'un "Kişisel Veri Güvenliği Rehberi (Teknik ve İdari Tedbirler)" dokümanındaki kategorilerle Personel'in somut teknik kontrollerini eşleştirir. Müşteri DPO'su bu tabloyu VERBİS "Alınan İdari ve Teknik Tedbirler" bölümüne doğrudan kopyalayabilir.

### 9.1. Teknik Tedbirler

| Kurul Rehberi Kategorisi | Personel Uygulaması | Kaynak |
|---|---|---|
| **Yetki matrisi** | RBAC: Admin, Manager, HR, DPO, Investigator, Auditor rolleri; PostgreSQL row-level security | `threat-model.md` Flow 2 |
| **Erişim logları** | Hash-zincirli append-only audit (5 yıl); PostgreSQL `audit_records` tablosu | `live-view-protocol.md` §Audit Hash Chain |
| **Yetki kontrol** | Sunucu tarafında her endpoint'te rol doğrulaması | `threat-model.md` Flow 2 |
| **Kullanıcı hesap yönetimi** | LDAP/AD entegrasyonu, hesap kilitleme, parola politikası, oturum zaman aşımı | `mvp-scope.md` |
| **Ağ güvenliği** | mTLS 1.3, sertifika sabitleme, CA per-tenant | `threat-model.md` Flow 1 |
| **Uygulama güvenliği** | gRPC proto validasyon, input sanitizasyon, CSP, SameSite cookies | `threat-model.md` Flow 2 |
| **Şifreleme** | AES-256-GCM (keystroke, clipboard); TLS 1.3 (wire); disk şifreleme (LUKS) | `key-hierarchy.md` |
| **Sızma testi** | Faz 1 exit criterion #9: bağımsız kırmızı takım testi | `mvp-scope.md` |
| **Bilgi teknolojileri sistemleri tedariki, geliştirme ve bakımı** | SBOM, imzalı artefakt zinciri, reproducible builds hedefi | `threat-model.md` Flow 4 |
| **Kişisel veri yedekleme** | Vault Shamir 3-of-5, pg_basebackup (şifreli), clickhouse-backup, MinIO mc mirror | `mvp-scope.md` |
| **Anahtar yönetimi** | HashiCorp Vault, transit engine, HSM-backed seal key opsiyonu, rotasyon | `key-hierarchy.md` |
| **Veri maskeleme** | DLP match snippet redaksiyonu; m.6 potansiyeli flagging | `key-hierarchy.md` |
| **Saldırı tespit ve önleme** | Agent tamper detection, gateway rate limit, Vault audit alerting | `threat-model.md` |
| **Log kayıtları** | 5 yıl hash-zincirli audit; Prometheus metrikler; OpenSearch | `overview.md` |
| **Güncel antivirüs / zafiyet yönetimi** | Müşteri sorumluluğu; kurulum gereksinimi dokümante edilir | Runbook |
| **Silme, yok etme, anonim hale getirme** | TTL + Vault key destroy + MinIO lifecycle | `data-retention-matrix.md` |

### 9.2. İdari Tedbirler

| Kurul Rehberi Kategorisi | Müşteri Sorumluluğu (Personel'in desteği) |
|---|---|
| **Kişisel veri işleme envanteri** | Personel, VERBİS şablonu sağlar (bkz. `verbis-kayit-rehberi.md`) |
| **Kurumsal politikalar** | Personel, örnek politika şablonları sağlar (aydınlatma, açık rıza, saklama-imha) |
| **Sözleşmeler** | Personel, lisans sözleşmesinde veri sorumlusu/yazılım sağlayıcı ayrımını netleştirir (§3.2) |
| **Gizlilik taahhütnamesi** | Müşteri, BT personeli için gizlilik sözleşmesi imzalatır |
| **Kurum içi periyodik denetim** | Personel, DPO denetim dashboard'u sağlar |
| **Risk analizi** | Personel, DPIA şablonu sağlar (`dpia-sablonu.md`) |
| **İş sözleşmesi, disiplin yönetmeliği** | Müşteri İK sorumluluğu; Personel örnek maddeler sağlar |
| **Kurumsal iletişim (kriz / ihlal)** | Personel, ihlal bildirim runbook'u sağlar |
| **Eğitim ve farkındalık** | Müşteri eğitim programı; Personel DPO eğitim materyali sağlar |
| **Veri sorumluları sicil bilgi sistemi (VERBİS)** | Personel, kayıt rehberi sağlar |

## 10. Çalışan Gizliliğinin Kriptografik Garantisi

Bu bölüm Personel'in en güçlü hukuki farklılaştırıcısını inceler: **klavye içeriğinin yöneticiler tarafından teknik olarak okunamaması**.

### 10.1. Mimari Gerçek (özet)

`docs/architecture/key-hierarchy.md` tam detayı içerir. Özet:

- Klavye içeriği uç noktada AES-256-GCM ile **PE-DEK** (Per-Endpoint Data Encryption Key) kullanılarak şifrelenir.
- PE-DEK, DLP servisine ait **DSEK** ile wrap'lenir. DSEK ise **TMK** (Tenant Master Key)'den HKDF ile türer.
- TMK, Vault transit engine'de **exportable: false** olarak tutulur; yalnızca `dlp-service` AppRole'ü derive edebilir.
- Admin Console ve Admin API, Vault'ta TMK'ya erişim hakkına **sahip değildir**.
- DLP servisi izole süreçte (ayrı host, seccomp, ptrace kapalı) çalışır; dışarıya yalnızca `dlp.match` meta verisi üretir.
- Match meta verisinde redakte edilmiş snippet (`TCKN: ***`) dışında ham içerik yoktur.
- CI linter, proto ve koda `keystroke` + `plaintext` kombinasyonu eklenmesini engeller.

### 10.2. Hukuki Çeviri (KVKK terminolojisinde) — İki Katmanlı İddia

**ADR 0013 (2026-04-11) sonrasında, Personel'in beyan edilebilir hukuki iddiası iki katmana ayrılır**; çünkü DLP servisi artık varsayılan olarak kapalıdır ve müşterinin kayıtlı opt-in seremonisiyle açılır. Her iki durumda kriptografik yapı aynıdır; farklılık, belirli bir anda herhangi bir sürecin TMK → DSEK → PE-DEK → ham içerik yolunu yürümek için **yetkili olup olmadığındadır**.

**Katman 1 — Varsayılan Durum Beyanı (DLP kapalı)**

> "Bu kurulumda hiçbir süreç klavye içeriği şifre çözme anahtarına sahip değildir ve hiçbir zaman sahip olmamıştır. Vault audit kayıtları, kurulumun ömrü boyunca `transit/keys/tenant/*/tmk` üzerinde **sıfır derive çağrısı** olduğunu gösterir. `dlp-service` AppRole Secret ID'si hiç düzenlenmemiştir. Yönetici erişimi, bir politika kısıtlaması olarak değil, **var olmayan bir şifre çözme yolu olduğu için matematiksel olarak imkânsızdır**."
>
> Bu iddia, varlığıyla ispatlanan bir olgudur (Vault audit device'ının sıfır kayıt göstermesi pozitif kanıttır), yokluk argümanı değildir.

**Katman 2 — Opt-In Sonrası Durum Beyanı (DLP açık)**

> "Yalnızca izole DLP motoru, önceden tanımlanmış desenlere karşı eşleşme amacıyla klavye içeriğini çözebilir. Yöneticiler ham içeriği okuyamaz; bu, kriptografik yapıyla zorlanır (by construction). DLP motoru yalnızca `dlp.match` meta verisi üretir — kural adı, önem derecesi ve redakte edilmiş bir snippet (`TCKN: ***`)."
>
> Bu iddia, Faz 0 revizyon turunda netleşen şekliyle korunur: DLP Dağıtım Profili 1 veya Profil 2 altında geçerlidir (bkz. §10.4). Ek olarak, müşterinin imzalı opt-in seremonisi bir **uyum artefaktı** olarak kayda geçer ve hukuki müdafaayı güçlendirir (bkz. §10.5).

**KVKK m.12 çerçevesinde konumlandırma**: Katman 1, m.12 "teknik tedbir" yükümlülüğünün en güçlü biçimidir — şifre çözme anahtarının var olmaması, bir erişim politikasından daha güçlü bir teknik tedbirdir. Kurul'un Güvenlik Rehberi'nin "gereklilik ilkesine uygun erişim" ve "amaç dışı erişimin engellenmesi" başlıkları, varsayılan olarak "erişim olanağının hiç yaratılmaması" ile tatmin edilir. Katman 2 ise, müşteri opt-in sonrası için aynı teknik tedbirin **uygulama yoluyla** (policy + isolation + audit) devam ettiğini gösterir.

### 10.3. Mahkemede Savunulabilirlik

Bir iş mahkemesinde veya Kurul denetiminde çalışan "tuş vuruşlarım okundu" iddiasında bulunursa, Personel'in sunduğu savunma **§10.2'deki iki katmanlı iddiaya göre** yapılandırılır:

**Eğer DLP o dönemde kapalıysa (ADR 0013 varsayılan durumu)**:

1. **Vault audit kayıtları (güçlü delil)**: Kurulumun ömrü boyunca `transit/keys/tenant/*/tmk` üzerinde sıfır `derive` çağrısı gösterilir. `dlp-service` AppRole Secret ID'sinin hiç düzenlenmediği kanıtlanır.
2. **Audit chain içinde hiç `dlp.enabled` olayı olmaması**: Append-only hash-zincirli log ve harici WORM sink birlikte, seremoninin hiç yapılmadığını değiştirilemez biçimde kanıtlar.
3. **Şeffaflık Portalı kayıtları**: Çalışanlara gösterilen herhangi bir "DLP aktif edildi" banner'ı olmaması.
4. **Matematiksel argüman**: Şifre çözme anahtarı hiç var olmadığı için okuma fiziksel olarak imkânsızdır.

**Eğer DLP opt-in sonrasında çalışıyorduysa**:

1. **Vault audit kayıtları**: TMK derive işlemlerinin tamamı loglanır; bir dönem için hiçbir derive olmaması = o dönem hiçbir içeriğin çözülmediği.
2. **RBAC kanıtı**: Yönetici rollerinin Vault politikası, kişinin derive hakkı bulunmadığını kanıtlar.
3. **Kaynak kodu incelemesi**: Proto dosyalarında `plaintext_keystroke` alanı yoktur; CI linter kuralı bu yapısal değişikliği engeller.
4. **Red team raporu**: Faz 1 exit criterion #9 uyarınca bağımsız kırmızı takım, yöneticinin içeriği okuyamayacağını doğrulayan bir rapor üretir. Bu rapor mahkemeye sunulur.
5. **Opt-in artefaktları**: §10.5'te tanımlanan imzalı DPIA amendment, imzalı opt-in formu, `dlp.enabled` audit chain olayı ve müşteri Şeffaflık Portalı banner loglaması, müşterinin kendi m.12 hesap verebilirliğini destekler.

Bu savunma, standart UAM ürünlerinin sunamadığı bir kriptografik güvencedir ve Personel'in "çalışan gizliliğine en az zarar veren UAM" pozisyonunu hukuki olarak destekler.

### 10.4. Sınırlar (Dürüst Açıklama) ve DLP Dağıtım Profilleri

Beyan edilebilir hukuki iddia (§10.2 sonunda yer alan "by construction" ifadesi), Personel'in DLP servisinin **nasıl dağıtıldığına** bağlıdır. Phase 0/1 mimari revizyonunda compliance-auditor ve security-engineer uzmanları iki profil üzerinde anlaşmıştır (tam ayrıntı: `docs/architecture/dlp-deployment-profiles.md`):

**Profil 1 — Sertleştirilmiş Konteyner (Faz 1 pilot varsayılanı)**

- DLP servisi, uygulama host'u üzerinde distroless, read-only, seccomp+AppArmor sınırlı, `mlockall`'lu, nftables egress kısıtlı bir konteyner olarak çalışır.
- Sertleştirme kontrolleri **non-negotiable** olarak `dlp-deployment-profiles.md` §Profile 1'de listelenmiştir ve Faz 1 sürüm kapısı bunları doğrular.
- **Hukuki iddia (Profil 1)**: "Yöneticiler, standart dağıtımın numaralandırılmış ve denetlenebilir sertleştirme varsayımları altında klavye içeriğini okuyamaz." Bu, konteynerin paylaşılan host üzerinde çalıştığı gerçeğini dürüstçe kabul eder; kontroller saldırının maliyetini, tespit edilebilirliğini ve denetlenebilirliğini yükseltir, ama fiziksel ayrımı yoktur.
- **Uygun müşteri profili**: Pilot, KOBİ, orta ölçekli kurumsal; Banka/kamu/regüle sektör değil.
- **Sözleşme eki**: Müşteri DPO'su Profil 1 sözleşme ekini kuruluma imzalar; ek, VERBİS kaydında referans verilir.

**Profil 2 — Dedike Host (Faz 2 GA varsayılanı, sıkı müşteriler için zorunlu)**

- DLP servisi, Personel stack'inin başka hiçbir bileşenini barındırmayan, müşteri DPO'su tarafından "yüksek kıymetli varlık" olarak sınıflanan bir dedike VM veya fiziksel host üzerinde çalışır.
- SSH erişimi varsayılan kapalıdır; break-glass müşteri PAM ile kayıt altındadır. HIDS, FIM, ayrık operatör kimlikleri uygulanır.
- **Hukuki iddia (Profil 2)**: "Yöneticiler klavye içeriğini **inşa yoluyla (by construction)** okuyamaz. Şifre çözme ortamı, yönetici düzleminden fiziksel olarak ayrılmış, ayrı operatör kontrolünde bir hardlenmiş host'tur." Compliance-auditor'un banka/kamu müşterileri için aradığı ifade budur.
- **Uygun müşteri profili**: Bankacılık, kamu, sağlık, büyük kurumsal; 2 000+ uç nokta.

**Profil seçimi nasıl karar verilir**:

| Müşteri Özelliği | Önerilen Profil |
|---|---|
| Pilot (≤ 500 uç nokta) | Profil 1 |
| KOBİ, tek tenant, regüle değil | Profil 1 (kabul edilebilir) veya Profil 2 (tercih edilebilir) |
| Regüle sektör (banka, kamu, sağlık) | **Profil 2 zorunlu** (Faz 2'den itibaren) |
| 2 000+ uç nokta | **Profil 2 zorunlu** |
| Faz 2 GA sonrası yeni kontrat | Varsayılan Profil 2; müşteri Profil 1'i yazılı gerekçe ile seçebilir |

**Her iki profilde geçerli kalan diğer sınırlar**:

- **Uç nokta tamamen ele geçirildiyse**: O uç noktadaki çalışanın o an yazdığı klavye içeriği, o noktadaki saldırgan için açıktır. Bu bir ürün hatası değil, tehdit modeli sınırıdır.
- **DLP kural seti kötüye kullanılabilir**: Eğer saldırgan "her şeyi eşleştir" kuralı eklerse, `dlp.match` ham içeriği sızdırabilir. Bu nedenle DLP kural değişikliği DPO onayı gerektirir ve denetlenir (bkz. `threat-model.md` Flow 5).
- **Vault root token'ı ele geçirildiyse**: Teorik olarak TMK derive politikası oluşturulabilir. Bu nedenle Vault unseal Shamir 3/5'tir ve kök token normal operasyonda mevcut değildir; break-glass prosedürü denetlenir (bkz. `security/runbooks/vault-setup.md`).

Her iki profilde de Faz 1 exit criterion #9 (bağımsız kırmızı takım) yöneticinin klavye içeriğini okuyamadığını doğrular; Profil 2'de ek olarak host ayrımı da kontrol edilir.

### 10.5. Opt-In Seremonisi Uyum Artefaktı Olarak (ADR 0013)

ADR 0013 ile birlikte DLP artık varsayılan olarak kapalıdır. Müşteri DLP'yi etkinleştirmek için belgelenmiş ve denetlenebilir bir **opt-in seremonisi** uygular. Bu seremoninin kendisi, KVKK savunulabilirliği açısından bir **uyum artefaktıdır** ve müşterinin kendi hesap verebilirlik (m.12/m.4) konumunu güçlendirir. Aşağıda adım adım ve hukuki anlamı verilmiştir.

**Seremoni adımları ve uyum kazanımları**:

| Adım | İşlem | KVKK karşılığı |
|---|---|---|
| 1 | **DPIA güncellemesi**: Müşteri DPO, mevcut Veri Koruma Etki Değerlendirmesi (DPIA) dosyasını aktif DLP işlemeyi kapsayacak şekilde günceller ve hukuk müşavirine imzalattırır. | m.4 ölçülülük, m.12 hesap verebilirlik. DPIA, özel nitelikli veri işlemede Kurul denetiminin ilk baktığı belgedir. |
| 2 | **İmzalı opt-in formu**: DPO + BT Güvenlik Direktörü + Hukuk Müşaviri, `docs/compliance/dlp-opt-in-form.md` şablonunu (compliance-auditor tarafından ayrı olarak oluşturulacak) tek sayfa form olarak imzalar. Form, işlemenin amacı, hukuki sebebi, süresi ve çalışan bilgilendirme yöntemini açıkça belirtir. | m.5 hukuki sebep belgelenmesi; m.10 aydınlatmanın kapsamının genişlediğinin kayıtlanması. |
| 3 | **Yerel arşivleme**: İmzalı PDF, müşteri sunucusunda `/var/lib/personel/dlp/opt-in-signed.pdf` yoluna yerleştirilir. | Belgenin operasyonel olarak erişilebilir olması; Kurul denetiminde anında sunulabilir olması. |
| 4 | **Yetkili operatör eylemi**: Personel operatörü (veya müşterinin yetkili BT'si) `infra/scripts/dlp-enable.sh` komutunu çalıştırır. Script: (a) imzalı dosyanın varlığını ve hash'ini doğrular, (b) Vault'ta tek kullanımlık AppRole Secret ID düzenler, (c) `dlp.enabled` olayını hash-zincirli denetim loguna yazar ve imzalı form hash'ini içerir, (d) DLP konteynerini başlatır, (e) uçtan uca doğrulama gerçekleştirir. | m.12 teknik+idari tedbir. Teknik eylemin hukuki belgeyle bağlanması, Kurul denetiminde "her şey belgelendi" savunmasını güçlendirir. |
| 5 | **Şeffaflık bildirimi**: Çalışan Şeffaflık Portalı, otomatik olarak `DLP aktif edildi: <tarih>` başlıklı bir banner gösterir. Banner'ın gösterildiği tarih, m.10 aydınlatmanın genişletilmiş kapsamının çalışana tebliğ tarihi olarak kabul edilir. | m.10 aydınlatma yükümlülüğü; aydınlatmanın güncellenmesi KVKK gereğidir. |
| 6 | **Audit checkpoint**: `dlp.enabled` olayı, bir sonraki günlük hash-zincirli audit checkpoint'e dahil edilir ve harici WORM sink'e taşınır (`docs/architecture/audit-chain-checkpoints.md`). | m.12 hesap verebilirlik; değiştirilemez delil. |

**Opt-out aynı seviyede belgelenir**: `infra/scripts/dlp-disable.sh` çalıştırıldığında Secret ID iptal edilir, konteyner durdurulur, `dlp.disabled` olayı audit'e yazılır, Portal banner'ı güncellenir.

**Çalışanın hukuki pozisyonuna etkisi**:

Bir çalışan bir iş mahkemesinde veya Kurul başvurusunda "klavye içeriğim izinsiz okundu" iddiasında bulunursa, müşteri DPO aşağıdaki savunmayı sunabilir:

1. **Opt-in öncesi dönem için**: "Bu dönemde DLP sistemi kapalıydı. Vault audit device, belirtilen tarihten önceki herhangi bir `derive` çağrısını göstermez. Hiçbir süreç şifre çözme anahtarına sahip olmamıştır. Bu, bir erişim kontrolü değil, var olmayan bir yoldur."
2. **Opt-in sonrası dönem için**: "DLP, imzalı DPIA amendment'ı, imzalı opt-in formu ve audit chain'e yazılan tarih ile aktive edildi. Çalışan bu tarihte Şeffaflık Portalı üzerinden bilgilendirildi (banner gösterimi loglanmıştır). Opt-in sonrası tüm erişimler yalnızca DLP motoru tarafından, yalnızca pattern matching amacıyla gerçekleşti ve `dlp.match` metadata'sında redakte edilmiş snippet dışında ham içerik hiçbir süreç tarafından saklanmadı veya sunulmadı."

**Müşteri için marketing ve sözleşme dili**:

- **Sözleşme eki (contract addendum)**: İki adet ek şablonu sağlanır — (a) "DLP varsayılan kapalı" ek (müşteri imzalar, opt-in yapmamayı seçer), (b) "DLP opt-in sonrası" ek (opt-in seremonisinin tamamlanmasının ardından geçerli olur). Her ikisi de VERBİS kaydına referans verilebilir.
- **Pazarlama ifadesi**: "Varsayılan olarak keystroke-blind olan tek KVKK-uyumlu UAM" — bu ifade, §10.2 Katman 1 iddiasına dayanır ve Vault audit ile matematiksel olarak kanıtlanabilir.

## 11. Canlı İzleme Hukuki Çerçeve

### 11.1. Anayasa m.20 ve KVKK Bağlamı

Anayasa m.20, özel hayatın gizliliğini ve kişisel verilerin korunmasını temel hak olarak tanımlar. Canlı ekran izleme, çalışanın fiziksel ve dijital çalışma alanına en "araya giren" müdahale biçimidir; bu nedenle en yüksek hukuki duyarlılığı hak eder.

### 11.2. Savunulabilirlik Argümanı

Personel'in canlı izleme tasarımı (bkz. `docs/architecture/live-view-protocol.md`) dört unsurla savunulur:

| # | Güvence | KVKK/Anayasa Bağlantısı |
|---|---|---|
| 1 | **Aydınlatma metninde ve kurulum sırasında önceden bildirim** (bkz. `aydinlatma-metni-template.md`) | m.10 aydınlatma; Anayasa m.20 öngörülebilirlik |
| 2 | **HR onay kapısı (dual control, `approver ≠ requester`)** | m.12 idari tedbir; amaç dışı kullanım kontrolü |
| 3 | **Hash-zincirli append-only audit** | m.12 hesap verebilirlik; ihlal halinde delil |
| 4 | **Amaçla sınırlılık: reason code zorunluluğu (ticket/investigation id)** | m.4/2-c amaç sınırlılığı |
| 5 | **Zaman sınırı (varsayılan 15 dk, max 60 dk); her uzatma yeni onay** | m.4/2-ç ölçülülük |
| 6 | **Kayıt dışı** (Faz 1'de disk'e kayıt yok; Faz 2'de ADR 0012 çerçevesinde LVMK ile şifreli, çift onaylı oynatma, DPO-only export) | m.4/2-ç ölçülülük |
| 7 | **Çalışan geçmiş görünürlüğü (default ON)** — Şeffaflık Portalı'nda çalışan kendini hedef alan canlı izleme oturumlarını görebilir; DPO'nun kapatması audit edilir | m.10 aydınlatma; m.12 hesap verebilirlik |

### 11.3. "Sessiz Mod" (No-In-Session Indicator) Savunması

Canlı izleme sırasında ekranda/tepside bir gösterge bulunmaması, hukuken tartışılabilir bir tasarım seçimidir. Savunması şudur:

1. **Aydınlatma zamanı (ex ante)**: Çalışana önceden "canlı izleme mümkündür" bildirildi; yani çalışanın "her an izlenebilirim" beklentisi zaten mevcut.
2. **Süreç bütünlüğü**: Eğer oturum sırasında gösterge verilirse, soruşturma hedefi olan çalışan davranışını değiştirir ve güvenlik soruşturmasının amacı boşa çıkar. Bu, İş K. m.25/II-e ve ilgili yargı kararlarında tanınan bir durumdur.
3. **Telafi**: Şeffaflık Portalı'nda geçmiş oturum listesi **varsayılan olarak AÇIK**tır (Faz 0 revizyonu — compliance-auditor gereği değiştirildi). Çalışan kendini hedef alan canlı izleme oturumlarını görür; her kapatma denetim loguna yazılır ve çalışan tek seferlik bildirim alır.

**[DOĞRULANMALI]**: "Sessiz canlı izleme" meselesi Türk iş hukukunda henüz tam yerleşmiş bir içtihat oluşturmamıştır. Müşteri hukuk müşavirinin, çalışan grubu ve sektöre göre özel değerlendirme yapması tavsiye edilir.

### 11.4. Sendikalı İşyerleri

İş K. m.26 ve sendikal hükümler çerçevesinde, canlı izleme özelliği sendika ile imzalanan toplu iş sözleşmesine bir "monitoring protokolü" eklenmesi halinde ek hukuki koruma sağlar. Müşteri İK'sı, sendika ile önceden istişare yapmalıdır. Kurul, monitoring uygulamalarında işçi temsilcileriyle istişareyi iyi uygulama olarak önerir.

## 12. Veri İhlali Bildirim Süreci

### 12.1. KVKK m.12/5 Yükümlülüğü

Veri ihlali halinde veri sorumlusu, **en kısa sürede ve 72 saati geçmemek üzere** Kurul'a bildirmekle yükümlüdür. Bildirim, Kurul'un "Kişisel Veri İhlali Bildirim Formu" ile yapılır.

### 12.2. Personel'in Destek Özellikleri

- **Hash-zincirli audit log**: İhlalin zamanı, kapsamı ve etkilenen kayıt sayısı forensik olarak belirlenebilir.
- **Forensic export**: Admin Console → "İhlal Soruşturması" ekranı; seçili zaman aralığında tüm admin eylemleri, erişim kayıtları ve veri akışı özetini CSV/PDF dışa aktarır.
- **Etkilenen veri sahibi sayısı hesaplaması**: endpoint_id → kullanıcı haritalaması üzerinden otomatik sayım.
- **İhlal runbook**: Personel, `docs/security/runbooks/incident-response-playbook.md` ile adım adım ihlal müdahale rehberi sağlar.

### 12.3. Müşteri DPO Görevleri (T+0 → T+72h)

| Saat | Görev |
|---|---|
| T+0 | Tespit; DPO'ya bildirim (otomatik alert) |
| T+0 → T+4 | Kapsam belirleme (Personel forensic export) |
| T+4 → T+24 | Ölçeklendirme değerlendirmesi, üst yönetim bilgilendirme |
| T+24 → T+48 | Etkilenen kişilere bildirim hazırlığı |
| T+48 → T+72 | Kurul bildirimi (online form); etkilenen kişilere tebligat |
| T+72+ | Kök neden analizi, azaltım, tekrarı önleme |

## 13. Ürün Tasarımında Tespit Edilen KVKK Uyum Boşlukları (Architect'e geri bildirilecek)

Bu çalışma sırasında mevcut mimariye eklenmesi önerilen KVKK uyum unsurları ve **Faz 0 revizyon turunda architect tarafından verilen çözümler**:

1. **(KRİTİK — ÇÖZÜLDÜ)** m.6 filtre politika motoru girişi: `proto/personel/v1/policy.proto` içinde `SensitivityGuard` mesajı (`window_title_sensitive_regex`, `sensitive_host_globs`, `sensitive_retention_days_override`, `auto_flag_on_m6_dlp_match`) ve `ScreenshotSettings.exclude_apps` alanları eklendi. `docs/architecture/agent-module-architecture.md` "Sensitivity Guard" modülü eklendi. `docs/architecture/mvp-scope.md` Phase 1 IN scope'una alındı.
2. **(YÜKSEK — ÇÖZÜLDÜ)** Özel nitelikli bayraklı kayıtlar için ayrı retention seti: `docs/architecture/data-retention-matrix.md` "Hassas-İşaretli (Sensitive-Flagged) Saklama Bucket'ı" bölümü eklendi — ekran görüntüsü 7 gün, pencere başlığı 15 gün vs. MinIO prefix ayrımı (`sensitive/…`) ve ayrı ClickHouse tabloları (`events_sensitive_*`) tanımlandı.
3. **(YÜKSEK — ÇÖZÜLDÜ)** m.11 başvuru workflow'u: `docs/architecture/mvp-scope.md` "KVKK m.11 Data Subject Request (DSR) Workflow" bölümünde API endpoint'leri, SLA timer, DPO dashboard widget'ları, at-risk/overdue otomatik eskalasyon tanımlandı. Phase 1 exit criterion #20 eklendi.
4. **(ORTA — ÇÖZÜLDÜ)** İmha raporu dashboard'u: `docs/architecture/mvp-scope.md` "KVKK Periodic Destruction Report" bölümünde otomatik (1 Oca / 1 Tem) imzalı PDF üretimi, içerik, DPO-only erişim tanımlandı. Phase 1 exit criterion #21.
5. **(ORTA — ÇÖZÜLDÜ)** Legal hold bayrağı mekanizması: `docs/architecture/data-retention-matrix.md` "Legal Hold" bölümü — `legal_hold` kolonu, dar kapsam, DPO-only, audit log, 2 yıl max. `docs/architecture/bounded-contexts.md` cross-cutting concern olarak işaretlendi. Phase 1 exit criterion #19.
6. **(ORTA — ÇÖZÜLDÜ)** Canlı izleme oturum tarihçesi varsayılan ayarı: `docs/architecture/live-view-protocol.md` ve `mvp-scope.md`'de **varsayılan AÇIK** yapıldı. DPO yazılı gerekçeli kapatabilir, kapatma audit log'a yazılır ve çalışan tek seferlik bildirim alır.
7. **(DÜŞÜK — FAZ 2'YE KAYDIRILDI)** Müşteri tarafı DPIA ihracı: Phase 2 scope'una alındı; Phase 1'de `docs/compliance/dpia-sablonu.md` statik şablonu kullanılır.
8. **(DÜŞÜK — ÇÖZÜLDÜ — pipeline level)** m.6 DLP kural paketi: `mvp-scope.md`'de "KVKK m.6 DLP signal pack" Phase 1 scope'a alındı; pipeline ve rule-loader Phase 1'dedir, somut kural yazımı security-engineer follow-up.

## 14. [DOĞRULANMALI] Bayraklı Unsurlar (Hukuk Müşaviri Teyidi Gerekli)

1. Kurul'un güç asimetrisi doktrinine ilişkin kararlarına referans (§2.1 madde 1) — güncel karar numarası teyit edilmelidir.
2. Kriptografik anahtar imhasının "yok etme" olarak kabulünün Kurul rehberindeki spesifik paragraf (§8.2).
3. "Sessiz canlı izleme"nin iş mahkemesi içtihadı karşısındaki konumu (§11.3).
4. 5651 sayılı Kanun kapsamında `network.flow_summary` kayıtlarının "iç ağ log tutma yükümlülüğü" kapsamına tam olarak girip girmediği (§5 tablo #24).
5. Sendikalı işyerlerinde canlı izleme için istişare yükümlülüğünün hukuki zorunluluk mu iyi uygulama mı olduğu (§11.4).

## 15. İlgili Belgeler

- `docs/compliance/aydinlatma-metni-template.md`
- `docs/compliance/acik-riza-metni-template.md`
- `docs/compliance/dpia-sablonu.md`
- `docs/compliance/verbis-kayit-rehberi.md`
- `docs/compliance/iltica-silme-politikasi.md`
- `docs/compliance/hukuki-riskler-ve-azaltimlar.md`
- `docs/compliance/calisan-bilgilendirme-akisi.md`
- `docs/architecture/overview.md`
- `docs/architecture/event-taxonomy.md`
- `docs/architecture/data-retention-matrix.md`
- `docs/architecture/key-hierarchy.md`
- `docs/architecture/live-view-protocol.md`
- `docs/security/threat-model.md`
