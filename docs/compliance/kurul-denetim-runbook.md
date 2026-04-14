# KVKK Kurul Denetim Hazırlık Runbook'u — Personel Platformu

> **Hukuki dayanak**: 6698 sayılı KVKK m.15 (şikayet üzerine veya re'sen inceleme), m.18 (Kurul'un görev ve yetkileri), m.12 (veri güvenliği), m.7 (silme/yok etme), m.10 (aydınlatma), m.11 (ilgili kişinin hakları).
>
> **Amaç**: Kişisel Verileri Koruma Kurulu'nun Personel Platformu'nu kullanan bir müşteri kurumda yerinde denetim (inceleme) yapması durumunda, ilk 24 saat ve sonraki günler için operatör (DPO + BT ekibi) hazır çalışma kılavuzu.
>
> **Kapsam**: Bu runbook, denetimin **teknik** ve **süreç** taraflarını kapsar. Hukuki temsil için müşterinin kendi hukuk müşavirine ek olarak danışması şarttır.
>
> **Faz 11 roadmap item #120**.
>
> Versiyon: 1.0 — Nisan 2026

---

## 1. İlk 24 Saat Checklist

Kurul müfettişi geldiğinde (veya yazılı bildirim düştüğünde) aşağıdaki adımlar **sırayla** uygulanır. Her adım için sorumlu rol ve çıktı belgelendirilir.

### Saat 0 (bildirim anı)

- [ ] **DPO bilgilendir** (mümkünse sözlü + yazılı, her ikisi de)
- [ ] **Hukuk müşaviri çağır** (dış hukuk bürosu varsa derhal)
- [ ] **Üst yönetim bilgilendir** (CEO, Yönetim Kurulu — KVKK m.12/1 sorumluluk)
- [ ] **Olay açığa çıkma kaydı** (`incident log` — saat, kim, ne, kim haberdar edildi)
- [ ] **Resmi tebligatın fotokopisi** DPO dosyasına eklenmeli

### Saat 0 → Saat 2

- [ ] **Audit chain snapshot** — mevcut state'i dondur:
  ```bash
  # Son 12 ay hash-zincirli audit log'u doğrula + imzala
  ./infra/scripts/verify-audit-chain.sh --verbose \
      --since "$(date -u -d '12 months ago' +%Y-%m-%d)" \
      > /var/log/personel/compliance/kurul-denetim-$(date +%Y%m%d)-audit.txt
  # Çıktı dosyasını DPO imzasıyla arşivle
  ```
- [ ] **Retention enforcement snapshot** — son 12 ayın retention kayıtları:
  ```bash
  ./apps/qa/cmd/retention-enforcement-test/retention-enforcement-test \
      --report /var/log/personel/compliance/kurul-denetim-$(date +%Y%m%d)-retention.csv
  ```
- [ ] **Keystroke admin-blindness red team** — canlı kanıt üret:
  ```bash
  ./apps/qa/cmd/audit-redteam/audit-redteam \
      --api https://personel.musteri.com.tr \
      --admin-token "$DPO_TOKEN" \
      --tenant "$TENANT_ID" \
      --endpoint "$SAMPLE_ENDPOINT" \
      --report /var/log/personel/compliance/kurul-denetim-$(date +%Y%m%d)-redteam
  ```
  Beklenen çıktı: "PHASE 1 EXIT CRITERION #9: PASS — Admin CANNOT read keystroke content"
- [ ] **Backup snapshot kilitle** — son full backup dosyasını immutable flag ile koru:
  ```bash
  sudo chattr +i /var/lib/personel/backups/last-full.tar.gz
  ```
- [ ] **Live view ve DSR iş akışı pause edilir mi?** — DPO karar verir. Bazı Kurul yaklaşımları "yeni mutasyonsuz donma" ister.

### Saat 2 → Saat 8

