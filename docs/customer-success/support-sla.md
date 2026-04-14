# Personel — Destek Tier'ları ve SLA (TR)

Bu doküman Personel Platform için sunulan destek seviyelerini, yanıt
sürelerini, eskalasyon matrisini ve severity sınıflandırmasını tanımlar.

**Hedef kitle**: Müşteri IT ekibi, Personel destek ekibi, CSM, Satış

---

## Destek Tier'ları

Üç tier mevcut. Müşteri sözleşmesinde seçilen tier lisans dosyasının
`tier` claim'i ile koreledir.

### Tier 1 — Standard

**Kimler için**: Küçük ekipler (50-100 endpoint), temel KVKK uyum, non-kritik kullanım.

| Metrik | Değer |
|---|---|
| Çalışma saatleri | Mesai içi (Pzt-Cum, 09:00-18:00, TRT) |
| İlk yanıt süresi | 24 saat |
| Çözüm hedef süresi (P1) | 3 iş günü |
| Çözüm hedef süresi (P2) | 5 iş günü |
| Kanallar | Email (destek@personel.local), Web ticket |
| Mesai dışı | Yok (24 saat yanıt kuralı uygulanır) |
| Haftalık check-in | Yok |
| Aylık rapor | Otomatik (email) |
| Yıllık eğitim saati | 8 saat (1 kez) |

**Dahil olanlar**: Bug fix, upgrade, troubleshooting, dokümantasyon erişimi.
**Dahil olmayanlar**: Özelleştirme, konfigürasyon danışmanlığı, on-site.

---

### Tier 2 — Priority

**Kimler için**: Orta ölçekli şirketler (100-300 endpoint), KVKK aktif denetim kapsamı, üretim kullanımı.

| Metrik | Değer |
|---|---|
| Çalışma saatleri | 8×5 (Pzt-Cum, 09:00-18:00) + P1 hafta sonu telefon |
| İlk yanıt süresi (P1) | 4 saat |
| İlk yanıt süresi (P2) | 8 saat |
| İlk yanıt süresi (P3) | 24 saat |
| Çözüm hedef süresi (P1) | 24 saat |
| Çözüm hedef süresi (P2) | 2 iş günü |
| Kanallar | Email, Web ticket, Slack Connect, Telefon (mesai içi) |
| Aylık check-in | 30 dakika CSM |
| Çeyrek QBR | 60 dakika |
| Yıllık eğitim saati | 16 saat |
| Aylık rapor | Otomatik + özelleştirilmiş |

**Bonus**: Patch pre-release erişimi, feature request öncelik.

---

### Tier 3 — Enterprise

**Kimler için**: Büyük şirketler (300+ endpoint), kritik altyapı, regülasyon baskısı yüksek, 7×24 kullanım.

| Metrik | Değer |
|---|---|
| Çalışma saatleri | 7×24 (tüm yıl) |
| İlk yanıt süresi (P1) | 1 saat |
| İlk yanıt süresi (P2) | 4 saat |
| İlk yanıt süresi (P3) | 8 saat |
| Çözüm hedef süresi (P1) | 8 saat |
| Çözüm hedef süresi (P2) | 24 saat |
| Kanallar | Email, Web ticket, Slack Connect, Telefon (7×24), Dedicated TAM |
| Haftalık check-in | 30 dakika Technical Account Manager (TAM) |
| Aylık QBR | 90 dakika |
| Yıllık eğitim saati | Sınırsız (makul kullanım) |
| On-site destek | Yılda 2 gün dahil, ek ücretsiz |
| Aylık rapor | Custom dashboard erişimi |
| HA Cluster desteği | Dahil |
| Erken release erişimi | Evet (RC build) |

**Bonus**: CTO-level escalation path, roadmap influence, özel feature geliştirme görüşmesi.

---

## Severity Matrix

Severity'yi müşteri bildirir, ama Personel destek ekibi teyit veya
re-sınıflandırma hakkına sahiptir. Yanlış severity ile ticket açmak
SLA'yı etkilemez — doğrusu geriye dönük uygulanır.

### P1 — Critical / Service Down

**Tanım**: Production stack tamamen çalışmıyor veya kritik veri kaybı/açılma riski var.

**Örnekler**:
- API sunucusu çökmüş, hiç kimse Console'a giremiyor
- Vault unseal fail, tüm stack down
- Postgres data corruption
- Tüm endpoint'ler offline (agent crash)
- Güvenlik zafiyeti aktif sömürü altında

**Aksiyon**: Anlık war-room, tüm kaynaklar bu sorunda.

### P2 — High / Critical Feature Degraded

**Tanım**: Bir kritik özellik çalışmıyor ama workaround mümkün.

**Örnekler**:
- DSR workflow submit oluyor ama fulfill edemiyoruz
- Ekran görüntüleri MinIO'ya yüklenmiyor
- Audit log'a yazılıyor ama okunamıyor
- Bir bölümdeki tüm endpoint'ler kopmuş
- Dashboard p95 süresi 10x yavaşlamış

