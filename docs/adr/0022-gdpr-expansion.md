# ADR 0022 — GDPR Expansion (EU Market Entry)

**Status**: Accepted (Phase 3)
**Amends**: Locked Decision 1 (Jurisdiction: Turkey only) — the Turkish legal framework remains fully in force and unchanged; GDPR is **added** alongside KVKK as a second compliance target for EU customers.
**Related**: ADR 0020 (SaaS Multi-Tenant), ADR 0024 (ISO 27701), `docs/compliance/kvkk-framework.md`, `docs/compliance/gdpr-kvkk-gap-analysis.md`

## Context

Phase 3 opens Personel to the EU market. The KVKK framework in Phase 1 was deliberately written to be forward-compatible with GDPR (see `kvkk-framework.md` §3.3). Phase 3 must formalize the GDPR posture for:

1. EU-based customers on SaaS (Art. 28 processor obligations, DPA, sub-processor disclosure).
2. EU-resident data subjects (Art. 12–22 rights).
3. EU region infrastructure (ADR 0021) for data residency.
4. Cross-border transfer analysis (Art. 44 et seq.) — including the TR↔EU axis where Turkey has no adequacy decision.

Key EU legal concepts that do not have direct KVKK equivalents:

- **Lead Supervisory Authority** (Art. 56) for cross-border establishment.
- **One-Stop-Shop** mechanism.
- **DPO mandatory appointment** (Art. 37) for large-scale systematic monitoring.
- **Mandatory DPIA** (Art. 35) for systematic monitoring of data subjects.
- **72-hour breach notification to supervisory authority** (Art. 33) and affected subjects (Art. 34).
- **Standard Contractual Clauses (SCC)** for any third-country transfer.

## Decision

### 1. Data Processing Agreement (DPA) template for EU customers

A GDPR Art. 28 compliant DPA is drafted as `docs/compliance/gdpr-dpa-template.md` (to be authored in Phase 3.3). It includes:

- Subject matter and duration of processing
- Nature and purpose of processing
- Types of personal data and categories of data subjects
- Obligations and rights of the controller
- Processor obligations (Art. 28(3)(a)–(h))
- Sub-processor list with flow-down DPA clause
- International transfer mechanism — **none needed** for EU-region tenants because data never leaves the EU region (see §4 below)
- Audit rights and cooperation clauses
- Breach notification clause with 72h sub-clock to controller

Turkish customers continue to sign the KVKK m.12/2 veri işleyen sözleşmesi (the SaaS variant — see `kvkk-framework.md` §3.3).

### 2. Article 30 records automation

Reuse the KVKK VERBİS machinery that already tracks processing activities. Extend the data model to emit both a VERBİS-compatible export and an Art. 30 (GDPR) records export. A single internal "processing activity" record maps to both. New export format in `apps/api/internal/compliance/exporters/gdpr_art30.go` (Phase 3.3).

The Art. 30 record includes (per Art. 30(2) for processors):

- Name and contact details of processor and controller
- Categories of processing carried out
- Transfers to third countries (for Personel SaaS: none, by region pinning)
- General description of security measures
- Where applicable, DPO contact

### 3. Article 28 processor obligations

In SaaS mode Personel is an Art. 28 processor. The processor obligations map to product features:

| Art. 28 obligation | Product feature |
|---|---|
| Process only on documented instructions | DPA clause + tenant-scoped config (no out-of-band processing) |
| Confidentiality of authorized personnel | Employment contracts + RBAC + access reviews (SOC 2 CC6.2) |
| Security measures (Art. 32) | ADR 0020 + ADR 0021 + threat model |
| Sub-processor authorization + flow-down | Sub-processor registry published at `/legal/sub-processors` + 30-day notice before change |
| Cooperation with data subject rights | KVKK DSR workflow already supports Art. 15–22 |
| Assist controller with Art. 32–36 obligations | Incident response runbook + DPIA support |
| Delete/return data at end of processing | Tenant off-boarding pipeline (ADR 0020) |
| Make available information for compliance audits | SOC 2 / ISO audit reports + right-to-audit clause |

### 4. Cross-border transfer mechanism

**No SCC needed for EU-region tenants.** Because ADR 0020 region-pins tenant data at signup and because tenant data never crosses regions, an EU-region tenant's personal data never transfers to Turkey. The TR region data-plane and EU region data-plane are legally disjoint.

**However**, three narrow cross-border data paths exist and must be documented:

