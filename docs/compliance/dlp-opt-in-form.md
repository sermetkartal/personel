# DLP Aktivasyon Onay Formu — Template

> **Hukuki dayanak**: 6698 sayılı KVKK m.5/2-f (meşru menfaat) + m.12 (veri güvenliği) + Personel Platformu ADR 0013.
>
> **Kullanım**: Bu form, Personel Platformu'nun Data Loss Prevention (DLP) servisini aktifleştirmek için **zorunlu** ön koşuldur. Formda istenen üç imzanın eksiksiz tamamlanması gerekir. Aktivasyon script'i (`infra/scripts/dlp-enable.sh`) imzalı formun sha256 hash'ini audit kaydına işler.
>
> **Saklama**: Form orijinali müşteri DPO arşivinde KVKK m.12 kanıtı olarak en az 5 yıl saklanmalıdır. Sayısal kopyası `/var/lib/personel/dlp/opt-in-signed.pdf` yoluna yerleştirilip okunur izinle operator'a sunulmalıdır.

---

## 1. Aktivasyon Kimliği

| Alan | Değer |
|---|---|
| Müşteri Kurum | `[{Şirket Tam Unvanı}]` |
| MERSİS No | `[{MERSİS}]` |
| VERBİS Kayıt No | `[{varsa}]` |
| Aktivasyon Tarihi | `[{YYYY-AA-GG}]` |
| Aktivasyon Gerekçe Kodu | `[{örn: soruşturma-2026-001, rutin-dlp-aktivasyon, vb}]` |
| Etkilenen Çalışan Sayısı | `[{tam sayı}]` |
| Geçerlilik Süresi | `[ ] süresiz  [ ] [{N}] ay` |

---

## 2. DPIA Ek Beyanı

KVKK yüksek riskli işleme faaliyeti olarak DLP aktivasyonu için DPIA'nın aşağıdaki maddelerinin güncellendiği beyan edilir. Güncellenen DPIA dosyası: `[{dosya yolu}]`.

- [ ] **§2.3 Veri Kategorileri**: `keystroke.content_encrypted` ve `clipboard.content_encrypted` işlem kapsamına dahil edildi
- [ ] **§3.1 Hukuki Dayanak**: Meşru menfaat dengesi testi bu özellik için yeniden yapıldı ve olumlu sonuçlandı
- [ ] **§3.3 Özel Nitelikli Veri Değerlendirmesi**: m.6 olasılığı için filtreleme kontrolleri (`screenshot_exclude_apps`, `window_title_sensitive_regex`) aktif tutuldu
- [ ] **§5 Risk Matrisi**: DLP servisi compromise riski (R12) değerlendirildi ve azaltımları belgelendi
- [ ] **§6 Tedbirler Planı**: Vault izolasyonu, audit chain, periyodik red team testi doğrulandı

---

## 3. Aydınlatma ve Şeffaflık

- [ ] Aktivasyon öncesi tüm etkilenen çalışanlara güncellenmiş Aydınlatma Metni (`docs/compliance/aydinlatma-metni-template.md` §8) dağıtıldı
- [ ] Şeffaflık Portalı'nda otomatik "DLP aktif edildi: [tarih]" banner'ının görüntüleneceği bilindi ve kabul edildi
- [ ] Çalışan işi temsilcisi (varsa sendika temsilcisi) bilgilendirildi ve istişareye alındı
- [ ] İş sözleşmesine veya personel yönetmeliğine uygun monitoring maddesi (`acik-riza-metni-template.md` Şablon B) eklenmiştir

---

## 4. Teknik Doğrulamalar

Aktivasyon öncesi aşağıdaki teknik kontroller BT güvenlik ekibi tarafından doğrulanmıştır:

