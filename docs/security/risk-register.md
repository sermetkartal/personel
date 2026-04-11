# Risk Register — Personel Platformu

> **Belge sahibi**: Compliance Officer (CO) / CISO co-sign
> **Metodoloji**: ISO/IEC 27005:2022
> **Versiyon**: 1.0 — 2026-04-10
> **Sonraki gözden geçirme**: 2026-07-10 (çeyreklik)
> **Onay**: CEO, CTO, DPO, CO (bkz. §8 İmza blokları)
> **İlgili belgeler**: ADR 0024 (ISO 27001+27701), ADR 0023 (SOC 2 Type II), `docs/compliance/kvkk-framework.md` §13, `docs/compliance/hukuki-riskler-ve-azaltimlar.md`, `docs/security/threat-model.md`, `docs/policies/incident-response.md`

## 1. Kapsam (Scope)

Bu kayıt, Personel User Activity Monitoring platformunun (on-prem ve Faz 3 SaaS baskıları) tasarım, geliştirme, dağıtım, operasyon ve destek süreçlerinden doğan bilgi güvenliği, gizlilik, operasyonel, uyum ve tedarikçi risklerini ele alır. ISMS kapsamı ADR 0024 §"ISMS scope statement" ile uyumludur.

Kapsam dışı: müşterinin kendi tesisinde işlettiği on-prem kurulumlardaki müşteri-işletim riskleri. Personel ürün sağlayıcıdır; müşteri veri sorumlusudur (`docs/compliance/kvkk-framework.md` §3).

## 2. Methodology / Metodoloji

ISO/IEC 27005:2022 senaryo tabanlı risk analizi uygulanır. 4 × 4 olasılık × etki matrisi kullanılır (ISO 27005 5-point yerine 4-point — karar gerekçesi: daha az "orta" kategorisinde biriken yanlılık; Kurul denetlenebilirliği için ayrık seviyeler).

### 2.1 Likelihood (Olasılık)

| Seviye | Etiket | Tanım | Yıllık olay sıklığı |
|---|---|---|---|
| 1 | Rare / Nadir | Neredeyse hiç gözlemlenmez | < 1 / 5 yıl |
| 2 | Unlikely / Düşük | Sektörde ara sıra | 1 / 1–5 yıl |
| 3 | Likely / Olası | Benzer kuruluşlarda düzenli | 1–4 / yıl |
| 4 | Almost Certain / Çok yüksek | Tedbirsiz kalırsa kesin | > 4 / yıl |

### 2.2 Impact (Etki)

| Seviye | Etiket | Finansal | Operasyonel | Uyum / Hukuki | Müşteri / İtibar |
|---|---|---|---|---|---|
| 1 | Minor / Düşük | < ₺250k | < 4 saat kesinti | İç gözlem | Küçük müşteri şikayeti |
| 2 | Moderate / Orta | ₺250k–₺2M | 4–24 saat kesinti | KVKK iç eylem planı | Müşteri eskalasyonu |
| 3 | Major / Yüksek | ₺2M–₺20M | 1–7 gün kesinti veya veri kaybı | Kurul bildirimi / Sup. Auth | Çoklu müşteri kaybı, basın |
| 4 | Severe / Felaket | > ₺20M | Sistem kaybı, kurtarılamayan veri | İdari para cezası, sözleşme feshi | Marka çöküşü |

### 2.3 Risk Rating (Derecelendirme)

Risk skoru = Likelihood × Impact (1–16). Derecelendirme:

- **1–3**: Low (Düşük) — kabul edilebilir, izlenir
- **4–6**: Medium (Orta) — azaltma planı gerekli, risk sahibi takibi
- **8–9**: High (Yüksek) — yönetim kararı gerekli, azaltma zorunlu
- **12–16**: Critical (Kritik) — CEO/CTO onayı gerekli, acil azaltma

### 2.4 Treatment options

