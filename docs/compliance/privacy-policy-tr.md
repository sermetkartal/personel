# Personel — Gizlilik Politikası (KVKK)

> Dil: Türkçe. Hedef okuyucu: Personel kullanan kurumsal çalışanlar ve müşteri kurumun resmi gizlilik politikası template'i.
>
> Bu doküman **template**'dir. Müşteri kurum kendi değerlerini doldurarak kendi gizlilik politikası olarak yayımlar. İlgili:
> - `docs/compliance/aydinlatma-metni-template.md` — KVKK m.10 aydınlatma
> - `docs/compliance/kvkk-framework.md` — tam KVKK çerçevesi
> - `docs/user-manuals/employee-manual-tr.md` — çalışan portalı kılavuzu
>
> Bu politika Personel platformunun kurumsal kullanımındaki **veri işleme** faaliyetlerine ilişkindir.

---

## 1. Kimiz?

Bu Gizlilik Politikası, **[KURUM ADI]** (bundan böyle "Kurum" olarak anılacaktır) tarafından yürütülen ve Personel UAM platformu aracılığıyla gerçekleştirilen kişisel veri işleme faaliyetlerini açıklamaktadır.

**Veri sorumlusu**: [KURUM ADI]
**Adres**: [ADRES]
**MERSIS**: [NO]
**VERBİS**: [KAYIT NO]

**Veri Sorumlusu İrtibat Kişisi (DPO)**:
- Ad: [DPO ADI]
- E-posta: dpo@[kurum].com.tr
- Telefon: [NUMARA]

**Teknik sağlayıcı**: Personel Platform (veri **işleyen** rolünde — `docs/compliance/dpa-template.md` kapsamında).

---

## 2. Hangi Verileri Topluyoruz?

Personel platformu, iş laptop'unuz üzerine kurulan **ajan** (agent) yazılımı aracılığıyla aşağıdaki **11 kategori** kişisel veriyi toplar:

### 2.1 Süreç ve Uygulama Verileri

- Hangi uygulamaların açık olduğu
- Ne kadar süre aktif olarak kullanıldığı
- Açılış ve kapanış zamanları

### 2.2 Pencere ve Ön Plan Verileri

- Ön planda hangi pencerenin bulunduğu
- Pencere başlıkları (ör. dosya adı — hassas pencereler **otomatik maskelenir**)
- Pencere odak değişim süreleri

### 2.3 Ekran Görüntüsü

- Belirli aralıklarla (varsayılan 1-5 dakikada bir) yakalanan ekran anlık görüntüleri
- Varsayılan olarak 30 gün saklanır, en fazla 90 gün
- Hassas uygulamalar (şifre yöneticileri, bankacılık uygulamaları) **dışlama listesinde**

### 2.4 Boşta / Aktif Zaman

- Klavye veya fare aktivitesi olup olmadığı
- Mola, toplantı, telefon görüşmesi süreleri

### 2.5 Dosya Sistemi Olayları

- Dosya oluşturma, okuma, yazma, silme, yeniden adlandırma, kopyalama
- Dosya içeriği **toplanmaz**, sadece meta veri

### 2.6 USB Cihaz Olayları

