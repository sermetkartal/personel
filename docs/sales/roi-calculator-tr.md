# Personel — ROI Hesap Modeli (TR)

Bu doküman, Personel'e yatırımın 3 boyutlu geri dönüşünü hesaplamak için
formüller ve tipik değerler içerir. Hedef kitle: CFO, IT Müdürü, CISO.

**Uyarı**: Bu model tahmini değerlere dayanır. Müşteri kendi operasyonel
rakamlarıyla özelleştirmelidir. Personel pazarlama iddiaları değil, kanıtlanmış
implementasyon temelli bir model sunar.

---

## Formül Özeti

```
ROI = (Verimlilik Kazancı + Sızıntı Önleme + Uyum Maliyeti Düşüşü - Personel Yıllık Lisans) / Personel Yıllık Lisans × 100
```

Tipik 100 kişilik bir müşteri için model aşağıda.

---

## 1. Verimlilik Kazancı

### Temel varsayımlar

- 100 çalışan × 8 saat × 220 iş günü = **176.000 iş saati/yıl**
- Ortalama saatlik maliyet (maaş + SGK + genel giderler): **₺300/saat**
- Pazar araştırmaları (Gartner, Gallup): UAM kullanan kurumlarda idle time
  azalması **%3-8 bandında**
- Personel muhafazakar tahmin: **%4 idle time azalması** (konsolide raporlar
  ile ölçülebilir — `/v1/reports/productivity` endpoint)

### Hesap

```
Kurtarılan saat = 176.000 × %4 = 7.040 saat/yıl
Verimlilik kazancı = 7.040 × ₺300 = ₺2.112.000/yıl
```

### Nereden geliyor?

- Dashboard şeffaflığı çalışanda **Hawthorne effect** oluşturur (izleme farkında olma → verim artışı)
- Yönetici zayıf performans gören ekip üyelerini erken fark eder, müdahale eder
- Uygulama envanteri ile lisans optimizasyonu (kullanılmayan SaaS abonelikleri iptal)
- İş akışı darboğazları (en çok zaman geçirilen uygulamalar, idle patternleri) ortaya çıkar

---

## 2. Veri Sızıntısı Önleme

### Temel varsayımlar

- Ponemon Institute 2023: Ortalama veri ihlali maliyeti **₺12-15 milyon** (Türkiye orta segment)
- KVKK Kurulu 2023 ortalama idari para cezası: **₺1-3 milyon** (orta/büyük şirket)
- 100 kişilik kurumda insider threat olasılığı (Verizon DBIR): **%3-5/yıl**
- UAM + DLP ile önlenebilir ihlal oranı: **%60-80** (Gartner)

### Hesap

```
Beklenen yıllık ihlal maliyeti (UAM'siz) = 0.04 × ₺13.000.000 = ₺520.000
Personel ile önlenen = ₺520.000 × %70 = ₺364.000/yıl
```

### Eklenebilir: KVKK Kurulu cezası önleme

```
KVKK cezası olasılığı (şikayet veya denetim) = %10/yıl
Ortalama ceza = ₺2.000.000
Beklenen maliyet = 0.10 × ₺2.000.000 = ₺200.000/yıl
Personel ile önleme (uyum kanıtları hazır → ceza düşürülebilir) = ₺200.000 × %80 = ₺160.000/yıl
```

**Toplam sızıntı + ceza önleme**: ₺524.000/yıl

---

## 3. Uyum Maliyeti Düşüşü

### KVKK uyum süreçleri manuel yapıldığında

- **DSR (m.11) yanıtı**: Yılda ortalama 15-30 başvuru × 4-8 saat DPO emeği
  - 20 DSR × 6 saat × ₺600/saat = ₺72.000/yıl
- **VERBİS güncellemesi**: Yılda 2 kez × 16 saat = 32 saat × ₺600 = ₺19.200/yıl
- **Denetim hazırlığı**: Yılda 1 iç denetim × 40 saat × ₺600 = ₺24.000/yıl
- **Çalışan aydınlatma süreci**: Yılda ortalama 20 yeni işe alım × 2 saat = 40 saat × ₺300 = ₺12.000/yıl
- **Aylık rapor üretimi**: 12 ay × 4 saat × ₺600 = ₺28.800/yıl
- **Hukuki danışmanlık (KVKK sorularına)**: Yılda 8 saat × ₺2.000 = ₺16.000/yıl

**Manuel toplam**: ₺172.000/yıl

### Personel ile otomatikleştirildiğinde

