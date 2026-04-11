# ADR 0019 — Live View Recording: Phase 2 Implementation Specification

**Status**: Proposed for Phase 2. Not implemented.
**Deciders**: microservices-architect (Phase 2 planner); ratified at Phase 2 kickoff by security-engineer + compliance-auditor + backend-developer.
**Supersedes**: the Phase 2 design envelope in **ADR 0012** (the envelope stands; this ADR provides the concrete implementation the envelope authorizes).
**Related**: ADR 0007 (LiveKit), ADR 0009 (key hierarchy), ADR 0013 (off-by-default pattern), ADR 0014 (WORM audit sink), `docs/architecture/phase-2-scope.md` §B.10.

## Context

ADR 0012 defined an implementation envelope for live view recording: default off, independent key hierarchy (LVMK), dual-control playback identical to initiation, DPO-only export with watermarking, 30-day retention, legal-hold eligible, audit chain integration. That ADR explicitly did not authorize implementation; it constrained it.

Phase 2 authorizes implementation. This ADR provides the concrete specification that engineers will build against. All constraints from ADR 0012 are preserved verbatim; this ADR adds the implementation detail necessary for engineering work.

## Decision

### Scope statement

Live view recording is a **per-session toggle**, not a per-tenant toggle. Every live view session defaults to `recording=false` even when Phase 2 recording is available on the tenant. An admin requesting a live view session **must explicitly request recording** in the same request form; the HR approver must **separately approve the recording** as part of the dual-control flow. An approved non-recording session cannot be upgraded to a recording session mid-flight — the admin must terminate and re-request.

Rationale: per-session explicit action ensures that the employee's transparency portal entry for each session accurately reflects whether a recording exists, and prevents "always-record" drift.

### Key hierarchy (distinct from keystroke TMK)

A **new Vault transit key** `live-view-master-key` (LVMK) is created per tenant at the time the tenant first enables live view recording. This is a separate `transit/keys/lvmk-<tenant>` entry from the keystroke TMK.

- **LVMK is non-exportable** (transit-standard for all Personel master keys).
- **Only `live-view-recorder` Vault AppRole** has `derive` permission on LVMK. No other AppRole — specifically not `dlp-service`, not `admin-api`, not `gateway`.
- **LVMK version rotation** is on a 180-day schedule, matching the TMK rotation schedule, but the versions are independent; rotating LVMK does not touch TMK and vice versa.
- **Destruction semantics**: when all recordings wrapped under LVMK version N have been deleted (TTL or explicit), the LVMK version N is destroyed (`transit/keys/lvmk-<tenant>/trim`). This is the "cryptographic shredding" pattern already used for keystroke data.

A **new AppRole** `live-view-recorder` is created by the install script only when the tenant explicitly opts into live view recording. Before that ceremony, no Secret ID is issued, no container path exists. This mirrors the ADR 0013 DLP opt-in pattern.

Per-session key derivation:

1. At session start, the `live-view-recorder` container asks Vault to derive a 256-bit session key from LVMK via HKDF with `context = session_id || tenant_id || "lv-session-dek-v1"`.
2. The derived session DEK is held in `live-view-recorder` container memory only for the duration of the session.
3. A **wrap** of the session DEK under LVMK is stored in Postgres (`live_view_recordings.dek_wrap`); the wrap is used by the playback flow to re-derive the same session DEK later via Vault.

### Storage

- **MinIO bucket**: `live-view-recordings` (new).
- **Object path**: `live-view-recordings/<tenant_id>/<yyyy>/<mm>/<session_id>.webm.enc`.
- **Object format**: WebM container (VP9 video, Opus audio if enabled — default disabled), streamed from LiveKit egress, AES-256-GCM encrypted in fixed-size 1 MiB chunks with chunk nonces derived from `HKDF(session_dek, chunk_index)`. Chunked encryption is required so playback can seek without decrypting the entire file.
- **Lifecycle policy**: 30-day default retention, aligned with the existing retention matrix bucket convention. Legal-hold eligible: an active legal hold suspends the lifecycle rule for affected objects.
- **Bucket policy**: write access via the `live-view-recorder` service account (`s3:PutObject` only, no `s3:DeleteObject`, no `s3:GetObject`). Read access via a separate `live-view-playback` service account (`s3:GetObject` only, scoped to objects where the admin has a valid playback approval — enforced at application layer via pre-signed URL issuance).

### Postgres schema — `live_view_recordings`

