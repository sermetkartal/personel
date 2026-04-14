# Personel — Yeni Yönetici Onboarding Kılavuzu

> Dil: Türkçe. Hedef okuyucu: yeni atanan Personel platform yöneticisi / DPO / SRE. Bu kılavuz ilk 4 haftayı planlar; sonunda yeni yönetici bağımsız operasyonel görev alabilir.

## Amaç

4 hafta sonunda yeni yönetici:
- 10 cihazı bağımsız enroll edebilir
- Bir DSR'ı başından sonuna işleyebilir
- Bir politika ihlalini araştırabilir
- Incident response sürecine girebilir
- KVKK çerçevesini özümsemiştir

---

## Gün 1 — Erişim Kurulumu

### Sabah (09:00 - 12:00)

- [ ] **Kurumsal giriş**: Keycloak hesabı (kendi e-postanızla; "admin" ortak hesap asla kullanılmaz)
- [ ] **VPN erişimi**: OpenVPN / WireGuard kurulumu, test
- [ ] **Bastion SSH**: `bastion.personel.musteri.local` → SSH key upload + test login
- [ ] **MFA aktif**: Authy / Google Authenticator kurulumu
- [ ] **Parola yöneticisi**: 1Password / Bitwarden takım vault'a davet
- [ ] **Slack / Teams kanal üyelikleri**:
  - `#personel-ops`
  - `#personel-alerts`
  - `#personel-oncall`
  - `#personel-kvkk`
  - `#personel-dpo` (sadece DPO onboarding'i ise)

### Öğleden Sonra (13:00 - 17:00)

- [ ] **Repository erişimi**: `github.com/<musteri>/personel` read + issue yetki
- [ ] **Docs okuma başlangıcı**: `CLAUDE.md` §0 + §1-§3 (projenin ne, neden, nasıl)
- [ ] **Admin Console turu**: Senior yönetici ile birlikte `docs/user-manuals/admin-manual-tr.md` §1-§3 üzerinden yürü
- [ ] **Test cihazı enroll**: Kendi laptop'unuza bir test MSI kur + enroll yap (üretime dokunmadan dev tenant'ta)

### Akşam Checkoff

- Tüm erişimler çalışıyor mu?
- Admin Console'a giriş yaptınız mı?
- Test cihazı event akıyor mu?

---

## Gün 2 — Shadow DSR + Canlı İzleme

### Sabah

- [ ] Senior DPO ile **açık bir DSR**'ı birlikte işleyin:
  - Başvuru okuma
  - Kapsam belirleme
  - Erişim export'u üretme
  - Cevap e-postasının hazırlanması
  - SOC 2 P7.1 evidence kaydının otomatik oluştuğunu doğrulama
- [ ] **DSR runbook oku**: `docs/compliance/iltica-silme-politikasi.md`

### Öğleden Sonra

- [ ] Senior investigator ile **canlı izleme akışının tamamı**:
  - Talep oluşturma (dummy ticket ID ile)
  - HR onayı (farklı kullanıcı — kurallar!)
  - LiveKit oturumu açma + sonlandırma
  - Audit log'a düşen kaydı doğrulama
  - CC6.1 evidence kaydının oluştuğunu görme
- [ ] Live view protokolü: `docs/architecture/live-view-protocol.md`

### Akşam Checkoff

- DSR akışını anlıyor musunuz?
- Çift onay kuralı neden bu kadar önemli?

---

## Gün 3 — Incident Response Masabaşı (Tabletop Exercise)

Senior ekiple birlikte 3 saatlik bir masabaşı tatbikatı:

- [ ] `docs/security/incident-response-playbook.md` oku
- [ ] **Senaryo 1**: Agent mass disconnect — 100 uç nokta aynı anda offline oldu. Adımları sırayla yürüt.
- [ ] **Senaryo 2**: Admin hesabı şüpheli aktivite — audit log arama + legal hold koyma + DPO bildirim
- [ ] **Senaryo 3**: Vault seal olayı — Shamir unseal seremonisi roleplay
- [ ] **Senaryo 4**: KVKK veri ihlali — 72 saat kural, Kurul bildirim süreci

Her senaryo için:
- Hangi dashboard'a baktınız?
- Hangi komutu yürüttünüz?
- Kim'i bilgilendirdiniz?
- Kararınızı audit log'a nasıl yazdınız?

---

## Gün 4 — KVKK Eğitimi

Bir gün tamamen hukuki çerçeveye ayrılır. Okuma + alıştırma formatı:

### Okuma Listesi (4 saat)

- [ ] `docs/compliance/kvkk-framework.md` — tam metni
- [ ] `docs/compliance/aydinlatma-metni-template.md`
- [ ] `docs/compliance/acik-riza-metni-template.md`
- [ ] `docs/compliance/dpia-sablonu.md`
- [ ] `docs/compliance/iltica-silme-politikasi.md`
- [ ] `docs/compliance/hukuki-riskler-ve-azaltimlar.md`
- [ ] `docs/compliance/verbis-kayit-rehberi.md`
- [ ] `docs/compliance/calisan-bilgilendirme-akisi.md`

