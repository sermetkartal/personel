# Veri Koruma Etki Değerlendirmesi (DPIA) — Personel Platformu Kurulum Şablonu

> Kapsam: KVKK kapsamında yüksek riskli veri işleme faaliyeti olarak değerlendirilen UAM sistemi kurulumu.
> Hedef: Müşteri DPO tarafından kurulum öncesi doldurulur ve Kurul denetiminde hesap verebilirlik belgesi olarak saklanır.
> Hukuki çerçeve: KVKK m.4, m.5, m.12; Kişisel Veri Güvenliği Rehberi (Kurul).
>
> **Not**: KVKK, DPIA'yı GDPR m.35 gibi açık şart olarak düzenlemez; ancak Kurul, yüksek riskli işleme faaliyetleri için DPIA benzeri risk değerlendirmesini iyi uygulama olarak önerir. Personel Platformu, yakaladığı veri kategorilerinin hassasiyeti nedeniyle yüksek risk sınıfındadır.

---

## Bölüm 1 — Kurum ve Proje Bilgileri

| Alan | Değer |
|---|---|
| Veri Sorumlusu | [{Müşteri Şirket Tam Unvanı}] |
| Proje Adı | Personel UAM Platformu Kurulumu |
| Proje Sahibi | [{Proje Yöneticisi}] |
| DPO / İrtibat Kişisi | [{Ad-Soyad}] |
| VERBİS Kayıt No | [{varsa}] |
| Değerlendirme Tarihi | [{YYYY-MM-DD}] |
| Değerlendirmeyi Yapan | [{Ad-Soyad, Unvan}] |
| Gözden Geçirme Tarihi | [{YYYY-MM-DD}] |

## Bölüm 2 — İşleme Faaliyeti Tanımı

### 2.1 Amaç Beyanı
Spesifik işleme amaçlarını listeleyin (bkz. Aydınlatma Metni §3). Muğlak ifade kullanmayın.

### 2.2 Kapsam
- Etkilenen çalışan sayısı: [{sayı}]
- Coğrafi konum: [{lokasyonlar}]
- Etkilenen sistemler: [{liste}]
- Süre: [{başlangıç tarihi; süresiz mi, belirli mi?}]

### 2.3 Veri Kategorileri
`kvkk-framework.md` §5 matrisindeki 36 olay türünden hangilerinin aktif edileceğini işaretleyin:

- [ ] Süreç olayları (1-3)
- [ ] Pencere başlığı (4)
- [ ] Oturum / boşta (5-9)
- [ ] Ekran görüntüsü (10)
- [ ] Ekran video klibi (11)
- [ ] Dosya sistemi olayları (12-17)
- [ ] Pano metadata (18)
- [ ] Pano içeriği şifreli (19)
- [ ] Yazıcı (20)
- [ ] USB (21-23)
- [ ] Ağ akış / DNS / TLS SNI (24-26)
- [ ] Klavye istatistik (27)
- [ ] Klavye içeriği şifreli (28)
- [ ] Politika blok (29-30)
- [ ] Canlı izleme (35-36)

### 2.4 Akış Diyagramı
Endpoint → Gateway → NATS → ClickHouse/MinIO → Admin Console akışını diyagram olarak ekleyin.

## Bölüm 3 — Hukuki Dayanak Değerlendirmesi

### 3.1 Ana Hukuki Sebep
- [ ] m.5/2-f meşru menfaat
- [ ] m.5/2-c sözleşmenin ifası
- [ ] m.5/2-ç hukuki yükümlülük
- [ ] m.5/1 açık rıza (tavsiye edilmez)

### 3.2 Meşru Menfaat Dengesi Testi (m.5/2-f seçildiyse)

| Aşama | Argüman | Karar |
|---|---|---|
| Meşru menfaatin varlığı | [{doldur}] | [{geçer/geçmez}] |
| İşlemenin zorunluluğu | [{doldur}] | [{geçer/geçmez}] |
| Denge — temel hak ve özgürlüklere zarar | [{doldur + azaltımlar}] | [{geçer/geçmez}] |

### 3.3 Özel Nitelikli Veri (m.6) Değerlendirmesi
- Kazara m.6 verisi toplama riski var mı? [Evet/Hayır]
- Risk azaltım: whitelisted app dışı ekran görüntüsü kısıtlaması, özel nitelikli pencere başlığı filtresi, kısaltılmış saklama.
- Aktif açık rıza alınması gerekli mi? [Evet/Hayır + gerekçe]

