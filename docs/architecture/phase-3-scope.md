# Faz 3 Kapsamı — Personel Platformu (SaaS + Sertifikasyon + GDPR)

> Language: Turkish executive summary + English technical sections.
> Status: PLANNING — version 0.1 (2026-04-10).
> Supersedes: nothing. Extends `docs/architecture/phase-2-scope.md`.
> Duration: 52 weeks (Phase 2'nin 32 haftasından belirgin olarak uzun — açık gerekçeleri §B'de).

---

## Bölüm A — Yönetici Özeti (Türkçe)

### Faz 3'ün Stratejik Tezi

Faz 1 "Türkiye'de KVKK-native on-prem UAM" tezini ispat etti. Faz 2 bu tezi macOS/Linux, ML, OCR, UBA, HRIS, SIEM ve mobil admin ile genişletti. **Faz 3'ün görevi, ürünü tek bir jurisdiksiyondaki tek deployment modelinden çok-bölgeli, çok-modelli ve kurumsal alıcının satın alma formlarını geçebilen bir ürüne dönüştürmektir.** Bunun üç ayağı vardır:

1. **Dağıtım modeli genişlemesi**: On-prem TEK BLESSED seçenek olmaktan çıkar; yönetilen **SaaS** eş değerli ikinci seçenek olarak devreye alınır. On-prem kaybolmaz (ADR 0008 iptal edilmez, **tadil** edilir). İki model aynı kod tabanından çıkar, aynı API, aynı UI, aynı event taxonomy.
2. **Güvenilirlik kanıtları**: Kurumsal satınalmada "SOC 2 Type II raporunuz var mı?", "ISO 27001 sertifikanız var mı?" soruları bir çarpandır. Sertifikasyon olmadan Faz 3'ün pazarı **konuşulmayan** pazardır. Faz 3'te üç sertifikasyon paralel yürür: **SOC 2 Type II**, **ISO 27001**, **ISO 27701** (PIMS).
3. **Coğrafi genişleme**: AB pazarı hem müşteri sayısı hem ortalama sözleşme değeri bakımından TR pazarının büyütücüsüdür. GDPR uyumu teknik olarak KVKK framework'unun ~%80 örtüşen bir üst kümesidir; **kod değişikliği minimumdur**, doküman yükü ağırdır. SCC + AB bölge sabitlemesi ile sınır ötesi aktarım problemi teknik mimariyle ortadan kaldırılır.

**Faz 3 = SaaS + Sertifikasyon + GDPR + Billing + Minifilter driver + White-label**. Paralel workstream'ler.

### Faz 3 Dışında Kalanlar (Faz 4+'a Ertelenenler)

