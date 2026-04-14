# Personel — Çalışan Bilgilendirme Brifingi (30 dk, TR)

Bu doküman, müşteri kurumda Personel Platform devreye girdiğinde **tüm
çalışanlara** verilmesi gereken 30 dakikalık bilgilendirme oturumunun
içeriğini tanımlar.

**Hedef kitle**: Tüm kurum çalışanları (İK veya DPO tarafından sunulur)
**Format**: 20 dakika sunum + 10 dakika soru-cevap
**KVKK temeli**: m.10 aydınlatma yükümlülüğü + şeffaflık prensibi

---

## Sunum Hedefi

Çalışan şu 5 soruya net yanıt alarak çıkmalı:

1. **Ne izleniyor?** (çalışma faaliyeti)
2. **Ne izlenmiyor?** (özel yaşam, kişisel iletişim içeriği)
3. **Veri ne kadar süre tutulacak?** (saklama matrisi)
4. **Kim erişebilir?** (rol bazlı)
5. **Haklarım neler?** (KVKK m.11 + Şeffaflık Portalı)

---

## 0:00-0:03 — Açılış: Neden Buradayız?

Konuşma notu (Örnek):

> "Merhaba, bugün size kurumumuzda devreye alınan 'Personel' adlı iş güvenliği
> ve verimlilik platformu hakkında bilgi vermek istiyoruz. Bu bilgilendirme
> yasal bir zorunluluktur — KVKK 6698 m.10 uyarınca işverenimizin size açıklama
> yapma yükümlülüğü vardır. Bizim de sizi şeffaf bir şekilde bilgilendirmek
> gibi bir sorumluluğumuz var."

---

## 0:03-0:08 — Ne İzleniyor? (5 dk)

### 11 Kategori

Çalışma saatleri içinde iş bilgisayarında şunlar toplanır:

1. **Uygulama kullanımı** — Hangi uygulamanın ne kadar süre aktif olduğu
2. **Web sitesi ziyaretleri** — URL başlıkları (Facebook.com, LinkedIn.com gibi)
3. **Dosya aktivitesi** — Oluşturulan/değiştirilen/silinen iş dosyaları (ad + yol)
4. **Ekran görüntüleri** — Düşük sıklıkla (dakikada 1'i geçmeyecek) + hassas uygulamalar hariç
5. **Yazıcı kullanımı** — Yazdırılan belge adı + sayfa sayısı (içerik DEĞİL)
6. **USB/harici cihaz** — Takılan cihaz adı + tipi
7. **Ağ kullanımı** — TCP/UDP bağlantı metadata
8. **Email metadata** — Gönderici/alıcı/konu/zaman (email İÇERİĞİ DEĞİL)
9. **Idle/Active süre** — Klavye ve fare aktivitesi (tuş içeriği DEĞİL)
10. **Oturum durumu** — Giriş/çıkış/kilitleme/uyku
11. **Cihaz durumu** — CPU, RAM, batarya, ekran durumu

**Önemli vurgu**: Bu veriler **çalışma saatleriniz** içinde ve **iş bilgisayarınızda** toplanır.

---

## 0:08-0:13 — Ne İzlenmiyor? (5 dk)

**10 Güven Maddesi** (`apps/portal/src/app/[locale]/neler-izlenmiyor/`):

1. ❌ Klavyede yazdığınız **içerik** (parola, email gövdesi, sohbet)
2. ❌ Email veya mesaj **gövdeleri**
3. ❌ Kameranız veya mikrofonunuz
4. ❌ Telefonunuz
5. ❌ Mesai dışı iş bilgisayarı kullanımı (ilke)
6. ❌ Kişisel web siteleri içeriği (sadece URL başlığı)
7. ❌ Banka ve sağlık uygulamaları (politika ile dışlanır)
8. ❌ Parola yönetici uygulamalarının içeriği
9. ❌ Kişisel e-posta hesabınızın içeriği
10. ❌ GPS/konum bilginiz

### Özel madde: Klavye içeriği

> "Merak ettiğiniz bir soru şu olabilir: 'Tuşlara bastıklarımı okuyabiliyor musunuz?'
> Cevap: HAYIR. Personel'de klavye içeriği varsayılan olarak **kapalıdır**.
> Açılsa bile şirket yönetimi teknik olarak okuma yeteneğine sahip değildir —
> sadece şirkete zarar verebilecek önceden tanımlı kurallarla (örneğin müşteri
> TCKN'sinin dışarı gönderilmesi) bir eşleşme arar ve eşleşme varsa yönetime
> uyarı verir. Uyarıda bile 'tam cümle' değil, 'TCKN tespiti' gibi kuru bir
> kategori bilgisi görülür."

---

## 0:13-0:18 — Veriler Nerede Saklanıyor, Ne Kadar Süre? (5 dk)

- **Nerede**: Kurumumuzun kendi sunucusunda (on-prem), veri YURT DIŞINA ÇIKMAZ
- **Kim erişir**:
  - IT Operator: Teknik bakım için sınırlı
  - Yönetici: Kendi ekibinin özet metrikleri
  - DPO (KVKK Sorumlusu): Tüm compliance işlemleri
  - Investigator: Sadece şüpheli durum araştırması (iç denetim + DPO onayı ile)
- **Saklama süreleri** (özet):
  - Uygulama kullanımı: 90 gün
  - Ekran görüntüleri: 30 gün
  - Audit log'u: 5 yıl (KVKK yasal zorunluluk)
  - Çalışan ayrılırsa: 30 gün içinde tüm veri silinir (KVKK m.7 ile)
- Detaylı matris: `https://portal.kurum.local/tr/verilerim`

---

## 0:18-0:23 — Haklarınız (KVKK m.11) (5 dk)

### 8 Hak

1. Bilgi isteme hakkı
2. Verilerinizin **kopyasını** isteme hakkı
3. Yanlışsa düzeltme isteme hakkı
4. Silme isteme hakkı (yasal saklama süreleri sona erdiyse)
5. Aktarılma durumları hakkında bilgi
6. Otomatik karar itirazı (örneğin algoritmik risk puanı)
7. Ziddarınızın tazminini isteme hakkı
8. Başvurunuz 30 gün içinde ücretsiz yanıtlanır

### Şeffaflık Portal'ı

Her çalışan `https://portal.kurum.local/tr` üzerinden:

- ✅ "Verilerim" sayfası: Hangi verinizin toplandığını görme
- ✅ "Haklarım" sayfası: 8 hakkı detaylı açıklamalar
- ✅ "Başvurularım" sayfası: Önceki DSR başvurularınız + durumları
- ✅ "Veri İndir" butonu: Kopya talep etme (m.11/b)
- ✅ "Neler İzlenmiyor" sayfası: Şirketin erişemediği veriler listesi
- ✅ "DLP Durumu" sayfası: Klavye içeriği analizi aktif mi (ADR 0013)
- ✅ "Canlı İzleme Geçmişi" sayfası: Size ait canlı izleme oturumları (varsa)

---

## 0:23-0:28 — Canlı İzleme ve Güvenceler (5 dk)

- **Canlı izleme** yalnızca şüpheli bir durumda yapılır
- **Tek bir kişi başlatamaz**: En az 2 yönetici onayı gerekir (requester ≠ approver)
- **Her oturum için sebep kodu** zorunlu (veri ihlali şüphesi, disiplin olayı gibi)
- **Maksimum süre**: 15 dakika (uzatılırsa tekrar onay)
- **Siz anında göreceksiniz**: Portal'da "Canlı İzleme Geçmişi" sayfası
- **DPO override**: Eğer oturum KVKK kapsamını aşarsa DPO tek tıkla sonlandırabilir
- **Hepsi audit log'a yazılır**: Tamper-proof, 5 yıl saklanır

**Vurgu**: "Canlı izleme rutin bir pratik DEĞİLDİR. Yıllık ortalama <%1 çalışan başına gerçekleşir."

---

## 0:28-0:30 — Kapanış + Soru-Cevap Daveti

- Tekrar özet: "5 saniye özeti"
- Detaylı metin: Aydınlatma metni her yeni işe alımda imzalanmıştır
- İlk girişte portal'a girdiğinizde zorunlu modal ile onayınızı yenilemiş olacaksınız
- Sorular için:
  - DPO: dpo@kurum.local
  - İK: ik@kurum.local
  - Kurul başvurusu (5 iş günü yanıt beklenmezse): kvkk.gov.tr

---

## Sunum Pratik Notları

- Konuşmacı: DPO veya İK Müdürü olmalı (teknik terim az)
- Soru-cevap kısmını **gerçekten** açık tut — insanlar çekinir, 1. sorudan sonra açılırlar
- "Bu sizin aleyhinize bir araç değil, hem sizi hem şirketi koruyan bir araçtır" çerçevesi
- Hukuki zorunluluk + güven unsurunu birlikte ver
- Güvensizliği cidi al — dinle, açıkla, yüzleş

## En Sık Gelen 5 Soru

1. **"Evden çalışıyorum, kişisel bilgisayarımı da izliyor musunuz?"**
   → Hayır, sadece şirket tarafından sağlanan iş bilgisayarı. Kişisel bilgisayarınıza agent kurulmaz.

2. **"İş saatleri dışında ne olur?"**
   → Politika ile "iş saatleri" tanımlanmıştır. Agent bu saatler dışında veri toplamaz (veya azaltılmış mod).

3. **"Eski verileri görüntülemek için geri dönebilir misiniz?"**
   → Saklama matrisinde tanımlı süre kadar, evet. Sonra otomatik silinir.

4. **"DSR açsam gerçekten cevap alıyor muyum?"**
   → Evet, 30 gün içinde, yasal olarak. Şirket bu süreyi aşarsa Kurul'a başvurabilirsiniz.

5. **"Arkadaşımla özel mesajlaşmam okunuyor mu?"**
   → Hayır. Mesaj **içeriği** okunmaz. Sadece şirket e-posta sisteminde gönderici/alıcı/zaman metadata'sı kaydedilir (m.5 meşru menfaat).

---

## Takip

- Katılım imza cetveli (KVKK m.10 aydınlatma kanıtı)
- Her katılımcıya PDF özet (isteğe bağlı)
- 1 hafta sonra kısa anket: "Bilgilendirme sonrası kaygı seviyeniz değişti mi?"

---

*Bu brifing her yeni işe alımda + her önemli mimari değişiklikte tekrarlanmalıdır.
İçerik revizesi: yılda 1 kez DPO tarafından.*
