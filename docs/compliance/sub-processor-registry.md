# Alt Veri İşleyen Kaydı — Personel Platformu

> **Hukuki dayanak**: 6698 sayılı KVKK m.12/2 (yazılı veri işleyen sözleşmesi), `docs/compliance/dpa-template.md` §5 ve Ek-D. GDPR çapraz referans: m.28/2 ve m.28/4 (sub-processor onay zinciri).
>
> **Amaç**: İşbu doküman, **Personel Yazılım**'ın müşterilerine sunduğu hizmetler kapsamında kullandığı (veya kullanması planlanan) tüm üçüncü taraf veri işleyenlerin **canlı, sürüm-kontrollü** kaydıdır. KVKK m.12 ve DPA ek-D kapsamında müşteriye duyurulan kamu dokümanı niteliğindedir.
>
> **Yayım**: Bu doküman repository'de versiyon kontrollü tutulur. Müşteri DPO'ları değişiklikleri RSS/git-watch veya e-posta abonesi olarak takip edebilir. Her değişiklik commit + change log entry + müşteri e-posta bildirimi üretir.
>
> Versiyon: 1.0 — Nisan 2026

---

## 1. Mevcut Durum (Phase 1 — On-Prem MVP)

> **Phase 1 MVP on-prem deployment'ta Personel Yazılım hiçbir alt veri işleyen kullanmaz. Tüm kişisel veri Müşteri lokasyonunda kalır; Personel Yazılım altyapısına hiçbir koşulda akmaz.**

### 1.1. Aktif Sub-processor Tablosu (Phase 1)

| # | Sub-processor | Hizmet | Veri Kategorisi | Lokasyon | Güvenlik Tedbirleri | Eklenme | Müşteri Bildirim |
|---|---|---|---|---|---|---|---|
| — | **YOK** | — | — | — | — | — | — |

**Toplam aktif sub-processor sayısı: 0 (sıfır)**

### 1.2. Phase 1 Mimari Garantisi

`docs/compliance/kvkk-framework.md` §3.1 ve `CLAUDE.md` §6 (Locked Decisions) uyarınca:

- Personel Yazılım on-prem-first stratejisi gereği müşteri verisine **erişmez**;
- Tüm bileşenler (PostgreSQL, ClickHouse, MinIO, Vault, Keycloak, NATS, OpenSearch, LiveKit) müşteri kendi veri merkezinde (Docker Compose + systemd) çalışır;
- Telemetri, hata raporlama, çökme dump'ı veya benzeri **hiçbir veri Personel Yazılım'a otomatik gönderilmez**;
- Destek talebi çerçevesinde yapılan log paylaşımı **yalnızca müşteri tarafından manuel ve PII redakte edilmiş** olarak iletilir.

Bu nedenle Phase 1'de bir alt veri işleyen ihtiyacı **kavramsal olarak** doğmaz.

---

## 2. Phase 2+ İçin Planlanan Olası Sub-processor'lar

Phase 2+ (potansiyel SaaS / hybrid / cloud teklifi) ile birlikte aşağıdaki sub-processor'lar **opt-in** olarak düşünülmektedir. Müşteri açıkça aktive etmedikçe **hiçbiri varsayılan değildir**.

> **Önemli**: Aşağıdaki tablo **planlama amaçlıdır**. Bir sub-processor gerçek hizmete alınmadan önce işbu registry'ye taşınır, müşterilere 30 gün önceden bildirilir ve resmi DPA imzalanır.

