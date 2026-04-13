# Personel Agent — GPO ile Dağıtım Runbook'u

> **Hedef kitle**: Active Directory yöneticileri, müşteri BT operasyonları, Personel DevOps ekibi.
> **Amaç**: Personel UAM ajanını kurumsal bir Active Directory ortamında Grup İlkesi (GPO) üzerinden merkezi olarak yönetmek.
> **Süre**: Yaklaşık 30-60 dakika (pilot OU için). Kademeli tam dağıtım günler sürebilir.
> **Ön koşul**: Müşteri zaten `personel-agent.msi` dosyasını yazılım dağıtım paketi olarak yayınlamış olmalı (veya `msiexec` yolunu kullanmalı). Bu runbook yalnızca **konfigürasyon** tarafını kapsar, MSI dağıtımını değil.

---

## 1. Genel Bakış

Personel ajanı iki kaynaktan konfigürasyon okur:

1. **Kurulum zamanlı ayarlar**: MSI yüklemesi sırasında `msiexec` komut satırı parametreleri (`GATEWAY_URL`, `TENANT_TOKEN`) — bunlar `HKLM\SOFTWARE\Personel\Agent\Config` altına yazılır ve `enroll.exe` tarafından tüketilir.
2. **Grup İlkesi ayarları**: `HKLM\SOFTWARE\Policies\Personel\Agent` altındaki değerler — bu runbook'un konusu. Bu yol, Grup İlkesi için ayrılmış standart Windows yoludur ve GPO yenilemelerinde otomatik olarak güncellenir.

**İki yol arasındaki öncelik**: Grup İlkesi değeri mevcutsa, kurulum zamanlı değerden önceliklidir ve bir sonraki politika yenilemesinde (`gpupdate /force` veya periyodik yenileme döngüsü) etkili olur.

### ADMX / ADML dosyaları

Personel, şu dosyaları sağlar:

| Dosya | Amaç |
|---|---|
| `personel-agent.admx` | Politika tanım dosyası (XML şeması) |
| `en-US\personel-agent.adml` | İngilizce görüntüleme metinleri |
| `tr-TR\personel-agent.adml` | Türkçe görüntüleme metinleri |

Bu dosyalar kaynak repoda `apps/agent/installer/admx/` altında yer alır ve MSI paketinin yanında müşteriye teslim edilir.

---

## 2. Ön Hazırlık

Başlamadan önce:

- [ ] Domain yönetici yetkiniz (`Domain Admins` veya en azından GPO oluşturma ve bağlama izni) olduğundan emin olun.
- [ ] Hedef OU kararlaştırılmış olmalı (`OU=Pilot,OU=Personel-Pilot,DC=kurumsal,DC=local` gibi).
- [ ] Müşteri DPO'su, **Gizlilik ve DLP** kategorisindeki herhangi bir ayar değişikliğini onaylamış olmalı (özellikle `ExcludedAppsAdditional` ve `DLPOptInAcknowledged`). Bu zorunlu bir KVKK gerekliliğidir — Aydınlatma Metni'nin güncel kalması gerekir.
- [ ] Pilot OU için en az 1, en fazla 5 endpoint belirlenmiş olmalı.
- [ ] Geri alma planı (rollback) yazılı hâle getirilmiş olmalı.
- [ ] Ajanın MSI üzerinden zaten kurulu olduğu doğrulanmalı (`sc query PersonelAgent` → `STATE: 4 RUNNING` veya en azından `STOPPED`).

---

## 3. ADMX Dosyalarını Merkezi Politika Deposuna Kopyalama

Active Directory merkezi politika deposu, tüm domain boyunca tutarlı GPO yönetimi için standart yoldur.

### 3.1 Merkezi depo yoksa oluştur

Domain Controller üzerinde, yükseltilmiş bir PowerShell veya Komut İstemi açın:

```cmd
mkdir "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions"
mkdir "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\en-US"
mkdir "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\tr-TR"
```

Not: `%USERDNSDOMAIN%` otomatik genişler (`kurumsal.local` gibi).

### 3.2 Personel dosyalarını kopyala

Personel teslim paketinden `admx/` dizinindeki dosyaları kopyalayın:

```cmd
copy personel-agent.admx "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\"
copy en-US\personel-agent.adml "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\en-US\"
copy tr-TR\personel-agent.adml "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\tr-TR\"
```

### 3.3 Replikasyonu bekle

SYSVOL replikasyonu tüm domain controller'lara yayılana kadar bekleyin (genelde 15 dakika, FRS yerine DFSR varsayılmıştır).