- [ ] **Kanıt paketini hazırla** (bkz. §3)
- [ ] **Müfettiş için readonly hesaplar** (bkz. §4)
- [ ] **Hazır cevaplar dosyası** kontrol et + güncelle (bkz. §5)
- [ ] **Hukuk müşaviri ile senaryo prova** (2 saat)

### Saat 8 → Saat 24

- [ ] **Müfettiş ziyareti / toplantı** (hazırlandıktan sonra)
- [ ] Her soruya hazır cevap + doküman referansı sun
- [ ] Hiçbir "bilmiyorum" cevabı verme — "o soruyu 2 saat içinde yanıtlayalım" de
- [ ] Müfettişten **yazılı soru listesi** iste — sözlü sorulara sonradan yazılı cevap hakkı ayrılsın
- [ ] Tüm toplantı audio/video kayıt altına alınır mı müfettişle netleştir (bazı kurul uygulamalarında izin gerekir)

---

## 2. Rol ve Sorumluluklar

| Rol | Kim | Sorumluluğu |
|---|---|---|
| **Veri Sorumlusu Yetkilisi** | Müşteri kurum CEO/GM | KVKK m.3/1-i temsil yetkisi; Kurul yazışmalarını imzalar |
| **DPO** | Müşteri kurum DPO | Tüm operasyonel koordinasyon; müfettişin birincil iletişim kanalı |
| **Hukuk Müşaviri** | İç/dış avukat | Her beyan ve belge paylaşımı öncesi hukuki onay |
| **BT Güvenlik Direktörü** | Müşteri kurum CISO | Teknik kanıt toplama, log export, sistem ekran görüntüsü |
| **Personel Yazılım Destek** | Bize ulaşım | Sadece teknik soru cevaplama — müşteri verisine erişim YOK (Phase 1 on-prem garantisi) |

---

## 3. Kanıt Paketi

Müfettişe sunulacak (veya talep edilirse sunulacak) kanıtlar:

### 3.1. KVKK m.10 — Aydınlatma yükümlülüğü

- [ ] `docs/compliance/aydinlatma-metni-template.md` — müşteri özelleştirilmiş versiyon
- [ ] Her çalışanın ilk-login modalında aydınlatmayı **onayladığı** audit kaydı (`audit_log.action = 'portal.acknowledge'`)
- [ ] Şeffaflık Portalı ekran görüntüsü (tüm çalışanlar erişebiliyor kanıtı)

### 3.2. KVKK m.11 — İlgili kişinin hakları

- [ ] `docs/compliance/hukuki-riskler-ve-azaltimlar.md` R5 — DSR süreci
- [ ] Son 12 ayın DSR istatistikleri:
  ```sql
  SELECT request_type, count(*), avg(extract(epoch from responded_at - created_at))/86400 AS avg_days
  FROM dsr_requests
  WHERE tenant_id = '$TENANT_ID' AND created_at >= now() - interval '12 months'
  GROUP BY request_type;
  ```
- [ ] 30 günlük SLA ihlali var mı kontrolü (`state = 'overdue'`)
- [ ] Örnek bir DSR fulfilment paketi (PII redakte edilmiş)

### 3.3. KVKK m.12 — Veri güvenliği (teknik tedbirler)

- [ ] **Anahtar hiyerarşisi**: `docs/architecture/key-hierarchy.md`
- [ ] **Keystroke admin-blindness red team raporu** (§1 Saat 2 çıktısı)
  - EC-9 = 1.0 (PASS) kanıtı
  - JSON rapor + insan okunur özet
- [ ] **Audit chain tamper-proof rapor** (§1 Saat 2 çıktısı):
  - 12 aylık `verify-audit-chain.sh` çıktısı
  - `broken_links=0` kanıtı
  - WORM bucket cross-check (varsa)
- [ ] **TLS/mTLS topolojisi**: `docs/architecture/mtls-pki.md`
- [ ] **RBAC matrisi**: `apps/api/internal/auth/rbac.go` (kodla senkron dokuman)
- [ ] **Penetrasyon testi raporu** (varsa — 3. taraf)