| # | Sub-processor (Aday) | Hizmet | İşlenecek Veri Kategorisi | Lokasyon | Güvenlik Tedbirleri | Müşteri Aktivasyonu | Statü |
|---|---|---|---|---|---|---|---|
| 1 | **Sentry.io** (Functional Software Inc.) | Hata raporlama, stack trace toplama | Console + portal frontend hata trace'leri (PII redakte edilmiş) | ABD/AB (müşteri tercihiyle) | TLS 1.3, OAuth, scrubbing rules, EU-Frankfurt opt | **Opt-in**: müşteri DPO açıkça aktive eder | **Planlanan — aktif değil** |
| 2 | **SMTP/Email Relay** (örn. AWS SES, Postmark, Mailgun) | İşlemsel e-posta gönderimi (DSR bildirimleri, audit alert) | Müşteri DPO/admin e-posta adresleri + bildirim metni (PII içerebilir) | AB (müşteri seçimi) | TLS 1.3, SPF/DKIM/DMARC, DPA + SCC | **Opt-in**: kendi SMTP kullanımı varsayılan | **Planlanan — aktif değil** |
| 3 | **HashiCorp Vault Enterprise** (HSM backend) | KMS/HSM unseal arka ucu | Kriptografik anahtar materyali (kişisel veri **değil**) | Müşteri data center | FIPS 140-2 Level 3 HSM, network isolation | **Opt-in**: yüksek güvenlik gereksinimli müşteri | **Planlanan — aktif değil** |
| 4 | **LiveKit Cloud** (LiveKit Inc.) | Canlı izleme WebRTC SFU (alternatif self-hosted) | Live view session metadata + relay (içerik yok, sadece routing) | AB | mTLS, end-to-end encryption, opt-in | **Opt-in**: self-hosted varsayılan | **Planlanan — aktif değil** |
| 5 | **Keycloak Cloud Hosting** (alternatif) | Yönetilen OIDC/SAML | Kullanıcı kimlik bilgileri (e-posta, ad) | AB | OIDC/SAML/SCIM, DPA | **Opt-in**: self-hosted Keycloak varsayılan | **Planlanan — aktif değil** |

### 2.1. Aktivasyon Kriterleri

Bir aday sub-processor **aktif** statüsüne geçmeden önce:

1. Yasal değerlendirme: hukuk müşaviri KVKK m.9 (yurt dışı transfer) ve GDPR m.46 (transfer mekanizması) açısından uygunluk teyit eder.
2. DPA + SCC: Personel Yazılım ile sub-processor arasında resmi DPA + Standard Contractual Clauses imzalanır.
3. Güvenlik incelemesi: SOC 2 Type II raporu, ISO 27001 sertifikası, penetrasyon testi sonuçları gözden geçirilir.
4. Müşteri bildirimi: 30 takvim günü öncesinden tüm aktif Müşteri DPO'larına e-posta + change log entry.
5. İtiraz penceresi: Müşterinin gerekçeli itirazı için 30 gün açık.
6. Onay sonrası aktivasyon: itiraz olmazsa tablonun §1.1 bölümüne taşınır.

---

## 3. Değişiklik Yönetimi Süreci

### 3.1. Yeni Sub-processor Ekleme

```
1. Talep + iş gerekçesi (product owner)
   └─ Yasal değerlendirme (DPO + Hukuk)
      └─ Güvenlik değerlendirme (CISO + Security Auditor)
         └─ DPA + SCC imza
            └─ Bu doküman güncelleme (PR)
               └─ §4 Change Log entry
                  └─ Müşteri DPO'lara e-posta bildirim (30 gün öncesi)
                     └─ İtiraz penceresi (30 gün)
                        └─ İtirazsızsa §1.1'e taşı + commit
                           └─ Audit zincirine `subprocessor.activated` event
```

### 3.2. Mevcut Sub-processor Çıkarma

```
1. Çıkarma kararı (müşteri talebi VEYA güvenlik olayı VEYA ticari karar)
   └─ Veri taşıma planı (varsa)
      └─ Müşteri DPO'lara e-posta bildirim
         └─ Çıkarma + DPA fesih
            └─ §1.1'den kaldır + §4 Change Log
               └─ Audit zincirine `subprocessor.deactivated` event
```

### 3.3. Müşteri İtiraz Hakkı

Bir müşteri DPO'su yeni sub-processor'a gerekçeli itiraz ederse:

- Personel Yazılım, alternatif çözüm sunmaya çalışır (örn. self-hosted alternatif, farklı bölge);
- Alternatif sunulamazsa müşteri sözleşmeyi tek taraflı feshetme hakkına sahiptir (DPA §5.2);
- Müşterinin kalan lisans bedeli oranlı iade edilir.

### 3.4. Bildirim Kanalları

