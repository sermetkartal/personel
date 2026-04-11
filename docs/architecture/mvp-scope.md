# Phase 1 MVP Scope

> Language: English. Status: LOCKED for Phase 1 planning. Deviation requires ADR.

## Goals

Deliver a Turkey-market, KVKK-compliant, on-prem UAM pilot capable of supporting 500 endpoints and demonstrating a credible path to 10 000 endpoints, with the full keystroke-content encryption guarantee and live-view governance working end to end.

## In Scope (IN)

### Agent (Windows, user-mode)
- Enrollment via signed token and mTLS bootstrap
- Encrypted local SQLite queue with eviction policy
- ETW-based process and file collectors
- Window focus and title collector (Win32)
- DXGI screenshot capture (interval + event-triggered)
- DXGI short screen clip capture (on-demand, ≤ 30 s)
- Clipboard metadata + optional encrypted content
- Keystroke metadata (counts) + encrypted keystroke content (PE-DEK, AES-256-GCM)
- WFP user-mode network flow summaries (no deep packet inspection)
- DNS/TLS-SNI capture via ETW
- WMI-based USB device event collector
- Print job metadata collector
- Idle/active detection
- Policy engine with app block, URL block, USB block, screenshot interval, collector on/off, plus KVKK m.6 guardrails: `screenshot_exclude_apps` (list of process image globs that suppress screenshot capture when they are foreground), `window_title_sensitive_regex` (title-regex list that flags matching events as sensitive-flagged for shortened retention), `sensitive_host_globs`, and `auto_flag_on_m6_dlp_match` (see `proto/personel/v1/policy.proto` → `SensitivityGuard`)
- Auto-update with signature verification, canary, rollback
- Anti-tamper: watchdog process, registry ACL checks, binary self-hash, DPAPI+TPM key sealing
- Live view producer (DXGI → LiveKit)

### Server
- Ingest Gateway (gRPC, mTLS, cert pinning, NATS write)
- NATS JetStream event bus with durable consumers
- ClickHouse writer (batched) and schema for top event types
- PostgreSQL schema for tenants, users, endpoints, policies, live-view requests, audit
- OpenSearch indexer for searchable text fields (window title, URL, path)
- MinIO buckets with per-tenant prefixes and lifecycle rules
- Admin API (Go): tenants, users, endpoints, policies, queries, live-view orchestration
- Policy Engine with signing
- DLP Service with Vault-backed key hierarchy and pattern rules (TCKN, IBAN, credit card, 20 built-in regex, custom). **Disabled by default per ADR 0013.** Delivered as an opt-in feature: install creates the `dlp-service` Vault policy and AppRole but does not issue a Secret ID, the Compose profile `dlp` is not activated on `docker compose up`, and the default policy bundle sets `KeystrokeSettings.content_enabled = false` until opt-in. Customers activate DLP via the documented opt-in ceremony (DPIA amendment → signed form → `infra/scripts/dlp-enable.sh` → transparency banner → audit checkpoint). Both the "off" path and the "on after ceremony" path are Phase 1 scope. The state endpoint `GET /api/v1/system/dlp-state` is the single source of truth consumed by Console header badge, Settings panel, and Transparency Portal banner; all state transitions are written to the hash-chained audit log.
- Audit Log Service (append-only hash-chained)
- Update Service (signed artifacts, canary cohorts)
- Vault PKI + transit + KV, initial unseal via Shamir 3-of-5
- LiveKit SFU, single-node

### UI
- Admin Console (Next.js): dashboards, endpoint list, policy editor (including SensitivityGuard section), reports, live-view request flow, audit viewer, retention settings, **legal-hold placement UI (DPO-only)**, **6-month periodic destruction report screen (auto-scheduled)**, **m.11 DSR dashboard with SLA timers**
- Transparency Portal (Next.js): employee notice, **live-view session history (default ON per revision round; configurable OFF only via audited DPO action)**, KVKK m.11 request form with automatic ticket id and 30-day SLA counter visible to employee
- HR approval screen (role-gated)
- DPO audit view, legal-hold dashboard, destruction-report viewer

#### KVKK m.11 Data Subject Request (DSR) Workflow — Phase 1 scope
(Added during Phase 0 revision round — Gap 3.) Concrete requirements for the nextjs-developer and admin-api specialists:

- **Endpoints** (Admin API):
  - `POST /v1/dsr` — employee submits via Portal; body includes request_type (`access`|`rectify`|`erase`|`object`|`restrict`|`portability`), scope, justification. Returns ticket id and SLA deadline (`created_at + 30 days`).
  - `GET /v1/dsr?state=open|overdue|closed` — DPO dashboard query.
  - `POST /v1/dsr/{id}/assign` — DPO assigns to handler.
  - `POST /v1/dsr/{id}/respond` — close with response artifact (PDF, data export, rejection reason). Appends audit entry.
  - `POST /v1/dsr/{id}/extend` — KVKK allows 30-day extension with written justification; produces audit entry and notifies employee.
