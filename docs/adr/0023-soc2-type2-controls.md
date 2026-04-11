# ADR 0023 — SOC 2 Type II Control Framework

**Status**: Accepted (Phase 3)
**Related**: ADR 0020, ADR 0021, ADR 0024 (ISO 27001 overlaps)

## Context

SOC 2 Type II is the most frequently demanded security attestation for US-adjacent enterprise buyers and is increasingly referenced by EU buyers. Unlike ISO 27001 (which certifies an ISMS), SOC 2 is an **attestation** by an independent CPA firm that a service organization's controls met their stated criteria **over a period of time** (Type II = operating effectiveness, typically 6 or 12 months; Type I = point-in-time design).

Constraints:

- Phase 3 is 52 weeks. A 12-month observation window must start on **day 1** of Phase 3.0 to produce a Type II report before Phase 3 GA. A 6-month window is a fallback with commercial concessions.
- Personel cannot select named auditors in this document; selection is captured elsewhere in Phase 3.0.
- Evidence collection must be automated as far as practical; manual evidence is acceptable but must be re-collected every observation period.

## Decision

### Trust Services Criteria (TSC) in scope

The Phase 3 SOC 2 engagement covers the following TSC categories:

1. **Security (CC)** — mandatory (Common Criteria, CC1.0–CC9.0).
2. **Availability (A)** — included; the product is operational-critical for customers.
3. **Confidentiality (C)** — included; customer data is explicitly confidential.
4. **Processing Integrity (PI)** — included; event ingestion must be complete and accurate for the audit chain to hold legal weight.
5. **Privacy (P)** — included; overlaps heavily with KVKK/GDPR work and ISO 27701.

All five TSCs are in scope. This is more demanding than a "Security-only" SOC 2 but is a more valuable report and maps cleanly to ISO 27001+27701 overlap.

### Control-to-service mapping (abbreviated, full matrix in Phase 3.3 deliverable)

| Criterion | Control statement | Implementation |
|---|---|---|
| CC1.1 | Commitment to integrity and ethical values | Code of conduct policy (new), signed by all employees |
| CC2.1 | Information and communication — internal | Company handbook, Slack #incidents, quarterly all-hands |
| CC3.1 | Risk identification | Risk register (`docs/security/risk-register.md` — to be formalized), updated quarterly |
| CC4.1 | Monitoring — ongoing evaluations | Internal audit program, quarterly control testing |
| CC5.1 | Control activities — selection and development | Control owner matrix, control change procedure |
| **CC6.1** | **Logical access security — authentication** | **Keycloak OIDC + RBAC (7 roles); MFA enforced for all admin and operator access** |
| CC6.2 | Logical access security — provisioning and de-provisioning | HRIS-driven user lifecycle (Phase 2.5); access review quarterly |
| CC6.3 | Logical access security — authorization based on role | RBAC in `apps/api/internal/httpserver/middleware/rbac.go`, audit logged |
| CC6.6 | Transmission and disposal of confidential information | mTLS everywhere (ADR 0021 Linkerd); encrypted-at-rest for all stores; retention matrix |
| CC6.7 | Restriction and monitoring of access to system assets | Vault audit logs + Keycloak session logs + hash-chained admin audit log |
| CC6.8 | Prevention and detection of unauthorized or malicious software | Image signing (cosign) + admission controller + SBOM scanning (Trivy) in CI |
| CC7.1 | Detection of changes and security events | Prometheus alerts + Loki log alerts + Falco runtime detection |
| CC7.2 | Security event monitoring | SIEM integration (Phase 2.7) + internal alerting |
| CC7.3 | Security incident response | Incident response runbook; 24/7 on-call rotation |
| CC7.4 | Recovery from security incidents | DR runbook; Velero backup restore drill quarterly |
| CC7.5 | Communication of security events | Status page + customer email + in-app banner for incidents |
| CC8.1 | Change management | Argo CD GitOps + GitHub branch protection + required reviewers |
| CC9.1 | Risk mitigation for business disruption | BCP/DR plan with RTO 4h / RPO 15min |
| CC9.2 | Vendor and business partner risk management | Sub-processor DPA + vendor review matrix |
| A1.1 | Availability — capacity planning | Prometheus capacity alerts + quarterly capacity review |
| A1.2 | Availability — environmental protection | Cloud provider SOC 2 inheritance (regional DC) |
| A1.3 | Availability — recovery | Velero + WAL + cross-region backup |
| C1.1 | Confidentiality — identification of confidential information | Data classification policy (new); retention matrix (existing) |
| C1.2 | Confidentiality — disposal | 6-month destruction report (existing) + MinIO lifecycle + ClickHouse TTL |
| PI1.1 | Processing integrity — quality of inputs | Input validation + schema enforcement at gateway (proto validation) |
| PI1.2 | Processing integrity — completeness and accuracy | Event hash chain + receipt acknowledgment from agent queue |
| PI1.3 | Processing integrity — timeliness | SLA alerts on ingest lag |
| PI1.4 | Processing integrity — authorization | Policy signing (Ed25519) + agent policy verification |
| PI1.5 | Processing integrity — corrections | Audit trail of corrections; no silent edits |
| P1.1 | Privacy — notice and communication | KVKK aydınlatma + GDPR notice in transparency portal |
| P2.1 | Privacy — choice and consent | DSR workflow + retention policy + portal |
| P3.1 | Privacy — collection | Purpose-limited collection per retention matrix |
| P4.1 | Privacy — use, retention, disposal | Retention matrix + 6-month destruction |
| P5.1 | Privacy — access | DSR workflow (KVKK m.11 / GDPR Art. 15) |
| P6.1 | Privacy — disclosure to third parties | Sub-processor registry |
| P7.1 | Privacy — quality | Data subject correction right |
| P8.1 | Privacy — monitoring and enforcement | DPO oversight + quarterly privacy review |

