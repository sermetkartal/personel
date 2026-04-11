# DLP Service Deployment Profiles

> Language: English. Status: Authoritative. Resolves Conflict B between compliance-auditor and security-engineer reviews. Amended by **ADR 0013 — DLP Disabled by Default in Phase 1** (2026-04-11).

## Default Operational State (ADR 0013)

**DLP is DISABLED by default on every new Phase 1 installation.** This section takes precedence over anything else in this document: the profiles below describe **how** DLP is deployed when a customer opts in, not **whether** it runs out of the box.

Concrete defaults on a fresh install:

- The `personel-dlp` container is defined in `infra/compose/docker-compose.yaml` behind the Compose profile `dlp` (`profiles: [dlp]`). The default `docker compose up` does **not** start it.
- The `dlp-service` Vault AppRole and policy are created at install time (so the policy is reviewable and audit-visible) but **no Secret ID is issued**. Without a Secret ID, the DLP process cannot authenticate to Vault and cannot derive DSEK even if started manually.
- The agent still encrypts keystroke content and the gateway still stores ciphertext under `minio://sensitive/keystroke/…` with the 14-day sensitive-flagged TTL. Ciphertext is simply never decrypted because no process holds the derivation path.
- Admin Console and Transparency Portal surface the DLP state (`Kapalı / Disabled`) in the header badge, Settings → DLP panel, and Portal banner respectively.
- Prometheus alerting treats "DLP not running" as the **expected** state; a separate alert `dlp_crashed_while_enabled` distinguishes failure from policy.

Profile selection (Profile 1 vs Profile 2) becomes relevant only **after** a customer executes the opt-in ceremony documented in ADR 0013 §Opt-In Ceremony. Until then, the choice is irrelevant because no DLP process is running in either shape.

### Opt-In Ceremony (reference)

Full definition in ADR 0013. Short form:

1. Customer DPO amends their DPIA to include active DLP processing.
2. DPO + IT Security Director + Legal Counsel sign `docs/compliance/dlp-opt-in-form.md` (template owned by compliance-auditor); the signed PDF is placed at `/var/lib/personel/dlp/opt-in-signed.pdf`.
3. A Personel operator runs `infra/scripts/dlp-enable.sh`, which verifies the sign-off file, issues the single-use AppRole Secret ID, writes a `dlp.enabled` audit chain event with the sign-off file hash, starts the container (`docker compose --profile dlp up -d`), and validates end-to-end processing on the sensitive NATS subject.
4. Transparency Portal surfaces a banner `DLP aktif edildi: <tarih>` to every affected employee.
5. The enable event is covered by the next daily audit checkpoint and carried to the external WORM sink.

Opt-out via `infra/scripts/dlp-disable.sh` — revokes the Secret ID, stops the container, writes a `dlp.disabled` audit event, surfaces a Portal banner.

### Which Profile When Enabled?

Once opted in, the customer's environment determines the profile:

- **Profile 1 (Hardened Container)** — Phase 1 pilot, small/mid-market; described below as "when enabled."
- **Profile 2 (Dedicated Host)** — Phase 2 GA default, regulated sectors; described below as "when enabled."

Everywhere below, read "DLP runs" as "DLP runs **when enabled via the opt-in ceremony**."

## Background

The DLP service is the sole component able to decrypt keystroke content (see `key-hierarchy.md`, `adr/0009-keystroke-content-encryption.md`). How we deploy it directly determines how strong the legal claim "admins cannot read keystrokes, by construction" can be.

Two legitimate positions surfaced during Phase 0 review:

- **Compliance-auditor**: Strongest legal defensibility needs the DLP service on a **dedicated host** separate from the admin plane. Container-only runs risk a sufficiently privileged operator extracting DSEK from memory on the shared host.
- **Security-engineer**: For Phase 1 pilot (500 endpoints, single-tenant, on-prem), a hardened container is defensible if every non-negotiable control is enforced. Dedicated host adds hardware and deployment complexity that a pilot customer should not have to absorb.

This document adopts both positions as **tiered profiles** and makes the trade-offs explicit.

## Profile 1 — Hardened Container (Phase 1 default **when opted in**, pilot-eligible)

**Note**: Per ADR 0013, DLP is off by default. This profile describes the runtime shape that applies **after** the customer completes the opt-in ceremony.

**Target**: Phase 1 pilot; single-tenant on-prem deployment; 500–2 000 endpoints; customer DPO has accepted the container-profile legal language in the contract addendum and has executed the ADR 0013 opt-in ceremony.

**Host layout**: When enabled, DLP runs as a container on the same Docker host as the rest of the application plane (separate from the data plane). Network-isolated via a dedicated Docker network; only NATS subscribe, Postgres read of `keystroke_keys`, MinIO read, and Vault login are permitted.

**Non-negotiable hardening controls** (Phase 1 release gate rejects any build that disables these):

