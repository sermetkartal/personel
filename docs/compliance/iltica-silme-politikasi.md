# Kişisel Veri Saklama ve İmha Politikası — Personel Platformu Şablonu

> Hukuki dayanak: 6698 sayılı KVKK m.7 ve Kişisel Verilerin Silinmesi, Yok Edilmesi veya Anonim Hale Getirilmesi Hakkında Yönetmelik (RG 28.10.2017).
> Not: Bu şablon Personel Platformu konuşlandıran müşteri kurum tarafından kendi politikasının temeli olarak kullanılmalıdır. Yönetmelik m.5, veri sorumlularının bu politikayı hazırlamasını zorunlu kılmaktadır.

---

## 1. Amaç ve Kapsam

İşbu politika, [{Müşteri Şirket Adı}] tarafından Personel UAM Platformu aracılığıyla işlenen kişisel verilerin saklanma sürelerini, imha yöntemlerini, periyodik imha takvimini ve sorumlulukları düzenler.

## 2. Tanımlar

| Terim | Tanım |
|---|---|
| **Silme** | Kişisel verilerin ilgili kullanıcılar için hiçbir şekilde erişilemez ve tekrar kullanılamaz hale getirilmesi (Yönetmelik m.8) |
| **Yok Etme** | Kişisel verilerin hiç kimse tarafından hiçbir şekilde erişilemez, geri getirilemez ve tekrar kullanılamaz hale getirilmesi (Yönetmelik m.9) |
| **Anonim Hale Getirme** | Kişisel verilerin başka verilerle eşleştirilse dahi kimliği belirli veya belirlenebilir bir gerçek kişiyle ilişkilendirilemeyecek hale getirilmesi (Yönetmelik m.10) |
| **Periyodik İmha** | Kanuni saklama süresi sona eren verilerin, kurum politikasında belirlenen tekrarlı zaman aralıklarında resen imha edilmesi (Yönetmelik m.11) |
| **İmha Takvimi** | 6 ayı geçmeyecek şekilde belirlenen otomatik imha döngüsü |

## 3. Saklama Süreleri Matrisi

`docs/architecture/data-retention-matrix.md` ve `kvkk-framework.md` §5 matrisine atıf yapılır. Özet:

| Veri Sınıfı | Saklama Süresi | İmha Yöntemi | Depolama | Dayanak |
|---|---|---|---|---|
| Ajan sağlık / heartbeat | 30 gün | Silme (TTL) | ClickHouse | m.4 |
| Süreç olayları | 90 gün | Silme | ClickHouse | m.4, m.5 |
| Pencere başlığı | 90 gün | Silme | ClickHouse + OpenSearch | m.4, m.5 |
| Oturum lock/unlock | 90 gün | Silme | ClickHouse | m.4 |
| Dosya sistemi olayları | 180 gün | Silme | ClickHouse | m.4, m.5 |
| **Ekran görüntüsü** | **30 gün** | Silme (MinIO lifecycle) | MinIO | **m.4, m.6 risk** |
| **Ekran video klibi** | **14 gün** | Silme | MinIO | **m.4, m.6 risk** |
| Pano meta | 90 gün | Silme | ClickHouse | m.4 |
| **Pano içeriği (şifreli)** | **30 gün** | **Yok etme (anahtar imhası + blob silme)** | MinIO | **m.4, m.6** |
| Klavye istatistik | 90 gün | Silme | ClickHouse | m.4 |
| **Klavye içeriği (şifreli)** | **14 gün** | **Yok etme (anahtar imhası + blob silme)** | MinIO | **m.6 özel hassasiyet** |
| USB olayları | 365 gün | Silme | ClickHouse | m.5 |
| Yazıcı metadata | 180 gün | Silme | ClickHouse | m.5 |
| Ağ akış özeti | 60 gün | Silme | ClickHouse | m.4, m.5 |
| DNS / TLS SNI | 30 gün | Silme | ClickHouse | m.4 |
| Politika bloklama | 365 gün | Silme | ClickHouse | m.5 |
| DLP match kayıtları | 2 yıl | Silme | PostgreSQL | m.5, hukuki zamanaşımı |
| **Canlı izleme denetim** | **5 yıl** | **Yok — append-only, arşiv** | PostgreSQL | **m.12 hesap verebilirlik** |
| **Yönetici denetim** | **5 yıl** | **Yok — append-only, arşiv** | PostgreSQL | **m.12** |
| Kimlik/oturum log | 1 yıl | Silme | PostgreSQL | m.12 |
| Sertifika yaşam döngüsü | 7 yıl | Arşiv | Vault audit | m.12 |
| Ajan tamper | 3 yıl | Silme | PostgreSQL | m.5, m.12 |
| Anonim agregasyonlar | Süresiz | — | ClickHouse | m.7 anonim hale getirme |

## 4. İmha Yöntemleri