**Aksiyon**: Dedicated engineer, normal iş saatlerinde birinci öncelik.

### P3 — Medium / Minor Issue

**Tanım**: İşlevsellik çalışıyor ama beklenmeyen davranış veya verimsiz.

**Örnekler**:
- Rapor sayfasında bir kolon yanlış
- UI'da typo veya lokalize edilmemiş string
- Belirli bir event türü ClickHouse'a yazılıyor ama filtrelenemiyor
- Bir endpoint 3 gündür sessiz (tek makine sorunu)

**Aksiyon**: Normal queue, sonraki patch release'e girer.

### P4 — Low / Cosmetic

**Tanım**: Kozmetik, iyileştirme önerisi, belgelendirme.

**Örnekler**:
- Dashboard renk önerisi
- UI kenar boşluğu
- Doküman cümle önerisi
- Rapor başlık alternatif

**Aksiyon**: Backlog'a eklenir, bir sonraki minor release veya hiç yapılmayabilir.

---

## Escalation Matrix

### L1 — Support

- İlk temas noktası, ticket açılır
- Bilinen sorunlar için KB'den yanıt verir
- Çözemediği sorunu L2'ye iletir

### L2 — Engineering

- Bug triaj, reproduction, patch yazma
- L1'in çözemediği veya production reproducibility gerektiren sorunlar
- L3'e eskalasyon yetkisi

### L3 — Architecture / Senior

- Mimari etki analizi gerektiren değişiklikler
- Çok-component sorunlar (gateway + API + agent etkileşimi)
- Güvenlik zafiyeti değerlendirmesi
- Performance bottleneck analizi

### Management — Exec

- Müşteri memnuniyetsizliği
- Sözleşmesel anlaşmazlık
- Müşteri P1 sorun uzun sürerse (4+ saat)
- Medya/PR riski

### Customer Escalation Path

Müşteri aşağıdaki durumlarda direkt escalation isteyebilir:

1. SLA 2 kez ihlal edilirse aynı ticket'ta
2. L1 + L2 aynı sorunda çözüm üretemiyorsa (≥3 tur)
3. P1 4 saati geçerse — otomatik CTO/VP eskalasyon
4. Üretim veri kaybı şüphesi — anlık executive sponsor call

---

## Response Time Tracking

Personel destek ekibi iç metriklerini aylık yayınlar:

| Metrik | Hedef | Ölçüm Noktası |
|---|---|---|
| İlk yanıt SLA compliance | >%95 | Ticket açılış → ilk insan yanıtı |
| Çözüm SLA compliance | >%90 | Ticket açılış → "resolved" state |
| Customer Satisfaction (CSAT) | >4.5/5 | Ticket kapanışında 1 soru |
| Net Promoter Score (NPS) | >40 | 3 ayda 1 email survey |
| First Contact Resolution | >%60 | İlk yanıtta çözülen ticket oranı |

Dashboard: internal Grafana, müşterilere paylaşılmaz.

---

## İzin Günleri ve Ulusal Tatiller

Tier 1 / Tier 2 için ulusal tatillerde (23 Nisan, 19 Mayıs, 30 Ağustos,
29 Ekim, yılbaşı vb.) destek kapalıdır. P1 aciliyetler için telefon numarası
Tier 2 sözleşmelerinde verilir.

Tier 3 her gün 7×24 destek verir.

Yıllık destek takvimi: `docs/customer-success/holiday-calendar.md` (CSM tarafından güncellenir).

---

## Ticket Yaşam Döngüsü

```
 Open ─────► Triaged ─────► In Progress ─────► Resolved ─────► Closed
   │            │               │                   │              │
   │            ▼               ▼                   ▼              │
   │         P1-P4          L1/L2/L3            Verify            │
   │                                             by CS             │
   │                                                               │
   └─────────── Reopen (if customer disagrees) ───────────────────┘
```

**Reopen politikası**: Müşteri resolved ticket'ı 7 gün içinde reopen edebilir
aynı konu için. 7 gün sonrası yeni ticket açılması istenir.

---

## İletişim Kanalları

| Kanal | Tier 1 | Tier 2 | Tier 3 |
|---|---|---|---|
| Email | ✅ | ✅ | ✅ |
| Web portal | ✅ | ✅ | ✅ |
| Slack Connect | ❌ | ✅ | ✅ |
| Telefon (mesai) | ❌ | ✅ | ✅ |
| Telefon (7×24) | ❌ | P1 only | ✅ |
| Dedicated TAM | ❌ | ❌ | ✅ |

**İletişim adresleri**:
- Email: destek@personel.local
- Web: https://support.personel.local (SSO ile)
- Slack: müşteriye özel Slack Connect channel (T2+)
- Telefon: sözleşmede verilen numara

---

*Bu SLA sözleşmenin bir parçasıdır. Değişiklikler yazılı tadil ile yapılır.
Güncelleme: yılda 1 kez, VP Customer Success tarafından onaylanır.*
