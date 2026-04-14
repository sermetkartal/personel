# Personel — Admin Certification Exam (TR)

Personel Platform admin rolü için 20 soruluk sertifikasyon sınavı.

**Format**: Çoktan seçmeli, tek doğru cevap
**Süre**: 30 dakika
**Geçme**: 15/20 (75%)
**Retake**: 1 hafta sonra, 1 kez

---

## Sorular

### 1. Aşağıdakilerden hangisi Personel'in **varsayılan olarak** toplamadığı bir veri türüdür?

- [ ] A) Uygulama kullanım süresi
- [ ] B) Ekran görüntüsü
- [x] C) Klavye tuş içerikleri
- [ ] D) Web sitesi başlığı

**Açıklama**: ADR 0013 ile klavye içeriği varsayılan KAPALI'dır. Etkinleştirmek için DPO + IT Security + Hukuk imzalı opt-in töreni gerekir.

---

### 2. Canlı izleme (live view) başlatmak için kaç kişi gerekir?

- [ ] A) 1 (tek Admin)
- [x] B) 2 (farklı kullanıcılar — requester ≠ approver)
- [ ] C) 3 (IT + DPO + Manager)
- [ ] D) Manager tek başına yetebilir

---

### 3. Audit log'u değiştirebilen rol hangisidir?

- [ ] A) Admin
- [ ] B) DPO
- [ ] C) Investigator
- [x] D) Hiçbir rol — audit log append-only + hash-zincirli

---

### 4. Personel'de bir DSR (KVKK m.11 başvurusu) kaç gün içinde yanıtlanmalıdır?

- [ ] A) 7 gün
- [ ] B) 15 gün
- [x] C) 30 gün
- [ ] D) 60 gün

---

### 5. Vault unseal ceremony kaç keyholder gerektirir?

- [ ] A) 1 of 3
- [ ] B) 2 of 3
- [x] C) 3 of 5 (Shamir)
- [ ] D) 5 of 5

---

### 6. Ekran görüntüleri nerede şifreli olarak saklanır?

- [ ] A) PostgreSQL
- [ ] B) ClickHouse
- [x] C) MinIO (AES-GCM envelope)
- [ ] D) Redis

---

### 7. Bir endpoint'in "revoke" edilmesi aşağıdakilerden hangisine yol açar?

- [x] A) mTLS cert geçersizleşir, agent bir daha bağlanamaz
- [ ] B) Endpoint'teki tüm veriler silinir
- [ ] C) Kullanıcı hesabı kilitlenir
- [ ] D) Hiçbir şey — sadece UI'dan kaldırılır

---

### 8. Aşağıdaki rollerden hangisi **keystroke ciphertext**'ini dekripte edebilir?

- [ ] A) Admin
- [ ] B) DPO
- [ ] C) Investigator
- [x] D) Hiçbir rol — sadece izole DLP motoru kural bazlı eşleşme arayabilir

---

### 9. Personel stack toplam kaç Docker container'dan oluşur (full production)?

- [ ] A) 6
- [ ] B) 12
- [x] C) 18
- [ ] D) 24

---

### 10. DLP'yi etkinleştirmek için aşağıdakilerden hangisi gereklidir?

- [ ] A) Sadece Admin UI'dan checkbox işaretlemek
- [ ] B) Docker Compose profile'ı aktifleştirmek
- [x] C) DPO + IT Security + Hukuk imzalı opt-in formu + Vault Secret ID issuance + Compose profile + Portal banner
- [ ] D) Hiçbir şey — zaten açık

---

### 11. Bir çalışan "verilerimi sil" (KVKK m.11/d) talebinde bulunursa:

- [ ] A) Tüm audit log kayıtları silinir
- [ ] B) Hiçbir şey olmaz — reddedilir
- [x] C) Crypto-erase: User PE-DEK'i Vault'tan silinir, ciphertext kurtarılamaz, `pii_erased=true` bayrağı konur
- [ ] D) User tablosundan row tamamen silinir