ISO 27005'e göre dört seçenek:
- **Mitigate (Azalt)**: kontrollerle olasılık veya etkiyi düşür.
- **Accept (Kabul)**: kalan risk kabul edilebilir seviyedeyse yönetim imzası ile kabul.
- **Transfer (Aktar)**: sigorta, sözleşme, SLA yoluyla üçüncü tarafa.
- **Avoid (Kaçın)**: faaliyeti/teknolojiyi terk et.

### 2.5 Risk acceptance criteria

- Low → otomatik kabul (CO log kaydı yeterli)
- Medium → Compliance Officer yazılı kabulü
- High → CTO + CISO imzası
- Critical → CEO + CTO + DPO imzası, Yönetim Kurulu bilgilendirmesi

## 3. Review Cadence (Gözden Geçirme Sıklığı)

- **Çeyreklik (zorunlu)**: tüm kayıtların gözden geçirmesi, CO tarafından yürütülür.
- **Yıllık**: ISO 27005 tam yeniden değerlendirme (management review girdisi — ADR 0024 §"Management review cadence").
- **Olay tetiklemeli**: aşağıdaki olaylardan biri gerçekleşirse 5 iş günü içinde etkilenen kayıtlar güncellenir:
  - Anlamlı mimari değişiklik (yeni ADR)
  - Yeni pazar/bölge (EU genişleme gibi)
  - Yüksek ciddiyetli güvenlik olayı (`docs/policies/incident-response.md` §4 class = high/critical)
  - Kurul/Supervisory Authority denetimi
  - Tedarikçi değişikliği (kritik vendor ekleme/çıkarma)

## 4. Integration with Incident Response

Her yüksek veya kritik seviye olay sonrası (`docs/policies/incident-response.md` §8 Post-Incident Review), olay nedeni bir veya daha fazla kayıt ID'sine bağlanır. Eğer olay mevcut bir kayda bağlanamıyorsa, yeni bir kayıt açılır. Bu, risk register'ın "gerçeklik testi"dir.

## 5. Risk Register Entries

> Sütunlar: ID · Kategori · Açıklama · Mevcut kontroller · Likelihood (L) · Impact (I) · Residual (L×I) · Treatment · Sahip · Son gözden geçirme · Sonraki gözden geçirme

### 5.1 Agent / Endpoint (Rust)

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-AGT-001 | Technical | Agent tamper: yönetici olmayan kullanıcı DPAPI store'a erişim kazanır ve offline queue'yu dışa aktarır | `docs/security/anti-tamper.md`, DPAPI user scope, watchdog, Vault sealed blob | 2 | 3 | 6 | Mitigate — Phase 3 minifilter (ADR 0025) | Agent Lead | 2026-04-10 | 2026-07-10 |
| R-AGT-002 | Technical | Certificate pinning bypass: MITM proxy agent'ın mTLS peer doğrulamasını atlatır | Cert pinning (`transport/pinning.rs`), Hello key-version handshake, rustls roots kısıtlı | 1 | 4 | 4 | Mitigate — pinning regression test (qa/test/security) | Agent Lead | 2026-04-10 | 2026-07-10 |
| R-AGT-003 | Technical | Watchdog defeat: kötü niyetli yerel admin agent servisini durdurur ve watchdog'u da öldürür | Sibling watchdog, systemd/SCM monitörü, heartbeat alert Flow 7 | 2 | 3 | 6 | Mitigate — dual-service TCB, kernel-mode Phase 3 | Agent Lead | 2026-04-10 | 2026-07-10 |
| R-AGT-004 | Technical | Offline queue sızdırma: SQLCipher anahtarı DPAPI store'dan çıkarılır | Queue rotation, TTL, at-rest encryption | 2 | 3 | 6 | Mitigate — rotation every 24h | Agent Lead | 2026-04-10 | 2026-07-10 |
| R-AGT-005 | Operational | Auto-update supply chain: kötü niyetli güncelleme imzalanır (signing key kompromize) | Dual-signed updates (`docs/security/runbooks/auto-update-signing.md`), HSM signing | 1 | 4 | 4 | Mitigate — HSM + dual control | DevOps Lead | 2026-04-10 | 2026-07-10 |

