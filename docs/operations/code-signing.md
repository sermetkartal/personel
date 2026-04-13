# Personel Agent — Kod İmzalama Operasyonel Rehberi

> Hedef kitle: müşteri DPO, IT yöneticisi, IT operatörü.
> Amaç: Personel Windows agent binary'lerinin (`personel-agent.exe`,
> `personel-agent-watchdog.exe`, `enroll.exe`) ve MSI installer'ının
> Authenticode kod imzalama sertifikası ile imzalanması süreci.
>
> Versiyon: 1.0 — Faz 4 #40 scaffold

---

## 1. Neden kod imzalama?

Personel agent, kullanıcının kurumsal cihazına yüklenir ve sistem servisi olarak
çalışır. İmzasız bir Windows binary'si:

- Windows SmartScreen tarafından "Bilinmeyen yayıncı" uyarısı gösterir
- UAC kurulum ekranında sarı/turuncu shield ile çıkar (mavi yerine)
- Bazı kurumsal antivirüs yazılımları (Defender for Endpoint, CrowdStrike,
  SentinelOne) heuristik karantinaya alabilir
- Group Policy ile katı SmartScreen politikası olan ortamlarda kurulum
  reddedilir
- Windows Installer log'larında "publisher: unknown" görünür

İmzalı bir binary:

- Yayıncı adı doğrulanmış olarak gösterilir (örn. "ACME Bilgi Teknolojileri A.Ş.")
- UAC penceresinde mavi shield + güvenli yayıncı başlığı
- EV (Extended Validation) sertifikası ile SmartScreen reputation **anında**
  oluşur — yeni binary için "smart screen filter" beklemesi yok
- Antivirüs imzalı PE'leri whitelist'e daha kolay ekler

---

## 2. KVKK / Aydınlatma Metni bağlantısı

Kod imzalama, KVKK m.5'in **veri sorumlusunun meşru menfaati** ve m.10
**aydınlatma yükümlülüğü** bağlamında doğrudan bir **güven sinyalidir**.
Şirket, çalışana "bu yazılım bilinen, güvenilir bir kurumsal uygulamadır"
mesajını teknik olarak da iletir. Aydınlatma Metni şablonu
(`docs/compliance/aydinlatma-metni-template.md`) "yazılım yayıncısı"
alanını referans alır — bu alan kod imzalama sertifikasında görünen
ortak ad (CN) ile tutarlı olmalıdır.

**Önemli**: Kod imzalama sertifikası, KVKK kapsamında bir "kişisel veri
işleme aracı" değildir. Sertifika öznesi (subject) **kurumsal kimlik**
içerir (şirket adı, vergi no, ülke). Bireysel çalışan adı **kullanılmaz**.

---

## 3. Sertifika seçimi

| Tip | Tipik fiyat | SmartScreen reputation | EV doğrulama | Saklama |
|---|---|---|---|---|
| **OV (Organization Validation)** | $200–$700 / yıl | Birikimle (haftalar/aylar) | Hayır | PFX dosyası, yazılım keystore |
| **EV (Extended Validation)** | $700–$1500 / yıl | **Anında** | Evet (kurumsal evraklar + telefon) | **Donanım** (FIPS 140-2 USB token veya HSM zorunlu) |

### Tavsiye

- **Pilot / iç dağıtım**: OV yeterlidir. PFX dosyası şifreli depoda saklanır.
- **Production / 50+ endpoint**: EV alın. SmartScreen sürtünmesi yok, ek olarak
  Windows Defender Application Control (WDAC) için de uygun.

### Önerilen vendor'lar (Turkey-friendly)

- **Sectigo** (eski Comodo) — TR distribütör mevcut, fatura USD veya TRY
- **DigiCert** — premium, en hızlı reputation
- **GlobalSign** — TR distribütör mevcut
- **SSL.com** — daha ucuz EV, USB token gönderim 5–10 iş günü

> ⚠️ Türkiye'de bulunan KamuSM gibi yerel sertifika otoriteleri **kod
> imzalama** sertifikası vermez (sadece TLS, e-imza). Kod imzalama için
> **mutlaka uluslararası bir CA** seçilmelidir.

