# Video Script 01 — Personel Tanıtım (3 dakika)

**Hedef kitle**: Potansiyel müşteri IT müdürü, CISO, DPO
**Hedef**: Personel'in ne olduğunu ve neden tercih edilmesi gerektiğini 3 dakikada anlatmak
**Platform**: YouTube (unlisted) + web sitesi gömülü
**Dil**: Türkçe anlatım + Türkçe altyazı (EN altyazı seçeneği)

---

## Sahne 1 — Açılış (0:00-0:15)

**Görsel**:
- Dinamik açılış: çalışma ofisi drone çekimi + yazı "Kurumsal Çalışan Aktivite İzleme — Yeniden Düşünüldü"
- Logo: Personel (animasyonlu)

**Ses**:
> "Türkiye'de 50-500 çalışanlı kurumlar, çalışan verimliliğini ölçmek ve veri
> sızıntısı risklerini yönetmek için uluslararası araçlara mecbur kalıyor. Ama
> bu araçlar KVKK ile uyumlu olmak için tasarlanmadı. İşte Personel tam da
> bu boşluğu kapatıyor."

---

## Sahne 2 — Problem (0:15-0:45)

**Görsel**:
- Split screen: Sol tarafta "yabancı SaaS" ekran görüntüsü, sağ tarafta KVKK işareti
- Kırmızı X işaretleri: "Veri yurt dışına çıkıyor", "KVKK m.11 yok", "Türkçe yok"

**Ses**:
> "Teramind, ActivTrak, Veriato gibi araçlar güçlü. Ancak:
> - Veriler yurt dışına gidiyor.
> - KVKK 6698'in getirdiği yükümlülükler sonradan yamanmış.
> - Klavye içeriği yöneticiye sınırsız açık — KVKK orantılılık prensibine aykırı.
> - Türkçe ara yüz yok.
> - On-prem seçenek ya yok ya pahalı."

---

## Sahne 3 — Çözüm: Personel (0:45-1:30)

**Görsel**:
- Personel Console ekran kaydı (dashboard → endpoint listesi → politika editörü)
- Türkçe arayüz öne çıkarılmış
- Alt köşede "On-Prem", "KVKK-Native", "Rust Agent" rozetleri

**Ses**:
> "Personel, Türkiye pazarı için özel tasarlanmış, on-prem çalışan bir UAM
> platformudur. Üç temel farkımız var:
>
> Bir: KVKK uyumu mimariye gömülüdür. VERBİS export, aydınlatma metni,
> DSR 30 gün SLA, hash-zincirli audit — hepsi çekirdekte.
>
> İki: Klavye içeriğini varsayılan olarak toplamayız. Etkinleştirilse bile
> yönetici kriptografik olarak okuyamaz. Sadece izole DLP motoru kurallarla
> eşleşme arayabilir.
>
> Üç: Her şey sizin sunucunuzda. Yurt dışına veri çıkışı sıfır."

---

## Sahne 4 — Mimari (1:30-2:00)

**Görsel**:
- C4 Container diyagramı animasyonlu (endpoint → gateway → storage → api → console)
- 18 container ikonu + bağlantı oklar
- "2 saatlik kurulum" yazısı

**Ses**:
> "Mimari olarak Rust yazılmış tek-binary agent endpoint'lerinizde %2 CPU'dan
> az kaynak kullanır. Sunucu tarafında Docker Compose + systemd ile 18 container
> tek VM'de çalışır. Kurulum 2 saat. 500 endpoint'e kadar tek VM yeterli."

---

## Sahne 5 — Demo Teaser (2:00-2:30)

**Görsel**:
- Hızlı ekran geçişleri:
  - Console dashboard
  - Endpoint detay
  - Policy editor
  - DSR workflow
  - Portal /verilerim sayfası (çalışan bakışı)
  - Audit log stream

**Ses**:
> "Yöneticiler Console'dan tüm operasyonu yürütür. Çalışanlar Şeffaflık Portal'ı
> üzerinden hangi verisinin toplandığını kendileri görebilir — hakların self-service
> kullanımı. DPO'lar KVKK başvurularını tek tıkla yanıtlar. Müfettiş geldiğinde
> denetim raporu hazır."

---

## Sahne 6 — Kapanış + CTA (2:30-3:00)

**Görsel**:
- "Personel" logo merkez
- QR code: poc-talebi.personel.local
- Alt yazı: "30 gün ücretsiz POC. Yerli destek. KVKK garantili."
- iletisim: satis@personel.local

**Ses**:
> "30 günlük ücretsiz POC'nizi şimdi başlatın. Kurulum bize 2 saat, değerlendirme
> size 1 ay. satis@personel.local adresine bir email yeter. Personel —
> Türkiye'nin kurumsal UAM standardı."

**Görsel son kare**: Personel logo + "personel.local"

---

## Prodüksiyon Notları

- **Video süre**: 3:00 dakika (strict)
- **Anlatıcı**: Erkek, 30-45 yaş, orta ton, hafif kurumsal
- **Tempo**: Orta-hızlı (bilgi yoğunluğu yüksek)
- **Müzik**: Orchestral-tech, düşük volume (-18 dB), ahenk vurguda hafif crescendo
- **Ekran kaydı**: 1920x1080, 30fps, cursor highlight efekti
- **Renk paleti**: Personel brand (TODO: brand guide)
- **Geçişler**: Sade (cut + fade), efekt dokunma
- **Altyazı**: Hard-coded TR + soft EN (YouTube CC)

---

## Onay Süreci

- [ ] Script taslağı — ürün ekibi onayı
- [ ] Storyboard — tasarım ekibi
- [ ] Seslendirme — stüdyo kayıt
- [ ] Kaba kurgu — üretim
- [ ] Fine cut — tüm ekip feedback
- [ ] Final — yayın

Tahmini süre: 4-6 hafta.