### 3.4 Doğrulama

Grup İlkesi Yönetim Konsolu'nu (`gpmc.msc`) açın. Sol paneldeki **Group Policy Objects** sağ tıklayıp **Edit** seçtiğiniz herhangi bir GPO içinde:

```
Computer Configuration
  └─ Policies
      └─ Administrative Templates
          └─ Personel                    ← YENİ
              └─ Personel Agent
                  ├─ Enrollment
                  ├─ Operations
                  └─ Privacy and DLP
```

Eğer "Personel" kategorisi görünmüyorsa, dosyaların doğru dizine kopyalanmadığını veya replikasyon henüz tamamlanmadığını gösterir.

---

## 4. GPO Oluşturma ve Hedef OU'ya Bağlama

### 4.1 GPO oluştur

`gpmc.msc` içinde:

1. **Group Policy Objects** → sağ tık → **New**.
2. Ad: `Personel Agent — Pilot OU` (veya ortamınızın adlandırma standardına göre).
3. Kaynak Başlangıç GPO'su: `(none)`.
4. **OK**.

### 4.2 Politika değerlerini ayarla

Oluşturduğunuz GPO'yu sağ tıklayıp **Edit** seçin. Hedef ayarları düzenleyin:

**Pilot için önerilen başlangıç değerleri** (güvenli + en az değişiklik):

| Kategori | İlke | Durum | Değer |
|---|---|---|---|
| Enrollment | Gateway URL | Etkin | `https://personel-gw.kurumsal.local:8443` |
| Operations | Diagnostic log level | Etkin | `info` |
| Operations | Auto-update channel | Etkin | `stable` |
| Privacy/DLP | DLP opt-in acknowledged | **Devre dışı** | — (KVKK güvenli varsayılan) |

Diğer ayarları ilk pilot turunda `Not Configured` bırakın. Ajan derlenmiş varsayılanlarını kullanacaktır.

### 4.3 GPO'yu hedef OU'ya bağla

1. `gpmc.msc` içinde hedef OU'yu bulun (`OU=Personel-Pilot,DC=kurumsal,DC=local`).
2. Sağ tık → **Link an Existing GPO...**.
3. Az önce oluşturduğunuz GPO'yu seçin → **OK**.
4. Bağlantı listesinde GPO'nun **Link Enabled** olduğunu doğrulayın.

### 4.4 Güvenlik filtreleme (opsiyonel)

GPO'nun yalnızca belirli bir bilgisayar grubuna uygulanmasını istiyorsanız, **Security Filtering** panelinde `Authenticated Users` yerine özel bir bilgisayar grubu ayarlayın.

---

## 5. Pilot Endpoint'te Doğrulama

### 5.1 Politika yenilemeyi zorla

Hedef endpoint'e (domain-joined) yükseltilmiş Komut İstemi ile bağlanın:

```cmd
gpupdate /force
```

Genel olarak 10-60 saniye sürer. Sonunda "Computer Policy update has completed successfully." görmelisiniz.

### 5.2 Registry değerlerini kontrol et

```cmd
reg query "HKLM\SOFTWARE\Policies\Personel\Agent"
```

Beklenen çıktı (yukarıdaki pilot değerlerle):

```
HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Personel\Agent
    GatewayUrl         REG_SZ     https://personel-gw.kurumsal.local:8443
    DiagnosticLogLevel REG_SZ     info
    AutoUpdateChannel  REG_SZ     stable
    DLPOptInAcknowledged REG_DWORD 0x0
```

### 5.3 GPO uygulama raporu oluştur

```cmd
gpresult /h C:\Temp\gpo-report.html /f
start C:\Temp\gpo-report.html
```

Raporda **Applied GPOs** bölümünde az önce oluşturduğunuz GPO listelenmelidir. Personel Agent kategorisi altındaki ayarları **Computer Details → Settings** altında görmelisiniz.

### 5.4 Servis durumunu kontrol et

```cmd
sc query PersonelAgent
sc query PersonelWatchdog
```

Her ikisi de `STATE: 4 RUNNING` olmalı. Servisi, politika değişikliklerini almaya zorlamak için yeniden başlatabilirsiniz:

```cmd
sc stop PersonelAgent && sc start PersonelAgent
```

> **Not**: Faz 4 itibarıyla ajan, bazı GPO anahtarlarını henüz okumamaktadır (bkz. [registry-policies.md](./registry-policies.md) durum sütunu). `[DEFINED]` işaretli ayarlar yazılır ancak bir sonraki ajan sürümüne kadar etkisizdir. `[WIRED]` işaretli ayarlar bugün etkilidir.