```sql
CREATE TABLE live_view_recordings (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id),
    session_id           UUID NOT NULL REFERENCES live_view_sessions(id) UNIQUE,
    lvmk_version         INT  NOT NULL,
    dek_wrap             BYTEA NOT NULL,              -- wrap of session DEK under LVMK version
    object_key           TEXT NOT NULL,               -- MinIO key
    object_sha256        BYTEA,                       -- populated after upload completes
    started_at           TIMESTAMPTZ NOT NULL,
    ended_at             TIMESTAMPTZ,
    bytes_total          BIGINT,
    frame_count          BIGINT,
    ttl_expires_at       TIMESTAMPTZ NOT NULL,        -- default started_at + 30 days
    legal_hold_id        UUID REFERENCES legal_holds(id),  -- nullable
    destroyed_at         TIMESTAMPTZ,                 -- set on key destruction
    CONSTRAINT recording_per_session_unique UNIQUE (session_id)
);

CREATE INDEX idx_lv_recordings_ttl ON live_view_recordings (ttl_expires_at) WHERE destroyed_at IS NULL;
CREATE INDEX idx_lv_recordings_hold ON live_view_recordings (legal_hold_id) WHERE legal_hold_id IS NOT NULL;
```

### Audit chain integration

Phase 1's audit entry reserved space for `recording_blob_ref` and `lvmk_wrap_ref`. Phase 2 populates these fields on the **session-end audit entry** (not the session-start entry; the recording artifact only exists when the session ends).

New audit entry types (added to the canonical action list):

- `live_view.recording_started` — written when LiveKit egress begins. Contains `session_id`, `recording_id`, `lvmk_version`.
- `live_view.recording_ended` — written when egress finishes. Contains `session_id`, `recording_id`, `object_key`, `object_sha256`, `bytes_total`, `frame_count`.
- `live_view.playback_requested` — admin requests playback. Contains `recording_id`, `requester_id`, `reason_code`.
- `live_view.playback_approved` — HR approver ≠ requester approves. Contains `recording_id`, `approver_id`.
- `live_view.playback_started` — actual playback begins. Contains `recording_id`, `viewer_id`, `pre_signed_url_hash`.
- `live_view.playback_ended` — actual playback finishes. Contains `recording_id`, `viewer_id`, `duration_seconds`.
- `live_view.recording_exported` — DPO exports a chain-of-custody package. Contains `recording_id`, `exporter_id` (must be DPO role), `export_package_sha256`.
- `live_view.recording_destroyed` — TTL or manual destruction. Contains `recording_id`, `lvmk_version`, `reason`.

All entries are hash-chained per Phase 1 audit design and replicated to the WORM audit sink per ADR 0014.

### Playback flow (dual-control, identical to initiation)

The playback flow is implemented as a **separate state machine** with the same transitions as the initiation state machine:

```
pending_request -> pending_hr_approval -> approved_playback -> playing -> ended
                                         \-> rejected        \-> aborted
```

- **Initiator (Investigator or Admin role)**: creates a `live_view.playback_requested` entry via `POST /v1/live-view/playbacks` with `recording_id`, `reason_code`, `justification`.
- **HR approver** (different user, HR role): approves via `POST /v1/live-view/playbacks/{id}/approve`. The same `approver ≠ requester` invariant from Phase 1 is enforced both in the admin API and in the UI.
- **Time limit for approved playback**: the approved state is valid for **30 minutes** after approval; if the viewer does not start playback in that window, the approval expires and a new request is required. Short window prevents "approved once, replayed forever" drift.
- **Playback artifact**: a short-lived MinIO pre-signed URL valid for the session duration **+ 5 minutes buffer**, capped at 120 minutes maximum (matching the Phase 1 live view session cap). Each pre-signed URL is recorded in audit (hashed, not the URL itself).
- **No download**: the pre-signed URL is used by a **browser-side WebM player in the admin console** which streams chunks and decrypts in JavaScript using the session DEK **delivered separately** via a server-sent event from the admin API. The DEK is never persisted to the browser's disk (in-memory only, cleared on page unload). The server-side logic holds the DEK briefly only to send it.
- **No file download button**. The admin console UI explicitly does not offer a download path.

### DPO-only export (chain-of-custody package)

For legal or regulatory inquiry, a DPO (and only a DPO role) can export a recording as a **chain-of-custody package**:

- ZIP archive containing:
  - `recording.webm` (plaintext, decrypted during export)
  - `manifest.json` with: `session_id`, `tenant_id`, `recording_id`, `started_at`, `ended_at`, `session_participants`, `recording_audit_chain_excerpt`
  - `chain.json` — the relevant range of the audit chain (start entry → end entry → playback entries → export entry)
  - `signature.sig` — control-plane signing key signature over `SHA256(recording.webm || manifest.json || chain.json)`
  - `pubkey.pem` — the control-plane public key for verification
