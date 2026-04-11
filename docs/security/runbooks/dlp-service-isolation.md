# Runbook — DLP Service Isolation

> Language: English. Audience: devops-engineer, backend-developer, security on-call, compliance-auditor. Scope: the operational controls that make the claim "admins cannot read raw keystrokes" legally and technically defensible.
>
> This runbook is the **primary operational artifact** behind ADR-0009 and is amended by **ADR 0013 — DLP Disabled by Default in Phase 1**. Read `docs/architecture/key-hierarchy.md`, ADR-0009, and ADR-0013 first.

## 0. Default Off (ADR 0013)

**DLP is DISABLED by default on every new Phase 1 installation.** Everything that follows in this runbook describes the configuration and operational controls that apply **when DLP has been enabled** via the opt-in ceremony — not the default runtime state.

What a fresh install looks like:

- The `personel-dlp` container is defined in `infra/compose/docker-compose.yaml` behind the Compose profile `dlp` (`profiles: [dlp]`). Default `docker compose up` does not start it.
- The `dlp-service` Vault policy and AppRole are created during install so that the policy is reviewable and audit-visible, but **no Secret ID is issued**. Without a Secret ID there is no login, no token, and no TMK derive capability.
- The Vault audit device shows zero `transit/keys/tenant/*/tmk` derive operations over the deployment's lifetime — this is a positive, provable fact about the default state.
- The agent's keystroke content collector is disabled in the default policy bundle (`KeystrokeSettings.content_enabled = false`) until opt-in. Keystroke counts (`KeystrokeWindowStats`) continue to be collected.
- The Admin Console header badge shows `DLP: Kapalı / DLP: Disabled`. The Transparency Portal shows the employee-facing "DLP disabled" notice. The API endpoint `GET /api/v1/system/dlp-state` returns `{"state":"disabled","secret_id_present":false,"last_transition":null}` and is the single source of truth for all UI state.
- Prometheus rule `dlp_disabled_by_design` is **info-level and expected**; the alert `dlp_crashed_while_enabled` only fires if the state endpoint says `enabled` but the container is not healthy. Never alert on "DLP not running" alone.

To move from default-off to enabled, see §11 Opt-In Ceremony at the bottom of this runbook, or follow ADR 0013 directly. To move back to disabled, run `infra/scripts/dlp-disable.sh`, which revokes the Secret ID, stops the container, writes a `dlp.disabled` audit chain event, and flips the transparency banner.

The remainder of this runbook assumes the opt-in ceremony has been completed and describes how the DLP service is configured, isolated, monitored, and rotated while it is running. If DLP is in the default-off state, only §0 applies.

## 1. Purpose and Threat Boundary

Personel's product promise is that platform administrators cannot read raw keystroke content under any circumstances permitted by the shipped code and configuration. That promise is implemented as a cryptographic fence: keystroke ciphertext is decryptable only by a process that can derive DSEK from TMK, and DSEK derivation is granted to exactly one Vault identity: the DLP Service.

If DLP Service isolation is broken, the product promise is broken. The controls in this runbook exist to make breaking that isolation (a) hard, (b) noisy, and (c) bounded in blast radius.

## 2. Deployment Topology

### 2.1 Recommended (Production)

DLP Service runs on a **dedicated physical host or dedicated hypervisor VM** with no other Personel components. This is the contractually recommended deployment for any customer who relies on the "admin blind" claim in their VERBİS filing.

- Host name convention: `personel-dlp-<n>.<tenant>.internal`
- Host OS: Ubuntu 22.04 LTS, CIS Level 2 hardened.
- No interactive shell access in normal operations. Access requires break-glass (see §9).
- No SSH exposure to the admin network — SSH listens only on a management VLAN reachable from a jump host, and login requires MFA via the customer's IdP, not a local account.

### 2.2 Minimum Acceptable (MVP pilot)

For pilots where a dedicated host is not available, DLP runs as a container on the main application host under strict isolation:

