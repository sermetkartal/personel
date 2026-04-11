# Faz 3 Exit Criteria — Machine-Readable

> Status: PLANNING — version 0.1 (2026-04-10).
> Format: each criterion has an id, a human statement, a verification method, and an owner. The table at the end is intended to be parseable by a gate-evaluation script (`apps/qa/ci/phase3_thresholds.yaml` — to be authored Phase 3.0).
> Exit gate: **all criteria must be MET** for Phase 3 closure and Phase 4 start.

---

## Certification Criteria

### P3-EX-01 — SOC 2 Type II report issued

- **Statement**: A SOC 2 Type II report covering TSC categories Security, Availability, Confidentiality, Processing Integrity, and Privacy has been issued by an AICPA-member CPA firm with an **unqualified opinion**.
- **Verification**: PDF report attached to compliance archive; hash recorded in hash-chained audit log.
- **Owner**: Compliance Officer.
- **Not-met condition**: any qualified opinion; any exception listed in the report.

### P3-EX-02 — ISO 27001:2022 certificate granted

- **Statement**: A valid ISO 27001:2022 certificate has been issued by an IAF-accredited certification body covering the ISMS scope defined in ADR 0024.
- **Verification**: certificate scan + certification body registry lookup.
- **Owner**: Compliance Officer.

### P3-EX-03 — ISO 27701:2019 certificate granted

- **Statement**: A valid ISO 27701 certificate has been issued as an extension of the ISO 27001 certificate.
- **Verification**: certificate scan + registry lookup.
- **Owner**: Compliance Officer.

---

## SaaS Criteria

### P3-EX-04 — Paying SaaS customers

- **Statement**: At least **3 paying SaaS customers** are live in production, with at least 1 tenant in the EU region and at least 1 tenant in the TR region.
- **Verification**: billing system query (Stripe + iyzico) for tenants in status=active with at least 1 paid invoice; cross-reference tenant region in tenants table.
- **Owner**: Head of Sales + CTO.

### P3-EX-05 — SaaS admin API latency SLO

- **Statement**: p99 latency for admin API endpoints is **< 100ms** over a rolling 14-day window, measured server-side at the ingress.
- **Verification**: Prometheus query `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{service="api"}[14d])) by (le))` < 0.1.
- **Owner**: DevOps.

### P3-EX-06 — Multi-tenant isolation penetration test passed

- **Statement**: An external penetration test targeting multi-tenant isolation (cross-tenant data reads, cross-tenant privilege escalation, cross-tenant cache poisoning, cross-tenant MinIO bucket access, cross-tenant Vault access) has been conducted by an independent firm and has issued a **"no tenant data leakage observed"** finding. Any critical or high-severity isolation findings must be remediated and re-tested with a clean result before this criterion is met.
- **Verification**: pen test report PDF + remediation evidence + re-test clean report.
- **Owner**: Security Engineering + Compliance Officer.

### P3-EX-07 — Region pinning enforced

- **Statement**: Automated test demonstrates that a tenant pinned to TR region has zero bytes of personal data in the EU region infrastructure (and vice versa). Tested via tenant-scoped data read probes in the non-home region that MUST fail.
- **Verification**: `apps/qa/test/security/region_pinning_test.go` (to be authored) green on both regions.
- **Owner**: QA-1.

---

## GDPR / Legal Criteria

### P3-EX-08 — GDPR DPA signed with EU customer

- **Statement**: At least 1 EU-region customer has signed the GDPR Art. 28 DPA (countersigned by Personel DPO).
- **Verification**: signed PDF in contract archive; hash recorded.
- **Owner**: Legal + Compliance Officer.

### P3-EX-09 — Art. 30 records exporter operational

- **Statement**: The Art. 30 records exporter runs on demand and produces a valid processor record for each EU tenant.
- **Verification**: `apps/api/internal/compliance/exporters/gdpr_art30_test.go` green; one real export attached to the compliance archive.
- **Owner**: Dev-B.

### P3-EX-10 — Sub-processor registry published

- **Statement**: A public sub-processor registry is available at a stable URL and enumerates every sub-processor used by Personel SaaS.
- **Verification**: HTTPS fetch + content hash recorded.
- **Owner**: Compliance Officer.

### P3-EX-11 — DPO appointed and contactable

- **Statement**: A named DPO (or equivalent privacy officer) is appointed, published on the legal page, and reachable by email and EU-addressable physical mail drop.
- **Verification**: public legal page crawl + test email round-trip.
- **Owner**: Compliance Officer.

### P3-EX-12 — KVKK framework unchanged for TR customers

- **Statement**: Existing on-prem TR customers continue to operate under the unamended KVKK framework (`kvkk-framework.md` not modified in substantive ways during Phase 3).
- **Verification**: doc diff review; no TR customer reports a regression in KVKK posture.
- **Owner**: Compliance Officer.

---

## Windows Minifilter Criteria

### P3-EX-13 — WHQL-signed minifilter driver delivered