- AI copilot / LLM destekli admin workflow (LLM zaten Faz 2'de ML classifier için ON-PREM; copilot Faz 4).
- On-prem için Kubernetes opsiyonu (müşteri K8s isterse Faz 3.5'te Helm chart kullanabilir ama blessed on-prem yol Compose olarak kalır).
- Mobil çalışan uygulaması (admin mobil uygulamasının aksine) — şeffaflık portalının web-first tezi korunur.
- Linux/macOS endpoint için sürücü seviyesi DLP — kernel düzeyinde müdahale yalnız Windows minifilter için planlanır.

### Faz 3'ün Kümülatif Hukuki Etkisi

SaaS modelinde Personel firması **veri işleyen** sıfatı kazanır. Bu, `docs/compliance/kvkk-framework.md` §3.3 uyarınca beklenen geçiştir ve **KVKK m.12/2 veri işleyen sözleşmesi** (müşteri için DPA) ile **GDPR m.28 data processing agreement** eşzamanlı olarak devreye girer. Faz 3'ün hukuki çıktıları:

1. KVKK DPA şablonu (TR müşteri)
2. GDPR DPA şablonu (EU müşteri, SCC ek'li)
3. Lead Supervisory Authority atanması (AB içinde — bölge seçimiyle birlikte)
4. VERBİS kaydı güncellemesi (rol değişimi + yeni kategori eklenmez, sadece veri işleyen sıfatı eklenir)
5. Art. 30 records of processing (GDPR) — VERBİS makinesinden otomatize
6. DPIA şablonu SaaS senaryosuna amendment
7. ISO 27701 PIMS dokümantasyon paketi (KVKK/GDPR ile %90 örtüşür)

### Faz 3 Başarı Kriterleri (Yönetici Düzeyi)

1. SOC 2 Type II raporu başarıyla yayımlanır (qualified opinion VEYA unqualified — **qualified kabul edilemez**).
2. ISO 27001 ve ISO 27701 sertifikaları ilk çevrim denetiminden geçer.
3. En az 3 ödeme yapan SaaS müşterisi (en az 1'i AB, en az 1'i TR, bölge sabitlemesi ile).
4. SaaS admin API p99 latency < 100ms; multi-tenant izolasyon penetration test'inden geçer (tenant-cross kaçak yok).
5. Windows minifilter driver WHQL-imzalı olarak en az 1 pilot müşteriye deploy edilir.
6. On-prem yol kesinti yaşamaz: Faz 2 müşterileri Faz 3 sürümüne regresyonsuz yükseltilir.

---

## Section B — Phase 3 Scope Items (English technical)

Each IN item below lists: **(i)** business rationale + market context (TR/EU), **(ii)** dependencies on Phase 1–2, **(iii)** locked decisions that must be reopened with new ADRs, **(iv)** KVKK→GDPR gap analysis touchpoints, **(v)** effort sizing (S ≤ 2 dev-weeks, M ≤ 5, L ≤ 10, XL ≤ 20, 2XL > 20), **(vi)** blocking risks.

### B.1 — SaaS Deployment Path (Multi-Tenant Managed Service)

**Business rationale.** On-prem is a Turkish enterprise default but a hard sell outside the Turkish banking/public perimeter. Mid-market EU customers (200–2000 endpoints) do not want to run Vault, ClickHouse, or MinIO. They want a credit card, a login URL, and a DPA. SaaS opens a segment that on-prem cannot economically serve. TR market: adds SMB + startup tier; EU market: adds the mid-market entry door entirely.

**Dependencies on Phase 1–2.** Multi-tenant code paths already exist in Postgres RLS scaffolding (ADR 0006) and in the `tenants` table from Phase 1. Vault policy templates are tenant-scoped. ClickHouse tables already carry `tenant_id`. The heaviest missing piece is **tenant provisioning automation**: a SaaS signup cannot require a human operator to run PKI ceremony. Also missing: per-tenant billing metering, rate limiting by plan tier, and customer-facing DPA ingestion.

**Locked decisions reopened.** Decision 2 (on-prem first; K8s deferred) is **amended** by ADR 0020 (SaaS multi-tenant architecture) and ADR 0021 (K8s SaaS deployment). Decision 1 (jurisdiction Turkey only) is amended by ADR 0022 (GDPR expansion). No other locked decisions are touched.

**KVKK→GDPR gap analysis touchpoint.** The on-prem "Personel is not a processor" posture (`kvkk-framework.md` §3.1) **flips** for SaaS: Personel becomes a KVKK veri işleyen AND a GDPR Art. 28 processor. This is the single biggest legal inflection point of Phase 3.

**Effort.** **2XL** (25–30 dev-weeks across 3 developers plus DevOps and compliance).

**Blocking risks.** (a) Tenant isolation bug becomes a cross-tenant data leak — catastrophic for SOC 2 and GDPR. Mitigation: RLS + dedicated Vault namespaces + pen test gate before GA. (b) Data residency enforcement bugs — an EU customer's data landing in TR is a GDPR Art. 44 violation. Mitigation: region-pinning at signup, no cross-region read paths, pen-test for residency.

---

### B.2 — SOC 2 Type II Preparation

**Business rationale.** US-adjacent buyers (and increasingly EU enterprise buyers) gate on SOC 2 Type II in vendor review. A scaffolded Type I is an interim deliverable (point-in-time attestation) but the real currency is Type II (operating effectiveness over 6–12 months). Phase 3 starts the **12-month observation window on day 1** so that Type II can be issued by the end of Phase 3.

**Dependencies on Phase 1–2.** Hash-chained audit log (ADR 0014) already provides evidence substrate. Keycloak RBAC already maps to CC6.1 logical access. Vault audit logs map to CC6.7. What is missing: **evidence collection automation** (a SOC 2 locker that scrapes audit sources weekly into a WORM archive), **control narratives written by the compliance officer**, and **policy documents** (access review policy, change management policy, incident response policy, vendor management policy, BCP/DR policy) that currently do not exist as formal documents.

**Locked decisions reopened.** None directly; SOC 2 is additive.

**KVKK→GDPR gap analysis touchpoint.** Trust Services Criteria "Privacy" category overlaps heavily with GDPR/KVKK work; a single control narrative serves both.

**Effort.** **XL** (12–18 person-weeks, mostly compliance officer + DevOps).

**Blocking risks.** (a) Type II requires evidence across the entire observation window; if evidence collection starts late, the window slips by that amount. (b) A "qualified opinion" rather than "unqualified" is commercially unacceptable; **any control test failure discovered late must be remediated and re-tested**, which can slip Phase 3.6 by weeks.

---

### B.3 — ISO 27001 Certification

**Business rationale.** The European RFP default. TÜRKAK-accredited and EU-accredited certification bodies both serve the Turkish market; choosing a dual-accredited body gives both AB and TR market coverage with one certificate. Phase 3 targets ISO 27001:2022 (the revised standard), not the 2013 version.

**Dependencies on Phase 1–2.** Threat model and risk register already exist (`docs/security/`). What is missing: **ISMS scope statement**, **Statement of Applicability** (SoA) mapping all 93 Annex A:2022 controls, **management review records**, **internal audit program**, and **risk treatment plan**.

**Locked decisions reopened.** None directly.

**KVKK→GDPR gap analysis touchpoint.** Annex A.5.34 (privacy and protection of PII) bridges directly into ISO 27701 and GDPR Art. 32.

**Effort.** **L** (8–12 person-weeks, mostly compliance officer with security engineer support).

**Blocking risks.** Internal audit must be conducted by someone independent of the operational team; if the compliance officer also runs the ISMS, an independent auditor must be hired for the internal audit stage — this is often underestimated.

---

### B.4 — ISO 27701 (Privacy Extension)

**Business rationale.** PIMS (Privacy Information Management System) extension on top of ISO 27001; targets customers where GDPR/KVKK maturity is a differentiator. Adds ~20% audit work to ISO 27001 but shares 80% of controls.

**Dependencies on Phase 1–2.** KVKK framework doc (`docs/compliance/kvkk-framework.md`) and KVKK VERBİS machinery cover most of the ground. Missing: **PIMS scope statement**, **controller/processor role mapping** (matches the on-prem → SaaS flip in B.1), and **data subject request handling controls** (the KVKK DSR workflow already satisfies most of this).

**Locked decisions reopened.** None directly.

**KVKK→GDPR gap analysis touchpoint.** This is the overlap work. See `docs/compliance/gdpr-kvkk-gap-analysis.md`.

**Effort.** **M** (4–6 person-weeks, compliance officer, mostly documentation).

**Blocking risks.** Certification body must offer combined 27001+27701 audit; some do not, which would double the external audit cost. Addressed in ADR 0024.

---

### B.5 — GDPR Expansion (EU Market Entry)

**Business rationale.** The addressable EU UAM market is ~5× the TR market by endpoint count and ~10× by revenue. Personel's KVKK-native architecture is forward-compatible with GDPR. The missing work is **legal and documentation**, not code.

**Dependencies on Phase 1–2.** KVKK framework, DSR workflow, 30-day SLA, transparency portal, data retention matrix — **all forward-compatible**. The critical additions are: DPA template, SCC-backed processor clauses, Art. 30 records automation, DPIA updated for GDPR Art. 35 (mandatory in some UAM scenarios), and Lead Supervisory Authority designation.

**Locked decisions reopened.** Decision 1 (Turkey-only jurisdiction) is **amended** by ADR 0022.

**KVKK→GDPR gap analysis touchpoint.** Entire ADR 0022 and `gdpr-kvkk-gap-analysis.md` are dedicated to this.

**Effort.** **L** (6–10 person-weeks, mostly compliance officer + external EU counsel review).

**Blocking risks.** (a) DPO appointment (Art. 37) for Personel-as-processor: must be named, must be contactable, must be EU-reachable (mail drop acceptable). (b) EU customer breach reporting (Art. 33) must be 72h from **Personel's awareness**, which is often earlier than the customer's awareness — careful contractual language needed in DPA.

---

### B.6 — Windows Minifilter Driver (Forensic-Grade DLP)

**Business rationale.** Segment 1 competitors (Veriato, Teramind Enterprise) use minifilter drivers for file-level forensic visibility that user-mode ETW cannot reach. Absence of a minifilter caps Personel in high-end forensics RFPs. Deferred from Phase 2 per ADR 0010.

**Dependencies on Phase 1–2.** Windows agent IPC surface must accept a new IOCTL channel. Policy engine must gain a minifilter-aware rule class. Installer (MSI) must install and uninstall the driver with reboot orchestration.

**Locked decisions reopened.** Decision 3 (Windows agent user-mode only Phase 1–2) is **amended** by ADR 0025 (explicitly the Phase 3 elevation).

**KVKK→GDPR gap analysis touchpoint.** Kernel-mode instrumentation requires DPIA re-evaluation (m.6 özel nitelikli veri / GDPR Art. 35 risk). Driver introduces a new tamper surface that the threat model must cover.

**Effort.** **XL** (15–20 dev-weeks for one senior Windows/C engineer; WHQL signing adds ~4–8 week calendar delay independent of dev effort).

**Blocking risks.** (a) Driver BSOD in customer environment catastrophic; requires extensive chaos/stress testing. (b) WHQL signing timeline and Microsoft HLK lab requirements. (c) Deployment rollback story requires reboot — not graceful.

---

### B.7 — Billing / Subscription (Stripe + iyzico)

**Business rationale.** SaaS without billing is a favor, not a business. Stripe covers EU/global; iyzico is the TR market local equivalent (3D Secure, BKM Express, TR Lira). Dual billing integration is standard for a Turkish-born SaaS targeting EU.

**Dependencies on Phase 1–2.** `tenants` table already has a metadata JSON field. Missing: **billing service** (new bounded context), **usage metering** (endpoint-count based, sampled hourly from ClickHouse), **plan tier enforcement** (rate limit middleware already exists; add plan-aware quota).

**Locked decisions reopened.** None (new greenfield subsystem).

**KVKK→GDPR gap analysis touchpoint.** Billing data (company address, card reference, invoice) is personal data. Stripe/iyzico become sub-processors requiring disclosure in DPA.

**Effort.** **M** (4–6 dev-weeks for Go backend + admin console UI).

**Blocking risks.** iyzico webhook reliability historically weaker than Stripe; requires reconciliation job.

---

### B.8 — White-Label / Reseller Portal

**Business rationale.** TR enterprise sales go through VARs (value-added resellers) in banking, public sector, and defense. A reseller portal lets VARs provision tenants under their own branding, manage commissions, and handle tier-1 support.

**Dependencies on Phase 1–2.** Requires SaaS multi-tenant to be live (B.1). Requires billing (B.7) for commission tracking. Requires tenant theming in console (new).

**Locked decisions reopened.** None.

**KVKK→GDPR gap analysis touchpoint.** VAR becomes a sub-processor OR a separate controller depending on contract; must be explicit.

**Effort.** **M** (5–8 dev-weeks).

**Blocking risks.** Tenant theming risks brand confusion if not carefully partitioned; legal review required for VAR DPAs.

---

### B.9 — Sector Benchmarks (Anonymized Pooled Data)

**Business rationale.** Admins routinely ask "how does my fleet compare to other firms in my sector?". Pooled anonymized benchmarks (e.g., "banking sector median off-hours activity is 2.3%") are a high-value retention lever. Strictly opt-in at the SaaS tenant level.

**Dependencies on Phase 1–2.** UBA feature pipeline (Phase 2.6) provides the numerators. Anonymization pipeline is new.

**Locked decisions reopened.** None.

**KVKK→GDPR gap analysis touchpoint.** Pooled benchmark data must be **irreversibly anonymized** (not pseudonymized) to escape KVKK/GDPR. K-anonymity ≥ 5 with differential privacy noise is the target. Opt-in must be tenant-level and withdrawable.

**Effort.** **M** (4–6 dev-weeks, heavy on ML/anonymization engineering).

**Blocking risks.** K-anonymity insufficient for small sectors; differential privacy noise may render benchmarks statistically useless for small tenants.

---

### B.10 — Webhook / Public API Marketplace

**Business rationale.** Integrations ecosystem is a moat. The Phase 2 SIEM framework already exposes an outbound webhook-class event taxonomy. Phase 3 formalizes this as a **public API with OAuth2 client credentials**, an **integration registry**, and a **marketplace page** for third-party integrations.

**Dependencies on Phase 1–2.** Phase 2 SIEM framework, HRIS framework, and admin API OpenAPI 3.1 contract provide the substrate. Missing: OAuth2 client credentials flow, rate limiting for external OAuth clients (different from internal session RBAC), developer portal, and marketplace review workflow.

**Locked decisions reopened.** None.

**KVKK→GDPR gap analysis touchpoint.** Third-party integrations become sub-processors if they receive personal data; must be disclosed in DPA and optionally gated by customer.

**Effort.** **L** (8–12 dev-weeks).

**Blocking risks.** Public API abuse (credential leakage, rate-limit evasion); requires dedicated public-API gateway (distinct from admin API) and WAF.

---

## Section C — Cumulative Effort Summary

| Item | Size | Person-Weeks |
|---|---|---|
| B.1 SaaS multi-tenant | 2XL | 25–30 |
| B.2 SOC 2 Type II | XL | 12–18 |
| B.3 ISO 27001 | L | 8–12 |
| B.4 ISO 27701 | M | 4–6 |
| B.5 GDPR expansion | L | 6–10 |
| B.6 Windows minifilter | XL | 15–20 |
| B.7 Billing | M | 4–6 |
| B.8 White-label portal | M | 5–8 |
| B.9 Sector benchmarks | M | 4–6 |
| B.10 Webhook marketplace | L | 8–12 |
| **Total (upper bound)** | | **91–128 person-weeks** |

With 6 devs + 2 QA + 1 compliance + 1 DevOps (9 FTEs) over 52 weeks = **468 available person-weeks**, gross capacity is sufficient but contingency, holiday, support, and Phase 2 debt absorption consume ~40% (see `phase-3-roadmap.md`).

---

## Section D — Exit from Phase 3

Machine-readable exit criteria in `docs/architecture/phase-3-exit-criteria.md`.

## Section E — Cross-References

- ADR 0020 — SaaS multi-tenant architecture (amends ADR 0008)
- ADR 0021 — Kubernetes SaaS deployment
- ADR 0022 — GDPR expansion (amends Decision 1)
- ADR 0023 — SOC 2 Type II controls
- ADR 0024 — ISO 27001 + ISO 27701 combined plan
- ADR 0025 — Windows minifilter driver (amends ADR 0010)
- `docs/compliance/gdpr-kvkk-gap-analysis.md`
- `docs/architecture/phase-3-roadmap.md`
- `docs/architecture/phase-3-exit-criteria.md`
