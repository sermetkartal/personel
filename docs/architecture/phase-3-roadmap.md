# Faz 3 Yol Haritası — 52 Haftalık Plan

> Language: Turkish headings with English scheduling details.
> Status: PLANNING — version 0.1 (2026-04-10).
> Scope: Phase 3 — SaaS + SOC 2 + ISO 27001/27701 + GDPR + Windows minifilter.
> Team: **6 software engineers + 2 QA engineers + 1 compliance officer + 1 DevOps engineer** (total 10 FTE).
> Duration: 52 weeks (Phase 2 was 32; Phase 3's length is driven by the SOC 2 12-month observation window and dual-audit calendar, not pure engineering effort).

## Takım Atamaları (Team Assignments)

| Role | Placeholder | Primary focus |
|---|---|---|
| Senior Go engineer | Dev-A | SaaS multi-tenant backend (provisioning, billing plumbing, region pinning) |
| Senior Go engineer | Dev-B | Admin API multi-tenant hardening, Art. 30 / evidence exporters, webhook marketplace |
| Senior DevOps + Go | Dev-C (overlaps with DevOps) | Helm chart, ArgoCD, Linkerd, operators; supports SRE runbooks |
| Senior Windows/C engineer | Dev-D | Windows minifilter driver (Phase 3.7 workstream, some prep earlier) |
| Senior frontend engineer | Dev-E | Console multi-tenant UI, billing UI, white-label portal, developer portal |
| Full-stack engineer | Dev-F | Sector benchmarks, ML anonymization, OAuth2 public API |
| QA engineer | QA-1 | SaaS integration, multi-tenant isolation tests, pen test coordination |
| QA engineer | QA-2 | Driver stability (Phase 3.7), regression across on-prem and SaaS |
| DevOps engineer | DevOps | Cluster operations, cert rotation, incident response, backup drills, evidence automation |
| Compliance officer | CO | SOC 2, ISO 27001/27701, GDPR documentation, auditor liaison, DPIA reviews |

**Capacity math**:
- 10 FTE × 52 weeks = 520 FTE-weeks gross
- Holidays/sick (10%): -52
- Support/on-call/incident (15%): -78
- Phase 2 tail debt absorption (5%): -26
- **Net ~364 FTE-weeks available for Phase 3 project work**
- Phase 3 scope upper-bound estimate: ~91–128 person-weeks of pure engineering (`phase-3-scope.md` §C) + ~50 person-weeks compliance + ~50 person-weeks DevOps platform work ≈ **190–230 person-weeks total**
- Slack: ~130–170 FTE-weeks, which is healthy for a 52-week plan but not luxurious. Scope cuts (Phase 3.7 driver slip; webhook marketplace slip) are pre-planned contingency.

## High-Level Sequence

```
Weeks 1–4:    3.0  Planning, auditor/cert body selection, SaaS region selection, onboarding
Weeks 5–12:   3.1  SaaS multi-tenant architecture implementation
Weeks 13–20:  3.2  K8s deployment + first managed pilot
Weeks 21–28:  3.3  SOC 2 evidence collection + GDPR documentation
Weeks 29–36:  3.4  ISO 27001 internal audit + remediation + billing + white-label
Weeks 37–44:  3.5  External SOC 2 + ISO audits; sector benchmarks; webhook marketplace
Weeks 45–48:  3.6  Certification issuance + SaaS GA
Weeks 49–52:  3.7  Windows minifilter driver (parallel workstream from week 21; intensive finish)
```

Phase 3.7 (minifilter) runs **in parallel** with other phases from week 21 onward; the 49–52 window is the intensive finish and pilot deployment.

---

## Phase 3.0 — Planning & Procurement (Weeks 1–4)

**Goal**: Clean starting line. Auditor selected, certification body selected, SaaS regions selected, team onboarded, SOC 2 evidence locker running day 1.

| Week | Dev-A | Dev-B | Dev-C | Dev-D | Dev-E | Dev-F | QA-1 | QA-2 | DevOps | CO |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | Phase 2 tail | Phase 2 tail | Helm chart scaffolding | Win SDK setup, WDK training | Console multi-tenant design | Benchmark scope doc | SaaS test plan | Phase 2 tail | Evidence locker kickoff | Auditor RFP draft |
| 2 | Multi-tenant gap analysis | Multi-tenant gap analysis | Argo CD / Linkerd POC | WDK basics + test-signing lab | Billing UX wireframes | Anonymization math review | Pen test vendor shortlist | Phase 2 tail | TR region provider eval | Cert body RFP draft |
| 3 | Tenant provisioning design doc | Art. 30 exporter design | TR/EU region decision docs | C1 driver skeleton | White-label design | Marketplace spec | Isolation test plan v1 | Driver test plan v0 | EU region provider eval | Policies draft (5 core) |
| 4 | Provisioning pipeline spec | DPA template draft | Helm chart v0.1 | Driver skeleton compiles | BFF/console multi-tenant spec | OAuth2 public API design | Test fixtures | Test fixtures | Evidence locker MVP running | Auditor + cert body selected |

**Exit from 3.0**: auditor + cert body selected; SaaS region providers selected; evidence locker recording day 1; team onboarded; core policy documents drafted.

---

## Phase 3.1 — SaaS Multi-Tenant Implementation (Weeks 5–12)

**Goal**: Multi-tenant backend runs end-to-end in staging with two synthetic tenants pinned to different regions.

Key deliverables:
- Tenant provisioning pipeline (Dev-A) — atomic, idempotent, fail-closed.
- Vault namespace-per-tenant automation (Dev-A + DevOps).
- Admin API tenant context middleware and RLS enforcement end-to-end (Dev-B).
- Region-aware ClickHouse partitioning (Dev-B).
- Bucket-per-tenant MinIO with IAM (Dev-A + DevOps).
- Keycloak realm-per-tenant automation (Dev-A).
- Isolation test suite (QA-1) — RLS fuzz, bucket cross-access, Vault policy deny tests.
- Console multi-tenant UI (Dev-E): tenant switcher, billing placeholder, plan tier badges.

QA-1 develops the isolation test suite in parallel; **it is a blocker for Phase 3.2 exit**.

---

## Phase 3.2 — K8s Deployment + First Managed Pilot (Weeks 13–20)

**Goal**: Helm chart deploys cleanly to both regions; first design-partner customer onboarded as a managed pilot.

Key deliverables:
- Helm chart v1.0 (all subcharts) — Dev-C + DevOps.
- Argo CD app-of-apps in both regions — DevOps.
- Linkerd mTLS everywhere, cert-manager, External Secrets Operator wired up.
- CloudNativePG, Altinity ClickHouse, Banzai Vault, MinIO operators configured.
- Velero + Restic backup running weekly + restore drill passed.
- Admin API passes full integration test against the K8s stack.
- First design-partner pilot provisioned, one tenant, real traffic at low volume.
- Observability dashboards live (Prometheus + Grafana + Loki + Tempo).

**Windows minifilter (Dev-D)** begins parallel workstream at week 13: driver skeleton → PreCreate/PreWrite callbacks → first HLK-test-ready build by week 20.

**Exit from 3.2**: design partner pilot stable for 2 weeks; isolation pen test passes (contracted external vendor); SOC 2 evidence locker has 20 weeks of evidence.

---

## Phase 3.3 — SOC 2 Evidence + GDPR Docs + Billing (Weeks 21–28)

**Goal**: SOC 2 readiness review passes; GDPR documentation complete; billing live.

Key deliverables:
- SOC 2 control narratives (CO): all controls from ADR 0023 documented.
- SoA v1 (CO) for ISO 27001.
- GDPR DPA template finalized + reviewed by external EU counsel (CO).
- `gdpr-kvkk-gap-analysis.md` finalized (CO).
- Art. 30 records exporter (Dev-B).
- DPIA amendment for SaaS mode (CO).
- Stripe billing integration (Dev-A).
- iyzico billing integration (Dev-A).
- Plan-tier quota enforcement (Dev-B middleware).
- Usage metering ClickHouse job (Dev-B).
- Console billing UI (Dev-E).
- External readiness assessment (external consultant) — output: gap list for 3.4 remediation.

**Minifilter (Dev-D)**: first HLK lab submission; debug + resubmit loop begins.

---

## Phase 3.4 — ISO Internal Audit + Remediation + White-Label + Benchmarks (Weeks 29–36)

**Goal**: Internal audit complete, remediation items closed; white-label portal ready; first benchmark export available.

Key deliverables:
- Internal audit by independent consultant (CO + external).
- Remediation backlog closed (all teams, triaged by CO).
- White-label / reseller portal (Dev-E + Dev-A) — tenant provisioning under VAR branding, commission tracking, VAR-scoped admin views.
- Sector benchmarks pipeline (Dev-F) — anonymization + k-anonymity ≥ 5 + differential privacy noise + opt-in toggle + first export.
- Management review meeting #2 conducted and minuted (CO).
- First customer-facing DPA signed with an EU customer (sales + CO).
- BCP/DR drill (DevOps + CO).

**Minifilter (Dev-D)**: driver passes HLK lab tests; MSI installer integration begins.

---

## Phase 3.5 — External Audits (Weeks 37–44)

**Goal**: SOC 2 Type II fieldwork complete; ISO 27001+27701 Stage 1 + Stage 2 audits complete; webhook marketplace ready.

Key deliverables:
- SOC 2 Type II auditor fieldwork (weeks 37–42).
- ISO 27001+27701 Stage 1 audit (week 38).
- ISO 27001+27701 Stage 2 audit (week 42).
- Any findings from audits are remediated in real time.
- Webhook / public API marketplace (Dev-F + Dev-E + Dev-B).
- OAuth2 client credentials flow for third-party integrations.
- Developer portal (Dev-E).
- Sub-processor registry page published.

**Minifilter (Dev-D + QA-2)**: WHQL submission; pilot install on test customers.

---

## Phase 3.6 — Certification + SaaS GA (Weeks 45–48)

**Goal**: SOC 2 Type II report issued; ISO 27001+27701 certificates granted; SaaS goes GA.

Key deliverables:
- SOC 2 Type II report received (unqualified).
- ISO 27001 + ISO 27701 certificates received.
- SaaS GA announcement (marketing).
- Status page live.
- 24/7 on-call rotation formalized.
- Post-mortem process documented and drilled.
- Customer onboarding runbook finalized.
- Minimum 3 paying SaaS customers live (at least 1 EU + 1 TR).

---

## Phase 3.7 — Windows Minifilter Driver Finish (Weeks 49–52)

**Goal**: WHQL-signed driver deployed to at least one pilot customer; soak-tested and stable.

Key deliverables (Dev-D lead, QA-2 support):
- WHQL-attested driver delivered.
- MSI installer with driver install option tested on Win10/Win11/Server 2022 matrix.
- 72h soak test clean.
- Pilot customer install + 2-week stability report.
- Runbook for driver support issues.
- Rollback procedure drilled on test fleet.

**Parallel from week 21**: this is the intensive finish. The preceding 28 weeks of driver work (weeks 21–48) proceed at Dev-D's single-FTE pace with WDK/WHQL calendar delays absorbed.

---

## Schedule Risks

| Risk | Impact | Mitigation |
|---|---|---|
| SOC 2 observation window slip | Pushes Type II report beyond week 52 | Start evidence locker on week 1; contract readiness assessor by week 8 |
| Pen test finds critical isolation bug | Blocks Phase 3.2 exit | QA-1 dedicated from week 5; pen test in week 18 gives 2 weeks remediation window |
| WHQL signing delays | Minifilter pilot slips past week 52 | Start HLK early (week 20); accept Phase 3.7 can overflow into Phase 4 week 1–4 without blocking Phase 3.6 |
| Cert body auditor unavailable for combined 27001+27701 | ISO cert delayed | Phase 3.0 cert body selection includes availability check |
| Billing integration regulatory delays (iyzico) | SaaS launch blocked in TR | Stripe-only first EU launch, iyzico follows within 3 weeks |
| Phase 2 tail debt (real API impls) not closed | Team capacity underrun | Weeks 1–2 reserved for Phase 2 closure; CO defers non-blocking Phase 2 items to Phase 4 |

## Dependency Graph

- 3.0 → 3.1 (provisioning design)
- 3.1 → 3.2 (isolation tests block K8s deploy)
- 3.2 → 3.3 (evidence collection needs deployed stack)
- 3.3 → 3.4 (readiness assessment informs internal audit)
- 3.4 → 3.5 (internal audit findings remediated before external)
- 3.5 → 3.6 (audit reports needed for certification)
- 3.7 runs in parallel, feeds into 3.6 exit criteria (minifilter pilot install).

## Cross-references

- `docs/architecture/phase-3-scope.md` — scope definition
- `docs/architecture/phase-3-exit-criteria.md` — machine-readable exit gates
- ADRs 0020, 0021, 0022, 0023, 0024, 0025
