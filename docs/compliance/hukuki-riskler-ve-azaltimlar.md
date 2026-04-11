# Hukuki Risk Kayıt Defteri — Personel Platformu (Türkiye)

> Kapsam: Personel UAM Platformu'nun Türkiye pazarında konuşlandırılmasına ilişkin hukuki risklerin tanımı, derecelendirilmesi, azaltımları ve sorumluları.
> Not: Olasılık ve etki 1-5 arası puanlanır; Risk Skoru = O × E.

---

## Risk Kayıt Defteri

### R1 — Kurul Şikayet ve İdari Para Cezası

| Alan | Değer |
|---|---|
| **Risk** | KVKK m.18 kapsamında Kurul'a yapılan çalışan şikayeti sonucu idari para cezası (aydınlatma yükümlülüğünün ihlali, veri güvenliğinin sağlanamaması, VERBİS kaydına uygunsuzluk) |
| **Olasılık** | 3 |
| **Etki** | 4 |
| **Skor** | 12 |
| **Azaltım** | (a) Tam Aydınlatma Metni teslimatı, imzalı kopya arşivi; (b) VERBİS kaydının eksiksiz tutulması; (c) DPIA ve saklama politikasının güncel tutulması; (d) Kurul denetimi için hash-zincirli audit delili hazır tutma; (e) ihlal bildirim runbook'u ile 72 saat süresine uyum |
| **Sahip** | Müşteri DPO |

### R2 — İş Mahkemesinde Çalışan Tarafından Açılan Haksız Fesih / Maddi-Manevi Tazminat Davası

| Alan | Değer |
|---|---|
| **Risk** | Personel Platformu üzerinden elde edilen kanıtla yapılan disiplin feshinin, aşırı izleme / özel hayatın gizliliği ihlali / oransızlık gerekçeleriyle iş mahkemesi tarafından haksız bulunması ve işveren aleyhine tazminata hükmedilmesi |
| **Olasılık** | 3 |
| **Etki** | 4 |
| **Skor** | 12 |
| **Azaltım** | (a) Aydınlatma ve yazılı iş sözleşmesi monitoring maddesi; (b) canlı izleme dual-control + gerekçe kodu zorunluluğu; (c) hash-zincirli audit ile hangi kanıtın ne zaman ve kimin tarafından görüldüğünün belgelenmesi; (d) ekran görüntüsü ve canlı izleme kullanımının orantılı olduğuna dair politika şablonu; (e) yetkisiz erişimin teknik olarak engellenmiş olması (RBAC + row-level security) |
| **Sahip** | Müşteri İK + Hukuk Müşaviri |

### R3 — Anayasa Mahkemesi Bireysel Başvuru (Anayasa m.20 Kişisel Verilerin Korunması / m.20 Özel Hayatın Gizliliği)

| Alan | Değer |
|---|---|
| **Risk** | Çalışanın iç hukuk yolları tükendikten sonra Anayasa Mahkemesi'ne bireysel başvurusu; canlı izleme, klavye kaydı veya ekran görüntüsü alımının temel hak ihlali sayılması |
| **Olasılık** | 2 |
| **Etki** | 5 |
| **Skor** | 10 |
| **Azaltım** | (a) Öngörülebilirlik: aydınlatma metni + şeffaflık portalı; (b) orantılılık: en kısa saklama süreleri, dual-control, kriptografik keystroke izolasyonu; (c) amaç sınırlılığı: reason code zorunluluğu; (d) ikinci kişi kontrolü: HR onay kapısı; (e) bağımsız denetlenebilirlik: hash-zincirli audit |
| **Sahip** | Müşteri Hukuk Müşaviri |

### R4 — İş K. m.75 ve Personel Özlük Dosyası Mevzuatı ile Uyum

| Alan | Değer |
|---|---|
| **Risk** | İş K. m.75 uyarınca tutulması zorunlu personel özlük dosyası belgelerinin Personel Platformu verileri ile karıştırılması; Personel verilerinin özlük dosyasına giren "işe ilişkin belge" kapsamına hatalı dahil edilmesi |
| **Olasılık** | 2 |
| **Etki** | 3 |
| **Skor** | 6 |
| **Azaltım** | (a) Personel verileri özlük dosyasından ayrı bir veri sistemi olarak konumlandırılmalı; (b) özlük dosyasına sadece disiplin kararı çıkış belgesi eklenmeli, ham izleme verisi eklenmemeli; (c) saklama süreleri İş K. özlük dosyası saklama süresinden (genellikle 10 yıl) ayrılmalı |
| **Sahip** | Müşteri İK |

