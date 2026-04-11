# ADR 0018 — HRIS Connector Interface and Initial Adapters

**Status**: Proposed for Phase 2. Not implemented.
**Deciders**: microservices-architect (Phase 2 planner); ratified by backend-developer + compliance-auditor at Phase 2 kickoff.
**Related**: ADR 0006 (PostgreSQL), ADR 0008 (on-prem first), `docs/architecture/phase-2-scope.md` §B.6.

## Context

Phase 1 ships with LDAP/AD as the only identity source. The Phase 1 pilot surfaced manual provisioning as the #1 operational friction: every hire, termination, or department move requires an admin action in Personel. For 500 endpoints this is tolerable; for 2000+ endpoints at a pilot-2 customer it is not.

HRIS integrations solve the operational pain and become a sales lever — Personel can claim "HRIS-driven UAM" and pitch it to HR buyers alongside security buyers. But HRIS integration is also a **source-of-truth conflict risk**: HRIS holds employment status, Personel holds platform-specific state (DSR history, live view approvals, legal holds). Clear conflict resolution rules are essential.

Customers run many different HRIS products. A pluggable connector interface is needed so we can add adapters without touching the core admin API, and so the contract with each HRIS is documented once in one place.

## Decision

### Core interface — `hris.Connector`

Go interface defined in `apps/api/internal/hris/` (to be added in Phase 2):

```go
package hris

// Connector is implemented by each HRIS adapter. All methods must be
// idempotent and safe to retry. Implementations MUST NOT cache credentials
// in process memory beyond a single operation; credentials come from the
// vault package on each call.
type Connector interface {
    // Name returns the adapter's unique identifier ("bamboohr", "logo-tiger").
    Name() string

    // TestConnection validates credentials and returns the HRIS tenant
    // identifier (used for audit). Called at connector creation and
    // periodically by a health check.
    TestConnection(ctx context.Context, cfg Config) (TenantInfo, error)

    // ListEmployees returns a full snapshot of employees. Used for the
    // initial sync and for daily reconciliation.
    ListEmployees(ctx context.Context, cfg Config) iter.Seq2[Employee, error]

    // GetEmployee fetches a single employee by HRIS employee ID. Used by
    // webhook handlers and on-demand refresh.
    GetEmployee(ctx context.Context, cfg Config, hrisID string) (Employee, error)

    // WatchChanges streams change events for the configured polling window.
    // Adapters that support webhooks use this method only as a
    // reconciliation fallback; webhook events are ingested separately.
    WatchChanges(ctx context.Context, cfg Config, since time.Time) iter.Seq2[Change, error]

    // Capabilities returns what the adapter supports so the sync service
    // can skip unsupported features without runtime errors.
    Capabilities() Capabilities
}

type Config struct {
    TenantID       uuid.UUID  // Personel tenant, not HRIS tenant
    AuthRef        string     // reference into Vault KV, never a raw secret
    BaseURL        string
    PollInterval   time.Duration
    WebhookSecret  string     // optional; from Vault
}

type Employee struct {
    HRISID         string        // opaque, stable
    Email          string        // primary key for matching to Personel user
    FullName       string
    Department     string
    ManagerHRISID  string        // nullable
    HireDate       time.Time
    TerminationDate *time.Time   // nil if active
    Status         Status        // active | terminated | on_leave | probation
    Locale         string        // "tr-TR", "en-US" — default to "tr-TR"
    CustomFields   map[string]string
}

type Change struct {
    Op       ChangeOp   // created | updated | terminated
    HRISID   string
    Employee *Employee  // full state after change
    At       time.Time
}

type Capabilities struct {
    Webhooks           bool
    IncrementalSync    bool
    ManagerHierarchy   bool
    CustomFields       bool
    TerminationDate    bool
}
```

Adapters live under `apps/api/internal/hris/adapters/<name>/`. Registration is via a compile-time init registry; no plugin loading at runtime.

### Authentication patterns per HRIS

| HRIS | Auth method | Vault storage |
|---|---|---|
| BambooHR | OAuth2 client credentials | `secret/hris/bamboohr/<tenant>` |
| Workday | SAML assertion + WSDL endpoint credential | `secret/hris/workday/<tenant>` |
| Personio | API key (bearer token) | `secret/hris/personio/<tenant>` |
| BordroPlus | Username + password (legacy) | `secret/hris/bordroplus/<tenant>` |
| Logo Tiger | Logo Object REST + session ticket | `secret/hris/logo-tiger/<tenant>` |

All adapters fetch credentials from Vault on each operation (no in-process caching) to support Vault-driven rotation. Legacy username/password auth (BordroPlus) is explicitly called out in the runbook as weak and customers are encouraged to use OAuth where available.

### Data mapping: HRIS employee → Personel user