- Separate Docker network namespace with its own bridge.
- No bind mounts from host except the systemd credential tmpfs.
- Runs under a dedicated user `personel-dlp` (uid 901) with no shell.
- Dropped `CAP_SYS_PTRACE`, `CAP_SYS_ADMIN`, all non-essential capabilities.
- seccomp profile applied (see §6).
- AppArmor profile `personel-dlp` in enforce mode.
- Sales contract records which deployment mode is in use; compliance-auditor reflects the choice in the customer's KVKK documentation pack.

### 2.3 Container Image

The DLP container image is built from `gcr.io/distroless/static-debian12:nonroot`. Single statically linked Go binary. No shell, no package manager, no libc surface beyond what Go uses. Read-only root filesystem, with a single writable tmpfs at `/var/cache/dlp` for the compiled pattern cache.

```yaml
# docker-compose.dlp.yml
services:
  dlp:
    image: personel/dlp:${DLP_VERSION}
    container_name: personel-dlp
    restart: unless-stopped
    user: "901:901"
    read_only: true
    tmpfs:
      - /var/cache/dlp:rw,mode=0700,size=64m,noexec,nosuid,nodev
      - /tmp:rw,mode=0700,size=16m,noexec,nosuid,nodev
    cap_drop: ["ALL"]
    security_opt:
      - no-new-privileges:true
      - seccomp=/etc/personel/dlp/seccomp.json
      - apparmor=personel-dlp
    networks:
      - dlp-net
    environment:
      VAULT_ADDR: "https://vault.internal:8200"
      NATS_URL: "nats://nats.internal:4222"
    volumes:
      - /etc/personel/dlp/tls:/tls:ro
    healthcheck:
      test: ["CMD", "/dlp", "healthcheck"]
      interval: 30s
      timeout: 5s
      start_period: 30s
    ulimits:
      core: 0
    sysctls:
      kernel.yama.ptrace_scope: 3  # host-level actually; documented here for completeness
networks:
  dlp-net:
    driver: bridge
    internal: true
    driver_opts:
      com.docker.network.bridge.enable_icc: "false"
```

Notes:
- `internal: true` — the Docker bridge has no route to the default gateway. DLP cannot initiate arbitrary outbound connections. Egress to NATS, Vault, Postgres, and MinIO is provided by explicit additional networks that each attach only the required peers.
- `core: 0` + `no-new-privileges` + read-only root + dropped caps — core dumps cannot be produced and privilege escalation cannot regain them.

## 3. Network Isolation

DLP speaks on exactly four wires. All other traffic is dropped by host firewall and by the Docker network internal flag.

| Peer | Direction | Protocol | Purpose |
|---|---|---|---|
| NATS JetStream | inbound pull (consumer) | TLS 1.3 mTLS | Subscribe to `events.v1.keystroke.content_encrypted.*` |
| NATS JetStream | outbound publish | TLS 1.3 mTLS | Publish `dlp.v1.match.*` and `dlp.v1.health.*` |
| Vault | outbound | TLS 1.3 | AppRole login, transit derive/decrypt |
| Postgres (read-replica) | outbound | TLS 1.3 | SELECT on `keystroke_keys` — read-only role |
| MinIO | outbound | TLS 1.3 | GET ciphertext blobs from `keystroke-blobs/*` — read-only access key |

Explicit denies:
- No inbound admin API or gRPC port. DLP exposes no RPC surface reachable by admin users.
- No outbound to the internet. No DNS to anything other than the internal resolver, which is locked to resolve only internal names for the allow-listed peers above.
- No outbound to the admin console or the reverse proxy. If DLP needs to raise an alert to a human, it goes through NATS `dlp.v1.match.*`, which the alerting service consumes; the alert path is one-way and never returns plaintext.

Host firewall (nftables) on the DLP host:

```nft
table inet personel-dlp {
  chain output {
    type filter hook output priority 0; policy drop;
    ct state established,related accept
    ip daddr <nats_ip>   tcp dport 4222  accept
    ip daddr <vault_ip>  tcp dport 8200  accept
    ip daddr <pg_ro_ip>  tcp dport 5432  accept
    ip daddr <minio_ip>  tcp dport 9000  accept
    ip daddr <dns_ip>    udp dport 53    accept
    log prefix "DLP-DROP-OUT: " drop
  }
  chain input {
    type filter hook input priority 0; policy drop;
    ct state established,related accept
    # SSH only from jump host, only on management VLAN
    iifname "mgmt0" ip saddr <jump_host> tcp dport 22 accept
    log prefix "DLP-DROP-IN: " drop
  }
}
```

