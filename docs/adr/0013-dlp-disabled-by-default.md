# ADR 0013 — DLP Service Disabled by Default in Phase 1

## Status

Accepted — 2026-04-11. Amended — 2026-04-11 (items 1–5 below, added to Implementation Follow-Up after architect propagation surfaced them).

Supersedes nothing. Amends the operational defaults documented in `docs/architecture/dlp-deployment-profiles.md`, `docs/architecture/key-hierarchy.md`, and `docs/security/runbooks/dlp-service-isolation.md`.

## Context

Decision 4 (2026-04-11 locked decisions) states: "Keystroke content is captured but encrypted end-to-end; admins cryptographically cannot read raw keystrokes; only the isolated DLP engine can decrypt for pattern matching."

The Phase 1 architecture realises this by:

1. Agent encrypts keystroke content with a per-endpoint data key (PE-DEK) wrapped by a DLP service key (DSEK) derived from the tenant master key (TMK).
2. TMK lives in HashiCorp Vault transit engine, `exportable: false`, and only the `dlp-service` Vault AppRole can derive DSEK.
3. The DLP service runs in a hardened container (Profile 1 per ADR-amended `dlp-deployment-profiles.md`) with distroless base, seccomp denylist, AppArmor, read-only filesystem, memguard, mlockall, ptrace_scope=3, isolated network.
4. The admin console and admin API have zero Vault policy granting TMK derive; the legal claim is "admins cannot read keystrokes by construction."

Security review (April 2026) surfaced three residual risks even with this design:

- **R1 — DLP service runtime compromise**: If an attacker achieves code execution inside the DLP container, they can read DSEK from memory and decrypt keystroke content for the duration of the compromise. All hardening controls reduce probability but cannot eliminate this class of failure.
- **R2 — Vault policy misconfiguration**: A human error during install, upgrade, or secret rotation that grants TMK derive to a non-DLP role would silently break the cryptographic guarantee without any runtime symptom. The Phase 1 install runbook is 7000+ lines; policy file drift is plausible.
- **R3 — Container escape**: A kernel-level or runtime-level escape from the hardened container onto the host would expose DSEK to any process with host-level access, including customer BT admins.

All three risks are "low probability, catastrophic legal impact." They are acceptable for a feature that delivers disproportionate value. They are not acceptable for a feature that runs by default on every deployment, because the first KVKK incident involving a Personel deployment will put the entire product's legal defensibility on trial — including deployments that never actively used DLP.

## Decision

**DLP is DISABLED by default in Phase 1 on every new installation.**

Specifically:

1. The `install.sh` installer does NOT start the `personel-dlp` container. The compose profile for DLP is marked `profiles: [dlp]` and is not activated by the default `docker compose up`.
2. The `dlp-service` Vault AppRole is CREATED during install (so the policy exists and is audit-reviewable) but NO Secret ID is issued. Without a Secret ID, the DLP service cannot authenticate to Vault even if its process is started.
3. The agent's keystroke collector STILL runs, but in the default state it emits only statistical metadata (`KeystrokeWindowStats` — tuş sayıları, pencere atıfları, zamanlama) and does NOT emit content. This is because PE-DEK is bootstrapped by the DLP service during enrollment; in the default state there is no DLP service, no PE-DEK exists, and therefore no key is available to encrypt content. The policy signer enforces the structural invariant `dlp_enabled=false ⇒ keystroke.content_enabled=false` and refuses to sign bundles that violate it. When DLP is opted in, `dlp-enable.sh` bootstraps PE-DEKs for all already-enrolled endpoints and delivers them via the sealed enrollment channel on next stream open.
4. When DLP is enabled and content ciphertext exists, the sensitive-flagged retention policy (14 days) applies; ciphertext is destroyed on schedule. If DLP is later disabled via `dlp-disable.sh`, existing ciphertext is NOT destroyed immediately — it is allowed to age out naturally via the 14-day TTL. This preserves forensic continuity for incidents that started before the disable and avoids coupling the disable operation to blob storage mutations.
5. The admin console displays the DLP state prominently in both the dashboard header ("DLP: Kapalı / DLP: Disabled") and the Settings > DLP panel. The panel explains the cryptographic guarantee and the opt-in ceremony.

### PE-DEK Bootstrap for Already-Enrolled Endpoints

When DLP is opted in on a deployment that already has enrolled agents, PE-DEKs do not yet exist for those endpoints. The opt-in script handles this by:

1. After issuing the DLP Secret ID and starting the container, the script calls an admin API endpoint (`POST /api/v1/system/dlp-bootstrap-keys`) that iterates all enrolled endpoints.
2. For each endpoint, the DLP service generates a fresh PE-DEK, wraps it with DSEK, and stores the wrapped PE-DEK in Postgres keyed by `endpoint_id`.
3. The wrapped PE-DEK is delivered to the agent on its next stream open via the sealed X25519 enrollment channel (the same path used at initial enrollment).
4. The agent unseals it, stores it in the TPM-bound keystore, and from that point forward begins emitting encrypted keystroke content alongside the existing stats.
5. An audit event `dlp.pe_dek_bootstrapped` is written for each endpoint.