- **SLA timer**: server-side job checks daily; transitions `open → at_risk` at day 20, `open → overdue` at day 30.
- **Notifications**:
  - Employee: in-portal banner + optional email on status changes.
  - DPO: dashboard badge + daily digest email of at-risk and overdue tickets.
  - At day 25, automatic email escalation to DPO secondary contact.
- **Dashboard widgets** (Admin Console, DPO view):
  - Open DSR count, at-risk count, overdue count
  - Median response time trailing 90 days
  - List view filterable by state, request_type, age
- **Data model**: Postgres `dsr_requests(id, tenant_id, employee_user_id, request_type, scope_json, state, created_at, sla_deadline, assigned_to, response_artifact_ref, audit_chain_ref)`.
- **Audit integration**: every state transition → hash-chained audit entry (`dsr.submitted`, `dsr.assigned`, `dsr.extended`, `dsr.responded`, `dsr.rejected`).
- **Export**: response artifacts stored in MinIO `dsr-responses/<tenant>/<yyyy>/<id>.pdf`, signed with the control-plane signing key for chain-of-custody.

#### KVKK Periodic Destruction Report — Phase 1 scope
(Added during Phase 0 revision round — Gap 4. Required by the Yönetmelik; the product must emit this report automatically.)

- **Schedule**: auto-generated twice per year at 00:00 local on **1 January** and **1 July** for the preceding six-month period.
- **Contents**:
  - Per-category deleted row counts (from retention TTL jobs and manual purges)
  - MinIO lifecycle deletion counts (per category / per prefix)
  - Key destruction events from Vault audit (TMK version destroys, per-endpoint DEK revocations, LVMK destroys when Phase 2 ships)
  - Legal-hold placements and releases during the period
  - DSR-triggered deletions during the period
  - Outstanding legal holds at period end (summary, not full content)
- **Format**: signed PDF with cryptographic signature using the control-plane signing key; companion JSON manifest in MinIO `destruction-reports/<tenant>/<yyyy>-<h1|h2>.pdf`.
- **Access**: DPO-only download; console "Destruction Reports" screen lists the archive with signatures verified on render.
- **Audit**: report generation is itself an audit entry (`retention.destruction_report_generated`).

#### Legal Hold UI — Phase 1 scope
(Added during Phase 0 revision round — Gap 5.)

- DPO-only console screen `/dpo/legal-holds`: place, list, release.
- Placement form requires reason_code, ticket_id, justification, scope (endpoint/user/date-range/event-types), max duration ≤ 2 years.
- List view shows active holds with days-remaining, placement reason, affected row count (approximate).
- Release requires a justification note and triggers an audit entry + recomputation of TTLs for affected records.
- See `data-retention-matrix.md` §Legal Hold for data model.

### Ops
- Docker Compose stack for full deploy
- systemd units for host-level services (vault-agent, backup, log rotation, watchdog for compose stack)
- Backup scripts: Postgres (pg_basebackup), ClickHouse (clickhouse-backup), MinIO (mc mirror), Vault raft snapshot
- Prometheus + Grafana for server self-observability
- Install runbook, upgrade runbook, DR runbook (first drafts)

### Compliance
- KVKK retention matrix enforced automatically (including sensitive-flagged bucket with shortened TTL)
- VERBİS-ready data inventory export
- One-time install notice flow
- Audit exports for regulator inspection
- 6-month periodic destruction report (auto-generated, DPO-downloadable, signed PDF)
- Legal hold mechanism with DPO-only placement and audited release
- KVKK m.11 DSR workflow with 30-day SLA counter, at-risk / overdue escalation
- Default-ON live-view session history in Transparency Portal (per Gap 6)
- **KVKK m.6 DLP signal pack** — default rule bundle for health / religion / union-related Turkish patterns that routes matches to the sensitive-flagged bucket. Rule authoring is a security-engineer follow-up; the pipeline and rule-loader are in scope for Phase 1 (Gap 8).

## Out of Scope (OUT) — Phase 1