| Kanal | Hedef | Sıklık |
|---|---|---|
| Bu doküman (git commit) | Tüm aktif/aday müşteri DPO'ları | Olay anında |
| E-posta bildirim | DPA Ek-D'de listelenen DPO e-postaları | Değişiklik anında |
| Müşteri Console — "Compliance Notices" sayfası | Müşteri admin/DPO rolü | Real-time (Phase 2.X+) |
| Status sayfası (`status.personel.com.tr`) | Kamu | Olay anında (Phase 3+) |

---

## 4. Versiyon ve Değişiklik Logu

> Yeni satırlar **en üste** eklenir. Her değişiklik için commit hash zorunludur.

| Tarih (YYYY-AA-GG) | Versiyon | Değişiklik | Etkilenen Sub-processor | Müşteri Bildirim Tarihi | Commit |
|---|---|---|---|---|---|
| 2026-04-12 | 1.0 | İlk yayın — Phase 1 MVP boş registry; Phase 2+ planlama tablosu eklendi | — | İlk yayın (yeni müşterilere DPA ile birlikte sunuluyor) | `[{ilk commit hash}]` |

### Değişiklik kategorileri (gelecek entry'ler için)

- **`ADD`**: yeni sub-processor aktivasyonu
- **`REMOVE`**: sub-processor pasifleştirme
- **`MODIFY`**: mevcut sub-processor için lokasyon, kapsam veya güvenlik tedbirlerinde değişiklik
- **`SCOPE`**: işlenen veri kategorisi değişikliği
- **`CONTRACT`**: DPA/SCC versiyon güncelleme
- **`STATUS`**: aday → aktif veya aktif → aday geçişleri

---

## 5. İlgili Dokümanlar

- `docs/compliance/dpa-template.md` — Ana DPA şablonu (§5 ve Ek-D bu registry'ye atıf yapar)
- `docs/compliance/kvkk-framework.md` — KVKK çerçevesi (§3 rol ayrımı)
- `docs/compliance/iltica-silme-politikasi.md` — Saklama ve imha matrisi
- `docs/compliance/verbis-kayit-rehberi.md` — VERBİS kayıt rehberi
- `docs/architecture/key-hierarchy.md` — Kriptografik tedbirler

---

## English Mirror — Summary

This is the live, version-controlled registry of all third-party sub-processors used by **Personel Yazılım** in delivering its services to customers. It is published per KVKK Art. 12 / GDPR Art. 28/2 transparency requirements and is referenced by Annex D of the DPA template.

### Current state (Phase 1 — On-Prem MVP)

**Personel Yazılım uses zero sub-processors in the Phase 1 on-prem deployment.** All personal data remains on customer premises and never reaches Personel Yazılım infrastructure. Total active sub-processors: **0**.

### Planned for Phase 2+ (opt-in)

Five candidate sub-processors are documented for forward planning, all opt-in and inactive: **Sentry.io** (error reporting), **SMTP/Email Relay** (transactional email), **HashiCorp Vault Enterprise HSM**, **LiveKit Cloud** (managed WebRTC SFU), **Keycloak Cloud Hosting**. None will be activated without legal review, security review, signed DPA + SCC, 30-day prior customer notification, and a customer objection window.

### Change management

Adding or removing a sub-processor follows a structured workflow: legal + security review → DPA signature → registry PR → 30-day customer notification → objection window → activation. Customers have a contractual right to object; if objected and no alternative is offered, the customer may terminate the licence with pro-rata refund.

### Notification channels

This document (git commits), email notifications to DPOs listed in DPA Annex D, the upcoming Compliance Notices page in the Customer Console, and the public status page.

### Change log

Latest entries appear at the top. Version 1.0 (2026-04-12) is the initial publication: Phase 1 empty registry plus Phase 2+ planning table.

---

*Bu doküman, hizmetin sunulduğu tüm müşteriler için bağlayıcıdır. Repository commit hash'i her versiyonun değiştirilemezliğinin kanıtıdır. Hukuki referanslar için bkz. KVKK 6698 m.12 ve `docs/compliance/dpa-template.md`. Versiyon 1.0 — Nisan 2026.*
