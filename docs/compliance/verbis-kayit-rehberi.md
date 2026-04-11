# VERBİS Kayıt Rehberi — Personel Platformu

> Hedef: Müşteri DPO'sunun, Personel Platformu'nun kurulumunu VERBİS (Veri Sorumluları Sicil Bilgi Sistemi) üzerinde doğru kategorilerle kayıt altına alması.
>
> **Not**: VERBİS dropdown menüleri zaman içinde Kurul tarafından güncellenmektedir. Aşağıdaki önerilen seçimler Nisan 2026 itibarıyla geçerlidir. Müşteri DPO, güncel VERBİS arayüzünde aynı veya en yakın seçeneği seçmelidir. **[DOĞRULANMALI — her kayıt öncesi VERBİS güncel listesi kontrol edilmelidir.]**

## 1. Kayıt Öncesi Hazırlık

- [ ] Veri sorumlusu kimliği ve MERSİS numarası hazır
- [ ] İrtibat kişisi atanmış (Kurul'a karşı muhatap)
- [ ] Kişisel Veri İşleme Envanteri (bkz. `kvkk-framework.md` §5) güncel
- [ ] Saklama ve İmha Politikası yazılmış (bkz. `iltica-silme-politikasi.md`)
- [ ] DPIA tamamlanmış (bkz. `dpia-sablonu.md`)
- [ ] Aydınlatma Metni yayınlanmış
- [ ] Güvenlik tedbirleri listesi hazır (bkz. `kvkk-framework.md` §9)

## 2. VERBİS Kayıt Adımları

### Adım 1 — Veri Sorumlusu Bilgileri
- Veri sorumlusu türü: **Türkiye'de yerleşik özel hukuk tüzel kişisi**
- Unvan, MERSİS, adres, KEP
- İrtibat kişisi bilgileri

### Adım 2 — Kişisel Veri İşleme Amaçları
Personel Platformu için önerilen seçimler (VERBİS dropdown karşılıkları):

- [x] Bilgi Güvenliği Süreçlerinin Yürütülmesi
- [x] Çalışanlar İçin İş Akdi ve Mevzuattan Kaynaklı Yükümlülüklerin Yerine Getirilmesi
- [x] Denetim / Etik Faaliyetlerinin Yürütülmesi
- [x] Faaliyetlerin Mevzuata Uygun Yürütülmesi
- [x] Fiziksel Mekan Güvenliğinin Temini (sınırlı uygulanır)
- [x] Hukuk İşlerinin Takibi ve Yürütülmesi
- [x] İç Denetim / Soruşturma / İstihbarat Faaliyetlerinin Yürütülmesi
- [x] Risk Yönetimi Süreçlerinin Yürütülmesi
- [x] Saklama ve Arşiv Faaliyetlerinin Yürütülmesi
- [x] Yetkili Kişi, Kurum ve Kuruluşlara Bilgi Verilmesi

**Seçilmemesi gerekenler**:
- Reklam / Kampanya / Promosyon Süreçleri (alakasız)
- Müşteri Memnuniyeti (alakasız)
- Ürün / Hizmet Pazarlama Süreçleri (alakasız)

### Adım 3 — Veri Kategorileri

VERBİS dropdown'unda karşılıkları şu şekilde seçilmelidir:

| Personel Veri Kategorisi | VERBİS Karşılığı |
|---|---|
| Kimlik / oturum verileri | **Kimlik Bilgisi** |
| Uygulama ve süreç kullanım verileri | **İşlem Güvenliği** |
| Ekran görüntüleri / video klipleri | **Görsel ve İşitsel Kayıtlar** |
| Dosya sistemi olayları | **İşlem Güvenliği** |
| Pano verileri | **İşlem Güvenliği** |
| Klavye istatistik / içerik | **İşlem Güvenliği** |
| Yazıcı verileri | **İşlem Güvenliği** |
| USB verileri | **İşlem Güvenliği** |
| Ağ akış verileri | **İşlem Güvenliği** |
| Canlı izleme denetim verileri | **İşlem Güvenliği** + **Görsel ve İşitsel Kayıtlar** |
| Politika / denetim olayları | **İşlem Güvenliği** |

**ÖNEMLİ**: "Sağlık Bilgileri", "Irk ve Etnik Köken", "Siyasi Düşünce", "Felsefi İnanç, Din, Mezhep", "Dernek, Vakıf ve Sendika Üyeliği", "Ceza Mahkumiyeti ve Güvenlik Tedbirleri", "Biyometrik Veri", "Genetik Veri" gibi **özel nitelikli kategoriler İŞARETLENMEMELİDİR**. Personel bu verileri amaçlı olarak toplamaz; kazara toplanma riski m.6 filtreleriyle azaltılır.

### Adım 4 — Alıcı / Alıcı Grupları

Personel on-prem olduğundan:
- **Aktarım yoktur** seçeneği işaretlenir.
- "Yetkili Kamu Kurum ve Kuruluşları" seçeneği, yalnızca yasal zorunluluk doğması ihtimaline binaen işaretlenebilir; açıklama: "Yalnızca yetkili mercilerin yazılı talebi hâlinde KVKK m.8/2-ç kapsamında".

### Adım 5 — Yabancı Ülkelere Aktarım
**Hayır** (on-prem mimari).

### Adım 6 — Veri Konusu Kişi Grupları
- [x] Çalışan
- [x] Çalışan Adayı (pilot test için kullanılıyorsa)
- [x] Stajyer (geçerliyse)

### Adım 7 — Saklama Süreleri
`kvkk-framework.md` §5 matrisi ve `iltica-silme-politikasi.md` uyarınca, VERBİS "Saklama Süresi" alanında kategori bazında en uzun süreyi belirtin. Örnek özet metin:

> "Kişisel Veri Saklama ve İmha Politikası'nda her veri sınıfı için ayrı süreler belirlenmiştir. Özet: ekran görüntüsü 30 gün, ekran video 14 gün, klavye şifreli içerik 14 gün, süreç/pencere olayları 90 gün, dosya olayları 180 gün, USB olayları 365 gün, politika/denetim olayları 1-5 yıl. Detaylar ekli Saklama ve İmha Politikası'nda."

### Adım 8 — Alınan İdari ve Teknik Tedbirler

`kvkk-framework.md` §9 tablolarının tamamı buraya yapıştırılabilir. Minimum metin:

**İdari**: Kişisel veri işleme envanteri, saklama-imha politikası, aydınlatma metni, gizlilik sözleşmeleri, erişim yetki matrisi, eğitim ve farkındalık, kurum içi periyodik denetim, risk değerlendirmesi (DPIA), disiplin süreci.

**Teknik**: mTLS 1.3 + sertifika sabitleme, AES-256-GCM şifreleme, HashiCorp Vault anahtar yönetimi, Shamir 3-of-5 seal, hash-zincirli append-only audit log, rol bazlı erişim (RBAC), PostgreSQL row-level security, LDAP/AD entegrasyonu, oturum zaman aşımı, rate limiting, DLP hizmeti izolasyonu, klavye içeriği kriptografik izolasyonu, disk şifreleme, yedekleme şifreleme, SBOM, imzalı yazılım güncellemeleri, sızma testi.

## 3. Kayıt Sonrası

- VERBİS kayıt numarasını Aydınlatma Metni'ne yerleştirin.
- Kayıt bilgileri, yılda bir veya değişiklik durumunda güncellenmelidir.
- Envanter değişiklikleri 30 gün içinde yansıtılmalıdır.

## 4. Sık Sorulan Sorular

**S**: Personel firması da VERBİS'e kayıt olmalı mı?
**C**: On-prem modelde Personel firması veri işleyen **değildir** (bkz. `kvkk-framework.md` §3). Kendi çalışan verisi için zaten kayıtlıdır ama müşteri kurum adına ayrı bir kayıt gerekli değildir.

**S**: VERBİS "Görsel ve İşitsel Kayıtlar" seçtik; bu özel nitelikli veri kaydı doğurur mu?
**C**: Hayır. Görsel ve İşitsel Kayıtlar KVKK m.6 özel nitelikli kategori listesinde değildir. Özel niteliklilik, içeriğin sağlık/din/etnik köken gibi olması hâlinde doğar; bu nedenle §9 filtreleme tedbirleri kritiktir.

**S**: Canlı izleme oturumlarını ayrı bir amaç olarak belirtmeli miyim?
**C**: Evet. "İç Denetim / Soruşturma / İstihbarat Faaliyetlerinin Yürütülmesi" amacı altında, açıklama alanında "canlı izleme yalnızca dual-control onay ile soruşturma amacıyla etkinleştirilir" notu eklenmelidir.