- **Statement**: The `personel-flt.sys` driver is WHQL-attested via Microsoft dashboard and deployed to at least 1 pilot customer with a clean 2-week stability report (no BSOD, no driver verifier errors, no customer-reported stability issues).
- **Verification**: WHQL signature validation + pilot stability report + Windows Error Reporting clean for installed endpoints.
- **Owner**: Dev-D + QA-2.

### P3-EX-14 — Driver fallback posture verified

- **Statement**: When the driver is absent or fails to load, the user-mode agent degrades gracefully to user-mode-only operation without service interruption.
- **Verification**: chaos test `apps/qa/test/security/driver_fallback_test.go` green.
- **Owner**: QA-2.

---

## Operational Criteria

### P3-EX-15 — Evidence locker operational for full 12 months

- **Statement**: The SOC 2 evidence locker has been operational for at least 12 consecutive months with weekly evidence collection runs, no gaps > 7 days, and WORM bucket integrity verified.
- **Verification**: evidence locker metadata query + WORM bucket object listing.
- **Owner**: DevOps + Compliance Officer.

### P3-EX-16 — Backup + restore drill passed (SaaS)

- **Statement**: A full backup + restore drill has been executed end-to-end in a non-production environment for both regions within the last 90 days, meeting RTO 4h and RPO 15min for Postgres, RPO 1h for ClickHouse, RPO 5min for Vault.
- **Verification**: drill report signed by DevOps + SRE lead.
- **Owner**: DevOps.

### P3-EX-17 — 24/7 on-call rotation live

- **Statement**: A formal 24/7 on-call rotation is in place with documented handoff, escalation policy, and pager coverage for both regions.
- **Verification**: on-call schedule export + one test page executed and acknowledged.
- **Owner**: DevOps.

### P3-EX-18 — Phase 2 tail debt closed

- **Statement**: All Phase 2 "real API" items flagged as blocking in `CLAUDE.md` (BambooHR real calls, Logo Tiger real calls, Splunk HEC real publishing, Sentinel DCR real publishing, Llama GGUF inference, Tesseract OCR real extraction, UBA ClickHouse real features, Live view WebM chunking) are either implemented OR explicitly deferred to Phase 4 by decision record.
- **Verification**: checklist in `docs/architecture/phase-2-exit-criteria.md` reviewed; all items either DONE or DEFERRED with link to Phase 4 scope.
- **Owner**: CTO.

### P3-EX-19 — On-prem customers unharmed

- **Statement**: Zero on-prem customer experiences a Phase 3-induced regression. Phase 3 release is backwards-compatible with Phase 2 on-prem installs.
- **Verification**: regression test suite green on on-prem stack; customer support tickets classified; no P0/P1 from on-prem customers tracing to Phase 3 changes.
- **Owner**: QA-2 + Support.

### P3-EX-20 — Documentation complete

- **Statement**: All Phase 3 ADRs (0020–0025), framework docs, runbooks, and compliance artifacts are merged and cross-referenced. No TODO-marked sections remain.
- **Verification**: grep `docs/` for "TODO" / "FIXME" / "TBD"; zero hits in Phase 3 authored files.
- **Owner**: CTO + Compliance Officer.

---

## Gate Table (machine-readable summary)

| id | category | criterion | owner | blocking? |
|---|---|---|---|---|
| P3-EX-01 | certification | SOC 2 Type II unqualified | CO | yes |
| P3-EX-02 | certification | ISO 27001 certificate | CO | yes |
| P3-EX-03 | certification | ISO 27701 certificate | CO | yes |
| P3-EX-04 | saas | 3+ paying customers (≥1 EU, ≥1 TR) | Sales+CTO | yes |
| P3-EX-05 | saas | p99 admin API < 100ms (14d) | DevOps | yes |
| P3-EX-06 | saas | isolation pen test passed | SecEng+CO | yes |
| P3-EX-07 | saas | region pinning enforced | QA-1 | yes |
| P3-EX-08 | gdpr | EU customer DPA signed | Legal+CO | yes |
| P3-EX-09 | gdpr | Art. 30 exporter operational | Dev-B | yes |
| P3-EX-10 | gdpr | sub-processor registry published | CO | yes |
| P3-EX-11 | gdpr | DPO appointed and contactable | CO | yes |
| P3-EX-12 | kvkk | TR KVKK framework unchanged | CO | yes |
| P3-EX-13 | driver | WHQL minifilter pilot stable 2w | Dev-D+QA-2 | yes |
| P3-EX-14 | driver | user-mode fallback verified | QA-2 | yes |
| P3-EX-15 | ops | evidence locker 12 months | DevOps+CO | yes |
| P3-EX-16 | ops | backup/restore drill < 90d | DevOps | yes |
| P3-EX-17 | ops | 24/7 on-call live | DevOps | yes |
| P3-EX-18 | tech debt | Phase 2 tail closed or deferred | CTO | yes |
| P3-EX-19 | regression | on-prem customers unharmed | QA-2+Support | yes |
| P3-EX-20 | docs | Phase 3 docs complete | CTO+CO | yes |