### 5.5 Ajan günlüklerini kontrol et

```
C:\ProgramData\Personel\agent\logs\agent.log
```

Son 5 dakikada, ajanın gateway'e bağlandığını ve heartbeat gönderdiğini doğrulayın. `GatewayUrl` değişikliği için yeniden bağlanma kaydı görülmelidir.

---

## 6. Kademeli Yayılım (Staged Rollout)

Pilot OU stabil çalıştıktan sonra şu sırayı izleyin:

### Aşama 1: Pilot (1-5 endpoint) — 3 gün

- BT operasyonları + en az 1 gönüllü son kullanıcı
- Günlük hata oranı, servis çökmeleri, performans (CPU < %2, RAM < 150 MB)
- İlk 24 saatte şikayet yoksa → Aşama 2
- Şikayet varsa → aşağıdaki §8 Rollback bölümüne git

### Aşama 2: %10 dağıtım — 1 hafta

- Tipik: 1-2 bölüm veya tek bir katın çalışanları
- Aydınlatma Metni teyit edilmiş olmalı
- Birinci hafta sonunda KVKK DSR (KVKK m.11) çağrısı geldi mi? Live view talepleri anormal mi?
- Temiz ise → Aşama 3

### Aşama 3: %50 dağıtım — 1 hafta

- Tüm orta segment çalışanları
- Gateway yükü, ClickHouse ingestion rate, MinIO depolama büyümesi izlenmeli
- Hata oranı %0,1 altında tutulmalı
- Temiz ise → Aşama 4

### Aşama 4: Tam dağıtım — 3 gün