Full control narrative documents are produced in Phase 3.3. Each control has:
- Owner (named role, not person)
- Control statement (what it does)
- Control frequency (continuous / daily / weekly / quarterly)
- Evidence source (automated log / manual record / policy document)
- Test procedure (how the auditor will validate)

### Evidence collection automation

**SOC 2 evidence locker** architecture (new, Phase 3.3):

1. A scheduled job collects evidence from:
   - Keycloak audit events → access provisioning/de-provisioning evidence
   - Vault audit logs → secret access evidence
   - Hash-chained admin audit log → privileged action evidence
   - Argo CD audit → change management evidence
   - Velero backup reports → backup evidence
   - Prometheus alert history → incident detection evidence
   - Internal ticket system → incident response evidence
2. Evidence is written to a WORM S3 bucket (object lock, compliance mode, retention 7 years).
3. Each evidence artifact is checksummed and the checksum appended to the hash-chained audit (ADR 0014).
4. A quarterly evidence report is generated automatically in PDF, signed by the compliance officer's Yubikey.

### 12-month observation period plan

- **Day 1** (Phase 3.0 week 1): controls implemented at design level; evidence collection automation begins.
- **Month 3** (Phase 3.1 end): internal gap assessment by compliance officer.
- **Month 6** (Phase 3.3 end): readiness assessment with external readiness consultant (optional but recommended).
- **Month 9** (Phase 3.4 end): internal dry-run audit.
- **Month 12** (Phase 3.5): external auditor fieldwork begins.
- **Month 12 + 4–6 weeks**: Type II report issued (Phase 3.6).

If the start slips past Phase 3.0 week 1, the report slips commensurately. This is the **most schedule-sensitive Phase 3 workstream**.

### Auditor selection criteria (no named firms)

Selection rubric to be applied in Phase 3.0:

1. **AICPA membership and SOC 2 attestation practice** — mandatory.
2. **Cloud-native client portfolio** — the auditor must understand Kubernetes, service mesh, GitOps, and Vault. Firms whose client base is primarily on-prem/traditional are rejected.
3. **Peer review status** — firm must be in good standing per AICPA peer review.
4. **Readiness services availability** — can the firm (or a partner firm, to avoid independence conflict) help with readiness before the audit?
5. **Cost** — Big-4 firms price 3–5× boutique specialized firms; for a Phase 3 startup, a boutique specialist auditor is a more commercially reasonable fit unless a specific customer RFP requires Big-4.
6. **Geographic reach** — the auditor must be able to evaluate controls operating in both TR and EU regions without requiring travel scope renegotiation.

Decision to be captured in a Phase 3.0 procurement document.

### Cost estimate (independent of firm selection)

- Readiness assessment: $15k–$40k one-time.
- Type I (optional bridge): $20k–$50k.
- Type II (12-month observation): $60k–$150k (boutique) or $150k–$350k (Big-4).
- Internal evidence automation engineering: 6–10 person-weeks (included in Phase 3.3 effort estimate).
- Total first-year cost: **$80k–$200k for boutique; $200k–$400k for Big-4** (ranges; exact numbers captured in Phase 3.0 procurement).

## Consequences

### Positive

- Type II report is a hard gate-pass for most US enterprise deals.
- Control infrastructure overlaps with ISO 27001 (ADR 0024) and GDPR Art. 32 evidence.
- Evidence locker creates a reusable compliance substrate for future certifications.

### Negative

- Observation period is unforgiving of schedule slip. Any control failure discovered in Month 11 can push issuance by months.
- Big-4 vs boutique is a real commercial tradeoff; some customer RFPs specify Big-4.
- Manual controls (access review, risk assessment, vendor review) require sustained compliance-officer time and cannot be fully automated.

## Alternatives Considered

- **SOC 2 Type I only**: rejected — Type I is a point-in-time design attestation with limited commercial weight.
- **SOC 1** (financial reporting): rejected — Personel is not a financial reporting service.
- **Deferring SOC 2 to Phase 4**: rejected — EU market entry (Phase 3) benefits directly; deferring wastes the window.
- **Self-attestation framework (CAIQ / SIG-Lite only)**: acceptable as an interim while SOC 2 is in progress; does not replace SOC 2.

## Cross-references

- ADR 0024 — ISO 27001 overlaps (shared control substrate)
- `docs/architecture/phase-3-roadmap.md` — observation period pacing
- `docs/architecture/phase-3-exit-criteria.md` — SOC 2 issuance as exit gate
