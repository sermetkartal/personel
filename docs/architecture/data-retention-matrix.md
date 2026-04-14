# Veri Saklama Matrisi (KVKK Uyumlu)

> Dil: Türkçe. Hukuki çerçeve: 6698 sayılı Kişisel Verilerin Korunması Kanunu (KVKK) ve Kişisel Verilerin Silinmesi, Yok Edilmesi veya Anonim Hale Getirilmesi Hakkında Yönetmelik.

## Hukuki Dayanak Özeti

| KVKK Maddesi | İçerik | Personel'de Uygulama |
|---|---|---|
| **m.4 — Genel ilkeler** | Hukuka ve dürüstlük kuralına uygunluk, doğruluk, belirli/açık/meşru amaç, amaçla bağlantılı-sınırlı-ölçülü olma, öngörülen sürelerde muhafaza | Her veri sınıfı bu matriste açık amaç ve ölçülü süreye bağlanmıştır. Amaç dışı kullanım politika motorunda engellenir. |
| **m.5 — Kişisel verilerin işlenme şartları** | Açık rıza; ilgili maddeler çerçevesinde rızasız işleme halleri (sözleşmenin ifası, hukuki yükümlülük, meşru menfaat) | İşveren meşru menfaati temel hukuki sebep; klavye içeriği için ayrıca çalışan bilgilendirmesi. |
| **m.6 — Özel nitelikli kişisel veriler** | Özel nitelikli verilerin işlenmesi için ek şartlar | Ekran görüntüleri ve klavye içeriği özel nitelikli potansiyeli taşır; erişim sıkı sınırlandırılmıştır. |
| **m.7 — Silme, yok etme, anonim hale getirme** | Amaç sona erdiğinde ya da sürelerde verilerin silinmesi | Otomatik saklama politikası ClickHouse TTL, MinIO yaşam döngüsü kuralları ve PostgreSQL planlanmış job'larla uygulanır. |
| **m.10 — Aydınlatma yükümlülüğü** | Veri sorumlusunun ilgili kişiyi bilgilendirmesi | Kurulumda şeffaflık portalı bildirimi + kalıcı erişim. |
| **m.11 — İlgili kişinin hakları** | Bilgi talebi, düzeltme, silme, itiraz | Şeffaflık Portalı üzerinden resmi başvuru akışı. |
| **m.12 — Veri güvenliği** | Teknik ve idari tedbirler | mTLS, sertifika sabitleme, anahtar hiyerarşisi, denetim logu, erişim kontrolü, disk şifreleme. |

## Saklama Süreleri Tablosu

| Veri Sınıfı | Saklama Süresi (Varsayılan) | Maksimum | Depolama Katmanı | Silme Yöntemi | KVKK Dayanağı |
|---|---|---|---|---|---|
| **Ajan sağlık / heartbeat** | 30 gün | 30 gün | ClickHouse hot | TTL | m.4 (ölçülü) |
| **Süreç olayları (process.*)** | 90 gün | 180 gün | ClickHouse hot→warm | TTL | m.4, m.5 |
| **Ön plan / pencere başlığı** | 90 gün | 180 gün | ClickHouse hot→warm + OpenSearch | TTL + index rollover | m.4, m.5, m.6 |
| **Boşta/aktif oturum** | 90 gün | 180 gün | ClickHouse hot→warm | TTL | m.4 |
| **Dosya sistemi olayları** | 180 gün | 365 gün | ClickHouse hot→warm | TTL | m.4, m.5 |
| **Ekran görüntüsü (WebP)** | 30 gün | 90 gün | MinIO (warm→cold) | S3 Lifecycle | m.4, m.6 (özel nitelikli) |
| **Ekran video klibi** | 14 gün | 30 gün | MinIO | S3 Lifecycle | m.4, m.6 |
| **Pano meta verisi** | 90 gün | 180 gün | ClickHouse | TTL | m.4 |
| **Pano içeriği (şifreli)** | 30 gün | 60 gün | MinIO (şifreli blob) | S3 Lifecycle + key destroy | m.4, m.6 |
| **Klavye istatistikleri (metadata)** | 90 gün | 180 gün | ClickHouse | TTL | m.4 |
| **Klavye içeriği (şifreli blob)** | 14 gün | 30 gün | MinIO (şifreli blob) | Anahtar iptali + blob silme | m.4, m.6 — özel nitelikli, en kısa süre |
| **USB olayları** | 365 gün | 730 gün | ClickHouse warm | TTL | m.5 (meşru menfaat — güvenlik) |
| **Yazıcı işleri metadata** | 180 gün | 365 gün | ClickHouse warm | TTL | m.5 |
| **Ağ akış özeti** | 60 gün | 90 gün | ClickHouse hot | TTL | m.4, m.5 |
| **DNS / TLS SNI** | 30 gün | 60 gün | ClickHouse hot | TTL | m.4 |
| **Politika bloklama olayları** | 365 gün | 730 gün | ClickHouse warm | TTL | m.5 |
| **DLP eşleşme (match) kayıtları** | 2 yıl | 5 yıl | PostgreSQL + ClickHouse warm | Scheduled purge | m.5 (hukuki uyuşmazlık süresi) |
| **Canlı izleme oturum denetimi** | 5 yıl | 10 yıl | PostgreSQL (hash zincirli) | Yok — append-only; arşivleme | m.12 (güvenlik kanıtı) |
| **Yönetici eylemleri denetimi** | 5 yıl | 10 yıl | PostgreSQL (hash zincirli) | Yok — append-only | m.12 |
| **Kimlik / oturum günlükleri** | 1 yıl | 2 yıl | PostgreSQL | Scheduled purge | m.12 |
| **Sertifika yaşam döngüsü kayıtları** | 7 yıl | 10 yıl | Vault audit | Arşiv | m.12 |
| **Ajan tamper tespit olayları** | 3 yıl | 5 yıl | PostgreSQL + ClickHouse cold | Scheduled purge | m.5, m.12 |
| **Anonimleştirilmiş agregasyonlar / raporlar** | Süresiz (anonim) | — | ClickHouse | — | m.7 (anonim hale getirme) |