1. **Control plane observability** — Personel's own SRE logs and metrics are aggregated into a central observability cluster. Customer personal data MUST NOT flow into this path. Aggregation is limited to service-level telemetry (latency, error rate, pod metrics). Enforced by the observability collector's PII denylist filter.
2. **Billing reconciliation** — Stripe (Ireland entity for EU customers) and iyzico (TR) receive only billing metadata (company name, invoice, email). This is incidental personal data; Stripe is GDPR-compliant by design, iyzico is KVKK-compliant.
3. **Cross-region support escalations** — if an EU customer's support ticket is handled by a TR-based Personel engineer, screenshare or support access must be logged and the engineer is treated as an Art. 28 processor personnel with appropriate training. No direct data flow; only time-limited interactive access under audit.

For TR→EU support path and EU→TR support path: engineers sign confidentiality agreements referencing GDPR Art. 28(3)(b) and KVKK m.12.

### 5. DPIA updates for GDPR-specific risks

The Phase 1 DPIA template (`docs/compliance/dpia-sablonu.md`) is extended with an Annex B covering GDPR Art. 35 risks:

- Systematic monitoring of data subjects on a large scale (Art. 35(3)(c)) — this applies to Personel almost by definition; DPIA is MANDATORY for every SaaS tenant above 250 endpoints, not optional.
- Processing of Art. 9 special category data (incidental, analogous to KVKK m.6)
- Automated decision-making — UBA (Phase 2.6) is advisory-only, not automated decision-making per Art. 22 definition, but this must be argued explicitly in the DPIA.

### 6. Lead Supervisory Authority designation

Personel's EU establishment determines the LSA under Art. 56. Options being evaluated (decision in Phase 3.0):

- Ireland (DPC) — common choice; English-speaking; balanced enforcement posture.
- Netherlands (AP) — active and technically literate.
- Germany (multi-state) — more fragmented; less attractive for LSA purposes.

Decision criterion: where is Personel's EU "main establishment" (Art. 4(16))? This is a legal fact (where the central administration is) not a preference. Until a physical EU office is opened, the mail-drop + contractable-DPO model is acceptable for smaller-scale processing but questionable for LSA One-Stop-Shop; a real establishment is required before Phase 3 GA.

### 7. EDPB guidelines alignment

The following EDPB guidelines are explicitly referenced in the compliance posture:

- Guidelines 05/2020 on consent — Personel does NOT rely on consent as primary basis (same as KVKK), so these are informational only.
- Guidelines 07/2020 on concepts of controller and processor — grounds the Art. 28 analysis.
- Guidelines 01/2021 on breach notification — feeds the incident response runbook.
- Guidelines 02/2023 on data subject rights — feeds the DSR workflow.
- Guidelines 04/2019 on DPIA — feeds the DPIA template update.
- Guidelines 01/2020 on connected vehicles — informational only.
- Guidelines on AI and data protection (as released) — informs UBA and ML classifier advisory-only posture.

### 8. DPO appointment

Personel (as processor) appoints a **group DPO** who is contactable by data subjects via `dpo@personel.example`. For EU-resident data subjects the DPO is also contactable via an EU-reachable physical mail drop. The DPO is independent (reports to the CEO, not to product/engineering) and is named in the Art. 30 records and DPA.

## Consequences

### Positive

- EU market entry unlocked without a full legal re-architecture; KVKK framework carries most of the weight.
- Region pinning eliminates the single hardest GDPR question (Chapter V transfers).
- DPA template and sub-processor registry become standard enterprise sales collateral.

### Negative

- Personel acquires regulatory overhead: mandatory DPIA per tenant, DPO appointment, LSA relationship, 72h breach clock.
- Breach notification clock starts when Personel becomes aware, which is often before the customer; contractual language must carefully propagate the clock without missing the 72h.
- Sub-processor change notice window (30 days) can slow down cloud provider migrations.

### On KVKK

- No change to KVKK framework for Turkish customers. KVKK remains the primary framework; GDPR is additive for EU customers. Dual-compliance documentation structure is maintained (`kvkk-framework.md` + `gdpr-kvkk-gap-analysis.md`).

## Alternatives Considered

- **Ignore GDPR, only sell to TR customers**: rejected — caps Phase 3 revenue to TR market size.
- **Transfer EU data to TR under SCCs**: rejected — Turkey has no adequacy decision; SCC plus transfer impact assessment would be contentious. Region pinning is cleaner.
- **Route EU tenants through an EU-resident local partner (VAR) acting as controller**: rejected — creates commercial friction and confuses the Art. 28 posture.
- **Delay EU entry to Phase 4**: rejected — SOC 2 and ISO work in Phase 3 is already EU-sellable; leaving the legal work to Phase 4 wastes the certification dividend.

## Cross-references

- `docs/compliance/kvkk-framework.md` — primary TR framework (unchanged)
- `docs/compliance/gdpr-kvkk-gap-analysis.md` — point-by-point mapping
- `docs/compliance/gdpr-dpa-template.md` — authored in Phase 3.3
- ADR 0020 — region pinning mechanics
- ADR 0024 — ISO 27701 overlap
