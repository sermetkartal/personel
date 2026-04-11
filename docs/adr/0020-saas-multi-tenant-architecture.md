# ADR 0020 — SaaS Multi-Tenant Architecture

**Status**: Accepted (Phase 3)
**Amends**: ADR 0008 (On-Prem First Deployment) — does NOT supersede; both coexist.
**Related**: ADR 0021 (Kubernetes SaaS Deployment), ADR 0022 (GDPR Expansion).

## Context

Phase 1 and Phase 2 shipped a single-tenant on-prem stack per ADR 0008. Phase 3 opens a second, equally blessed deployment path: a **multi-tenant managed SaaS** offering. Three questions must be answered:

1. **Isolation model**: single-tenant-per-customer (STPC) stacks on shared infrastructure, or **true multi-tenant (TMT)** where one logical stack serves many tenants with row-level isolation?
2. **Data residency**: where does a given tenant's data live, and how is region pinned?
3. **Network and secrets isolation**: do tenants share a Vault namespace, or do they each get a dedicated one?

Constraints:

- Phase 1 scaffolding already carries `tenant_id` in every Postgres table, every ClickHouse event, and every NATS subject. RLS policies exist in `apps/api/internal/postgres/migrations/`. Vault tenant-scoped policies exist as templates. The code base is **TMT-ready by design**.
- SaaS economics only work at TMT. STPC forces per-tenant Postgres/ClickHouse instances, which at 100 SMB tenants means 100 stacks — operationally untenable.
- Cross-tenant data leakage is the single biggest reputational and legal risk (GDPR Art. 32, SOC 2 CC6.1, KVKK m.12).
- On-prem (ADR 0008) must keep working for existing customers; code divergence is not acceptable.

## Decision

SaaS edition uses **true multi-tenant (TMT) isolation** with the following layering:

### Data layer

| Store | Isolation mechanism | Rationale |
|---|---|---|
| **PostgreSQL** | Row-Level Security (RLS) with `tenant_id` enforced by policy; connection pool sets `app.tenant_id` GUC per request | Already implemented in Phase 1 scaffolding; zero code migration |
| **ClickHouse** | Tenant-scoped partitions on every table (`PARTITION BY (tenant_id, toYYYYMM(ts))`); query layer injects `WHERE tenant_id = $1` and denies unscoped queries | Partition isolation gives efficient deletes on tenant off-boarding |
| **MinIO** | **Bucket-per-tenant** (`personel-tenant-{uuid}`); lifecycle policies per bucket; IAM policy on object prefix belt-and-braces | Bucket-level isolation is strongest; lifecycle per tenant supports retention matrix per plan |
| **OpenSearch** | Index-per-tenant alias (`audit-{tenant_uuid}`) | Enables per-tenant search quotas |

### Identity and secrets

- **Keycloak**: realm-per-tenant (`realm-{tenant_uuid}`). Shared Keycloak deployment; tenant admin users provisioned in their own realm. This supports per-tenant IdP federation (customer Azure AD, customer Okta) without cross-realm bleed.
- **Vault**: **dedicated namespace per tenant** (`personel/tenant/{uuid}/`). Not shared. Each tenant namespace has its own PKI root, TMK, LVMK, DLP keys. This is non-negotiable: sharing a Vault namespace across tenants would allow policy-misconfiguration to leak one tenant's keys to another.
- **Cryptographic key hierarchy** (ADR 0013, ADR 0019): re-instantiated per tenant.

### Network

- No network-layer tenant isolation within a region (shared gateway, shared admin API pods).
- Tenant identity is established at the **edge** via the OIDC session's `tenant_id` claim and at the **agent** via the enrollment certificate's SAN (which encodes tenant UUID).
- Every request carries `tenant_id` in the request context (Go `context.Context`), enforced by middleware (`internal/httpserver/middleware/tenant.go` — new).
- **No shared caches that cross tenants.** Any caching layer must key by tenant_id and have eviction that is tenant-scoped.

### Data residency

Two launch regions:

1. **TR region** — for Turkish customers. Frankfurt is ruled out for TR-residency tenants; the TR region MUST be inside Turkey (DC selection in Phase 3.0).
2. **EU region** — for EU customers. Must be inside the EEA; target Frankfurt or Amsterdam for GDPR-friendly jurisdiction; Ireland ruled IN as a secondary.