## Hassas-İşaretli (Sensitive-Flagged) Saklama Bucket'ı — KVKK m.6

**Faz 0 revizyonunda compliance-auditor tarafından eklendi (Gap 2).** Belirli olaylar, politika motoru veya DLP eşleşmesi tarafından **özel nitelikli potansiyeli** taşıyor olarak işaretlendiğinde, aşağıdaki daha kısa TTL'lere tabi tutulur ve ayrı bir depolama lokasyonuna yönlendirilir. Bu, m.6 gereği "en kısa süre" ilkesini teknik olarak uygulanır kılar.

**İşaretleme kaynakları** (bkz. `proto/personel/v1/policy.proto` → `SensitivityGuard`):

1. `SensitivityGuard.window_title_sensitive_regex` eşleşmesi (ör. sağlık, sendika, ibadet anahtar kelimeleri)
2. `SensitivityGuard.sensitive_host_globs` altında bir domain'e navigasyon
3. `auto_flag_on_m6_dlp_match = true` ise `kvkk_m6:` önekli DLP kural eşleşmesi
4. DPO manuel işaretleme (audit kaydı ile)

**Sensitive-Flagged TTL Tablosu**:

| Veri Sınıfı | Normal TTL | **Sensitive-Flagged TTL** | Maksimum (sensitive) | Depolama Yalıtımı |
|---|---|---|---|---|
| Ekran görüntüsü (WebP) | 30 gün | **7 gün** | 15 gün | MinIO prefix `sensitive/screenshots/` (ayrı lifecycle policy) |
| Ekran video klibi | 14 gün | **7 gün** | 14 gün | MinIO prefix `sensitive/screenclips/` |
| Pano içeriği (şifreli) | 30 gün | **7 gün** | 14 gün | MinIO prefix `sensitive/clipboard/` |
| Klavye içeriği (şifreli blob) | 14 gün | **7 gün** | 14 gün | MinIO prefix `sensitive/keystroke/` (zaten izole) |
| Pencere başlığı | 90 gün | **15 gün** | 30 gün | ClickHouse ayrı tablo `events_sensitive_window` |
| Pano meta verisi | 90 gün | **15 gün** | 30 gün | ClickHouse ayrı tablo `events_sensitive_clipboard_meta` |
| Klavye istatistikleri | 90 gün | **15 gün** | 30 gün | ClickHouse ayrı tablo `events_sensitive_keystroke_meta` |
| Dosya olayları (m.6 hast./sendika dizinlerinde) | 180 gün | **15 gün** | 30 gün | ClickHouse ayrı tablo `events_sensitive_file` |

**Teknik uygulama notları**:

- ClickHouse: `sensitive` boolean kolonu; ayrı `Distributed` tablo ve `TTL occurred_at + INTERVAL <n> DAY` cümlesi. Normal ve sensitive varyantlar arasında sorgu birleşimi view üzerinden yapılır; DPO rolü dışında normal yönetici rolü yalnızca normal varyantı görür.
- MinIO: `sensitive/` altında ayrı bir bucket policy; lifecycle rule `expiration.days` farklı. IAM policy ayrı; DLP servisi ve DPO dışında okuma yok.
- Anahtar imhası: sensitive blob için, TTL dolduğunda ilgili DEK Vault'ta **öncelikli** olarak iptal edilir (normal blob'lardan önce). Bu, m.6 gereği en hızlı silmeyi sağlar.
- İşaretleme geriye dönük uygulanabilir: DPO bir olayı manuel sensitive işaretlerse, job ilgili kayıt ve bağlı blob'u sensitive bucket'a taşır ve TTL'i yeniden hesaplar (asla uzatmaz, yalnızca kısaltır).
- Sensitive-flagged kayıtlar **legal hold**'a tabi olabilir; legal hold TTL'i geçersiz kılar ancak bu durum ayrı bir audit log kaydına yansır.

**Hukuki gerekçe**: KVKK m.6/3 "özel nitelikli kişisel veriler ... yeterli önlemlerin alınması kaydıyla" işlenebileceği için, ürünün bu bucket'ı sunması "ek yeterli önlem" olarak savunulabilir. m.4/2-ç ölçülülük ilkesi de teknik olarak uygulanmış olur.

## Legal Hold (Yasal Saklama) Mekanizması

**Faz 0 revizyonunda compliance-auditor tarafından netleştirildi (Gap 5).** Devam eden bir disiplin soruşturması, iç denetim, hukuki dava veya Kurul incelemesi sırasında ilgili kayıtların TTL'i durdurulur. Bu mekanizma veri modelinde aşağıdaki şekilde somutlaşır:

| Unsur | Detay |
|---|---|
| **Alan** | `legal_hold` (bool, varsayılan FALSE) tüm olay tabloları, MinIO blob metadata'sı ve `dlp_matches` tablosuna eklenir |
| **Kapsam** | Dar: belirli `endpoint_id`, `user_sid`, tarih aralığı, olay tipi kombinasyonu. Asla tenant genelinde değil. |
| **Uygulayıcı rol** | Sadece **DPO**. Yönetici/Admin rolü legal hold koyamaz veya kaldıramaz. |
| **API** | `POST /v1/legal-holds` (koyma), `POST /v1/legal-holds/{id}/release` (kaldırma). Her ikisi de mandatory `reason_code`, `ticket_id`, `justification` ister. |
| **TTL etkisi** | `legal_hold = TRUE` olan kayıtlar için ClickHouse TTL cümlesi `WHERE legal_hold = FALSE` filtresi ile devre dışı kalır (materialized view üzerinden); MinIO blob'ları lifecycle'dan muaf tutulur (`x-amz-meta-legal-hold: true` header). |
| **Denetim** | Her `place` ve `release` olayı hash-zincirli audit log'a yazılır (`legal_hold.placed`, `legal_hold.released`). Kayıt, kimin, ne zaman, hangi kapsamda, hangi gerekçeyle işlem yaptığını içerir. |
| **Çalışan görünürlüğü** | Çalışan legal hold varlığını görmez (soruşturma gizliliği), ancak hold kaldırıldıktan ve sonucuna göre, DPO takdirinde şeffaflık portalında bilgilendirilir. |
| **Süre sınırı** | Varsayılan maksimum 2 yıl; uzatma yeni DPO kararı + yeni audit kaydı ister. Süresiz legal hold yasak. |
| **Silme** | Legal hold yalnızca kayıtları **korur**, bir "silme bypass" değildir. Hold kaldırıldığında, kayıtların TTL'i orijinal `occurred_at` + normal süreye göre hesaplanır; çoktan geçmişse, hold kaldırılmasını takip eden bir sonraki TTL döngüsünde silinir. |

Legal Hold, `docs/architecture/bounded-contexts.md` "Audit & Compliance" bağlamı içinde cross-cutting bir kavramdır ve context map'te bu şekilde işaretlenmiştir.

## Varsayılan Davranış Kuralları

1. **En kısa süre ilkesi**: Özel nitelikli veriler (klavye içeriği, ekran görüntüsü/video) için varsayılan süreler kasıtlı olarak düşüktür; müşteri yalnızca yazılı gerekçe ile maksimuma kadar uzatabilir.
2. **Anonim hale getirme**: Rapor agregasyonları kimlikten arındırılır (hash + ≥k eşiği).
3. **Silme isteği (m.11)**: Çalışan, Şeffaflık Portalı üzerinden silme talebi açtığında; meşru menfaat dengesi DPO tarafından incelenir, uygunsa silme job'u denetim kaydıyla çalıştırılır.
4. **Kanuni saklama istisnası**: Devam eden bir disiplin soruşturması veya mahkeme emri varsa, ilgili veri üzerine "legal hold" bayrağı konulur ve TTL atlanır; bu durum denetim loguna hash zincirli olarak yazılır.
5. **Anahtar imhası ile silme**: Özellikle şifreli blob'lar için (klavye/pano içeriği), veri sınıfı saklama süresi dolduğunda, ilgili DEK (data encryption key) Vault'tan iptal edilir ve ardından blob fiziksel olarak silinir. Çift aşamalı silme denetlenebilirdir.
6. **VERBİS uyumu**: Bu matris, VERBİS "Saklama ve İmha Politikası" ekine doğrudan kaynak olarak sağlanır.