### 3.4. KVKK m.7 — Silme, yok etme, anonim hale getirme

- [ ] `docs/architecture/data-retention-matrix.md` — süre tablosu
- [ ] `docs/compliance/iltica-silme-politikasi.md` — süreç
- [ ] **Retention enforcement raporu** (§1 Saat 2 çıktısı):
  - Son 12 ay aylık CSV'ler
  - `offending_rows = 0` kanıtı her kategori için
  - İhlal varsa düzeltici eylem kayıtları
- [ ] **DSR erasure (crypto-erase) kanıtı**: en az bir örnek başarılı silme
  - Kanıt: Vault transit key destroy audit satırı
  - `users.pii_erased=true` tombstone kaydı
- [ ] **Legal hold register**: `legal_holds` tablosundaki aktif hold'lar
  ```sql
  SELECT id, reason_code, ticket_id, created_at FROM legal_holds
  WHERE tenant_id = '$TENANT_ID' AND released_at IS NULL;
  ```

### 3.5. KVKK m.6 — Özel nitelikli veri

- [ ] `docs/adr/0013-dlp-disabled-by-default.md` — DLP varsayılan kapalı
- [ ] `docs/compliance/dlp-opt-in-form.md` — eğer DLP aktif edilmişse imzalı form
- [ ] Ekran görüntüsü exclusion policy'si (`screenshot_exclude_apps`)
- [ ] m.6 sensitive bucket TTL tablosu (retention matrix §Sensitive-Flagged)

### 3.6. VERBİS kaydı

- [ ] `docs/compliance/verbis-kayit-rehberi.md` tamamlanmış
- [ ] VERBİS sicil numarası
- [ ] VERBİS ekran görüntüsü (müşteri hesabından)

### 3.7. DPIA

- [ ] `docs/compliance/dpia-sablonu.md` — doldurulmuş müşteri versiyonu
- [ ] Son güncelleme tarihi ve revizyon geçmişi
- [ ] Risk matrisi + azaltım planı

### 3.8. Alt veri işleyenler

- [ ] `docs/compliance/sub-processor-registry.md` — Phase 1: **sıfır sub-processor**
- [ ] DPA template + imzalı müşteri DPA'sı

---

## 4. Müfettiş için Readonly Hesaplar

Müfettiş bizzat sistemi incelemek isterse, iki gerçek readonly hesap oluşturulur:

### 4.1. Postgres readonly

```sql
-- Müfettiş için sadece SELECT hakkı
CREATE ROLE kurul_denetim_readonly LOGIN PASSWORD '<rastgele-32char>';
GRANT CONNECT ON DATABASE personel TO kurul_denetim_readonly;
GRANT USAGE ON SCHEMA public, audit TO kurul_denetim_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO kurul_denetim_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA audit TO kurul_denetim_readonly;
-- Hassas tablo maskelemesi (keystroke_keys yalnızca metadata):
REVOKE SELECT (wrapped_dek, iv) ON keystroke_keys FROM kurul_denetim_readonly;
```

### 4.2. Console readonly (Auditor role)

Keycloak'ta **auditor** rolü zaten var. Müfettiş için:

1. Keycloak'ta `kurul-denetim@kvkk.gov.tr` kullanıcısı oluştur
2. Role `auditor` ata (bkz. `apps/api/internal/auth/rbac.go`)
3. MFA zorunlu
4. Session TTL: 4 saat (günlük yenilenir)
5. IP kısıtlaması: müfettişin bildirdiği IP aralığı

**Auditor rolü yetkileri** (kod ile senkron — `rbac.go`):
- Audit log GÖRÜNTÜLEYEBİLİR (tüm)
- DSR GÖRÜNTÜLEYEBİLİR (tüm)
- Policy GÖRÜNTÜLEYEBİLİR
- Evidence pack indirebilir
- Live view BAŞLATAMAZ
- Keystroke plaintext OKUYAMAZ (admin-blindness)
- Endpoint mutate EDEMEZ