- Export is logged as `live_view.recording_exported` and the export file is itself tracked in MinIO with a 2-year retention (separate lifecycle rule). The exported package lives in `destruction-reports/<tenant>/exports/` adjacent to destruction reports so the legal hold flow already covers it.

### UI surfaces

- **Admin console → Live View → Sessions list**: each row shows "recording available" badge if a recording exists. Badge is visible only to DPO and Investigator roles. For Admin/HR/Manager roles, the badge is hidden (they can see the session metadata but not the recording existence).
- **Transparency portal → Canlı İzleme Geçmişi**: for the employee, each session shows "kayıt alındı: Evet/Hayır" per ADR 0012 envelope constraint. If recorded, the row also shows "oynatma sayısı: N" and a list of playback timestamps (without viewer identities, to preserve investigator anonymity where legally required).
- **Admin console → Settings → Live View → Recording**: tenant-level enable/disable (only visible to DPO). Enabling creates LVMK, issues the Secret ID to `live-view-recorder`, brings up the container. Disabling revokes the Secret ID, stops the container, pauses TTL (does not destroy existing recordings).

## Consequences

### Positive

- Cryptographic separation between LVMK and TMK prevents "blast radius" cross-contamination. A compromise of keystroke DLP does not compromise recordings and vice versa.
- Dual-control playback means no single actor can replay historical sessions. The approver can change (HR team members rotate) but the principle holds.
- 30-day default retention is legally defensible and storage-efficient. Longer retention is customer-configurable via policy but subject to KVKK review.
- Transparency portal visibility gives employees a direct feedback loop; they see recording availability and playback counts.
- DPO-only export with chain of custody satisfies Turkish labor court / KVKK Kurul evidentiary needs.
- All transitions are audit-chained and WORM-replicated, preserving tamper-evidence.

### Negative / Risks

- **Complex state machine**: four new state transitions for playback on top of Phase 1's four live view states. Careful testing and a state-diagram doc in the runbook are required.
- **Browser decryption UX**: in-browser decryption with DEK delivered via SSE is novel for our frontend team. Prototype risk. Mitigation: a JS reference implementation is built in Phase 2.6 week 25 before UI integration.
- **WebM encryption chunk size tradeoff**: 1 MiB chunks balance seek latency vs crypto overhead. Smaller chunks = faster seeks, more nonce management; larger chunks = slower seeks. 1 MiB is the compromise; validated in Phase 2.7.
- **LiveKit egress must speak to `live-view-recorder`**: LiveKit has a native `egress` service that writes to S3. We reconfigure it to write to `live-view-recorder` (a local endpoint in the Docker stack) which encrypts and then uploads to MinIO. This is a small adapter shim; LiveKit egress already supports custom destinations.
- **Audit chain length grows faster** with 7 new entry types per session. Expected load: ~10-50 sessions per tenant per day; chain grows by ~100 entries/day/tenant. Negligible on Phase 1 infrastructure.
- **Disk footprint**: at 1 Mbps H.264, 30-minute sessions, 50 sessions/day × 30-day retention = ~11 GB per tenant per day × 30 = ~330 GB. Customers with high session volumes need disk planning. Documented in sizing guide.

## Alternatives Considered

- **Shared TMK with keystroke hierarchy**: rejected (ADR 0012 explicit forbid; retained).
- **Single playback approval by Admin only**: rejected (same reason as live view initiation — dual control is the whole point).
- **Download button for authorized DPO**: rejected (watermarking is not a substitute for access control; see ADR 0012 rejection of the same).
- **Recording always on (off-switch only)**: rejected (per-session toggle matches ADR 0012 "default off").
- **Store recordings unencrypted and rely on MinIO bucket ACL**: rejected (ACL-only is policy-controlled, not cryptographic; we have Vault anyway, no reason to downgrade).
- **Client-side recording on the endpoint agent**: rejected (tamper surface, disk usage on endpoint, exfiltration vector).
- **Keep playback to 5 minutes** (even shorter validity): rejected — too short to let a DPO pull in another reviewer or fetch context.

## Related

- `docs/adr/0012-live-view-recording-phase2.md` (envelope)
- `docs/adr/0007-livekit-webrtc-live-view.md`
- `docs/adr/0009-keystroke-content-encryption.md`
- `docs/adr/0013-dlp-disabled-by-default.md` (opt-in pattern)
- `docs/adr/0014-worm-audit-sink.md`
- `docs/architecture/live-view-protocol.md`
- `docs/architecture/key-hierarchy.md`
- `docs/architecture/phase-2-scope.md` §B.10
