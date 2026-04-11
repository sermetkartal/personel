# Access Review Policy / Erişim Gözden Geçirme Politikası

> **Belge sahibi**: Compliance Officer, co-signed by DPO
> **Kapsam**: Keycloak insan kullanıcıları + Vault servis hesapları + PostgreSQL operasyonel hesaplar
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2027-04-10 (yıllık)
> **İlgili**: SOC 2 CC6.2 (ADR 0023), ISO 27001:2022 A.5.15, A.5.16, A.5.18, A.8.2, A.8.3, KVKK m.12, ADR 0018 (HRIS lifecycle), `docs/compliance/kvkk-framework.md`

## 1. Amaç ve Kapsam (Purpose & Scope)

Personel platformundaki tüm erişim haklarının meşru iş ihtiyacına uygun kalmaya devam ettiğinden emin olmak için periyodik olarak gözden geçirilmesini sağlar. Kapsam:

- **İnsan kullanıcılar**: Keycloak `personel-realm` içindeki tüm kullanıcılar (Admin, DPO, Investigator, Manager, HR, Auditor, Viewer rolleri).
- **Servis hesapları**: Vault AppRole'leri, Keycloak service accounts, PostgreSQL operasyonel roller, ClickHouse rolleri, MinIO access keys.
- **Break-glass hesapları**: ayrı bir kategori (§7).

Kapsam dışı: müşterinin kendi on-prem kurulumunda yönettiği son kullanıcı hesapları; müşteri kurum kendi access review'ünü yürütür.

## 2. Policy Statement / Politika Beyanı

Personel, en az ayrıcalık (least privilege) ilkesini uygular. Hiçbir kullanıcı veya servis hesabı, görevi için gerekenin ötesinde yetkiye sahip olamaz. Her yetki periyodik olarak gözden geçirilir ve gözden geçirme sonucu hash-zincirli audit log'a yazılır.

## 3. Responsibilities / Sorumluluklar

| Rol | Sorumluluk |
|---|---|
| Compliance Officer | Gözden geçirme sürecini sahiplenir, takvimi yönetir, raporları saklar |
| DPO | Yüksek ayrıcalıklı rolleri (Admin, DPO, Investigator) co-sign eder |
| Department Manager | Kendi ekibindeki kullanıcıların rol uygunluğunu onaylar |
| Platform Lead (IT/DevOps) | Servis hesaplarının erişim kapsamını doğrular |
| HR | HRIS lifecycle değişikliklerinin otomasyon ile eşleştiğini teyit eder |
| Audit Chain Recorder | Her onay/reddi hash-chained audit log'a yazar (otomatik) |

## 4. Review Frequency / Gözden Geçirme Sıklığı

| Rol sınıfı | Sıklık | Onay | Gerekçe |
|---|---|---|---|
| Admin, DPO, Investigator | **Çeyreklik** | Manager + DPO co-sign | SOC 2 CC6.2 yüksek ayrıcalık gereği |
| Legal Hold operatörleri | **Çeyreklik** | DPO + Legal Counsel | m.6 özel nitelikli veri erişimi |
| HR, Manager | Altı aylık (semi-annual) | Manager + CO | Operasyonel rol |
| Auditor (read-only) | Altı aylık | CO | Gözlem rolü |
| Viewer | Altı aylık | Manager | Düşük ayrıcalık |
| Vault root token holders | **Çeyreklik** | CEO + Security Lead | Unseal ceremony kritikliği |
| Diğer servis hesapları | Altı aylık | Platform Lead | — |
| Break-glass hesaplar | **Çeyreklik** | CO + CEO | §7 |

## 5. Procedure / Prosedür

### 5.1 Trigger

CO, ilgili çeyreğin ilk iş günü otomatik iş (cron job) tetikler.

### 5.2 Evidence extraction

Hash-chained audit chain, aşağıdaki kayıt setlerini export eder:

1. Keycloak REST API üzerinden tüm kullanıcı + rol grant'ları
2. Vault `sys/internal/counters` + `sys/policies` listesi
3. PostgreSQL `pg_roles` dump
4. HRIS aktif çalışan listesi (ADR 0018 sync)
5. Son gözden geçirme döneminde log'lanan login, session, token refresh olayları

Export WORM bucket'a yazılır; SHA-256 checksumları audit chain'e eklenir.

### 5.3 Review execution