### 4.3. Query şablonları (müfettiş için hazır)

```sql
-- Son 30 gün DSR talepleri
SELECT id, request_type, state, created_at, sla_deadline, responded_at
FROM dsr_requests WHERE created_at > now() - interval '30 days'
ORDER BY created_at DESC;

-- Son 30 gün live view oturumları
SELECT id, requester_id, approver_id, reason_code, state, created_at, actual_duration_seconds
FROM live_view_sessions WHERE created_at > now() - interval '30 days'
ORDER BY created_at DESC;

-- Hash zincir bütünlük check
SELECT count(*) FROM audit.audit_events WHERE tenant_id = '$TENANT_ID';
-- broken_links kontrolü: verify-audit-chain.sh çıktısı
```

---

## 5. Muhtemel Sorular + Hazır Cevaplar

### S1. "Çalışanın klavye basışları saklanıyor mu?"

**Cevap**: Sadece **istatistiksel metadata** (WPM, dakikada tuş sayısı) varsayılan olarak saklanır. **Klavye içeriği** varsayılan olarak **kapalıdır**. DLP (Data Loss Prevention) aktifleştirilirse sadece **şifreli** biçimde (AES-256-GCM + PE-DEK) saklanır; hiçbir yönetici rolü (Admin, DPO dahil) bu içeriği **düz metin olarak okuyamaz**. Sadece kapalı-kutu DLP motoru, önceden tanımlı DLP kurallarıyla eşleşme için içeriği çözer. **Kanıt**: §3.3 keystroke admin-blindness red team raporu.

### S2. "Ekran görüntüleri ne kadar saklanıyor?"

**Cevap**: Varsayılan 30 gün, maksimum 90 gün (`data-retention-matrix.md`). m.6 sensitive bucket'a işaretlenirse 7 güne indirilir. Süre dolduğunda MinIO lifecycle policy otomatik siler. **Kanıt**: `docs/architecture/data-retention-matrix.md` + son retention enforcement raporu.

### S3. "Bir çalışan verilerinin silinmesini talep etti, ne oldu?"

**Cevap**: Personel Platformu'nun KVKK m.11/f crypto-erase pipeline'ı Phase 1 item #69'da implement edildi. Süreç: MinIO blob'ları silinir → ClickHouse time-series satırlar silinir → Postgres subject satırları silinir → Vault transit anahtarı destroy edilir (mathematically unrecoverable). Audit log asla silinmez (m.12 invariant). Kanıt: `apps/api/internal/dsr/fulfillment.go` + integration test `dsr_erasure_test.go`.

### S4. "Canlı izleme (live view) nasıl kontrol ediliyor?"

**Cevap**: HR dual-control. Sistem **talep eden** (investigator) ile **onaylayan** (HR representative) rollerinin **farklı kişiler** olmasını **zorunlu** kılar. Her oturum maksimum 15 dakika (tek onay) veya 60 dakika (iki onay ile uzatılmış). Oturum başlama, uzatma ve sonlandırma olayları hash-zincirli audit log'a yazılır. Kanıt: `apps/api/internal/liveview/` + §3.3 audit chain raporu.

### S5. "Özel nitelikli veri koruması nasıl yapılıyor?"

**Cevap**: m.6 kapsamındaki potansiyel veriler için:
1. **Ekran görüntüsü exclusion policy**: bankacılık, şifre yöneticisi, incognito mode uygulamalarında capture YOK
2. **Window title regex**: sağlık, sendika, ibadet anahtar kelimelerinde sensitive flag
3. **Sensitive bucket** ayrı MinIO prefix + daha kısa TTL (7 gün)
4. **DLP** varsayılan KAPALI — açılırsa üçlü imza (DPO + CISO + Hukuk) + DPIA update
Kanıt: `docs/architecture/data-retention-matrix.md` §Sensitive-Flagged + ADR 0013.