| Control | Value |
|---|---|
| Base image | `gcr.io/distroless/static:nonroot` or equivalent; no shell |
| User | Non-root, UID 65532; `USER` set in Dockerfile |
| Filesystem | `read_only: true`; tmpfs for `/tmp` only |
| Capabilities | Drop ALL; no `cap_add` |
| `no-new-privileges` | Enabled |
| seccomp | Custom profile denying `ptrace`, `process_vm_readv`, `process_vm_writev`, `kcmp`, `perf_event_open`, `bpf`, `userfaultfd` |
| AppArmor / SELinux | Confined profile; deny read of `/proc/*/mem` and `/proc/*/maps` from other containers |
| Host sysctl | `kernel.yama.ptrace_scope=3` (no attach ever) |
| nftables | Egress allowlist: NATS, Postgres, MinIO, Vault only; deny all other egress |
| Memory locking | `mlockall(MCL_CURRENT|MCL_FUTURE)`; `CAP_IPC_LOCK` granted solely for this purpose via specific sysctl, not cap_add |
| Core dumps | `ulimit -c 0`; `prctl PR_SET_DUMPABLE 0` |
| Swap | Host swap disabled OR DLP cgroup `memory.swap.max=0` |
| Secrets delivery | Vault AppRole Secret ID via systemd credentials (tmpfs), **never env vars** |
| Sensitive buffers | `memguard`-style locked pages; zeroize on drop |
| Logs | Redaction filter; no plaintext ever serialized; logs shipped to host journald, not files |
| Runtime monitoring | Falco rule alerting on any shell/exec attempt into the DLP container |

**Defensible legal claim (container profile)**:

> "Admins cannot read keystrokes under the standard deployment's hardening assumptions, which are enumerated and externally auditable. The list of enforced controls is reproduced in the customer's contract addendum and is verified by an annual red-team exercise (Phase 1 exit criterion #9)."

This is slightly weaker than "by construction" because an operator with root on the shared host can, in principle, defeat user-space memory protection. The controls above raise the cost of that attack enough to make it detectable and auditable, not impossible.

## Profile 2 — Dedicated Host (Phase 2 default **when opted in**, GA-required for strict customers)

**Note**: Per ADR 0013, DLP is off by default. This profile describes the runtime shape that applies **after** the customer completes the opt-in ceremony; the "default" label refers to the dedicated-host topology being the expected shape under which a Phase 2 customer opts in, not to DLP itself being on.

**Target**: Phase 2 and beyond. Any customer whose KVKK posture requires the strongest claim; any customer above 2 000 endpoints; any regulated-sector customer (banking, public sector, healthcare).

**Host layout**: When enabled, DLP runs on a **dedicated physical or virtual machine** that hosts **nothing else from the Personel stack**. The machine has its own hardware profile, its own operator credentials, and is classified by the customer DPO as a "high-value asset" with corresponding host security (HIDS, FIM, restricted SSH, break-glass logging).

**Additional controls beyond Profile 1**:

| Control | Value |
|---|---|
| Host isolation | Dedicated VM or physical node; no other Personel workloads; no shared admin accounts |
| SSH access | Disabled by default; break-glass via customer PAM with recording |
| Operator separation | Personel operator accounts for this host are disjoint from accounts on the main application host |
| Host firewall | Inbound: nothing. Outbound: NATS, Postgres, MinIO, Vault on explicit IPs only |
| HIDS | Wazuh or equivalent; tamper alerts fed to customer SIEM |
| FIM | File integrity monitoring on `/usr/bin/personel-dlp`, AppRole file, seccomp profile |
| Backups | No backups of the DLP host (stateless); any exception is a red flag |
| Compute | Dedicated CPU / memory; no noisy-neighbor risk from shared host |

**Defensible legal claim (dedicated-host profile)**:

> "Admins cannot read keystrokes, by construction. The decryption environment is a dedicated, hardened host under separate operational control; raw keystroke content is cryptographically inaccessible from the admin plane and physically separated from any admin-accessible surface."

This matches the wording compliance-auditor wants for banking/regulated customers.

## Comparison

| Dimension | Profile 1 (Container) | Profile 2 (Dedicated Host) |
|---|---|---|
| Phase availability | Phase 1 + Phase 2 | Phase 2 |
| Hardware cost | 0 (shared host) | +1 VM/host |
| Deployment complexity | Standard Compose | Two-host install procedure |
| Operator burden | One host to patch | Two hosts to patch |
| Legal claim strength | "Enumerated hardening controls" | "By construction" |
| Target customer | Pilot, SMB, mid-market | Regulated sectors, large enterprise |
| Default in contract template | Container | Dedicated host (for new contracts after Phase 2 GA) |

## Upgrade Path (Profile 1 → Profile 2)

A customer on Profile 1 can migrate to Profile 2 without downtime:

1. Install the DLP host per the Profile 2 procedure.
2. Issue a new `dlp-service` Vault AppRole bound to the new host identity.
3. Drain the container-profile DLP by stopping new NATS deliveries (consumer pause).
4. Let the in-flight match pipeline finish.
5. Start the dedicated-host DLP; resume consumer.
6. Revoke the container-profile AppRole.
7. Update the contract addendum to the dedicated-host language.

No data migration is required because DLP is stateless — wrapped DEKs live in Postgres, ciphertext in MinIO.

## Contract Addendum Hook

The compliance-auditor owns the contract addendum templates. Both profiles have distinct addendum paragraphs. Customer DPO must sign the profile-appropriate addendum at install time; the signed addendum is referenced by the VERBİS registration.

## Operational Verification

For both profiles, Phase 1 exit criterion #9 (red team) must verify that a hypothetical admin attacker cannot retrieve plaintext keystrokes. For Profile 2, the red team additionally verifies host separation. The red-team report is filed as a controlled artifact and is referenced in any legal proceeding per `docs/compliance/kvkk-framework.md` §10.3.

## Related

- `docs/architecture/key-hierarchy.md` §Trust Model and §DLP Service Isolation Requirements
- `docs/adr/0009-keystroke-content-encryption.md`
- `docs/security/runbooks/dlp-service-isolation.md`
- `docs/security/security-architecture-decisions.md`
- `docs/compliance/kvkk-framework.md` §10.4