### 4.1 Silme
- **ClickHouse**: Tablo TTL ile otomatik, günlük merge süreci. `ALTER TABLE ... MODIFY TTL` ile ayarlanır.
- **PostgreSQL**: Scheduled pg_cron job; `DELETE` sonrası `VACUUM FULL`.
- **OpenSearch**: Index rollover + ILM policy.

### 4.2 Yok Etme (Kriptografik)
Şifreli blob içeren veri kategorileri için (klavye içeriği, pano içeriği):
1. Vault transit engine üzerinde ilgili TMK versiyonu planlı imhaya alınır (bkz. `docs/architecture/key-hierarchy.md`).
2. PostgreSQL `keystroke_keys` tablosundaki ilgili `wrapped_dek` kaydı silinir.
3. MinIO lifecycle policy ciphertext blob'unu siler.
4. Vault audit device imha olayını kayıt altına alır; hash-zincirli audit log'a hash'i yazılır.

Bu yöntem, Yönetmelik m.9/2 kapsamında "veriye yeniden erişilemez hale getirme" şartını AES-256-GCM kriptografik garanti ile karşılar. **[DOĞRULANMALI — hukuk müşavirinin kriptografik yok etmenin Kurul rehberinde kabulüne ilişkin güncel teyidi alınmalıdır.]**

### 4.3 Anonim Hale Getirme
- Rapor agregasyonları için: kullanıcı kimliği hash + k-anonimlik (k≥5 eşiği) uygulanır.
- Birleşik sütun hash'i: SHA-256 + tenant-specific salt. Geri döndürülebilir değildir.

## 5. Periyodik İmha Takvimi

Yönetmelik m.11 uyarınca periyodik imha 6 ayı aşmayacak şekilde yapılır. Personel Platformu'nda:

| Döngü | Sıklık | Kapsam |
|---|---|---|
| **Günlük otomatik** | 24 saat | TTL tabanlı ClickHouse, MinIO lifecycle, PostgreSQL pg_cron |
| **Haftalık kontrol** | Her Pazar | DPO dashboard'u TTL sayaçlarını doğrular |
| **6 aylık formal rapor** | 1 Ocak ve 1 Temmuz | Silinen kayıt sayısı, anahtar imhaları, manuel istisnalar raporu |
| **Yıllık denetim** | Aralık | Politika gözden geçirme, saklama sürelerinin ölçülülük incelemesi |

## 6. Legal Hold (Hukuki Bekletme)

Devam eden bir disiplin soruşturması, iş mahkemesi davası, Kurul talebi veya savcılık tahkikatı durumunda, ilgili kayıt(lar)a `legal_hold=true` bayrağı konulur ve otomatik imha atlanır. Legal hold:
- DPO onayıyla konulur.
- Gerekçe ve süre audit log'a kayıtlanır.
- Uygulanan kayıt kapsamı daraltılmış tutulur (genel tüm-tenant legal hold yasaktır).
- Kaldırıldığında normal imha sürecine geri döner.

## 7. Sorumluluklar

| Rol | Sorumluluk |
|---|---|
| **DPO** | Politika sahibi; periyodik imha raporu onayı; legal hold kararı |
| **BT Güvenlik Yöneticisi** | TTL yapılandırmasının teknik uygulaması; Vault anahtar imhası |
| **Operatör** | ClickHouse / PostgreSQL / MinIO TTL konfigürasyonu |
| **Hukuk Müşaviri** | Legal hold gerekçesinin hukuki değerlendirmesi |
| **Üst Yönetim** | Politikanın onaylanması ve yıllık gözden geçirme |

## 8. Veri Sahibi Silme Talebi (m.11/1-e)

Çalışan, Şeffaflık Portalı üzerinden silme talebi açabilir. Akış:
1. Talep kaydı oluşturulur, DPO'ya bildirim gider.
2. DPO meşru menfaat dengesi incelemesi yapar.
3. Hukuki saklama yükümlülüğü çakışması varsa kısmen reddedilir (gerekçe çalışana bildirilir).
4. Onaylanan kapsam için silme iş emri üretilir; audit log'a yazılır.
5. 30 gün içinde çalışana sonuç bildirilir (m.13).

## 9. Politika Gözden Geçirme

İşbu politika yılda en az bir kez ve aşağıdaki durumlarda ara güncelleme ile gözden geçirilir:
- KVKK ve ilgili mevzuatta değişiklik
- Kurul kararı veya rehberi güncellemesi
- Yeni bir olay türünün platformda aktif edilmesi
- Veri ihlali olayı sonrası kök neden analizi
- DPIA güncellemesi

## 10. Yürürlük

İşbu politika [{Tarih}] itibarıyla yürürlüğe girer. Onaylayan: [{DPO Ad-Soyad}], [{Üst Yönetici Ad-Soyad}].
