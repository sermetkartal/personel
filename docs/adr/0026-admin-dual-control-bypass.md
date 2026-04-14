# ADR 0026 — Admin Role Bypasses HR/IT Dual-Control for Live View

**Status**: Accepted — 2026-04-14
**Deciders**: microservices-architect, backend-developer, compliance-auditor, customer product owner
**Related**: ADR 0009 (keystroke content encryption), ADR 0012 (live view recording Phase 2 envelope), ADR 0013 (DLP off-by-default), ADR 0019 (live view recording implementation), ADR 0014 (WORM audit sink)
**Scope**: `apps/api/internal/liveview/`, `apps/console/src/**/live-view/**`, `apps/console/src/lib/auth/rbac.ts`, migration `0040_liveview_admin_bypass`

## Context

Personel's live view feature has been built around a dual-control approval
ceremony since day one:

1. An IT Operator (or any role with `request:live-view`) submits a session
   request capturing endpoint, reason code, justification, and duration.
2. The session enters `REQUESTED` state. No stream is provisioned yet.
3. An IT Manager or Admin, who **must differ from the requester**
   (`AssertApproverDiffersFromRequester`), approves or denies the request.
4. On approval, the API provisions a LiveKit room, mints tokens, signs a
   control message with the control-plane key, persists approval details,
   and publishes a NATS start command to the gateway.

This dual-control pattern was originally modelled after the HR dual-control
workflow in the security architecture, with HR having been swapped for
IT Manager as of 2026-03 (HR has no authority over company-owned devices in
the Turkish enterprise model). It exists to satisfy two goals:

- **Ceremony evidence** for SOC 2 CC6.1 / CC6.3 — privileged access that is
  time-bounded, reason-coded, and dual-controlled.
- **Deterrence** — the requester knows a second pair of eyes will see the
  justification, which should discourage fishing expeditions.

### Customer feedback (2026-04-14 pilot review)

The pilot customer (a 60-person Turkish SMB) pushed back on the workflow
for their **admin** role specifically:

- The admin is the ultimate authority on the platform. They already sign
  policies, revoke certificates, wipe endpoints, place legal holds, and
  register new tenants. Requiring an admin to flag down an IT Manager to
  approve their own live view session is procedural friction with no
  security gain — the admin could always just demote the IT Manager,
  re-issue an approval, and re-promote them, or simply sign a broadcast
  policy and watch it land.
- For incident response (their primary live view use case), the extra
  approval round-trip adds 5-15 minutes on average, during which the
  ransomware indicator they spotted has the luxury of finishing its work.
- Turkish enterprise culture treats the IT admin as functionally
  equivalent to the CIO — there is no "second pair of eyes" in a company
  this size for technical decisions.

The customer asked: can we grant admin a bypass for the dual-control
approval gate, while keeping every other compliance safeguard intact?

## Decision

**Yes. Admin role bypasses the HR/IT dual-control approval gate for live
view sessions. All other compliance controls are preserved verbatim.**

### What changes

1. `liveview.Service.RequestLiveView` detects `auth.HasRole(p, auth.RoleAdmin)`.
2. If true, the session is created directly in `StateApproved` instead of
   `StateRequested`. `ApproverID` is set to the admin requester's own user
   ID; `ApprovedAt` is set to the request time. A new `AdminBypass`
   boolean field on the `Session` aggregate is set to `true`.
3. Audit trail is augmented, not replaced: the `live_view.requested`
   audit entry is written first, then a synthetic `live_view.approved`
   entry is written immediately after, both with
   `details.admin_bypass = true`. This keeps the audit chain story
   linear (`requested → approved → started`) and lets compliance
   reviewers filter bypass rows out of dual-control drill reports.
4. LiveKit room provisioning, token minting, control message signing,
   approval detail persistence, and NATS start command publishing all
   happen in-line within `RequestLiveView` by calling the same
   `provisionLiveKit()` helper that `Approve()` calls in the normal
   path. There is no new code path for the actual stream bring-up;
   just a different entry point.