---

## 4. Sertifika satın alma kontrol listesi

- [ ] Şirket ticaret sicil belgesi (apostilli olabilir, vendor'a göre değişir)
- [ ] Vergi levhası (kurumsal kimlik doğrulama için)
- [ ] DUNS numarası (DigiCert + SSL.com için zorunlu — ücretsiz başvuru)
- [ ] Telefon doğrulama için kurumsal sabit hat (mobil **kabul edilmez**)
- [ ] EV alıyorsanız: vendor'ın USB token gönderebileceği fiziksel adres
- [ ] Kart sahibi veya kurumsal kredi kartı + ödeme onayı
- [ ] (EV için) Telefon görüşmesi sırasında İngilizce konuşabilen yetkili kişi

---

## 5. Sertifikanın güvenli saklanması

### OV (PFX dosyası)

- **Asla** repo içine commit edilmez. `.gitignore`'da `*.pfx` zaten mevcuttur.
- Şifre minimum 24 karakter, password manager'da saklanır
  (1Password, Bitwarden, Vault).
- Yedek: PFX + şifre **iki farklı şifreli ortamda** (örn. Vault + USB safe).
- Erişim: sadece IT Security Lead + DPO.

### EV (USB token / HSM)

- USB token fiziksel olarak kilitli kasada (ya da HSM rack'inde).
- Token PIN'i password manager'da, **token'ın kendisi ile aynı yerde olmaz**.
- Token kayıp/çalıntı durumunda **derhal CA'ya revoke** çağrısı yapılır.

---

## 6. CI/CD entegrasyonu — GitHub Actions

Repo'da iki ilgili dosya vardır:

- `.github/workflows/build-agent.yml` — koşullu imzalama yapan workflow
- `apps/agent/installer/sign-binaries.ps1` — yerel imzalama wrapper

### 6.1 Sertifikayı CI'a yüklemek (3 adımda aktivasyon)

Sertifika satın alındıktan sonra **sadece** üç adım gerekir:

#### Adım 1 — PFX'i base64 encode et

```powershell
# Yerel makinede (PFX dosyası elinizdeyse):
$pfx = [Convert]::ToBase64String([IO.File]::ReadAllBytes("C:\secure\codesign.pfx"))
$pfx | Set-Clipboard
# Artık base64 string clipboard'da
```

#### Adım 2 — GitHub repo secret'larını ekle

GitHub web UI:

1. Repo → **Settings** → **Secrets and variables** → **Actions**
2. **New repository secret**:
   - Name: `CODE_SIGNING_CERT_PFX_BASE64`
   - Value: clipboard'daki base64 string'i yapıştır
3. Yeni secret daha:
   - Name: `CODE_SIGNING_CERT_PASSWORD`
   - Value: PFX şifresi

#### Adım 3 — Workflow'u tetikle

```bash
# Bir tag push'u yeterli — release de oluşturulur:
git tag v0.2.0
git push origin v0.2.0
```

Workflow otomatik olarak secret'ları algılar (`steps.signconf.outputs.enabled`),
PFX'i runner'ın temp dizinine decode eder, üç binary + MSI'yi imzalar,
`signtool verify /pa` ile doğrular, temp PFX'i siler ve imzalı artifact'ları
GitHub Release'e ekler.

> Secret'lar **tanımlı değilse** workflow yine çalışır, sadece imzalama
> adımları atlanır ve bir uyarı log'lanır. Bu sayede pilot dönemde
> imzasız dev build'leri kırılmaz.

### 6.2 Vendor / TSA değişikliği

Eğer Sectigo dışında bir CA seçtiyseniz, timestamp authority URL'i değişebilir.
Repo → **Settings** → **Variables** → **Actions** altına bir **variable**
(secret değil) ekleyin:

- Name: `CODE_SIGNING_TIMESTAMP_URL`
- Value: vendor'ın TSA URL'i
  - Sectigo: `http://timestamp.sectigo.com` (varsayılan)
  - DigiCert: `http://timestamp.digicert.com`
  - GlobalSign: `http://timestamp.globalsign.com/scripts/timstamp.dll`
  - SSL.com: `http://ts.ssl.com`

---

## 7. Yerel build'de imzalama

Geliştirici makinesinde imzalı bir MSI üretmek için:

```powershell
# Önce environment variable'ları set et:
$env:CODE_SIGNING_CERT_PFX      = "C:\secure\codesign.pfx"
$env:CODE_SIGNING_CERT_PASSWORD = "<pfx-password>"

# Standart build:
cd C:\personel\apps\agent\installer
.\build-msi.ps1

# Sonra imzala:
.\sign-binaries.ps1
```

`sign-binaries.ps1` script'i CI workflow'u ile **birebir aynı** signtool
parametrelerini kullanır — yerel ve CI imzaları arasında fark yoktur
(timestamp dışında).

Env var'ları **set etmediyseniz**, script "code signing not configured" mesajı
verir ve exit 0 ile çıkar (build kırılmaz).

---

## 8. İmzanın doğrulanması

Bir kullanıcının elinde imzalı bir MSI olduğunda doğrulama:

### 8.1 PowerShell ile

```powershell
Get-AuthenticodeSignature .\personel-agent.msi | Format-List
```

Beklenen çıktı:

```
SignerCertificate      : [Subject] CN=ACME Bilgi Teknolojileri A.Ş., O=ACME ...
                         [Issuer]  CN=Sectigo Public Code Signing CA R36
                         [Serial Number] ...
                         [Not Before] ...
                         [Not After] ...
TimeStamperCertificate : [Subject] CN=Sectigo RSA Time Stamping Signer #4, ...
Status                 : Valid
StatusMessage          : Signature verified.
SignatureType          : Authenticode
IsOSBinary             : False
```

`Status: Valid` + `TimeStamperCertificate` dolu → her şey yolunda.

### 8.2 signtool ile (ayrıntılı)

```cmd
"C:\Program Files (x86)\Windows Kits\10\bin\10.0.22621.0\x64\signtool.exe" verify /pa /v personel-agent.msi
```

Beklenen çıktı son satırı: `Successfully verified: personel-agent.msi`

### 8.3 GUI ile

Windows Explorer'da MSI'ye sağ tık → **Properties** → **Digital Signatures**
sekmesi → imzayı seç → **Details** → **View Certificate**.

---

## 9. Sertifika rotasyon prosedürü

OV sertifikalar 1-3 yıl, EV sertifikalar 1-3 yıl geçerlidir. Süre dolmadan
**en az 30 gün önce** yenileme başlatılmalıdır.

### 9.1 Standart rotasyon (süresi dolmadan)

1. Vendor'dan yeni sertifikayı satın al (OV) veya yenile (EV).
2. Yeni PFX dosyasını alın (OV) veya yeni token'a key roll-over yapın (EV).
3. Yeni PFX'i base64 encode et (Adım 1, §6.1).
4. GitHub Actions secret'ını **güncelle** (silme → yeniden oluştur):
   - `CODE_SIGNING_CERT_PFX_BASE64` → yeni değer
   - `CODE_SIGNING_CERT_PASSWORD` → yeni şifre (eğer değiştiyse)
5. **Eski PFX'i hâlâ saklayın** — daha önce imzalanmış release'lerin
   timestamp doğrulaması için referans gerekebilir (5 yıl saklayın).
6. Bir test tag push'u ile yeni sertifika ile build alın
   (`git tag v0.x.0-rc1 && git push origin v0.x.0-rc1`).
7. İmzanın `Get-AuthenticodeSignature` ile yeni issuer/subject gösterdiğini
   doğrulayın.
8. Sürüm changelog'una "code signing certificate rotated" notu ekleyin.

### 9.2 Acil rotasyon (compromise / kayıp)

1. Vendor'a **derhal revoke** isteği gönderin (telefonla en hızlı).
2. CRL/OCSP'nin güncellendiğini doğrulayın
   (`certutil -url <crl-url>`).
3. Yeni sertifika satın alın (vendor genelde compromise durumunda hızlı
   yeniden basım yapar).
4. §9.1 adımlarını izleyin.
5. **Compromise edilen sertifika ile imzalanmış tüm release'leri** GitHub
   Release sayfasından "yellow banner" ile işaretleyin: "compromised cert,
   re-download required".
6. Müşteri DPO'larına e-posta + portal duyuru.

---

## 10. Test prosedürü (yeni sertifika veya yeni release)

1. CI workflow'unu manuel tetikle: GitHub → Actions → "Build Agent
   (signed, release-ready)" → **Run workflow** → branch: main.
2. Workflow başarılı tamamlandığında "personel-agent-msi" artifact'ını indir.
3. Yerel Windows makinede:
   ```powershell
   Get-AuthenticodeSignature .\personel-agent.msi
   ```
   `Status: Valid` görmelisin.
4. MSI'yi bir test VM'inde kur (`msiexec /i personel-agent.msi /qn ...`).
5. UAC ekranının **mavi** shield gösterdiğini ve "Verified publisher" satırının
   şirket adınızı içerdiğini doğrula.
6. Defender for Endpoint / kullandığınız EDR'in dosyayı karantinaya almadığını
   konfirme et (5-10 dakika bekleyin).

---

## 11. Sorun giderme

### "Status: NotSigned" veya "HashMismatch"

- PFX bozulmuş veya yanlış şifre. Yerel olarak `signtool sign /debug` ile
  yeniden imzala, hatayı incele.

### "Status: NotTrusted"

- Sertifika zinciri eksik. PFX'i export ederken **"Include all certificates
  in the certification path"** seçeneğinin işaretli olduğundan emin ol.

### "TimeStamperCertificate is empty"

- `/tr` URL'i ulaşılamaz veya cevap vermedi. Vendor'ın TSA'sının ayakta
  olduğunu kontrol et, alternatif TSA dene (örn. DigiCert TSA Sectigo PFX
  ile çalışır — TSA cross-vendor uyumludur).

### CI'da "signtool not found"

- Windows SDK runner image'ında değişmiş olabilir. `build-agent.yml` içindeki
  "Locate signtool.exe" adımı en yüksek SDK sürümünü tarar; yine bulamazsa
  workflow'a `actions/setup-msbuild@v2` ekleyin.

### "Cannot import the keys" (EV USB token)

- Token sürücüsü kurulmamış. SafeNet Authentication Client veya vendor'ın
  driver paketini yükleyin. CI'da EV token kullanmıyorsanız (genelde
  kullanılmaz — EV tokenlar manuel imzalama içindir), bu adımı atla.

---

## 12. Bekleyen üst yönetim kararları

- [ ] OV mı EV mi? (Rakipler genelde EV kullanıyor — Teramind, Veriato.)
- [ ] Yıllık bütçe (sertifika + token + TSA + revoke ücretleri)
- [ ] Hangi tüzel kişilik adına alınacak (ana şirket, satıcı, distribütör)
- [ ] Token fiziksel saklama lokasyonu (HQ kasası, IT kasası, off-site safe)
- [ ] Yenileme sorumluluğu (DPO mu, IT Lead mi — calendar reminder kim atar)

---

## 13. Referanslar

- Microsoft — Sign tool reference:
  https://learn.microsoft.com/en-us/dotnet/framework/tools/signtool-exe
- Microsoft — SmartScreen application reputation:
  https://learn.microsoft.com/en-us/windows/security/threat-protection/microsoft-defender-smartscreen/microsoft-defender-smartscreen-overview
- Sectigo — Code signing certificate guide:
  https://www.sectigo.com/resource-library/what-is-code-signing
- DigiCert — Code signing best practices:
  https://www.digicert.com/blog/code-signing-best-practices
- Personel Aydınlatma Metni şablonu:
  `docs/compliance/aydinlatma-metni-template.md`
- ADR 0013 — DLP disabled by default (kod imzalama bağlamı):
  `docs/adr/0013-dlp-disabled-by-default.md`

---

*Versiyon 1.0 — Faz 4 #40 scaffold. Sertifika satın alındığında §6.1 üç-adım
prosedürü ile aktive edilir; başka kod değişikliği gerekmez.*