| HRIS field | Personel user field | Notes |
|---|---|---|
| `Email` | `users.email` | primary match key |
| `HRISID` | `users.hris_id` | new column; nullable; unique per tenant |
| `FullName` | `users.display_name` | |
| `Department` | `users.department` | new column |
| `ManagerHRISID` | `users.manager_user_id` | resolved post-sync; may be null if manager not yet synced |
| `HireDate` | `users.hired_at` | new column |
| `TerminationDate` | `users.terminated_at` | new column |
| `Status` | `users.status` | expanded enum to match HRIS |
| `Locale` | `users.locale` | default `tr-TR` if HRIS has no value |
| `CustomFields` | `users.custom_fields_jsonb` | new column, for display and filtering; not searchable |

Matching a new HRIS employee to an existing Personel user:

1. Match by `hris_id` if already linked.
2. Otherwise match by lowercase `email`.
3. If no match, create a new Personel user in state `pending_enrollment` (no endpoint yet; an endpoint association is created when the agent enrolls using this user's email). This is the normal "new hire" flow.
4. On match, backfill `hris_id` if missing and update the fields marked as HRIS-owned.

### Sync cadence and delivery

- **Default poll interval**: 1 hour via `WatchChanges(since=last_sync)`.
- **Webhook push**: if the adapter advertises `Capabilities.Webhooks=true`, the `hris-connector` container exposes `POST /v1/hris/webhooks/<adapter>` with HMAC signature validation. Webhook events are ingested immediately; the hourly poll becomes a reconciliation fallback.
- **Daily full reconciliation**: `ListEmployees` runs once a day (off-hours) to catch any webhook drops.
- Sync operations are serialized per (tenant, adapter) via a Postgres advisory lock to prevent concurrent full syncs.

### Conflict resolution rules

When Personel and HRIS disagree:

1. **Employment status** (active/terminated/on_leave): **HRIS wins**. HRIS is the legally authoritative source; Personel cannot have a "still active" user whom HR has terminated.
2. **Contact data** (email, name, department, manager): **HRIS wins**.
3. **Platform state** (DSR history, live view approval trail, legal holds, consent acknowledgement history, audit entries): **Personel owns exclusively**. HRIS never touches these fields; adapters have no write path.
4. **Policy assignments** (which endpoint policy the user's endpoint uses): **Personel owns**. Personel uses HRIS department as an *input* to policy assignment (via rules), but the current effective policy is Personel-owned.
5. **Role assignments** (admin, hr, dpo, investigator, manager, auditor, employee): **Personel owns**. HRIS does not push roles. Role sync is via SCIM (ADR not written; Phase 2 scope item B.7), which is a separate pathway.

### Deletion semantics

When HRIS marks an employee `terminated` (or the employee disappears from `ListEmployees` output):

1. Personel sets `users.status = inactive` and records `terminated_at = <hris value or now()>`.
2. The user's endpoints are marked `retired`. The agent on those endpoints can be force-uninstalled via operator runbook, but we do not auto-uninstall (defensive: losing an endpoint mid-investigation is worse than keeping an idle agent).
3. The KVKK retention countdown begins for data tied to that user, per the retention matrix. Default: data stays as long as the policy matrix says (e.g., 2 years for audit, shorter for screenshots), then purged via the usual TTL mechanism.
4. **Legal hold override**: if the terminated user is under legal hold, the countdown is paused until the hold is released.
5. DSR history, audit, and destruction reports retain the terminated user as a historical subject; these records are not deleted on termination.
6. If HRIS later reactivates the employee (rehire), Personel can reactivate the same user record, preserving history. This is a soft-delete pattern.

All of the above actions write audit entries: `hris.employee_terminated`, `hris.employee_rehired`, `hris.field_changed`.

### Two concrete adapters for Phase 2 shipping

#### Adapter 1 — BambooHR (`apps/api/internal/hris/adapters/bamboohr/`)

- **Auth**: OAuth2 client credentials grant. BambooHR requires a subdomain (`<company>.bamboohr.com`) plus client ID and secret. Tokens are short-lived (1 hour); adapter refreshes via standard `client_credentials` flow on each operation; no caching.
- **APIs used**:
  - `GET /api/gateway.php/<company>/v1/employees/directory` — list
  - `GET /api/gateway.php/<company>/v1/employees/<id>?fields=...` — per-employee detail with field selection
  - `POST /api/gateway.php/<company>/v1/reports/custom` — used for `WatchChanges` when webhooks are off
  - Webhook: BambooHR supports outbound webhooks on employee changes; our `hris-connector` container exposes a receiver.
- **Rate limit**: BambooHR throttles at 500 req/min per subdomain. Adapter uses token-bucket with 400 req/min limit.
- **Capabilities**: `Webhooks=true`, `IncrementalSync=true`, `ManagerHierarchy=true`, `CustomFields=true`, `TerminationDate=true`.
- **KVKK note**: BambooHR is a SaaS product; data flows to BambooHR are the customer's responsibility (not Personel's — we only read from it). The customer's DPIA covers BambooHR; our runbook cross-references this.

#### Adapter 2 — Logo Tiger (`apps/api/internal/hris/adapters/logo-tiger/`)

- **Auth**: Logo Object REST API uses a session ticket obtained via `POST /api/v1/login` with username + password or a delegation token. Session tickets last ~30 minutes; adapter obtains a fresh ticket for each poll operation (no in-process cache).
- **APIs used**:
  - `GET /api/v1/employees` — paginated list (200 per page default)
  - `GET /api/v1/employees/<id>` — detail
  - No native webhook support; fall back to hourly polling with delta detection via `updated_at` field.
- **Data quirks**:
  - Logo Tiger stores Turkish fields (`AD`, `SOYAD`, `DEPARTMAN`, `UNVAN`) — adapter maps them to the canonical `Employee` struct.
  - Termination is represented as `AKTIFMI=0` plus `CIKISTARIHI` (exit date). Adapter detects this and surfaces as `Status=terminated`.
  - Email is **optional** in many Turkish Logo deployments (employees may not have company email). Adapter falls back to `<hrisid>@<tenant-locale-domain>` synthetic emails, flagged in the user record as `email_synthetic=true`, and the admin is prompted to fix matching before enrollment.
- **Capabilities**: `Webhooks=false`, `IncrementalSync=true` (via updated_at), `ManagerHierarchy=true`, `CustomFields=false` (Logo Tiger's custom field model is proprietary; Phase 3), `TerminationDate=true`.
- **KVKK note**: Logo is an on-prem Turkish product; the data flow is purely internal to the customer's network. Strong privacy posture, emphasized in sales material.
- **Testing**: Logo provides a demo install via its partner program; our QA pulls this image and writes adapter tests against it (no real customer data).

### Runtime container: `hris-connector`

New Go container in the Compose stack. Responsibilities:
- Runs the per-tenant sync scheduler (poll timer + webhook HTTP server).
- Exposes `POST /v1/hris/webhooks/<adapter>` for webhook delivery.
- Calls into `apps/api/internal/hris` via gRPC (internal). This keeps the API container stateless; the connector holds the cron/timer state.
- Writes audit entries through the standard admin API audit channel.
- Health check: surfaces last-sync timestamps and last error per (tenant, adapter) on `GET /v1/hris/status` (admin-only).

## Consequences

### Positive

- Clean extension point for future HRIS adapters (Workday, Personio, SAP SuccessFactors, Oyak, etc.). Each adapter is an isolated module.
- Operational pain relief for customers with >1000 employees.
- Sales lever with HR buyers.
- Logo Tiger adapter is a Turkish-market differentiator; no international UAM competitor ships it.
- Audit is automatic: every HRIS-driven change is logged.

### Negative / Risks

- **Credential storage is a new attack surface.** Vault storage is the only allowed location; no `.env`, no files, no process memory beyond a single operation.
- **Rate limits are adapter-specific and surface in production only.** BambooHR specifically has aggressive rate limits; a sloppy implementation will burn through the budget.
- **Synthetic emails for Logo Tiger** are a UX compromise. Documented clearly in the runbook.
- **Conflict rules must be tested carefully.** A regression where HRIS incorrectly overwrites a manually-set Personel role would be destructive.
- **"HRIS down" degradation**: if the HRIS is offline during scheduled sync, Personel continues with the last known state and surfaces a `hris_sync_stale` warning after 24 hours.

## Alternatives Considered

- **Direct HRIS integration into the admin API** (no separate connector container): rejected — couples HRIS polling cycles to API container deployment and makes HRIS outages visible in API health checks.
- **A generic CSV import** as the only integration path: rejected — customers will not manually export/import daily, and CSV has no deletion semantics.
- **SCIM as the only integration path**: rejected — most Turkish HRIS products (including Logo) do not speak SCIM.
- **Shipping 5 adapters in Phase 2**: rejected — we ship 2 well (BambooHR + Logo Tiger) and add more in Phase 2.5 or Phase 3. Spreading engineering effort across 5 adapters dilutes quality.
- **Plugin loading at runtime**: rejected — complicates security review and signature verification; compile-time registry is simpler and adequate.

## Related

- `docs/architecture/phase-2-scope.md` §B.6
- `docs/architecture/c4-container-phase-2.md`
- `docs/compliance/kvkk-framework.md` §3 (data controller relationships)
- `docs/compliance/iltica-silme-politikasi.md` (retention schedule on termination)