---

### 12. Agent offline çalıştığında veri nerede birikir?

- [ ] A) Bellek — reboot olunca kaybolur
- [ ] B) Plain text JSON dosyası
- [x] C) SQLCipher encrypted queue (AES-256 page)
- [ ] D) MinIO'ya direkt yazılır

---

### 13. NATS JetStream'de `events_raw` stream'inin amacı nedir?

- [x] A) Agent'lardan gelen olayları enricher tüketimi için buffer'lamak
- [ ] B) ClickHouse backup
- [ ] C) MinIO upload queue
- [ ] D) Prometheus metrics

---

### 14. Bir policy push edildiğinde imzalayan anahtar nerede yaşar?

- [ ] A) API binary'sine gömülü
- [x] B) Vault transit engine (control-plane Ed25519 key, `exportable: false`)
- [ ] C) PostgreSQL TLS cert
- [ ] D) Policy yaml dosyasında base64

---

### 15. Dashboard p95 sorgu süresi için Faz 1 exit kriteri hedefi nedir?

- [ ] A) 100 ms
- [ ] B) 500 ms
- [x] C) 1 saniye
- [ ] D) 5 saniye

---

### 16. Bir çalışan portal'da "Neler İzlenmiyor" sayfasında kaç madde görür?

- [ ] A) 5
- [x] B) 10
- [ ] C) 15
- [ ] D) 20

---

### 17. Screen capture politika ile hangi tür uygulamaları hariç tutar?

- [x] A) Banka, parola yöneticisi, private browsing modu
- [ ] B) Hiçbir şey — her şey yakalanır
- [ ] C) Sadece Office uygulamaları
- [ ] D) Sadece web tarayıcıları

---

### 18. Agent'ın minimum Rust sürümü (MSRV) nedir?

- [ ] A) 1.75
- [x] B) 1.88
- [ ] C) 1.70
- [ ] D) 1.65

---

### 19. Evidence locker (SOC 2 Type II) kaç kontrol için collector'a sahiptir?

- [ ] A) 5
- [ ] B) 7
- [x] C) 9 (CC6.1, CC6.3, CC7.1, CC7.3, CC8.1, CC9.1, A1.2, P5.1, P7.1)
- [ ] D) 12

---

### 20. Aşağıdaki ifadelerden hangisi **yanlıştır**?

- [ ] A) Personel Rust agent tek binary olarak dağıtılır
- [ ] B) Postgres RLS (Row-Level Security) multi-tenant izolasyonunu sağlar
- [x] C) ClickHouse aynı zamanda metadata veritabanı olarak kullanılır
- [ ] D) Keycloak OIDC/SAML/SCIM destekler

**Açıklama**: Metadata PostgreSQL'de tutulur. ClickHouse sadece time-series event store.

---

## Değerlendirme Rehberi (Eğitmen)

- 18-20 doğru: **Uzman seviyesi** — ekstra bir şey gerekmez
- 15-17 doğru: **Başarılı** — sertifika verilir
- 10-14 doğru: **Retake** önerilir — zayıf alanlar identify edilip 1 hafta çalışma
- <10 doğru: **Fail** — baştan eğitim gerekir

## Sertifika

```
┌─────────────────────────────────────────────┐
│          PERSONEL CERTIFIED ADMIN           │
│                                             │
│   [Ad Soyad]                                │
│                                             │
│   Has successfully completed the Personel   │
│   Platform Administrator Training Program   │
│                                             │
│   Score: X/20                               │
│   Date: YYYY-MM-DD                          │
│   Valid until: YYYY-MM-DD (2 yıl)           │
│                                             │
│   [Signature — Personel Training Lead]      │
└─────────────────────────────────────────────┘
```

Dijital imzalı PDF olarak üretilir. Yıllık yenileme: 2 yılda bir (önemli
release sonrası güncelleme testine tabi).

---

*Sorular Personel pilot ve Faz 1 bilgi tabanına dayanır. Her major release
sonrası revize edilir.*