If the bootstrap fails partway (e.g., gateway loses connection to an endpoint), subsequent stream opens from that endpoint will trigger the bootstrap again — the flow is idempotent.

### Opt-In Ceremony Rollback Semantics

If any step of `dlp-enable.sh` fails after the point of no return (Vault Secret ID issued), the script MUST execute rollback in reverse order:

1. `docker compose --profile dlp down` — stop the DLP container
2. Revoke the Secret ID via Vault API
3. Write a `dlp.enable_failed` audit event with the failure reason (this event is on the same hash chain as the original `dlp.enabled` event, preserving integrity)
4. Notify the transparency portal to NOT surface the "DLP activated" banner
5. Leave the Vault policy and AppRole in place (they existed before opt-in)

The script must be idempotent — re-running after a failed attempt is safe, and each re-run is audited.

### Opt-In Ceremony

A customer DPO activates DLP by following this documented ceremony, which is recorded as an audit trail event:

1. **DPIA amendment**: Customer DPO updates their DPIA to include active DLP processing. Signed off by legal counsel.
2. **Written sign-off**: DPO + IT Security Director + Legal Counsel sign a one-page form (template in `docs/compliance/dlp-opt-in-form.md`, to be created by compliance-auditor follow-up).
3. **Vault Secret ID issuance**: A Personel operator with `vault-admin` role runs `infra/scripts/dlp-enable.sh`. The script:
   - Verifies the written sign-off file is present at `/var/lib/personel/dlp/opt-in-signed.pdf`.
   - Issues a single-use AppRole Secret ID to the DLP service.
   - Writes a `dlp.enabled` event to the append-only audit chain with the sign-off file hash.
   - Starts the DLP container via `docker compose --profile dlp up -d`.
   - Validates the DLP service successfully authenticates to Vault and begins processing the sensitive NATS subject.
4. **Transparency notification**: The employee transparency portal automatically surfaces a banner: "DLP aktif edildi: [tarih]" so every employee can see when keystroke content began being decrypted by the DLP engine.
5. **Audit checkpoint**: The enable event becomes part of the daily hash-chained audit checkpoint and is carried in the external WORM sink.

### Opt-Out

Customer can disable DLP at any time by running `infra/scripts/dlp-disable.sh`, which revokes the Secret ID, stops the container, writes a `dlp.disabled` event to audit, and surfaces a banner in the transparency portal.

## Consequences

### Positive

- **Default-safe**: The modal deployment has zero keystroke decryption path active. The legal claim "this system is keystroke-blind by default" is trivially true in the standard configuration, without relying on any argument about container hardening, policy correctness, or kernel integrity.
- **Compliance-forward marketing**: "Varsayılan olarak keystroke-blind olan tek UAM" becomes a defensible tagline. No competitor can match it, because none of them ship DLP off by default.
- **Reduced legal blast radius**: If an incident occurs on a Personel deployment that has DLP disabled, the first-line legal defense is "no process on this system has ever held the keystroke decryption key" — provable from Vault audit logs showing zero Secret ID issuance.
- **Forced compliance checkpoint**: Every customer that chooses to enable DLP executes a documented, signed ceremony. This creates a compliance artifact that strengthens the customer's own KVKK defensibility in future disputes.
- **Reduced attack surface on pilot deployments**: Phase 1 pilot customers evaluate the full product without running the DLP service, shrinking the pilot's attack surface by one critical trust boundary.

### Negative

- **Pilot customers lose immediate DLP functionality**: A customer who wants to trial DLP on day 1 must schedule the opt-in ceremony. Typical delay: 1-3 business days depending on their internal legal review speed. Sales must set this expectation explicitly.
- **Storage overhead without value**: Encrypted keystroke content is still captured and stored for 14 days even though it is never read in the default configuration. Storage cost is bounded (sensitive retention is short) but non-zero. **Mitigation**: Phase 1 exit criterion #18 (new) — validate that disabling keystroke content capture entirely is available as a second-level opt-out in the policy engine for customers who never plan to enable DLP.
- **Increased install test surface**: Both "DLP off" and "DLP on after ceremony" paths must be tested in the QA framework. **Mitigation**: Add a dedicated e2e test for the opt-in ceremony flow to `apps/qa/test/e2e/dlp_opt_in_test.go`.
- **Operational complexity**: Operators must know that "DLP is not running" is an expected state, not a monitoring alert. **Mitigation**: Prometheus alert rules must distinguish "DLP disabled by design" from "DLP service crashed."

### Neutral

- The threat model remains unchanged; this decision is a defense-in-depth layer on top of the existing Profile 1 and Profile 2 deployment models, not a replacement for them.
- The key hierarchy documentation is unaffected at the cryptographic level; the change is in defaults, not structure.

## Alternatives Considered