### S6. "Veri ihlali (data breach) durumunda ne yapıyorsunuz?"

**Cevap**: 72 saat KVKK m.12/5 bildirim yükümlülüğü. Incident response runbook: `docs/security/runbooks/incident-response-playbook.md`. SOC 2 CC7.3 collector bu olayları evidence locker'a kaydeder. Son 12 ay breach: [`SELECT count(*) FROM incident_closures WHERE tenant_id = '$TENANT_ID'`].

### S7. "Yönetici kötü niyetli olsa bile hangi veri koruması var?"

**Cevap**: İki katmanlı:
1. **Kriptografik**: Keystroke/DLP içeriği admin ayrıcalıklarıyla bile düz metin okunamaz (ADR 0013 + key hierarchy)
2. **Audit chain immutable**: `audit.audit_events` tablosu append-only + hash chained + Vault-signed checkpoints + WORM bucket sink. Yönetici audit kaydını silemez.
Kanıt: §3.3 red team + audit chain raporu.

### S8. "Alt veri işleyen var mı?"

**Cevap**: Phase 1 on-prem MVP: **sıfır**. Tüm veri müşteri data center'ında kalır. Personel Yazılım kurumunun altyapısına hiçbir veri otomatik akmaz. Kanıt: `docs/compliance/sub-processor-registry.md` §1.

### S9. "Saklama süresi matriksi gerçekten uygulanıyor mu?"

**Cevap**: Evet. Üç katmanlı garantili:
1. **ClickHouse TTL** — tablo tanımında declarative (SHOW CREATE TABLE kanıt)
2. **MinIO Lifecycle** — bucket policy (mc ilesekretlistele kanıt)
3. **Gece çalışan retention enforcement test** — ihlal varsa alarm (`infra/runbooks/retention-enforcement.md`)
Kanıt: §3.4 retention enforcement raporları.

### S10. "Çalışanlar bu sisteme rıza verdi mi? Açık rıza mı, meşru menfaat mi?"

**Cevap**: İşveren meşru menfaati temel hukuki dayanaktır (KVKK m.5/2-f). Açık rıza **sadece** DLP keystroke content scanning için gereklidir (ADR 0013 opt-in ceremony). Aydınlatma yükümlülüğü (m.10) tüm çalışanlara şeffaflık portalı + ilk-login modal ile gerçekleştirilir. Kanıt: `docs/compliance/aydinlatma-metni-template.md` + her çalışanın `portal.acknowledge` audit kaydı.

### S11. "VERBİS kaydınız güncel mi?"

**Cevap**: [VERBİS sicil no]. Son güncelleme: [tarih]. Ekran görüntüsü §3.6.

### S12. "Personel Yazılım'ın kendi çalışanları müşteri verisine erişebiliyor mu?"

**Cevap**: Phase 1 on-prem: **hayır**. Destek talepleri için müşteri manuel ve PII-redakte log paylaşabilir. Personel Yazılım çalışanları kesinlikle müşteri altyapısına uzaktan erişim yapmaz. `docs/compliance/sub-processor-registry.md` §1.2 garanti.

### S13. "Eski çalışan bilgileri nasıl silinir?"

**Cevap**: İş akışı:
1. HR sisteminde termination kaydı (HRIS webhook → Keycloak devre dışı)
2. Keycloak'ta 4 saat içinde kullanıcı devre dışı (ADR 0018)
3. Eski çalışanın verisi normal TTL matrisine tabi (en uzun 5 yıl)
4. DSR erasure talebi ile daha önce silinebilir (anında crypto-erase)
5. Legal hold varsa TTL durur

### S14. "Audit log gerçekten değiştirilemez mi?"