### 5.2 Gateway / API

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-GW-001 | Technical | mTLS certificate chain failure: CA ara sertifika süresi geçer, tüm agent'lar düşer | `docs/architecture/mtls-pki.md`, cert TTL 90 gün, Vault PKI renewal, Prometheus expiry alert 30d | 2 | 4 | 8 | Mitigate — pre-expiry renewal otomasyonu | Platform Lead | 2026-04-10 | 2026-07-10 |
| R-GW-002 | Technical | Rate-limit bypass via parallel connections → DoS ingest tier | Gateway per-tenant bucket, NATS backpressure, Prometheus ingest lag alert | 2 | 3 | 6 | Mitigate — tenant isolation limit | Platform Lead | 2026-04-10 | 2026-07-10 |
| R-API-001 | Technical | Audit chain DBA bypass: PostgreSQL superuser audit trigger'ını devre dışı bırakır | Migration RBAC, WORM sekonder sink (ADR 0014), hash chain verifier (`docs/security/runbooks/admin-audit-immutability.md`) | 2 | 4 | 8 | Mitigate — sekonder WORM sink (Faz 1 tech debt, Phase 3.0 blocker) | API Lead | 2026-04-10 | 2026-07-10 |
| R-API-002 | Technical | RBAC role expansion: yetkisiz rol investigator'a yükseltilir | Keycloak role mapping, audit log, quarterly access review | 2 | 3 | 6 | Mitigate — access review policy | CO | 2026-04-10 | 2026-07-10 |
| R-API-003 | Compliance | DSR SLA breach (KVKK m.13 30-day / GDPR Art. 12 one-month) | DSR workflow, Prometheus SLA alert, DPO dashboard | 2 | 3 | 6 | Mitigate — automated escalation | DPO | 2026-04-10 | 2026-07-10 |
| R-API-004 | Technical | Unauthorized live view session: approver=requester bypass | Dual-control enforcement (API + UI), hash-chained audit | 1 | 4 | 4 | Mitigate — unit test gate in CI | API Lead | 2026-04-10 | 2026-07-10 |

### 5.3 Data Tier

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-DAT-001 | Technical | ClickHouse compromise: read replica node çalınır, 1B+ olay sızdırılır | ClickHouse at-rest encryption, network segmentation, Vault-issued TLS, no internet egress | 1 | 4 | 4 | Mitigate — backup zero-trust, per-tenant encryption Phase 3 | Data Lead | 2026-04-10 | 2026-07-10 |
| R-DAT-002 | Technical | MinIO exfiltration: screenshot bucket leaked (14-90 günlük görüntüler) | MinIO server-side encryption, presigned URL 5-min TTL, lifecycle policy | 2 | 4 | 8 | Mitigate — per-object encryption + audit | Data Lead | 2026-04-10 | 2026-07-10 |
| R-DAT-003 | Technical | Vault compromise: TMK material sızar → tüm tenant envelope'ları çözülebilir | Shamir 3-of-5, sealed audit log, HSM unseal Phase 3, `docs/architecture/key-hierarchy.md` | 1 | 4 | 4 | Mitigate — HSM unseal + break-glass procedure | Security Lead | 2026-04-10 | 2026-07-10 |
| R-DAT-004 | Technical | OpenSearch full-text index içerik sızıntısı (audit arama) | Ayrı credential, TLS zorunlu, RBAC ile read-only | 2 | 3 | 6 | Mitigate — field-level encryption review | Data Lead | 2026-04-10 | 2026-07-10 |
| R-DAT-005 | Technical | Backup medium compromise (Velero S3 / WAL) | Backup encryption key rotation, cross-region replication, restore drill quarterly | 2 | 4 | 8 | Mitigate — object-lock WORM + drill | DevOps Lead | 2026-04-10 | 2026-07-10 |
| R-DAT-006 | Technical | Keystroke content decryption key leakage (opt-in DLP enabled) | ADR 0013 off-by-default, DLP service isolation (`docs/security/runbooks/dlp-service-isolation.md`), seccomp, AppArmor | 1 | 4 | 4 | Mitigate — DLP profile audit, default OFF | Security Lead | 2026-04-10 | 2026-07-10 |