**Region pinning is set at tenant signup and is IMMUTABLE thereafter.** A tenant that wants to change region must off-board and on-board as a new tenant (with data export/import as a professional-services operation). This rule eliminates any runtime cross-region read path and simplifies both GDPR Art. 44 analysis and SOC 2 CC6.7 controls.

### Tenant onboarding workflow

1. **Signup** (marketing site → billing service): email, company name, region choice, plan tier, card capture (Stripe or iyzico).
2. **DPIA upload** (mandatory for paid plans > 50 endpoints; optional otherwise — free-tier signup still accepts a stored self-attestation).
3. **DPA signature** — click-to-sign for the customer; hand-signed countersign by Personel DPO for EU customers over 500 endpoints.
4. **Vault provisioning** — automated pipeline creates tenant namespace, mints tenant PKI root, seeds TMK, creates tenant-scoped policies. Fails closed if any step errors.
5. **Keycloak realm provisioning** — realm created, admin invite email sent, optional IdP federation config captured.
6. **Data stores provisioning** — Postgres RLS rows, ClickHouse partitions, MinIO bucket, OpenSearch index alias.
7. **First agent enrollment token** — one-shot token delivered to the admin; agent download URL region-aware.
8. **Observation period start** — SOC 2 evidence locker begins recording tenant-level events.

The pipeline is idempotent and re-runnable; failures are rolled back with compensation actions.

## Consequences

### Positive

- Shared infrastructure costs → SaaS gross margins viable at mid-market pricing.
- Vault namespace dedication keeps the cryptographic blast radius tenant-scoped even if a policy is misconfigured.
- Region pinning eliminates any runtime cross-region data flow — GDPR Art. 44 stops being a question.
- The same code runs on-prem (single-tenant install, the tenant UUID is just "default") and SaaS. Zero code divergence.
- Bucket-per-tenant gives clean tenant off-boarding (delete bucket → delete data — auditable).

### Negative

- Shared Postgres/ClickHouse means a noisy tenant can impact others; requires aggressive per-tenant rate limiting and query budget enforcement.
- RLS bugs become cross-tenant leaks; requires RLS fuzz testing in CI and penetration test gate before GA.
- Vault namespace-per-tenant creates operational weight: rotation, backup, audit all scale by tenant count.
- Bucket-per-tenant at 1000+ tenants puts pressure on MinIO bucket metadata; must validate at load.

### Legal

- SaaS-mode Personel is a **KVKK veri işleyen** and a **GDPR Art. 28 processor**. The on-prem "yazılım sağlayıcı" posture (`kvkk-framework.md` §3.1) no longer applies. DPA is mandatory for every tenant; Art. 30 records must be maintained by Personel.
- Sub-processors (AWS/GCP/Azure for the region infra, Stripe, iyzico, email provider) must be disclosed and flow-down DPAs signed.

### On-prem impact

- Zero code regression. On-prem install = single-tenant TMT install with tenant UUID "default". RLS still applies. Vault namespace is still used (just one). The code doesn't care.
- Existing customers upgrade to Phase 3 release with a single schema migration (add `region` column to `tenants`, default 'on-prem').

## Alternatives Considered

- **Single-tenant per customer stacks (STPC)** — operationally untenable at SaaS scale; rejected.
- **Schema-per-tenant (Postgres)** — weaker isolation than RLS with namespace dedication, and Postgres catalog bloat at high tenant counts; rejected.
- **Shared Vault namespace with fine-grained policies** — one policy misconfiguration = cross-tenant key leak; rejected on SOC 2 CC6.1 and GDPR Art. 32 grounds.
- **Region = tenant-selectable at runtime** — opens cross-region read paths and GDPR Art. 44 complications; rejected in favor of signup-time immutable pinning.
- **No on-prem coexistence (SaaS-only)** — breaks ADR 0008 commitments to existing customers and abandons the Turkish market's preferred posture; rejected.

## Cross-references

- `docs/adr/0008-on-prem-first-deployment.md` — amended (coexistence clause added)
- `docs/adr/0021-kubernetes-saas-deployment.md` — runtime for this architecture
- `docs/adr/0022-gdpr-expansion.md` — legal basis for EU region
- `docs/compliance/kvkk-framework.md` §3.3 — foretold the veri işleyen transition
- `docs/architecture/phase-3-scope.md` §B.1