## Bölüm 4 — Taraf Analizi

| Taraf | Sıfat | Hak ve Çıkarlar |
|---|---|---|
| Çalışanlar | İlgili kişi / veri sahibi | Mahremiyet, öngörülebilirlik, hak kullanımı |
| İşveren | Veri sorumlusu | Güvenlik, performans, varlık koruma |
| İK | İç denetim rolü | Disiplin, uyum |
| DPO | Denetim rolü | KVKK uyumu |
| Personel firması | Yazılım sağlayıcı | Veri işleyen değil |
| Kurul | Düzenleyici | Denetim, yaptırım |
| İş mahkemeleri | Yargı | Uyuşmazlık çözümü |

## Bölüm 5 — Risk Matrisi

Aşağıdaki şablonu doldurun. Olasılık (1-5), Etki (1-5), Skor = O × E.

| # | Risk | Olasılık | Etki | Skor | Mevcut Kontrol | Kalıntı Risk |
|---|---|---|---|---|---|---|
| R1 | Kazara özel nitelikli veri toplanması | [1-5] | [1-5] |  | Filtreleme, erken imha, m.6 bayrağı |  |
| R2 | Yetkisiz yönetici erişimi (ekran görüntüsü) | [1-5] | [1-5] |  | RBAC, audit log |  |
| R3 | Klavye içeriğinin kötüye kullanımı | [1-5] | [1-5] |  | Kriptografik izolasyon (§10) |  |
| R4 | Canlı izleme yetkisiz tetikleme | [1-5] | [1-5] |  | Dual control, audit |  |
| R5 | Veri saklama süresinin aşılması | [1-5] | [1-5] |  | Otomatik TTL, periyodik imha raporu |  |
| R6 | Harici saldırgan → veri tabanı sızdırması | [1-5] | [1-5] |  | mTLS, Vault, disk şifreleme |  |
| R7 | İçerden (insider) DLP kuralı kötüye kullanımı | [1-5] | [1-5] |  | Kural değişikliği DPO onayı, audit |  |
| R8 | Ajan tamper ve yanlış kayıt üretimi | [1-5] | [1-5] |  | Tamper detect, imzalı update |  |
| R9 | Çalışan hak talebine geç yanıt (m.13 30 gün) | [1-5] | [1-5] |  | Portal SLA sayacı |  |
| R10 | İhlal bildiriminin 72 saat geçmesi | [1-5] | [1-5] |  | Forensic export, runbook |  |
| R11 | İş mahkemesinde hukuki dayanak itirazı | [1-5] | [1-5] |  | Aydınlatma, dual control, matris |  |
| R12 | Sendika / TİS uyuşmazlığı | [1-5] | [1-5] |  | Ön istişare |  |

## Bölüm 6 — Tedbirler Planı

Skoru ≥15 olan her risk için azaltım aksiyonu, sorumlu kişi ve tarih belirleyin.

| Risk # | Aksiyon | Sorumlu | Tarih | Durum |
|---|---|---|---|---|
|  |  |  |  |  |

## Bölüm 7 — Kurul Bildirim Gerekliliği Değerlendirmesi

KVKK ve Kurul ilkeleri ışığında aşağıdakileri cevaplayın:

- [ ] Kalıntı risk skoru ≥ 20 olan risk var mı? (Evetse, Kurul'a ön-istişare değerlendirmesi yapılmalı.)
- [ ] Faaliyet yeni/deneysel mi? ([Evet/Hayır])
- [ ] Büyük çaplı işleme mi (>1000 çalışan)? ([Evet/Hayır])

## Bölüm 8 — Sonuç ve Onay

| Rol | Ad-Soyad | Tarih | İmza |
|---|---|---|---|
| DPO | | | |
| Hukuk Müşaviri | | | |
| BT Güvenlik Yöneticisi | | | |
| İK Direktörü | | | |
| Üst Yönetim | | | |

**DPIA Sonucu**: [ ] Risk seviyesi kabul edilebilir — kuruluma devam. [ ] Risk seviyesi yüksek — ek kontroller gerekli. [ ] Risk seviyesi kabul edilemez — faaliyet yeniden tasarlanmalı.

## Bölüm 9 — Gözden Geçirme Takvimi

DPIA yılda bir kez veya aşağıdaki durumlarda güncellenir:
- Yeni bir olay türü aktif edildiğinde
- Saklama süresi değiştiğinde
- Yeni bir entegrasyon eklendiğinde
- Bir veri ihlali sonrası
- Kurul rehberi güncellendiğinde