## Saklama Süresi Değişiklik Akışı

1. DPO talebi oluşturur.
2. Hukuk onayı (KVKK m.4 ölçülülük değerlendirmesi).
3. Policy Engine üzerinden tenant bazında etkinleştirme.
4. Değişiklik denetim loguna kaydedilir.
5. Çalışanlar Şeffaflık Portalı'nda yeni süreyi görür.

## Sorumlu Rol

| Rol | Sorumluluk |
|---|---|
| DPO | Matrisin doğruluğu, m.10/m.11 başvuruları |
| Security Engineer | Teknik kontrol ve anahtar iptali akışı |
| Operator | ClickHouse TTL ve MinIO lifecycle politikalarının uygulanması |
| Compliance Auditor | Yıllık uyum incelemesi |

## Faz 2 + Faz 8 Yeni Veri Kategorileri

Faz 2 (collector fleet genişlemesi) ve Faz 8 (ML/Analytics) yeni veri
sınıflarını ekler. Bunların saklama süreleri:

| Veri Sınıfı | Saklama (Varsayılan) | Maksimum | Depolama | KVKK Dayanağı |
|---|---|---|---|---|
| **Browser history (Chromium + Firefox)** | 90 gün | 180 gün | ClickHouse hot→warm | m.4, m.5 — verimlilik amacıyla |
| **Cloud storage sync olayları** | 90 gün | 180 gün | ClickHouse hot→warm | m.5 (veri sızıntısı tespiti) |
| **E-posta metadata (PST/OST)** | 180 gün | 365 gün | ClickHouse warm | m.5 — içerik **asla** toplanmaz |
| **Office MRU (son kullanılan dosyalar)** | 90 gün | 180 gün | ClickHouse hot→warm | m.5 |
| **Sistem olayları (login, lock, AV)** | 180 gün | 365 gün | ClickHouse warm | m.12 (güvenlik) |
| **Bluetooth cihaz eşleşme** | 365 gün | 730 gün | ClickHouse warm | m.5 (güvenlik) |
| **MTP cihaz bağlantısı** | 365 gün | 730 gün | ClickHouse warm | m.5 |
| **Cihaz durum anlık (CPU/RAM/battery)** | 30 gün | 30 gün | ClickHouse hot | m.4 |
| **GeoIP çözümleme** | 60 gün | 90 gün | ClickHouse hot | m.4 |
| **Pencere URL çıkarımı (browser title parse)** | 90 gün | 180 gün | ClickHouse hot→warm | m.4, m.5 |
| **ML kategori çıktısı (enriched event)** | 180 gün | 365 gün | ClickHouse warm | m.4 — türetilmiş veri |
| **OCR ekstresi (redacted)** | 30 gün | 90 gün | OpenSearch + MinIO | m.4, m.6 — m.6 otomatik redaksiyon |
| **UBA risk skor (nightly)** | 365 gün | 730 gün | Postgres | m.5 — güvenlik, m.11/g itiraz hakkı |
| **Prodüktivite skoru (günlük)** | 365 gün | 730 gün | Postgres | m.4 |
| **Evidence locker kayıtları (SOC 2)** | 5 yıl | 10 yıl | Postgres + MinIO WORM | m.12 — denetim kanıtı |
| **DSR başvuru + cevap** | 5 yıl | 10 yıl | Postgres + MinIO | m.13 — 5 yıl idari uyum |

### Özel notlar

- **Browser history**: Sadece `url` + `title` + `visited_at`. **Asla** çerez, password, bookmark, history arama sorgusu, form içerik.
- **E-posta metadata**: Sadece `sender`, `recipients`, `subject`, `timestamp`, `pst_size_delta`. **Asla** body, attachment content, preview.
- **OCR çıktısı**: KVKK m.6 redaksiyonu servis sınırı içinde uygulanır (TCKN, IBAN, kredi kartı Luhn, telefon, email → `[TAG]`).
- **UBA skor**: KVKK m.11/g gereği çalışan bu skora itiraz edebilir; skor **istişari** olarak işaretlenir (disclaimer her API cevabında).