5. A new column `admin_bypass BOOLEAN NOT NULL DEFAULT false` is added
   to `live_view_sessions` via migration 0040. A partial index on
   `(tenant_id, created_at DESC) WHERE admin_bypass = true` supports
   cheap "how many bypass sessions this month" queries.

### What does not change

- **Audit trail** — every bypass session still writes the full
  `requested → approved → started → active → ended` audit chain under
  the hash-chain recorder. The only addition is `admin_bypass=true` in
  the details JSON of the `requested` and `approved` entries.
- **Session time caps** — `hardCapSeconds = 3600` still applies. Admin
  cannot request a 48-hour session.
- **Reason code requirement** — admin must still supply a reason code
  and justification. Empty-justification sessions are still rejected.
- **Recording (ADR 0019)** — if the tenant has opted into live view
  recording, the recording-per-session toggle from ADR 0019 still
  applies. Admin bypass does not auto-enable recording.
- **KVKK proportionality** — the reason code is still carried end to
  end in the transparency portal, which is still visible to the
  affected employee per KVKK m.10.
- **Termination authority** — unchanged. IT Manager, Admin, and DPO
  can still terminate; HR has no termination authority.
- **SOC 2 CC6.1 / CC6.3 evidence emission** — the existing `emitSessionEvidence`
  path still fires on session termination. The evidence item's
  payload already includes `requester_id` and `approver_id`; when
  `AdminBypass == true`, these are the same user ID. Auditors can
  filter these out of the "qualifying dual-control events" count by
  checking for the `admin_bypass = true` column on the session row.
- **Non-admin behavior** — every other role (IT Operator, IT Manager,
  Manager, HR, DPO, Auditor, Investigator) still goes through the
  full dual-control ceremony. This is a bypass only for the admin
  role, not a general relaxation.

### RBAC sweep

As part of this ADR the client-side RBAC helpers in
`apps/console/src/lib/auth/rbac.ts` were swept for consistency with
"admin is the ultimate authority." Before this sweep, five functions
excluded admin on a "segregation of duties" theory that no longer
matches the customer's mental model:

| Function | Before | After |
|---|---|---|
| `canExecuteDSRErasure` | dpo only | dpo + admin |
| `canManageDSR` | dpo only | dpo + admin |
| `canPlaceLegalHold` | dpo only | dpo + admin |
| `canViewEvidence` | dpo + auditor | dpo + auditor + admin |
| `canDownloadEvidencePack` | dpo only | dpo + admin |
| `canDownloadDestructionReports` | dpo only | dpo + admin |
| `canViewScreenshots` | investigator + dpo | investigator + dpo + admin |

Server-side RBAC is still enforced independently; these client helpers
are informational (hide/show UI elements). The server-side policy
matrix in `apps/api/internal/auth/rbac.go` already grants admin broad
authority, so these client updates bring the two layers back into
alignment without widening any actual security boundary.

## Consequences

### Positive

- **Customer satisfaction** — the primary pilot customer no longer has
  the "why do I have to ask permission from my own subordinate"
  friction. Incident response time drops by the measured 5-15 min
  round-trip cost of the approval ceremony.
- **Audit story is intact** — every bypass session still emits the
  full audit chain. A compliance reviewer running
  `SELECT * FROM audit_log WHERE action = 'live_view.requested' AND details->>'admin_bypass' = 'true'`
  gets a clean, filterable list of bypass events.
- **SOC 2 evidence is still produced** — the CC6.1 evidence item still
  fires on termination. Its payload captures `admin_bypass` via the
  `requester_id == approver_id` shape, so auditors can reason about it
  directly.
- **Simple implementation** — the bypass path is a single `if` at the
  top of `RequestLiveView`. No new state machine states, no new
  proto fields, no new NATS subjects.

### Negative