- [ ] Vault `unsealed` durumda ve `dlp-service` AppRole policy'si `docs/security/runbooks/vault-setup.md` ile eşleşiyor
- [ ] `dlp-service` AppRole'ü hiçbir admin role veya insan kullanıcı tarafından erişilebilir değil (Vault policy audit'inde doğrulandı)
- [ ] DLP container imajı hash'i bilinen iyi değerle eşleşiyor (`docker image ls personel/dlp --digests`)
- [ ] DLP container seccomp + AppArmor profile'ı `docs/security/runbooks/dlp-service-isolation.md` ile eşleşiyor
- [ ] MinIO `sensitive/` prefix'i yalnızca DLP servis kullanıcısı için okunur
- [ ] Audit chain Postgres + WORM sink eşitliği son 24 saatte doğrulandı

---

## 5. Kabul ve Sorumluluklar

Aşağıda imzası bulunan kişiler, işbu formun müşteri kurumun KVKK uyumluluğu için resmi hesap verebilirlik kaydı olduğunu, DLP aktivasyonunun `ADR 0013` kapsamında kontrollü bir opt-in ceremony ile gerçekleştiğini ve her aktivasyonun müşteri veri sorumlusunun sorumluluğunda olduğunu kabul eder.

### Veri Koruma Görevlisi (DPO)

| Alan | Değer |
|---|---|
| Ad-Soyad | `[{Ad Soyad}]` |
| Unvan | Veri Koruma Görevlisi |
| Sicil No | `[{sicil}]` |
| E-posta | `[{dpo@musteri.com.tr}]` |
| Tarih | `[{YYYY-AA-GG}]` |
| İmza | ________________________________ |

### BT Güvenlik Direktörü / CISO

| Alan | Değer |
|---|---|
| Ad-Soyad | `[{Ad Soyad}]` |
| Unvan | BT Güvenlik Direktörü / CISO |
| Sicil No | `[{sicil}]` |
| E-posta | `[{ciso@musteri.com.tr}]` |
| Tarih | `[{YYYY-AA-GG}]` |
| İmza | ________________________________ |

### Hukuk Müşaviri

| Alan | Değer |
|---|---|
| Ad-Soyad | `[{Ad Soyad}]` |
| Unvan | Hukuk Müşaviri |
| Sicil No | `[{sicil}]` |
| E-posta | `[{legal@musteri.com.tr}]` |
| Tarih | `[{YYYY-AA-GG}]` |
| İmza | ________________________________ |

### İsteğe Bağlı: Üst Yönetim Onayı (Yönetim Kurulu / CEO)

| Alan | Değer |
|---|---|
| Ad-Soyad | `[{Ad Soyad}]` |
| Unvan | `[{ör: CEO, GM}]` |
| Tarih | `[{YYYY-AA-GG}]` |
| İmza | ________________________________ |

---

## 6. Pasifleştirme Bildirim

Aktivasyon, müşteri tarafından dilediğinde `infra/scripts/dlp-disable.sh` script'i ile devre dışı bırakılabilir. Pasifleştirme durumunda:

- Mevcut şifreli klavye ciphertext blob'ları 14 günlük TTL'ye bırakılır (ADR 0013 A4 — forensic continuity)
- Şeffaflık Portalı'nda "DLP devre dışı bırakıldı: [tarih]" banner'ı gösterilir
- Audit chain'e `dlp.disabled` olayı yazılır
- Pasifleştirme sonrası yeni klavye içeriği üretilmez (agent politika `dlp_enabled=false` aldığında collector content emit etmez — sadece stats)

Pasifleştirme kararı DPO tarafından bu formun arka yüzüne imzalı olarak yazılmalıdır.

---

## 7. Referanslar

- `docs/adr/0013-dlp-disabled-by-default.md` — bu ceremony'nin hukuki ve teknik dayanağı
- `docs/architecture/key-hierarchy.md` — DLP anahtar türetme akışı
- `docs/compliance/kvkk-framework.md` §10 — çalışan gizliliğinin kriptografik garantisi
- `docs/compliance/dpia-sablonu.md` — DPIA şablonu
- `docs/compliance/hukuki-riskler-ve-azaltimlar.md` R12 — DLP kural seti kötü niyetli değişiklik riski

---

*Template versiyonu: 1.0 — Nisan 2026. Müşteri hukuk müşavirinin onayı olmadan tam olarak aynı metin kullanılmamalı; şirket özelinde uyarlama gerekir.*