### R5 — Özel Nitelikli Veri İhlali ve Artırılmış Ceza

| Alan | Değer |
|---|---|
| **Risk** | Ekran görüntüsü veya klavye içeriğinin kazara sağlık, din, sendikal bilgi içermesi ve bu verinin Kurul tarafından özel nitelikli veri ihlali sayılması. Özel nitelikli veri ihlali cezaları m.18/1-b uyarınca ağırlaşmış olabilir. **[DOĞRULANMALI]** |
| **Olasılık** | 3 |
| **Etki** | 5 |
| **Skor** | 15 |
| **Azaltım** | (a) Ekran görüntüsü exclude list (sağlık uygulamaları, HR portalları); (b) özel nitelikli pencere başlığı regex filtreleri; (c) bayraklı kayıtlar için kısaltılmış saklama (7 gün); (d) DPO + Investigator ikili onayı ile erişim; (e) özel nitelikli amaç VERBİS'te seçilmemesi; (f) DPIA'da m.6 riskinin "kabul edilen kalıntı risk" olarak açıkça belgelenmesi |
| **Sahip** | Müşteri DPO + BT Güvenlik |

### R6 — Canlı İzleme Sırasında Çalışan Mahremiyeti İhlal İddiası

| Alan | Değer |
|---|---|
| **Risk** | Canlı izleme oturumunun (a) orantısız uzun sürmesi, (b) gerekçesiz başlatılması, (c) ekranda özel yazışma görünmesi durumunda çalışan mahremiyet iddiası |
| **Olasılık** | 3 |
| **Etki** | 4 |
| **Skor** | 12 |
| **Azaltım** | (a) Dual control zorunluluğu; (b) reason code zorunluluğu; (c) 15/60 dk maksimum süre; (d) HR ve DPO sonlandırma yetkisi; (e) hash-zincirli audit ile her oturumun kaydı; (f) kayıt disk'e alınmaması (Faz 1); (g) Şeffaflık Portalı'nda çalışanın kendi oturum geçmişini görmesi (açık ayar önerilir) |
| **Sahip** | Müşteri İK + DPO |

### R7 — Tuş Vuruşu İçeriğinin Kötüye Kullanımı İddiası

| Alan | Değer |
|---|---|
| **Risk** | Çalışanın "tuş vuruşlarım okundu / şifrelerim alındı" iddiasında bulunması |
| **Olasılık** | 3 |
| **Etki** | 4 |
| **Skor** | 12 |
| **Azaltım** | **Kriptografik imkansızlık savunması** (bkz. `kvkk-framework.md` §10): (a) Vault policy'leri yöneticilere TMK derive hakkı vermez; (b) Vault audit derive olaylarını loglar, derive yokluğu = okuma yokluğu; (c) Bağımsız red team raporu (Faz 1 exit criterion #9); (d) proto/kod seviyesinde plaintext keystroke dönen RPC bulunmaması (CI linter ile zorlanır); (e) DLP match metadata'sında ham içerik olmaması |
| **Sahip** | Personel firması (ürün) + Müşteri DPO (delil saklama) |

### R8 — Sendika ve İş K. m.26 (Özel Yaşama Saygı) Uyumu

| Alan | Değer |
|---|---|
| **Risk** | Sendikalı işyerinde TİS'e uygun olmayan monitoring uygulaması nedeniyle sendikal uyuşmazlık; iş kolu denetim kurullarında sorun |
| **Olasılık** | 2 |
| **Etki** | 3 |
| **Skor** | 6 |
| **Azaltım** | (a) Kurulum öncesi sendika ile istişare ve TİS eki monitoring protokolü; (b) işçi temsilcisinin DPIA sürecine dahil edilmesi; (c) ekran görüntüsü ve canlı izleme kapsamında sendikal haberleşmenin (ör. sendika uygulaması) exclude list'e alınması |
| **Sahip** | Müşteri İK |