1. **Keep DLP enabled by default with enhanced hardening**: Rejected. Hardening is already strong; adding more controls (e.g., hardware attestation, formal verification of the container) hits diminishing returns and cannot address R2 (policy misconfiguration) at all.
2. **Dedicated host for DLP from Phase 1**: Rejected. This addresses R3 (container escape) but not R1 (runtime compromise) or R2 (policy misconfiguration). It also adds hardware cost ($500-2000/pilot) and delays install, while not providing the "default-safe" property that the opt-in model gives for free.
3. **Remove keystroke content capture entirely from Phase 1**: Rejected. This would eliminate DLP as a product feature for the foreseeable future, destroying a major differentiator against Teramind/Safetica. The opt-in model preserves the feature without running it by default.
4. **Two-person authorization for each DLP decryption call**: Rejected. Incompatible with real-time DLP matching; the DLP engine processes thousands of events per second across all endpoints, and per-call authorization is infeasible.
5. **Homomorphic or zero-knowledge pattern matching**: Rejected as out of scope for Phase 1. Research-grade, not production-ready for the breadth of pattern matching Personel needs. Revisit in Phase 3.

## Implementation Follow-Up

Owner: microservices-architect (propagation to architecture docs — **done 2026-04-11**), devops-engineer (installer + scripts), backend-developer (API surfacing state), frontend-developer (console + portal UI), compliance-auditor (opt-in form template), rust-engineer (collector gating + PE-DEK bootstrap handshake).

**Amendment items** added after architect propagation surfaced gaps in the original decision text:

- **A1** — Decision §3 was originally ambiguous about whether the agent emits encrypted keystroke content in the default state. It does NOT. The agent's keystroke collector runs but emits only `KeystrokeWindowStats`; content emission is gated on `dlp_enabled=true`. The policy signer refuses bundles violating this invariant. Owner: rust-engineer enforces the collector gate; backend-developer enforces the signer invariant.
- **A2** — PE-DEK bootstrap flow for already-enrolled endpoints on opt-in is now specified (see "PE-DEK Bootstrap for Already-Enrolled Endpoints" above). Owner: devops-engineer wires the script; backend-developer owns the `POST /api/v1/system/dlp-bootstrap-keys` endpoint; rust-engineer handles the sealed channel receive.
- **A3** — Opt-in ceremony rollback semantics now specified (see "Opt-In Ceremony Rollback Semantics" above). Owner: devops-engineer.
- **A4** — `dlp-disable.sh` does NOT destroy already-captured ciphertext; it relies on the 14-day TTL to age out blobs naturally. Owner: devops-engineer documents this in the opt-out runbook.
- **A5** — Policy signer code-level invariant: any `PolicyBundle` where `dlp_enabled=false` AND `keystroke.content_enabled=true` MUST be rejected at sign time with a typed error. Owner: backend-developer adds this rule to the policy signer and a CI test enforces it.

Specific changes required:

- `docs/architecture/dlp-deployment-profiles.md`: add "Default State" section at the top stating DLP is disabled.
- `docs/architecture/key-hierarchy.md`: update the "Runtime Guarantees" section to state that in default configuration, DSEK is never derived.
- `docs/architecture/mvp-scope.md`: change "DLP with 20+ built-in rules" from a Phase 1 **exit** feature to a Phase 1 **opt-in** feature; add exit criterion #18 validating the opt-in ceremony end-to-end.
- `docs/compliance/kvkk-framework.md` §10: update the legal claim language. Default-state claim: "no process holds the decryption key". Enabled-state claim: unchanged (existing "DLP service is the only process that can decrypt").
- `docs/compliance/dlp-opt-in-form.md`: new document to be created (compliance-auditor follow-up).
- `docs/security/runbooks/dlp-service-isolation.md`: add "Default Off" section at the top.
- `docs/security/runbooks/vault-setup.md`: change the `dlp-service` policy and AppRole to state that Secret ID is NOT issued during install.
- `infra/compose/docker-compose.yaml`: add `profiles: [dlp]` to the DLP service; default startup excludes it.
- `infra/install.sh`: create the Vault policy and AppRole but do not issue Secret ID; print a clear message that DLP is disabled by default and the enable script must be run separately.
- `infra/scripts/dlp-enable.sh`: new script implementing the opt-in ceremony.
- `infra/scripts/dlp-disable.sh`: new script implementing the opt-out.
- `apps/api/`: expose DLP state endpoint (`GET /api/v1/system/dlp-state`); audit every state transition.
- `apps/console/`: header badge showing DLP state; settings panel explaining the ceremony.
- `apps/portal/`: banner showing DLP state to employees; historical timeline of state transitions.
- `apps/qa/test/e2e/dlp_opt_in_test.go`: new e2e test covering the full ceremony.
- New Phase 1 exit criterion #18: "DLP opt-in ceremony executable end-to-end within 1 hour including all sign-off verification steps, validated on staging."

## References

- Locked decisions 2026-04-11 — decision 4 (keystroke cryptographic isolation)
- `docs/adr/0009-keystroke-content-encryption.md`
- `docs/architecture/dlp-deployment-profiles.md`
- `docs/security/runbooks/dlp-service-isolation.md`
- `docs/security/runbooks/vault-setup.md`
- `docs/compliance/kvkk-framework.md` §10 (Cryptographic Guarantee of Employee Privacy)