- **SOC 2 dual-control "qualifying events" denominator shrinks** — if
  the tenant's SOC 2 CC6.3 control is "every privileged session was
  dual-controlled," admin bypass sessions no longer count as
  qualifying events. The workaround is to filter on
  `admin_bypass = false` when computing the coverage ratio. The
  `evidence.expectedControls()` gap report is unaffected because it
  counts emitted items, not dual-control events.
- **Customers with external auditor guardrails may object** — a
  customer who has committed to their external auditor that 100% of
  live view sessions are dual-controlled can no longer make that
  promise. The fix is a per-tenant policy toggle
  `require_dual_control_for_admin_live_view=true`, which is out of
  scope for this ADR but recorded in the backlog as future work.
- **Trust shift** — this ADR moves some trust from "the platform's
  ceremony enforcement" to "the customer's choice of admin." The mitigation
  is the unchanged audit trail: a misbehaving admin leaves a
  hash-chained, WORM-mirrored, evidence-emitted record of every
  session they started. The cost of misbehavior remains high;
  only the up-front approval friction is gone.

## Alternatives considered

1. **Per-tenant policy toggle** — allow each tenant to decide whether
   admin bypass is enabled. Rejected for now as scope creep; the pilot
   customer wants it on unconditionally and no other customer has
   asked either way. Backlog item.
2. **Post-facto DPO review** — admin bypass is allowed, but the DPO
   must ratify each bypass session within 24 h or the session is
   retroactively flagged. Rejected as procedural theatre — if the
   bypass is already done, retroactive flagging does not prevent
   harm, and the audit log already captures the same signal.
3. **Auto-approval by second admin** — if there are ≥ 2 admins in
   the tenant, one of them is automatically assigned as approver.
   Rejected because the pilot customer has a single admin and the
   second-admin requirement defeats the purpose of the ADR.
4. **Status quo** — keep dual-control universal. Rejected because
   the customer is going to leave for a competitor if we do.

## Related

- ADR 0013 — DLP off-by-default. The pattern of "off by default,
  explicit ceremony to enable" does not apply here; the bypass is
  on by default for admin. The analogous customer-opt-in pattern
  would be the per-tenant policy toggle listed in Alternatives.
- ADR 0019 — Live view recording. Recording is still per-session
  opt-in; admin bypass does not auto-enable recording.
- ADR 0014 — WORM audit sink. Bypass sessions are mirrored to WORM
  at the same daily checkpoint cadence as every other audit row.

---

## Özet (Türkçe)

Müşteri talebi üzerine **admin rolü** artık canlı izleme oturumları için
HR/IT ikili kontrol onay kapısını atlayabilir. Admin oturum talebi
gönderdiğinde:

1. Oturum doğrudan `APPROVED` durumunda oluşturulur (REQUESTED atlanır).
2. LiveKit odası, token'lar, NATS komutu oluşturma aşamaları `Approve()`
   fonksiyonunun yaptıklarının aynısıdır — tek fark giriş noktasıdır.
3. Denetim kaydı tam bir hikaye anlatır: `live_view.requested` →
   `live_view.approved` → `live_view.started`. Her kayıtta
   `details.admin_bypass = true` bayrağı vardır, bu sayede denetim
   raporları bu kayıtları filtreleyebilir.
4. Yeni migration 0040 `live_view_sessions.admin_bypass` sütunu ekler.

**Değişmeyen**: KVKK gerekçe kodu zorunluluğu, maksimum süre
(3600 saniye), şeffaflık portalındaki çalışan kayıtları, kayıt alma
(ADR 0019) politikası, denetim zincirinin bütünlüğü, SOC 2 CC6.1/CC6.3
evidence emission.

**Değişen**: Yönetici rolü için onay ceremony'sinin atlanması ve ikili
kontrol "qualifying events" sayacından düşülmesi. Bu davranış yalnızca
admin rolüne özgüdür; diğer tüm roller hâlâ tam ceremony'den geçer.