- DSR otomatik fulfillment (imzalı ZIP export) → DPO emeği: 0.5 saat/başvuru × 20 = 10 saat × ₺600 = ₺6.000
- VERBİS otomatik export → 1 saat × 2 = 2 saat × ₺600 = ₺1.200
- Denetim runbook hazır → 4 saat × ₺600 = ₺2.400
- İlk-giriş modal otomatik → 0 ek maliyet
- Otomatik raporlama → 0 ek maliyet
- Hukuki soru azalır (framework hazır) → 2 saat × ₺2.000 = ₺4.000

**Personel ile toplam**: ₺13.600/yıl

**Uyum maliyet tasarrufu**: ₺172.000 - ₺13.600 = **₺158.400/yıl**

---

## 4. Personel Lisans Maliyeti (Örnek)

- 100 endpoint × Business tier (örnek: ₺2.400/endpoint/yıl) = **₺240.000/yıl**
- Eklenti modüller (UBA + OCR) = ₺84.000/yıl
- Kurulum + ilk yıl danışmanlığı (tek seferlik) = ₺50.000
- Destek SLA (Priority tier dahil) = ₺0 (lisansa dahil)

**Toplam yıllık operasyonel**: ₺324.000

**Not**: Rakamlar örnektir, gerçek fiyatlandırma müşteriye özel teklifle belirlenir.

---

## 5. Net ROI Hesabı (100 Kullanıcı, Yıl 1)

| Kalem | Değer (₺) |
|---|---|
| Verimlilik kazancı | +2.112.000 |
| Sızıntı + ceza önleme | +524.000 |
| Uyum maliyet düşüşü | +158.400 |
| **Toplam fayda** | **+2.794.400** |
| Personel lisans (ilk yıl) | -324.000 |
| Kurulum (tek seferlik) | -50.000 |
| **Net fayda (yıl 1)** | **+2.420.400** |
| **ROI %** | **647%** |

**Geri ödeme süresi**: ~52 gün

---

## 6. Muhafazakar Senaryo (En Kötü Varsayımlar)

Eğer tüm varsayımları yarıya indirirsek:

| Kalem | Değer (₺) |
|---|---|
| Verimlilik kazancı (%2) | +1.056.000 |
| Sızıntı önleme (%50 önleme) | +262.000 |
| Uyum düşüşü (%50) | +79.200 |
| **Toplam fayda** | **+1.397.200** |
| Personel lisans + kurulum | -374.000 |
| **Net fayda** | **+1.023.200** |
| **Muhafazakar ROI %** | **274%** |

Muhafazakar senaryoda bile **geri ödeme süresi ~3 ay**.

---

## 7. Müşteri Özelleştirme İçin Boş Tablo

Müşteri kendi rakamlarını doldurur:

| Parametre | Değer |
|---|---|
| Çalışan sayısı | ___ |
| Ortalama saatlik maliyet (₺) | ___ |
| Yıllık iş saati | ___ |
| İdle time azalma hedefi (%) | ___ |
| Sektör ihlal maliyeti (₺) | ___ |
| Yıllık KVKK cezası olasılığı (%) | ___ |
| DSR başvuru sayısı/yıl | ___ |
| DPO saatlik maliyet (₺) | ___ |
| Personel lisans teklifi (₺/yıl) | ___ |

**Formül**:
```
Verimlilik = Çalışan × 8 × 220 × Saatlik × (İdleAzaltma/100)
Sızıntı = 0.04 × İhlalMaliyeti × 0.70
Uyum = DSRAdet × 5.5 × DPOSaat  +  35.000
Toplam Fayda = Verimlilik + Sızıntı + Uyum
Net = ToplamFayda - Lisans - 50.000
ROI = Net / Lisans × 100
```

---

## 8. Satış Ekibi İçin Kritik Notlar

1. **Verimlilik kazancı iddialarına dikkat**: Gartner'ın %3-8 bandı akademik,
   müşteriye somut vaat vermek riskli. "Ölçülebilir" ifadesini kullan.
2. **Sızıntı önleme = garantili DEĞİL**: UAM sızıntıyı tamamen engellemez,
   sadece **erken tespit** ve **proof-of-incident** sağlar.
3. **Uyum maliyetleri gerçek**: KVKK Kurulu cezaları artıyor, denetimler
   sıkılaşıyor. Bu en güvenilir ROI kalemi.
4. **Geri ödeme süresi müşteriye göre değişir**: Finans sektöründe hızlı,
   imalat sektöründe yavaş (verimlilik kazancı sektöre bağlı).
5. **Teklif ederken kendi rakamını yaz**: Bu template'i kopyala, müşteri
   özelleştir, PDF'e dönüştür.

---

*Bu model `docs/product/competitive-analysis.md` rekabet teardown'u + KVKK
Kurulu karar veritabanı + Ponemon + Gartner + Verizon DBIR 2023 raporları
temel alınmıştır. Güncelleme: yılda 1 kez (Aralık).*
