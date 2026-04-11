# Security Architecture Decisions

> Language: English. Scope: decisions made by security-engineer while turning the architect's high-level security design into operational runbooks. Each decision records the alternatives considered, the choice, and the reason. Decisions marked "candidate ADR" warrant a formal ADR in `docs/adr/`.

## SD-1 — PKI ceremony tooling: `step-ca` + Vault PKI

**Options**: `openssl` alone; `cfssl`; `step-ca`; HashiCorp Vault PKI only.

**Decision**: `step-ca` for the air-gapped root ceremony, Vault PKI for all online operations.

**Rationale**: step-ca has first-class Ed25519 and ECDSA P-384 support, a clean CLI that is auditable line-by-line, and does not require running as a service (we use it only during the ceremony). Vault PKI is already required for online cert issuance. Using Vault for the offline ceremony would require exporting the root key in a non-standard way. openssl is error-prone for CA templates. cfssl is effectively unmaintained.

**Candidate ADR**: No. Operational detail, not architectural.

## SD-2 — Agent client cert TTL: 14 days (not 30)

**Options**: 30 days (as in `mtls-pki.md`); 14 days; 7 days.

**Decision**: 14 days.

**Rationale**: rotation is automatic on the existing gRPC stream, so there is no operational cost to shorter TTLs. 14 days is half the revocation window of 30 days, which meaningfully tightens the worst-case offline window. 7 days is achievable but closer to rotation noise and OCSP cache pressure.

**Concern to raise with architect**: RESOLVED in Phase 0 revision round. The architect accepted 14 days. `mtls-pki.md` has been updated and **ADR 0011 — Agent Cert TTL** was created. This decision is now authoritative.

**Candidate ADR**: Shipped as `docs/adr/0011-agent-cert-ttl.md`.

## SD-3 — Vault auto-unseal: Shamir (manual) for MVP