- USB cihaz takma ve çıkarma
- Cihaz türü (mass storage, klavye, fare, vb.)
- Seri numarası (hash'lenmiş)

### 2.7 Yazıcı İşleri

- Yazdırma işi meta verisi (belge adı, sayfa sayısı, tarih)
- Belge içeriği **toplanmaz**

### 2.8 Ağ Akış Özeti

- Hangi IP'lere / domain'lere bağlantı kuruldu
- Bağlantı süresi ve bayt hacmi
- Web trafiğinin **içeriği** toplanmaz

### 2.9 Klavye İstatistikleri

- Tuş vuruş **sayısı** (per dakika)
- Backspace / paste sayısı
- **İçerik toplanmaz** — varsayılan davranıştır ve ADR 0013 uyarınca kriptografik olarak garanti altındadır

### 2.10 Pano (Clipboard) Meta

- Pano içeriğinin **boyutu** ve zamanı
- İçerik **toplanmaz**

### 2.11 Sistem Sağlığı

- CPU, RAM, disk, batarya durumu
- Ajan sürüm bilgisi
- Heartbeat

---

## 3. Neden Topluyoruz? (Hukuki Sebepler)

| Veri Kategorisi | KVKK Hukuki Sebep | Amaç |
|---|---|---|
| Süreç, pencere, dosya, USB, ağ | m.5/2-f (meşru menfaat) | Çalışanın iş performansının ölçülmesi, yazılım lisans uyumu |
| Ekran görüntüsü | m.5/2-f (meşru menfaat) + m.6 (özel nitelikli istisna — yoğun tedbir) | Güvenlik olaylarının soruşturulması, compliance kanıtı |
| Klavye istatistikleri | m.5/2-f (meşru menfaat) | Aktivite ölçümü, aşırı çalışma tespiti |
| Pano / Ağ / Yazıcı | m.5/2-f (meşru menfaat) + m.12 (güvenlik) | Veri sızıntısı tespiti, KVKK güvenlik tedbirleri |
| Sistem sağlığı | m.5/2-ç (sözleşmenin ifası) | Teknik destek, arıza yönetimi |

**Meşru menfaat dengelemesi** her veri kategorisi için DPIA'da (Data Protection Impact Assessment) yapılmış ve çalışanın mahremiyet hakkı ile işverenin meşru menfaati arasında denge kurulmuştur. DPIA: `docs/compliance/dpia-sablonu.md`.

---

## 4. Ne Kadar Saklıyoruz?

Her veri kategorisi için saklama süreleri **Veri Saklama Matrisi**'nde tanımlıdır (`docs/architecture/data-retention-matrix.md`):

| Kategori | Varsayılan | Maksimum |
|---|---|---|
| Süreç / pencere / boşta | 90 gün | 180 gün |
| Ekran görüntüsü | 30 gün | 90 gün |
| Dosya olayları | 180 gün | 365 gün |
| USB / yazıcı | 365 gün | 730 gün |
| Ağ akışı / DNS | 30-60 gün | 90 gün |
| Klavye istatistikleri | 90 gün | 180 gün |
| Pano meta | 90 gün | 180 gün |
| Audit log | 5 yıl | 10 yıl |
| Canlı izleme kaydı | 5 yıl | 10 yıl |

Süreler KVKK m.7 uyarınca dolduğunda veriler **otomatik olarak silinir**. Şifreli veriler için "kripto-shred" (anahtar imhası) uygulanır — blob kullanılamaz hale gelir.

Daha uzun saklama, sadece yazılı DPO kararı ile ve maksimum süreyi aşmayacak şekilde mümkündür.

---

## 5. Kimlerle Paylaşıyoruz?

### 5.1 Yurt İçi Paylaşım

- **Kurum içi yetkili personel**: Size atanmış yönetici, insan kaynakları uzmanı, İK DPO — yalnızca göreviyle ilgili ölçüde.
- **Yasal zorunluluk**: Savcılık, mahkeme veya Kurul (KVKK) talebi halinde — ilgili belge ile.
- **Disiplin soruşturması**: İç soruşturma kapsamında ve yazılı DPO kararı ile.

### 5.2 Yurt Dışı Aktarım

**Hayır**. Personel platformu **on-prem** olarak Kurum'un kendi sunucusunda çalışır. Verileriniz Kurum dışına **gönderilmez**.

İstisna: Kurum bulut tabanlı yedekleme veya felaket kurtarma için yurt dışı depolama kullanıyorsa, bu durum ayrıca aydınlatma metninde belirtilir ve KVKK m.9 kapsamında gerekli güvence mekanizmaları (Kurul kararı, taahhütname, vb.) uygulanır.

### 5.3 Alt İşleyiciler (Sub-processors)

Faz 1 MVP'de Personel'in Kurum adına çalıştırdığı **alt işleyici yoktur**. Alt işleyici listesi güncellendiğinde bu politika da güncellenir.

Detay: `docs/compliance/sub-processor-registry.md`.

---

## 6. Haklarınız (KVKK m.11)

KVKK m.11 uyarınca **yedi hak** size tanınmıştır:

| Hak (madde) | İçerik |
|---|---|
| m.11/a | Kişisel verinizin işlenip işlenmediğini öğrenme |
| m.11/b | Kişisel verileriniz işlenmişse buna ilişkin bilgi talep etme |
| m.11/c | Kişisel verilerin işlenme amacını ve amacına uygun kullanılıp kullanılmadığını öğrenme |
| m.11/d | Yurt içinde veya yurt dışında kişisel verilerin aktarıldığı üçüncü kişileri bilme |
| m.11/e | Kişisel verilerin eksik veya yanlış işlenmiş olması halinde bunların düzeltilmesini isteme |
| m.11/f | KVKK m.7'de öngörülen şartlar çerçevesinde kişisel verilerin silinmesini veya yok edilmesini isteme |
| m.11/g | İşlenen verilerin münhasıran otomatik sistemler vasıtasıyla analiz edilmesi suretiyle kişinin kendisi aleyhine bir sonucun ortaya çıkmasına itiraz etme |

### Haklarınızı Nasıl Kullanırsınız?

**Şeffaflık Portalı** üzerinden: `[portal-url]` → **Başvurularım** → **Yeni Başvuru**

Veya yazılı olarak:
- **E-posta**: dpo@[kurum].com.tr
- **Posta**: [Kurum Adres + "Dikkat: DPO"]

Başvurunuza en geç **30 gün** içinde yanıt verilir (KVKK m.13/2).

Başvurunuz reddedilirse, **Kişisel Verileri Koruma Kurulu**'na şikayet etme hakkınız bulunmaktadır:
- Web: [www.kvkk.gov.tr](https://www.kvkk.gov.tr)

---

## 7. Güvenlik Tedbirleri (KVKK m.12)

Personel platformu KVKK m.12 teknik ve idari tedbirler kapsamında:

### Teknik

- Uçtan uca **mTLS** şifreli iletişim
- Disk şifreleme (ajan lokal SQLite → SQLCipher)
- **HashiCorp Vault** PKI + anahtar yönetimi
- Hash zincirli **append-only** denetim logu
- Her bir klavye içeriğinin **kriptografik olarak** yöneticiden gizlenmesi (ADR 0013)
- **Dual-control** canlı izleme (HR onayı zorunlu, kriptografik olarak enforced)
- KVKK m.6 özel nitelikli alan **otomatik tespit ve kısa TTL**
- **SOC 2 Type II** evidence locker (9 kontrol)

### İdari

- Rol tabanlı erişim kontrolü (RBAC)
- En az yetki prensibi
- **DPIA** (Data Protection Impact Assessment)
- Saklama ve imha politikası
- Düzenli güvenlik eğitimi
- Incident response playbook (72 saat KVKK m.12 bildirim)
- Audit trail ve kanıt koruma

Detay: `docs/compliance/kvkk-framework.md`, `docs/security/threat-model.md`.

---

## 8. Çerezler ve İzleme Teknolojileri

**Şeffaflık Portalı** yalnızca **teknik zorunlu** çerezleri kullanır (session yönetimi, CSRF koruması). Analitik, reklam veya üçüncü parti çerez **yoktur**.

Kurum web sitesinde ayrıca çerez politikası bulunabilir — o politika bu Gizlilik Politikası'ndan bağımsızdır.

---

## 9. Çocukların Verileri

Personel platformu yalnızca Kurum çalışanlarına yöneliktir. **18 yaş altı** verilerinin işlenmesini öngörmemektedir. Staj / öğrenci çalışan durumlarında ebeveyn / veli onayı Kurum İK tarafından alınır.

---

## 10. Değişiklikler

Bu Gizlilik Politikası, yasal veya operasyonel değişiklikler gerektirdiğinde güncellenir. Değişiklikler:

- Şeffaflık Portalı üzerinden bildirilir (30 gün önceden)
- Kurum intranetinde duyurulur
- Önemli değişikliklerde e-posta ile bireysel bildirim yapılabilir

---

## 11. Kabul Tarihi

- **İlk yayım**: [TARİH]
- **Son güncelleme**: 2026-04-13
- **Sonraki periyodik inceleme**: 2027-04-13

---

## İletişim

Bu Gizlilik Politikası veya kişisel verileriniz hakkında sorularınız için:

**Veri Sorumlusu İrtibat Kişisi (DPO)**
[DPO ADI]
dpo@[kurum].com.tr
[TELEFON]

**Kişisel Verileri Koruma Kurulu**
[www.kvkk.gov.tr](https://www.kvkk.gov.tr)

---

## Versiyon

| Sürüm | Tarih | Değişiklik |
|---|---|---|
| 1.0 | 2026-04-13 | Faz 15 #166 — İlk template sürümü (TR) |