## 4. Identity and Authentication

DLP authenticates to Vault via AppRole. The `dlp-service` policy is defined in `vault-setup.md` §4.2 — it grants only `transit/derive/tenant/+/tmk`, `transit/encrypt|decrypt/tenant/+/tmk`, and explicitly denies every other path.

SPIFFE/SPIRE was considered and rejected for MVP: introduces a whole new control plane (SPIRE server, node/workload attestation) that adds more attack surface than it removes for a single-host on-prem deployment. Vault AppRole + systemd credentials is simpler, auditable, and sufficient for Phase 1.

AppRole flow:
1. Role ID is baked into `/etc/personel/dlp/role_id` (readable only by `personel-dlp`).
2. Secret ID is delivered by a systemd `ExecStartPre=` hook that calls a tightly-scoped provisioning token held by devops, rotated on every DLP restart. The Secret ID lives in `/run/credentials/personel-dlp.service/secret_id` (tmpfs, per-service, private namespace).
3. DLP reads Role ID and Secret ID at startup, exchanges for a Vault token, then shreds the files from its own namespace.
4. DLP renews its Vault token via lease renewal; if Vault is unreachable for longer than the token TTL, DLP exits. systemd restarts it; the fresh Secret ID ritual repeats.

**No Vault credential ever appears in a container environment variable.** Any commit that adds one fails CI via a lint rule (owned by devops-engineer).

## 5. Key Access Mechanics

DLP never sees the raw TMK. All key operations are Vault round-trips.

1. On PE-DEK wrap (during agent enrollment): DLP generates a 32-byte random PE-DEK in memory, calls `transit/encrypt/tenant/<tenant_id>/tmk` with a derived context `endpoint_id||"pe-dek-v1"`, persists the returned ciphertext into `keystroke_keys.wrapped_dek`. The PE-DEK plaintext is zeroized from Go memory after transmission to the endpoint via the sealed channel.
2. On blob decrypt: DLP fetches `wrapped_dek` from Postgres, calls `transit/decrypt/tenant/<tenant_id>/tmk` with the same context to unwrap PE-DEK into process memory, fetches ciphertext blob from MinIO, performs AES-256-GCM decrypt locally, runs pattern matching, emits `dlp.v1.match` metadata, and calls `zeroize()` on the PE-DEK buffer and the plaintext buffer before returning to the worker pool.

Key material lifetime in memory is bounded by the span of a single batch (typically <200 ms). DLP never persists PE-DEK or plaintext to disk, swap, or log.