CO, bir reviewer worksheet üretir ve her manager'a gönderir:

- Kullanıcı adı, rolü, son giriş tarihi, son ayrıcalıklı eylem tarihi, HRIS statüsü
- Manager: **Keep / Modify / Revoke** seçeneklerinden birini işaretler
- Yüksek ayrıcalıklı roller için DPO ek imzası zorunludur

Manager yanıtı 10 iş günü içinde teslim edilmelidir. Yanıt gelmeyen kullanıcılar için varsayılan eylem: **erişim askıya alınır** (revoke değil — eski haline getirme 1 iş günü içinde yapılabilir).

### 5.4 Automation via HRIS (ADR 0018)

HRIS bir çalışanı terminated olarak işaretlediğinde:

- **4 saat içinde** Keycloak hesabı disable edilir (otomatik)
- **24 saat içinde** Vault AppRole'leri revoke edilir
- **7 gün içinde** tüm oturumlar zorla sonlandırılır ve tokens revoked
- HRIS-driven revocation ayrı bir audit chain kategorisinde (`access.hris.termination`) log'lanır

HRIS kesintisi durumunda: CO 1 iş günü içinde manuel doğrulama yapar, HR-ops checklist izlenir.

### 5.5 Sign-off and recording

Tamamlanan worksheet'ler:

- PDF'e dönüştürülür ve CO tarafından Yubikey ile imzalanır
- WORM bucket'a yazılır (retention: **3 yıl**)
- Checksum audit chain'e eklenir
- Özet bir rapor quarterly management review'e sunulur (ADR 0024)

## 6. Exceptions / İstisnalar

İstisnalar yazılı olarak belgelenmelidir. Her istisna:

- İstek gerekçesi, riski azaltan dengeleyici kontroller
- CO + DPO onayı
- Azami süre: 90 gün (yenilenebilir)
- Sonraki çeyreklik review'de otomatik bayrak

## 7. Break-glass Access

Break-glass (acil durum yüksek ayrıcalık) hesapları:

- Vault root token'ı Shamir share'leriyle offline saklanır
- Kullanım gerektiğinde: CEO + CO + Security Lead üçlü onay
- Kullanım sonrası 24 saat içinde post-hoc gözden geçirme, aksiyon loglarının hash chain incelenmesi
- Tüm break-glass kullanımları çeyreklik incidence report'ta listelenir

## 8. Metrics / Ölçütler

- Çeyreklik completion rate: hedef %100
- HRIS → Keycloak revocation gecikmesi: p95 < 4 saat
- Manager response SLA: hedef %95 within 10 gün
- Zombi hesap sayısı (son 90 gün giriş yok): hedef 0

## 9. Related Documents

- SOC 2 ADR 0023 CC6.1–CC6.3
- ISO 27001 ADR 0024 Annex A.5.15, A.5.18, A.8.2, A.8.3
- ADR 0018 HRIS connector interface
- `docs/security/runbooks/vault-setup.md`
- `docs/security/runbooks/admin-audit-immutability.md`
- `docs/policies/change-management.md`

## 10. Review Cycle

- Policy document review: yıllık (2027-04-10)
- Erişim review execution: §4'te tanımlı

## 11. Approval / İmzalar

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______ | _______ | _______ |
| DPO | _______ | _______ | _______ |
| CO | _______ | _______ | _______ |
| CTO | _______ | _______ | _______ |

---

## English Summary (for auditors)

This policy implements SOC 2 CC6.2 and ISO 27001:2022 Annex A.5.15, A.5.18, A.8.2, A.8.3. Human users in Keycloak and service accounts in Vault, PostgreSQL, ClickHouse, and MinIO are reviewed quarterly for Admin/DPO/Investigator/Legal-Hold/Vault-root/break-glass classes and semi-annually for all other classes. Each review cycle exports grants from the hash-chained audit chain, distributes worksheets to department managers, and requires DPO co-signature for high-privilege roles. HRIS-driven termination (ADR 0018) revokes Keycloak access within 4 hours and Vault within 24 hours. Completed worksheets are Yubikey-signed PDFs stored in a WORM bucket with 7-year retention (audit) and 3-year policy retention. Break-glass access requires tri-party approval (CEO + CO + Security Lead) and post-hoc review within 24 hours. Exceptions are time-bounded (90 days) and tracked by the Compliance Officer.
```

---
