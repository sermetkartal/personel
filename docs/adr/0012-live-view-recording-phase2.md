# ADR 0012 — Live View Recording (Phase 2 Design Envelope)

**Status**: Accepted for Phase 2. **Out of Scope for Phase 1.**

## Context

Phase 1 `live-view-protocol.md` states that disk recording of live-view sessions is out of scope. This was the right call — recording is the single highest-risk feature in a KVKK-sensitive UAM product. But leaving the Phase 2 design entirely undefined would allow Phase 1 decisions to foreclose the cleanest options. Security-engineer flagged this gap during the revision round: we need a design envelope now so that (a) the audit hash chain can reference recordings cleanly when they arrive, (b) the proto has a reservation shape that doesn't force a breaking change, and (c) the key hierarchy for recordings is decoupled from the keystroke hierarchy up front.

## Decision

Phase 2 will introduce live-view session recording under the following **design envelope**. This ADR does not authorize implementation; it constrains it.

1. **Default OFF, tenant-wide**. Enabling it is an affirmative DPO action logged to the audit chain.
2. **Independent key hierarchy**. A new **Live-View Master Key (LVMK)** in Vault transit engine, distinct from TMK. Per-session AES-256-GCM DEK wrapped by LVMK; wrap stored in Postgres `live_view_recordings`. No shared keys with the keystroke hierarchy under any circumstances.
3. **Retention**: 30 days default; legal-hold eligible; destruction = key-destroy-then-blob-delete, matching the keystroke pattern.
4. **Playback = dual control, identical to initiation**. Same HR approval gate, same reason-code requirement, same state machine applied to playback requests. New audit entry types: `live_view.playback_requested|approved|started|ended`.
5. **DPO-only export** with watermarking and transparency-portal visibility. No bulk export, no "download MP4" button, no browser caching beyond playback buffer.
6. **Audit chain linkage**: the Phase 1 audit entry for a session reserves space for `recording_blob_ref` and `lvmk_wrap_ref` fields; Phase 1 code MUST leave them empty.
7. **Single-viewer recordings**. Multi-viewer live sessions (a separate Phase 2 feature) do not imply multi-viewer recording rights.
8. **Browser playback uses a short-lived LiveKit room or pre-signed URL**; never a permanent link.

## Consequences

- Phase 2 engineers inherit a constrained but legally defensible recording design; the key hierarchy is already isolated from the keystroke subsystem.
- Phase 1 proto fields can be added conservatively (`reserved` tags or explicit optional fields left empty); no breaking change at Phase 2.
- The audit chain already supports extending entry types without rechaining (new `entry_type` values).
- Customer DPOs get a clear "recording is off by default and requires my affirmative action" talking point for KVKK reviews.
- Regulators reviewing Personel during Phase 2 can see that the recording subsystem was designed, not retrofitted.

## Alternatives Considered

- **Leave Phase 2 design entirely undefined until Phase 2 kickoff**: rejected — invites Phase 1 decisions that foreclose good options (e.g., reusing TMK, which we explicitly forbid here).
- **Use TMK to wrap recording keys**: rejected — cryptographic blast radius conflation with keystroke subsystem.
- **Allow opt-in from Admin role (not DPO)**: rejected — too easy to turn on without KVKK review.
- **Allow direct download by Admins with watermarking**: rejected — watermarking is not a substitute for access control.
- **Store recordings on the endpoint and stream later**: rejected — creates a much larger local-storage and tamper surface.

## Related

- `docs/architecture/live-view-protocol.md` §Phase 2 Recording — Future Design Note
- `docs/architecture/key-hierarchy.md` (LVMK is explicitly NOT part of the keystroke hierarchy)
- `docs/compliance/kvkk-framework.md` §11 (Live view legal framework)