Memory hygiene:
- `mlockall(MCL_CURRENT|MCL_FUTURE)` at startup — prevents swap of any DLP memory. `IPC_LOCK` capability is granted in the Compose fragment for this reason alone.
- `memguard` (https://github.com/awnumar/memguard) for PE-DEK and plaintext buffers. Memguard uses guard pages and canary buffers and zeroes on free.
- `GODEBUG=madvdontneed=1` to hand pages back to the kernel promptly.
- `runtime/debug.SetPanicOnFault(true)` — any segfault during crypto ops crashes the process rather than silently continuing on corrupted state.

## 6. Process and Syscall Isolation

### 6.1 seccomp profile `/etc/personel/dlp/seccomp.json`

Denylist the obvious disclosure syscalls:

```json
{
  "defaultAction": "SCMP_ACT_ALLOW",
  "syscalls": [
    {"names": ["ptrace", "process_vm_readv", "process_vm_writev"], "action": "SCMP_ACT_ERRNO"},
    {"names": ["kexec_load", "kexec_file_load", "init_module", "finit_module", "delete_module"], "action": "SCMP_ACT_ERRNO"},
    {"names": ["perf_event_open", "bpf"], "action": "SCMP_ACT_ERRNO"},
    {"names": ["mount", "umount", "umount2", "pivot_root", "chroot"], "action": "SCMP_ACT_ERRNO"},
    {"names": ["reboot", "swapon", "swapoff"], "action": "SCMP_ACT_ERRNO"},
    {"names": ["setuid", "setgid", "setreuid", "setregid", "setresuid", "setresgid"], "action": "SCMP_ACT_ERRNO"}
  ]
}
```

Phase 2 plan: convert to allowlist once the Go runtime syscall surface is fully inventoried (Go's cgo-less stdlib is large; an allowlist requires test coverage we do not have yet).

### 6.2 AppArmor profile `personel-dlp`

```apparmor
profile personel-dlp flags=(attach_disconnected) {
  # Default deny.
  deny /** wklx,
  # Read-only roots.
  /usr/local/bin/dlp rix,
  /etc/personel/dlp/** r,
  /tls/** r,
  # Writable tmpfs.
  /var/cache/dlp/** rw,
  /tmp/** rw,
  # Network and standard runtime.
  network inet stream,
  network inet6 stream,
  /proc/sys/net/core/somaxconn r,
  /proc/self/** r,
  # Deny all /proc and /sys access that could leak memory of other processes.
  deny /proc/*/mem rwklx,
  deny /proc/*/maps r,
  deny /sys/kernel/debug/** rwklx,
}
```

### 6.3 Host ptrace_scope

`/etc/sysctl.d/99-personel-dlp.conf`:

```
kernel.yama.ptrace_scope = 3
```

`ptrace_scope=3` means even root cannot ptrace without reboot. Kernel module loading is disabled via `kernel.modules_disabled=1` on the DLP host.

### 6.4 Runtime constraints summary

| Constraint | Mechanism | Honest limit |
|---|---|---|
| Cannot write to screen or console | read-only root fs; no tty; no logger to host stdout | an attacker with root on the host can still attach a kernel debugger pre-boot |
| Cannot open outbound network except allowlist | nftables + Docker internal network | root on host bypasses nftables |
| No debugger attach | ptrace_scope=3 + seccomp deny ptrace + yama | kernel compromise bypasses |
| No core dump | ulimits, PR_SET_DUMPABLE=0 at startup, disabled in container | kernel compromise bypasses |
| No debug endpoints exposed | no HTTP or gRPC listener on DLP | — |
| Keystroke plaintext never persisted | memguard, zeroize, mlockall | cold-boot attack on RAM if physical access |

## 7. Pattern Update Flow

DLP matching rules (TCKN, IBAN, Luhn credit card, tenant regex rules) are not compiled into the binary. They are loaded from a signed rule bundle at startup and reloadable via a signed reload notification.

### 7.1 Rule bundle format

```
rulebundle_v1 {
  bundle_id: uuid
  tenant_id: uuid
  version: monotonic u64
  not_before: rfc3339
  not_after: rfc3339
  rules: repeated Rule
  signature: ed25519 over canonical encoding
  signing_key_id: string
}
```

### 7.2 Authoring and deployment

1. Rule is drafted by the tenant DPO via Admin Console. Drafting requires the `dpo` role and an approval workflow from a second DPO or the customer security officer (dual control).
2. On save, Admin API serializes the bundle, submits it to the Control-Plane Signing Service (which holds the control signing key — same hierarchy as policy bundles and pin updates, see `auto-update-signing.md` §4), receives the Ed25519 signature.
3. Signed bundle is published to NATS subject `dlp.v1.rulebundle.available` with the bundle contents inline (small; bundles are typically <50 KB).
4. DLP subscribes to this subject. On receipt, DLP verifies the Ed25519 signature against the baked-in control-signing public key set, validates schema, checks `version` is strictly greater than the currently loaded version (monotonic), and validates `not_before <= now <= not_after`.
5. If all checks pass, DLP compiles the new bundle into its pattern cache (regex → automaton) in a separate goroutine, swaps the active bundle pointer atomically, and emits `dlp.v1.rulebundle.activated` on NATS.
6. Old bundle is retained for 60 seconds to let in-flight batches complete, then zeroed.

### 7.3 Audit trail

Every rule bundle authoring, signing, publishing, and activation event emits a corresponding entry to the admin audit log (see `admin-audit-immutability.md` §7, actions `dlp.rule.drafted`, `dlp.rule.approved`, `dlp.rule.signed`, `dlp.rule.activated`). Vault audit device records the signing call.

### 7.4 Failure modes

- Bundle signature invalid → DLP logs `dlp.tamper_detected`, keeps the previous bundle, emits a critical alert.
- Bundle version monotonicity violated (downgrade attempt) → rejected, alert.
- Bundle parse error → rejected, alert, previous bundle remains active.

## 8. Failure and Compromise Modes

### 8.1 DLP offline

- Encrypted keystroke blobs continue to be written to MinIO by agents normally.
- Metadata events accumulate on NATS JetStream with its retention buffer (configured 7 days minimum; see `event-taxonomy.md`).
- DLP pattern match alerts are delayed but not lost.
- Retention clocks are unaffected because they run on the metadata timestamps, not match timestamps.
- Gateway, agent, admin API are unaffected.

SLO: DLP availability target 99%, downtime budget 7.2h/month. Incident fires at >15 min DLP unavailability.

### 8.2 DLP compromised (attacker executes code in DLP process)

Blast radius, by design:
- Attacker can decrypt the keystroke blobs currently streaming through DLP during the compromise window.
- Attacker can read the pattern rules currently loaded.
- Attacker can call `transit/decrypt` for any endpoint's wrapped DEK, because the Vault policy grants it. This is the irreducible power DLP must have.
- Attacker **cannot** export TMK (policy denies `transit/export`).
- Attacker **cannot** reach the admin console, admin API, or any user-facing surface (no network path).
- Attacker **cannot** persist their access across a restart unless they also compromised the Vault AppRole Secret ID delivery path, which is rotated per restart via systemd credentials.
- Attacker **cannot** exfiltrate via a human-readable channel — the only outbound is NATS `dlp.v1.match.*`, and the match schema is fixed (no free-form fields). An attacker could encode plaintext into match metadata, but the schema enforces byte-length limits and redacted snippet formats (see `key-hierarchy.md` §DLP match). An adversary who can modify DLP code can bypass this, but match events are audited and a surge in match rate on one rule is a detection signal.

Containment on suspected compromise:
1. Cut DLP's Vault token via `vault token revoke -accessor <accessor>` (1 minute) — blocks all further decrypts.
2. Cut DLP's NATS publish rights — blocks data exfil via match stream.
3. Snapshot the DLP host for forensics.
4. Rotate TMK (`transit/keys/tenant/<tenant_id>/tmk/rotate`) — new blobs use a new key version, old blobs are re-wrapped by a controlled batch job or destroyed per retention.
5. Rotate PE-DEKs cluster-wide — new enrollment wrap for every active endpoint. Agents receive rewrapped PE-DEK via the existing rekey control message.

Full procedure is in `incident-response-playbook.md` §5.

### 8.3 Vault compromised

DLP becomes powerless because every key op is a Vault round-trip. See `incident-response-playbook.md` §4.

## 9. Break-Glass Access to the DLP Host

Interactive access is forbidden in normal ops. Break-glass ritual:

1. Requester files an incident ticket citing cause.
2. Two-party approval (security-engineer + customer security officer) via incident channel.
3. Time-bound SSH access granted via a short-lived SSH certificate issued by `step-ca` with a 1-hour TTL.
4. Session is recorded (tmux session log shipped in real time to the audit DB).
5. On session close, the SSH CA certificate is revoked.
6. Post-incident review required within 5 business days.

Every break-glass session produces an `admin.breakglass.dlp_host.opened` and `admin.breakglass.dlp_host.closed` pair in the admin audit log (see `admin-audit-immutability.md` §7).

## 10. Threat Model Table

| Attacker | Capability | Can exfil keystrokes? | Why not |
|---|---|---|---|
| Platform admin with console access | Full admin API, full console UI | No | Admin API proto has no decrypted keystroke surface; admin's Vault policy denies transit derive; admin has no network path to DLP. |
| Platform admin with read-only DB access | SELECT on Postgres | No | `keystroke_keys.wrapped_dek` is wrapped with DSEK which requires TMK derive via Vault, which admin cannot call. |
| Platform admin with backup tarball access | Full Postgres + MinIO backup | No | Same. Backup includes ciphertext + wrapped DEKs, but TMK lives only in Vault and is not in backups. |
| Malicious insider operator with host-level root on the application host | Kernel and memory access on the main host | No (if DLP is on a dedicated host per §2.1) | DLP process runs elsewhere; insider cannot reach DLP memory. |
| Malicious insider operator with host-level root on the DLP host | Kernel and memory access on the DLP host | **Yes** within the compromise window | Honest limit: root on the DLP host can read DLP memory and see currently-in-flight plaintext. Mitigations: dedicated host reduces the population who have this access; break-glass ritual produces a paper trail; DPO is alerted on any interactive session. |
| Attacker who stole a laptop with Vault unseal shares | 1 of 5 shares | No | Shamir threshold is 3; one share is inert. |
| Attacker who stole ≥3 unseal shares and the break-glass token | Full Vault | Yes, eventually | Honest limit: this is the worst case. `incident-response-playbook.md` §4 is the response. Rotate TMK, re-enroll all endpoints. |
| Compromised Admin API | Full Postgres read, full Admin API identity | No | Admin API has no Vault transit derive policy; even if attacker injects code, the Vault policy fence holds. |
| Compromised Gateway | Full agent stream | No | Gateway never sees decrypted keystroke content; it just routes ciphertext blobs to NATS. Its Vault policy denies transit derive. |
| Compromised DLP Service | Full DLP capabilities | Yes, in-flight only; cannot export TMK | Blast radius detailed in §8.2. Bounded by Vault policy and by audit surface. |
| Compromised customer endpoint | The local employee's own machine | Yes, **for that one machine** | Documented design boundary per threat-model.md residual risk 1. |
| Lawful intercept request | Legal order on the customer | No technical back door | Personel has no feature for decrypting keystrokes for law enforcement. The customer, as data controller, must respond to legal orders; our architecture offers no bypass. |

## 11. Compliance Framing (KVKK)

The DLP isolation controls directly support three KVKK obligations:

1. **KVKK m.12 güvenlik yükümlülüğü (security obligation)**: encrypted-at-rest, encrypted-in-transit, role-based cryptographic access enforced by independent infrastructure (Vault), separation of duties between admin and DLP, auditability.
2. **KVKK m.6 özel nitelikli veri (special-category data)**: keystroke content can contain passwords, credentials, and potentially health or financial information. Article 6 requires additional technical and administrative measures. The DLP isolation architecture is the "additional technical measure" described in the VERBİS filing.
3. **KVKK m.7 silme/yok etme (deletion/destruction)**: cryptographic destruction via TMK version deletion is a defensible silme mechanism. Data that cannot be decrypted because its key is destroyed is no longer personal data in any practical sense.

The product claim "admins cannot read keystrokes" is quotable in a Turkish court subject to the following attestations (to be produced by compliance-auditor):

- The Vault policy files in `/etc/personel/vault/policies/` match the versions committed in the Personel monorepo at the time of installation.
- The DLP container image matches its reproducible build hash recorded in the release manifest.
- The seccomp profile, AppArmor profile, and nftables rules match the versions in this runbook.
- The admin audit log shows no `dlp-service` AppRole login from a non-DLP source.
- The Vault audit device shows every transit derive/decrypt call, correlated with a corresponding `dlp.v1.match` NATS message or enrollment ceremony.

A nightly conformance check runs against all of the above and writes a daily compliance attestation to `/var/log/personel/compliance/dlp-attest-<date>.json`, signed with the control signing key. This attestation is the evidence a lawyer hands to the court.

## 12. Handoffs

- **devops-engineer**: owns Docker Compose fragment, nftables rules, AppArmor profile, systemd unit, seccomp profile, break-glass ritual automation.
- **backend-developer**: owns the DLP Go service itself — in particular the memguard usage, Vault client code, and the `dlp.v1.match` schema enforcement. MUST NOT add any RPC that returns plaintext.
- **compliance-auditor**: owns the daily attestation pipeline, the KVKK VERBİS narrative, and the court-ready evidence pack.
- **rust-engineer**: owns the agent side of the PE-DEK sealed channel handshake and the zeroize discipline on the endpoint.