- macOS and Linux agents (Phase 2)
- Kernel minifilter driver (Phase 3)
- OCR of screenshots (Phase 2)
- ML-based behavioral anomaly detection (Phase 2)
- Productivity scoring / time categorization ML (Phase 2)
- Mobile admin app (Phase 2)
- SAML / OIDC SSO (Phase 2; Phase 1 ships LDAP/AD only)
- Multi-tenant active use (single-tenant per install for MVP)
- Managed SaaS deployment, cloud billing (Phase 3)
- EU / GDPR features (Phase 3)
- Kubernetes and Helm charts (Phase 3)
- Live-view session recording to disk
- Two-way audio during live view
- Remote shell / RDP takeover
- On-agent face/webcam capture
- Geolocation tracking
- Browser extension companion
- Teams/Slack integration for alerts (Phase 2)
- Full-text OCR across historical screenshots
- Data warehousing integrations (Snowflake/BigQuery) — Phase 3
- Auto-generated DPIA export wizard — Phase 2 (Gap 7). Phase 1 ships the static DPIA template in `docs/compliance/dpia-sablonu.md`; the auto-fill tool comes later.
- Live-view session recording to disk — Phase 2 (ADR 0012)

## Exit Criteria (Phase 1 Must All Be True)

| # | Criterion | Target |
|---|---|---|
| 1 | Pilot deployment | 500 endpoints running stably for 14 consecutive days |
| 2 | Agent CPU | < 2% average on a typical corporate laptop (i5/i7, SSD) |
| 3 | Agent memory | < 150 MB RSS |
| 4 | Agent disk footprint | < 500 MB including queue at steady state |
| 5 | Dashboard query p95 | < 1 s for the standard dashboard set (top 10 apps, active time, alerts) |
| 6 | Event loss rate | < 0.01% under normal network; zero loss in offline-buffer scenario up to 48 h |
| 7 | End-to-end event latency p95 | < 5 s (endpoint → queryable) |
| 8 | Server uptime | ≥ 99.5% over the 14-day pilot window |
| 9 | Keystroke-content isolation proof | Independent red team confirms admin cannot decrypt |
| 10 | Live-view governance | Dual-control enforced; 100% of sessions have hash-chained audit; hash chain passes integrity check |
| 11 | KVKK review | DPO sign-off on retention matrix, transparency portal, VERBİS export |
| 12 | Auto-update rollback | Demonstrated canary + automatic rollback on canary failure |
| 13 | mTLS revocation | Revocation propagates to all gateways within 5 minutes |
| 14 | Anti-tamper baseline | Kill/restart via Task Manager recovers within 10 s; registry ACL tamper detected within 60 s |
| 15 | Documentation | Install runbook, admin guide, DPO guide, incident runbook all published |
| 16 | Security scan | No critical/high CVEs in release artifacts; SBOM published |
| 17 | ClickHouse replication plan validated | Replication plan per `clickhouse-scaling-plan.md` proven in staging before any customer beyond pilot; failover tested |
| 18 | **DLP opt-in ceremony end-to-end (ADR 0013)** | Full ceremony executable within 1 hour including sign-off verification, Secret ID issuance, container start, state endpoint flip, Console badge update, Portal banner, audit chain event written and carried to external WORM sink, and a synthetic keystroke content flow successfully decrypted and producing a `dlp.match`. Validated on staging. Reverse ceremony (`dlp-disable.sh`) also validated: Secret ID revoked, container stopped, UI state flipped, audit entry written. Default state (no Secret ID issued) additionally validated by inspecting Vault audit device for zero `derive` calls over the lifetime of a fresh install. |
| 19 | Sensitive-flagged bucket end-to-end | A synthetic m.6 signal (regex + host) is correctly routed to sensitive bucket, its TTL is the shortened value, and its key destruction runs ahead of normal TTL |
| 20 | Legal hold end-to-end | Placement, TTL bypass, release, and audit entries all functional; red-team verifies TTL is honored after release |
| 21 | DSR SLA timer | Synthetic DSR ticket correctly transitions open → at_risk → overdue with notifications and audit entries |
| 22 | Destruction report | First H1 report renders correctly, signature verifies, contents match independent count |

## Phase 1 Non-Goals That Are NOT Technical Debt

These are deliberate omissions, not shortcuts to revisit:

- We are not shipping a pretty "productivity score." Scores without ML context invite misinterpretation and legal risk under KVKK m.4.
- We are not shipping agent self-uninstall from console. All uninstalls go through operator scripts.
- We are not shipping raw-content search for keystrokes; this is by design and will not change in Phase 2.

## Phase 1 Timeline Target (for reference by PM agent)

- Week 0–2: Repo bootstrap, proto lock, key hierarchy implementation, CI/CD
- Week 3–8: Collector core, ingest/storage, admin API skeleton, console skeleton
- Week 9–12: Policy engine, DLP service, live view, updater
- Week 13–14: Hardening, pilot install, runbooks
- Week 15–16: Pilot operation and exit-criteria verification