### Quiz (1 saat)

Aşağıdaki soruları yanıtlayın:

1. KVKK m.11'de sayılı 7 hakkı listeleyin.
2. DSR başvurusunun cevap süresi kaç gündür? Hangi maddeden?
3. Özel nitelikli kişisel veri nedir? Hangi koşullarda işlenebilir?
4. Aydınlatma metni hangi bilgileri içermek zorundadır?
5. Kişisel veri ihlali durumunda Kurul bildirimi kaç saat içinde yapılmalıdır?
6. Meşru menfaat dengelemesi ne demek, nasıl yapılır?
7. Klavye içerik DLP'sinin varsayılan durumu nedir, neden?

### Alıştırma (3 saat)

Sentez: Kendi dilinizle bir sahne senaryosu yazın:
> "Çalışan Ayşe, şirketten ayrılmasından 2 ay sonra DSR ile tüm verilerinin silinmesini istiyor. Ancak Ayşe'nin geçen ayki bir dosya hareketi aktif bir yolsuzluk soruşturmasının parçası ve üzerinde legal hold var. Siz DPO olarak ne yaparsınız?"

---

## Hafta 1 Sonu Değerlendirme

**Bağımsız yapabildikleriniz**:

- [ ] 10 cihaz enroll etme
- [ ] Bir DSR başından sonuna işleme
- [ ] Bir politika oluşturma + push etme
- [ ] Audit log üzerinden arama yapma
- [ ] Incident playbook'u açıklama

Eksikleri senior ekiple çözün.

---

## Hafta 2-4 — Gözlemli Solo Çalışma

### Hafta 2

- Düşük önem seviyesindeki ticket'larda **solo çalışma**:
  - Yeni kullanıcı onboarding
  - Rutin DSR erişim talepleri
  - Politika ince ayarları
- Senior **günde 30 dakika** kod/karar review ile destek
- **Kendi dokümantasyonunuzu başlatın**: ilk hafta sırasında çözdüğünüz sorunları `docs/operations/troubleshooting.md` içine PR olarak ekleyin

### Hafta 3

- Orta önem seviyesi ticket'lar:
  - Uzaktan wipe komutları
  - Canlı izleme talep değerlendirmeleri
  - Politika çatışma çözümü
- Senior **haftada 2 kez** review
- **Nöbete katılma** (secondary on-call) — asla solo değil

### Hafta 4

- Yüksek önem seviyesi ticket'lar (senior mentor gözetiminde):
  - Kompleks DSR ret kararları
  - Çoklu endpoint incident response
  - Faz 5 cluster işlemleri (Postgres replica restart, ClickHouse compaction)
- **Primary on-call rotasyonuna katılma** (ilk nöbet senior shadowing ile)

---

## 4 Hafta Sonu Final Değerlendirme

Senior DPO / SRE tarafından:

- [ ] Teknik: 10 cihaz enroll, 1 DSR tam, 1 incident tabletop solo
- [ ] Hukuki: KVKK quiz ≥ 80%
- [ ] Operasyonel: Audit log arama, policy editor, live view akışları
- [ ] Dokümantasyon: En az 5 troubleshooting senaryosu PR'ı
- [ ] İletişim: Slack kanalına en az 10 sorun raporu

Başarılı → nöbet rotasyonuna tam dahil. Eksik → 2 haftalık uzatma.

---

## Kaynaklar

### Dokümanlar

- `CLAUDE.md` — proje state + mimari
- `docs/architecture/overview.md` — exec summary (TR)
- `docs/architecture/c4-container.md` — container diagramı
- `docs/user-manuals/admin-manual-tr.md` — bu kılavuzla birlikte
- `docs/operations/installation-guide.md` — kurulum
- `docs/operations/ops-runbook.md` — günlük işletim
- `docs/operations/troubleshooting.md` — hata giderme
- `docs/security/incident-response-playbook.md` — P1-P4 IRP
- `docs/compliance/kvkk-framework.md` — KVKK tam çerçeve

### Slack Kanalları

- `#personel-ops` — günlük operasyon
- `#personel-alerts` — Prometheus alert akışı
- `#personel-oncall` — nöbet rotasyon
- `#personel-kvkk` — hukuki konular
- `#personel-dpo` — DPO-private

### On-Call Rosteri

`infra/runbooks/oncall-roster.md` — TBD (kurum belgesi).

### Yardım Kanalı

- Mentor: senior yönetici
- Şikayet / geri bildirim: bölüm yöneticisi
- KVKK soru: kurum DPO
- Acil: Slack `#personel-oncall` → `@here` + telefon

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #164 — İlk sürüm |