**Options**: Shamir manual; Transit auto-unseal via a peer Vault; Cloud KMS auto-unseal; HSM (PKCS#11) auto-unseal.

**Decision**: Shamir 3-of-5, manual.

**Rationale**: Aligns with on-prem-only deployment; no external dependencies; custodian ritual is auditable and defensible under KVKK; Vault restarts are rare under `restart: unless-stopped`. HSM auto-unseal requires Vault Enterprise (license cost) and is deferred to Phase 2 for regulated customers. Cloud KMS violates the on-prem constraint. Peer Vault introduces circular trust.

**Consequence**: every Vault restart blocks gateway/DLP/API cert renewals until custodians respond. Mitigated by caching fresh credentials before restart and by rehearsing an unseal drill monthly.

**Candidate ADR**: Yes, will propose ADR-0011 or similar.

## SD-4 — DLP deployment mode: distroless container, dedicated host preferred

**Options**: Dedicated physical host; dedicated VM; container on shared host; bare-metal VM with gVisor.

**Decision**: Dedicated host is the production-recommended mode; distroless container on shared host with strict isolation (seccomp + AppArmor + dropped caps + read-only fs + internal-only network) is the minimum acceptable for MVP pilots.

**Rationale**: a dedicated host is the only way to ensure a compromised admin with root on the main application host cannot read DLP memory. Container-only mode is acceptable for pilot because the customer population is small and vetted, but this is reflected in the contract addendum so the customer understands the tradeoff.

**Candidate ADR**: Yes — DLP isolation has product and contract implications that deserve formal ADR status.

## SD-5 — DLP identity: Vault AppRole via systemd credentials (not SPIFFE/SPIRE)

**Options**: SPIFFE/SPIRE with node and workload attestation; Vault AppRole + systemd credentials; Vault AppRole + file-based secret id.

**Decision**: Vault AppRole with Secret ID delivered via systemd `LoadCredential=`.

**Rationale**: SPIRE adds a whole new control plane for marginal benefit in a single-host on-prem setup. systemd credentials provide per-service tmpfs isolation that is sufficient for the threat model. File-based secret IDs leave the secret readable by anything with filesystem access; rejected.

**Candidate ADR**: No, covered by the DLP isolation runbook.

## SD-6 — Signed update: dual signature (EV code signing + Ed25519 in Vault)

**Options**: Single code-signing cert; dual signature with a project Ed25519 key; dual signature with an HSM-backed key; Sigstore / Rekor transparency log.

**Decision**: Dual signature — Sectigo EV Code Signing (outer) plus Ed25519 in Vault with 2-of-3 approver quorum (inner).

**Rationale**: outer signature gives Windows SmartScreen reputation and enterprise allowlist compatibility. Inner signature is defense in depth: a compromise of Sectigo or the EV Yubikey alone cannot push an update. Sigstore is attractive but requires internet connectivity agents cannot rely on in air-gapped deployments.

**Candidate ADR**: Yes.

## SD-7 — Signing algorithms: Ed25519 for control plane, ECDSA for TLS

**Options**: Ed25519 everywhere; ECDSA P-256 everywhere; mixed.

**Decision**: Mixed. Ed25519 for the control-plane signing key, project signing key, and agent client certs (small, fast, modern). ECDSA P-384 for root CA, ECDSA P-256 for tenant CA and server TLS certs (broad compat with Windows Schannel, admin console browsers, and reverse proxies).

**Rationale**: Ed25519 interop with Windows TLS stacks is still spotty at the server side; agents use rustls end-to-end so they are fine. Tenant CA must be verifiable by any tool a customer auditor brings, including legacy openssl.

**Candidate ADR**: No, covered in PKI bootstrap runbook.

## SD-8 — Audit log: Postgres hash chain with external checkpoint (no separate ledger)

**Options**: Separate ledger database (QLDB-like); Postgres hash chain; append-only blockchain.

**Decision**: Postgres hash chain with nightly external checkpoint (append-only file or S3 Object Lock).

**Rationale**: aligns with the architect's explicit direction. A separate ledger DB would add an operational component with its own compromise model. Hash chain + external checkpoint provides tamper-evidence sufficient for KVKK defensibility, at the honest cost of being detection-only, not prevention. Blockchain is rejected for on-prem: nowhere to publish.

**Candidate ADR**: No, implements existing architect decision.

## SD-9 — Audit checkpoint signing uses the control-plane signing key

**Options**: Dedicated audit-only signing key; reuse control-plane signing key; per-customer offline key.

**Decision**: Reuse the control-plane signing key (same Ed25519 key used for policy bundles, pin updates, live-view control, and DLP rule bundles).

**Rationale**: fewer keys to manage and rotate; rotation of the control plane key automatically refreshes audit checkpoint signing. Alternative: if a customer's compliance review demands separation, a dedicated key can be introduced in Phase 2 with a minor schema addition (checkpoint records already carry a `signing_key_id` field).

**Candidate ADR**: No, covered in admin-audit-immutability runbook.

## SD-10 — TMK rotation cadence: annual + on compromise

**Options**: Monthly; quarterly; annual; on-compromise only.

**Decision**: Annual + on compromise.

**Rationale**: TMK rotation triggers a cluster-wide PE-DEK re-wrap (cheap at 500 endpoints, expensive at 10k). Annual is the Vault transit default and aligns with typical compliance cadence. Monthly rotation would be paranoid given that TMK never leaves Vault and is protected by transit non-exportability.

**Candidate ADR**: No.

## SD-11 — Agent enrollment bootstrap token: 15 minute, single use

**Options**: 1h multi-use per batch; 15 min single use; 24h multi-use tied to MAC range.

**Decision**: 15 minute, single use.

**Rationale**: minimizes replay window. Installer ritual requires the token to be generated just before the install step, which fits typical MSI deployment workflows. Multi-use tokens are operationally convenient but expand the attack surface on the admin API's enrollment endpoint.

**Candidate ADR**: No.

## SD-12 — Break-glass custodianship separated from PKI custodians where possible

**Options**: Same custodians for Vault unseal and PKI Shamir shares; separated.

**Decision**: Separated where customer staff counts allow.

**Rationale**: separation of duties. A customer with only 2 security personnel cannot fully separate, and we accept that reality in the contract.

**Candidate ADR**: No.

## SD-13 — Pattern rule bundle distribution via NATS (not a separate API)

**Options**: Pull from admin API; push via NATS; push via file volume.

**Decision**: Push via NATS subject `dlp.v1.rulebundle.available`.

**Rationale**: NATS is already the bus DLP subscribes to; reuses existing authenticated channel; fits the event-driven pattern of the rest of the system. Pull would require DLP to expose an outbound to the admin API, violating the tight DLP egress allowlist.

**Candidate ADR**: No.

## Concerns Raised for Architect Reconsideration

These are questions I am NOT unilaterally overriding. They are flagged for the architect (and compliance-auditor) to review.

1. **Agent cert TTL**: **RESOLVED (Phase 0 revision round).** ADR 0011 adopts 14 days. `mtls-pki.md` updated.
2. **DLP on a dedicated host**: **RESOLVED (Phase 0 revision round).** A tiered model was adopted: Profile 1 (Hardened Container) for Phase 1 pilot with an enumerated non-negotiable control list; Profile 2 (Dedicated Host) as Phase 2 default and mandatory for regulated customers. See `docs/architecture/dlp-deployment-profiles.md` and `kvkk-framework.md` §10.4.
3. **HSM-backed Vault unseal (Phase 2)**: Still open — flagged to product for Phase 2 budget.
4. **Reproducible builds**: Still open — deferred to Phase 2 release engineering work.
5. **Audit checkpoint sink**: **PARTIALLY RESOLVED.** `docs/architecture/audit-chain-checkpoints.md` now specifies three customer-selectable sink profiles (WORM volume, Customer SIEM, Object-locked S3) with explicit trade-offs. High-assurance customers can combine profiles.
6. **NATS JetStream encryption at rest**: Still open — devops-engineer to confirm disk-level encryption is in the install baseline.
7. **Watchdog termination by SeDebugPrivilege**: **RESOLVED (Phase 0 revision round).** `docs/security/threat-model.md` now contains Flow 7 "Employee-Initiated Agent Disable" with heartbeat monitoring, gap classification, and DPO alerting. `anti-tamper.md` cross-references it.
8. **Live view session recording retention**: **RESOLVED (Phase 0 revision round).** ADR 0012 "Live View Recording (Phase 2 Design Envelope)" and `live-view-protocol.md` §Phase 2 Recording specify an independent LVMK hierarchy, 30-day default retention, dual-control playback, and DPO-only export. Phase 1 remains no-recording.
9. **DLP default-off state coherence (ADR 0013)**: **OPEN — coordination concern.** ADR 0013 made DLP disabled by default in Phase 1. The state is now surfaced in four places that must agree at all times: (a) `infra/install.sh` and `infra/compose/docker-compose.yaml` (no Secret ID, Compose profile `dlp` not activated), (b) `apps/api` state endpoint `GET /api/v1/system/dlp-state` (single source of truth; cross-validates container presence, Vault Secret ID existence, and latest `dlp.enabled|disabled` audit chain event), (c) `apps/console` header badge + Settings → DLP panel (reads state endpoint, re-renders on transition), (d) `apps/portal` employee-visible banner and historical timeline of state transitions. Any one of these drifting from the others breaks the legal claim. The risk: a transitional inconsistency (e.g., container running but state endpoint says `disabled` because the audit event was not yet committed) would either confuse the legal defense or, worse, let the default-off claim be undermined by a live decryption path the UI does not reflect. **Mitigation**: (i) the state endpoint is the only authority — install, opt-in script, Console, and Portal all read from it; (ii) the opt-in script is the only writer of the Secret ID and the only writer of `dlp.enabled|disabled` audit events; (iii) an integration test (Phase 1 exit criterion #18) drives the full ceremony end-to-end and asserts all four surfaces flip in the correct order; (iv) a Prometheus rule `dlp_state_mismatch` alerts if container presence disagrees with the state endpoint. Owners: devops-engineer, backend-developer (api), frontend-developer (console + portal). This concern is tracked until exit criterion #18 passes on staging.