### R9 — KVKK m.11 Başvurusuna 30 Gün İçinde Yanıt Verilmemesi

| Alan | Değer |
|---|---|
| **Risk** | Çalışan başvurusunun zamanında yanıtlanmaması → Kurul şikayeti → idari para cezası |
| **Olasılık** | 3 |
| **Etki** | 3 |
| **Skor** | 9 |
| **Azaltım** | (a) Şeffaflık Portalı SLA sayacı; (b) DPO dashboard uyarısı (T+20); (c) standart yanıt şablonları; (d) aylık başvuru raporu |
| **Sahip** | Müşteri DPO |

### R10 — Veri İhlalinin 72 Saat İçinde Kurul'a Bildirilmemesi

| Alan | Değer |
|---|---|
| **Risk** | KVKK m.12/5 kapsamında bildirim süresinin kaçırılması |
| **Olasılık** | 2 |
| **Etki** | 5 |
| **Skor** | 10 |
| **Azaltım** | (a) İhlal runbook'u ve prova tatbikatı; (b) forensic export aracı; (c) tamper_detected ve anomaly alerting; (d) DPO 7/24 iletişim zinciri |
| **Sahip** | Müşteri DPO + BT Güvenlik |

### R11 — Veri Sorumlusu / Yazılım Sağlayıcı Ayrımının Sözleşmede Netleştirilmemesi

| Alan | Değer |
|---|---|
| **Risk** | Lisans sözleşmesinde Personel firmasının veri işleyen sayılması, istenmeden kapsam genişlemesi doğurması |
| **Olasılık** | 2 |
| **Etki** | 3 |
| **Skor** | 6 |
| **Azaltım** | Lisans sözleşmesinde §3.2 metninin aynen yer alması; destek hizmetinin "istisnai, yazılı, denetime tabi" ifadesiyle kapsamlanması; uzaktan destek oturumlarının müşteri onayı zorunluluğu |
| **Sahip** | Personel firması Hukuk + Müşteri Hukuk |

### R12 — DLP Kural Setinin Kötü Niyetli Değişikliği ile Ham İçerik Sızıntısı

| Alan | Değer |
|---|---|
| **Risk** | BT personeli DLP kural setine "her şeyi eşleştir" kuralı ekleyip `dlp.match` metadata üzerinden ham klavye içeriği sızdırması |
| **Olasılık** | 2 |
| **Etki** | 5 |
| **Skor** | 10 |
| **Azaltım** | (a) DLP kural değişikliği DPO onayı zorunlu; (b) match metadata şemasının fix ve dar olması; (c) kural değişiklik audit; (d) anomali tespit (match volume spike alert); (e) red team yıllık testi |
| **Sahip** | Personel firması (ürün) + Müşteri DPO |

### R13 — Faz 3 SaaS Geçişinde Konumun Değişmesi ve Önceki Beyanlarla Çelişme

| Alan | Değer |
|---|---|
| **Risk** | SaaS modeline geçişte Personel firmasının veri işleyen sıfatı kazanması; on-prem döneminde verilmiş aydınlatma metinlerinin güncellenmemesi |
| **Olasılık** | 2 |
| **Etki** | 3 |
| **Skor** | 6 |
| **Azaltım** | (a) Mevcut aydınlatma metninde "on-prem kurulum" ibaresi; (b) SaaS geçiş planında yeni aydınlatma metni zorunluluğu; (c) veri işleyen sözleşmesinin hazır şablonunun tutulması |
| **Sahip** | Personel firması PM + Hukuk |

## Özet Risk Matrisi

| Skor Aralığı | Risk Sayısı | Örnekler |
|---|---|---|
| 15+ (Yüksek) | 1 | R5 (özel nitelikli) |
| 10-14 (Orta-Yüksek) | 6 | R1, R2, R3, R6, R7, R10, R12 |
| 6-9 (Orta) | 5 | R4, R8, R9, R11, R13 |
| <6 (Düşük) | 0 | — |

Risk defteri yılda bir ve aşağıdaki durumlarda güncellenir: mevzuat değişikliği, önemli Kurul kararı, ihlal olayı, yeni olay türü aktivasyonu, DPIA güncellemesi.