**Cevap**: Evet. Üç katmanlı korumalı:
1. **Postgres RLS + REVOKE**: `audit.audit_events` için sadece INSERT + SELECT izin, UPDATE/DELETE yok
2. **Hash chain**: her satır `prev_hash = SHA256(previous_row.canonical)`, kırılma tespit edilir
3. **Signed daily checkpoint**: Vault Ed25519 ile imzalı günlük checkpoint + WORM bucket yazımı
Müfettiş `verify-audit-chain.sh` ile bunu canlı doğrulayabilir (§3.3).

### S15. "Çalışan hakları başvurusu geldiğinde kim karar veriyor?"

**Cevap**: DPO (müşteri kurum). Personel Platformu sadece altyapı sağlar — her DSR fulfilment kararı müşteri DPO yetkisindedir. KVKK m.11 süreci + 30 gün SLA uygulanır.

---

## 6. Yasaklı Eylemler

Denetim sırasında **asla yapılmayacak**:

- [ ] Log dosyalarını silme / değiştirme (tamper)
- [ ] Audit chain'e manuel giriş ekleme
- [ ] Ekran görüntüsü retention'ını uzatma veya kısaltma
- [ ] DLP aktif ise ceremony belgesini hazırlamadan aktiflik iddia etme
- [ ] Müfettişe içerik erişimi olan admin hesabı verme (sadece auditor role)
- [ ] Müşteri çalışanlarına "bu denetim hakkında konuşma" talimatı (whistleblower chilling)
- [ ] Müfettişin sorusunu cevapsız bırakma — zaman iste ama cevapla
- [ ] Avukat olmadan beyan imzalama

---

## 7. Sonraki Adımlar

Denetim tamamlandığında:

1. **Yazılı rapor beklenir** (Kurul 30 gün içinde gönderir)
2. **Bulgular varsa düzeltme eylem planı** (90 gün)
3. **Cezai yaptırım** (m.18/1 — 50.000 TL'den 5.000.000 TL'ye) — hukuk müşaviri değerlendirir
4. **İdari yaptırıma itiraz** (60 gün — idare mahkemesi)
5. **Runbook güncelle** — sorulan sorular + öğrenilen dersler §5'e eklenir

---

## 8. İlgili Dokümanlar

- `docs/compliance/kvkk-framework.md` — ana çerçeve
- `docs/compliance/hukuki-riskler-ve-azaltimlar.md` — risk register
- `docs/compliance/dpia-sablonu.md` — DPIA şablonu
- `docs/compliance/verbis-kayit-rehberi.md` — VERBİS kayıt
- `docs/compliance/iltica-silme-politikasi.md` — silme/imha
- `docs/compliance/sub-processor-registry.md` — alt işleyen kaydı
- `docs/compliance/dlp-opt-in-form.md` — DLP açık rıza
- `docs/compliance/aydinlatma-metni-template.md` — KVKK m.10
- `docs/compliance/acik-riza-metni-template.md` — KVKK m.5/1
- `docs/architecture/data-retention-matrix.md` — saklama matrisi
- `docs/architecture/key-hierarchy.md` — kriptografik hiyerarşi
- `docs/architecture/mtls-pki.md` — mTLS topolojisi
- `docs/adr/0013-dlp-disabled-by-default.md` — DLP varsayılan kapalı
- `infra/runbooks/retention-enforcement.md` — retention enforcement operatör
- `infra/scripts/verify-audit-chain.sh` — audit chain doğrulayıcı
- `apps/qa/cmd/audit-redteam/main.go` — keystroke admin-blindness red team

---

*Versiyon 1.0 — Nisan 2026. Bu runbook her gerçek denetim sonrası §5 ve §6 güncellenerek öğrenilen derslerle zenginleştirilmelidir. Kurul yaklaşımı her 2-3 yılda bir yenilenen Kurul kararlarıyla evrilir; yıllık bir gözden geçirme şarttır.*