- Tüm domain çalışanları (eğer tüm OU'lar hedefleniyorsa)
- İlk 72 saat 7x24 on-call
- Bu noktada retrospektif + final KVKK DPO onayı

---

## 7. Sorun Giderme

### Problem: "Personel" kategorisi GPO editöründe görünmüyor

**Nedenler**:
1. ADMX dosyası `PolicyDefinitions\` altında değil (kök dizinde olmalı).
2. ADML dosyası `PolicyDefinitions\en-US\` (veya `tr-TR\`) altında değil.
3. SYSVOL replikasyonu tamamlanmamış.
4. GPO editörünüz merkezi deponun yerine yerel (workstation) depodan okuyor.

**Çözüm**:
```cmd
dir "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\personel-agent.admx"
dir "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\en-US\personel-agent.adml"
```
İki komut da dosyayı listelemelidir. `dfsrdiag syncnow /partner:<başka-dc>` komutu ile replikasyonu zorlayabilirsiniz.

### Problem: `gpresult` raporunda GPO görünmüyor

**Nedenler**:
1. GPO OU'ya bağlı değil.
2. Bağlantı devre dışı (Link Enabled false).
3. Güvenlik filtrelemesi endpoint'i dışarıda bırakıyor.
4. Endpoint domain-joined değil (workgroup).
5. WMI filtresi uygulanamıyor.

**Çözüm**: `gpresult /v` detaylı çıktıyı verir. `Applied Group Policy Objects` altında GPO'yu arayın. `Denied/Filtered` altındaysa nedeni orada gösterilir.

### Problem: Registry değeri ayarlanmış ama ajan davranışı değişmiyor

**İlk kontrol**: Ayar `[WIRED]` mi yoksa `[DEFINED]` mi? [registry-policies.md](./registry-policies.md) tablosuna bakın. `[DEFINED]` ayarlar şimdilik sadece yazılır, okunmaz.

`[WIRED]` bir ayar işe yaramıyorsa:
1. Servisi yeniden başlatın: `sc stop PersonelAgent && sc start PersonelAgent`
2. `C:\ProgramData\Personel\agent\logs\agent.log` dosyasında politika okuma hata kayıtlarını arayın.
3. Hâlâ çalışmıyorsa sorunu [troubleshooting.md](../../infra/runbooks/troubleshooting.md) rehberine götürün.

### Problem: Ajan servisi başlatılamıyor

Ajan servis başlatma problemlerinin çoğu GPO konfigürasyonundan bağımsızdır. [infra/runbooks/troubleshooting.md](../../infra/runbooks/troubleshooting.md) dosyasındaki "Agent service fails to start" bölümüne bakın.

---

## 8. Rollback

### 8.1 Tek ayar geri alma

GPO editöründe ilgili ayarı **Not Configured** yapın → **OK** → hedef endpointlerde `gpupdate /force`. Ajan, bir sonraki politika yenileme çevriminde derlenmiş varsayılana döner.

### 8.2 Tüm GPO'yu devre dışı bırakma

`gpmc.msc` içinde GPO bağlantısını sağ tıklayıp **Link Enabled** kutusunu kaldırın. Hedef endpointlerde `gpupdate /force`. Registry `HKLM\SOFTWARE\Policies\Personel\Agent` anahtarı temizlenir.

### 8.3 Tam geri alma (ADMX dosyalarını kaldır)

Sadece Personel ajanı müşteri ortamından tamamen kaldırılıyorsa:

```cmd
del "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\personel-agent.admx"
del "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\en-US\personel-agent.adml"
del "\\%USERDNSDOMAIN%\SYSVOL\%USERDNSDOMAIN%\Policies\PolicyDefinitions\tr-TR\personel-agent.adml"
```

---

## 9. KVKK Değerlendirmesi

Personel Türkiye pazarı için tasarlanmıştır ve KVKK 6698 sayılı Kanuna göre kurumsal verilerin işlenmesi için tasarlanmış bir platformdur. GPO ile yapılan her politika değişikliği, müşteri kurumun **KVKK Veri Sorumlusu** tarafından tutulmak zorunda olan kayıtlara işlenmelidir.

### 9.1 Zorunlu DPO incelemesi gereken ilkeler

Aşağıdaki ilkeler gizlilik/hassas alana doğrudan etki eder ve **DPO onayı olmadan değiştirilemez**:

| İlke | Etki |
|---|---|
| `ExcludedAppsAdditional` | Hangi uygulamaların ekran görüntüsünden haric tutulduğunu belirler; Aydınlatma Metni'nde listelenmelidir |
| `DLPOptInAcknowledged` | DLP motorunun çalışmasının son kapısıdır; ADR 0013 töreninin tamamlandığının makine tarafından zorlanabilir kanıtıdır |
| `DataDirectoryOverride` | DPAPI ile muhurlenmis anahtarların depolandığı yerdir; yetkisiz dizinler saldırı yüzeyini artırabilir |

### 9.2 Kayıt yükümlülüğü

Her GPO değişikliği için:

1. Değişikliğin nedeni (operasyonel / KVKK uyumu / sorun giderme)
2. DPO onayı (e-posta, imzalı form, veya ticket referansı)
3. Değişiklik tarihi ve yetkili operatör
4. Etkilenen OU / endpoint sayısı

müşteri kurum değişiklik yönetim sistemine (change management system) veya Personel yönetim konsolundaki denetim kaydı (audit log) sistemine kaydedilmelidir. SOC 2 Type II CC8.1 kanıt toplayıcısı bu kaydı destekleyecektir.

### 9.3 Aydınlatma Metni güncelleme tetikleyicileri

Şu değişiklikler çalışan Aydınlatma Metni'nin **yeniden yayımlanmasını** gerektirir:

- Yeni bir ekran görüntüsü istisnasının kaldırılması (yani bir alanın artık izlenmeye dahil edilmesi)
- `DLPOptInAcknowledged` 0'dan 1'e değişmesi (ADR 0013 törenine tabidir)
- `ScreenshotIntervalSeconds` değerinin önemli ölçüde düşürülmesi (örn. 300'den 60'a)

Yeni bir istisnanın **eklenmesi** (yani daha fazla gizlilik koruması) genelde yeniden yayımlama gerektirmez ancak sonraki Aydınlatma Metni sürümünde belgelenmelidir.

---

## 10. İlgili Dokümanlar

- [registry-policies.md](./registry-policies.md) — HKLM registry anahtar referans tablosu
- [../../apps/agent/installer/README.md](../../apps/agent/installer/README.md) — MSI yapı ve kurulum rehberi
- [../../infra/runbooks/install.md](../../infra/runbooks/install.md) — Backend tarafı kurulum runbook'u
- [../../infra/runbooks/troubleshooting.md](../../infra/runbooks/troubleshooting.md) — Genel sorun giderme
- [../compliance/kvkk-framework.md](../compliance/kvkk-framework.md) — KVKK uyum çerçevesi
- [../compliance/aydinlatma-metni-template.md](../compliance/aydinlatma-metni-template.md) — Aydınlatma Metni şablonu
- ADR 0013 — DLP disabled by default (opt-in ceremony)

---

*Sürüm 1.0 — Faz 4 Wave 3 #38. GPO politika yüzeyi tanımlandı; çoğu ayar Phase 5 baglanacak. Güncelleme: ajan yeni politika anahtarları okumaya başladığında registry-policies.md'deki durum sütunu güncellenir.*