### 5.4 Compliance / Privacy

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-CMP-001 | Compliance | DPO absence / unstaffed: KVKK m.11 başvurularına 30 gün içinde yanıt verilemez | DPO rolü atanmış, yedek DPO identifikasyonu | 2 | 3 | 6 | Mitigate — named DPO + delegate, SLA alarmı | CEO | 2026-04-10 | 2026-07-10 |
| R-CMP-002 | Compliance | VERBİS kaydı güncellenmez (yıllık bildirim ihlali) | VERBİS kayıt rehberi (`docs/compliance/verbis-kayit-rehberi.md`), annual review | 2 | 2 | 4 | Mitigate — takvim + CO ownership | DPO | 2026-04-10 | 2026-07-10 |
| R-CMP-003 | Compliance | Aydınlatma metni eski versiyonda kalır, yeni veri kategorisi için güncellenmez | `docs/compliance/calisan-bilgilendirme-akisi.md` state machine, transparency portal version log | 2 | 2 | 4 | Mitigate — CI check + version diff | DPO | 2026-04-10 | 2026-07-10 |
| R-CMP-004 | Compliance | Unauthorized live view monitoring (çalışan rızası dışında) | HR dual-control, approver≠requester, reason code, 15/60 dk cap, audit chain | 1 | 4 | 4 | Mitigate — UI + API enforcement | DPO | 2026-04-10 | 2026-07-10 |
| R-CMP-005 | Compliance | Cross-border transfer violation (EU → TR): GDPR Art. 44 ihlali (Phase 3 SaaS) | Regional data residency (ADR 0021), SCC template, DPA (ADR 0022) | 2 | 4 | 8 | Mitigate — region pinning + SCC, transfer impact assessment | DPO | 2026-04-10 | 2026-07-10 |
| R-CMP-006 | Compliance | m.6 özel nitelikli veri kazara yakalama (ekran görüntüsü → sağlık uygulaması) | `screenshot_exclude_apps` policy, OCR redaction (Phase 2.8), short retention 14–30 gün | 2 | 4 | 8 | Mitigate — policy enforcement + OCR filter | DPO | 2026-04-10 | 2026-07-10 |
| R-CMP-007 | Compliance | 72-hour breach notification SLA failure (KVKK m.12/5, GDPR Art. 33) | IR playbook (§4), DPO alerting, communication templates | 2 | 4 | 8 | Mitigate — tabletop quarterly | DPO | 2026-04-10 | 2026-07-10 |

### 5.5 Vendor / Third Party

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-VND-001 | Vendor | HRIS (BambooHR/Logo Tiger) credential leak: employee roster çalınır | ADR 0018 credential storage (Vault), scoped API key, IP allowlist | 2 | 3 | 6 | Mitigate — credential rotation + scope | Platform Lead | 2026-04-10 | 2026-07-10 |
| R-VND-002 | Vendor | Splunk HEC token compromise: forged events injected to customer SIEM | Per-customer HEC token, Vault storage, rotation SLA 90d | 2 | 2 | 4 | Mitigate — rotation + replay nonce | Platform Lead | 2026-04-10 | 2026-07-10 |
| R-VND-003 | Vendor | Microsoft Sentinel DCR key compromise | Managed identity (Phase 3 K8s), token rotation | 2 | 2 | 4 | Mitigate — managed identity default | Platform Lead | 2026-04-10 | 2026-07-10 |
| R-VND-004 | Vendor | Cloud provider regional outage (Phase 3 SaaS) | Multi-AZ, cross-region backup, RTO 4h | 2 | 3 | 6 | Transfer — SLA + Mitigate DR plan | DevOps Lead | 2026-04-10 | 2026-07-10 |
| R-VND-005 | Vendor | Upstream OSS dependency compromise (supply chain: Keycloak/Vault/MinIO image) | Cosign verification, SBOM scan (Trivy), pinned digests | 2 | 3 | 6 | Mitigate — admission controller | Security Lead | 2026-04-10 | 2026-07-10 |
| R-VND-006 | Vendor | Certificate authority (Let's Encrypt / public CA) issuance disruption for portal domains | Multi-CA fallback, staging renewal, monitoring | 1 | 2 | 2 | Accept — low tier portal | DevOps Lead | 2026-04-10 | 2026-07-10 |

### 5.6 Operational / People

| ID | Kategori | Açıklama | Mevcut kontroller | L | I | L×I | Treatment | Sahip | Son RVW | Sonraki |
|---|---|---|---|---|---|---|---|---|---|---|
| R-OPS-001 | Operational | Single-person key ceremony bus factor (Vault unseal) | Shamir 3-of-5, dokümante edilmiş key-holder listesi | 2 | 4 | 8 | Mitigate — 5 key holder + yearly drill | Security Lead | 2026-04-10 | 2026-07-10 |
| R-OPS-002 | Operational | Insider threat — compliance officer privilege abuse | Dual-control, audit chain, quarterly access review, DPO cross-sign | 1 | 4 | 4 | Mitigate — separation of duties | CEO | 2026-04-10 | 2026-07-10 |
| R-OPS-003 | Operational | Security training lapse (new hire missed KVKK training) | Onboarding checklist, LMS tracking, manager sign-off | 3 | 2 | 6 | Mitigate — automation via HRIS | HR | 2026-04-10 | 2026-07-10 |

## 6. Risk acceptance log (Kabul kayıtları)

> Medium ve üzeri her kabul edilen kalan risk burada CO imzasıyla kayıt edilir. Şu anda: boş.

## 7. Related documents

- ADR 0023 — SOC 2 Type II control framework (CC3.1 referans)
- ADR 0024 — ISO 27001 + 27701 (ISO 27005 metodoloji sabitlemesi)
- `docs/compliance/kvkk-framework.md` §13 (gap listesi)
- `docs/compliance/hukuki-riskler-ve-azaltimlar.md` (13 hukuki risk — bu register ile cross-reference)
- `docs/security/threat-model.md`
- `docs/policies/incident-response.md` §8
- `docs/policies/business-continuity-disaster-recovery.md`

## 8. Approval / İmza bloğu

| Rol | Ad | İmza | Tarih |
|---|---|---|---|
| CEO | _______________ | _______________ | ____________ |
| CTO | _______________ | _______________ | ____________ |
| DPO | _______________ | _______________ | ____________ |
| Compliance Officer | _______________ | _______________ | ____________ |

---

## English Summary (for external auditors)

**Purpose**: This register applies ISO/IEC 27005:2022 to identify, analyse, evaluate, and treat information security and privacy risks relevant to the Personel platform. It uses a 4×4 likelihood-impact matrix and the four ISO 27005 treatment options (mitigate / accept / transfer / avoid). The register contains 27 initial entries spanning agent/endpoint, gateway/API, data tier, compliance, vendor, and operational categories, drawn from the existing threat model, KVKK risk register, and Phase 1–2 engineering reality checks. Review cadence is quarterly minimum, with event-triggered updates for significant changes. Critical and high residual risks require CEO/CTO sign-off per the acceptance criteria in §2.5. Integration with the incident response policy (§4) ensures every high-severity incident is mapped back to a register entry; new entries are opened when an incident does not map to an existing risk. The document is owned jointly by the Compliance Officer and CISO and is subject to annual full reassessment per ISO 27005.
```

---
