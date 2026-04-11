# Runbook — Secret Rotation

> Language: English. Audience: devops-engineer, security on-call, compliance-auditor. Scope: every secret in Personel Phase 1, its lifetime, rotation mechanism, rollover window, blast radius, and customer-facing impact.

## 1. Inventory and Rotation Table

| # | Secret | Lifetime | Rotation method | Rollover window | Blast radius if compromised | Customer action required |
|---|---|---|---|---|---|---|
| 1 | **Tenant Master Key (TMK)** — `transit/keys/tenant/<tenant_id>/tmk` | 1 year (auto), immediate on compromise | `vault write -f transit/keys/tenant/<id>/tmk/rotate`; Vault retains old version for decrypt until grace expires | 30 days grace for old version; all blobs wrapped under the old version must be re-wrapped or purged before destruction | Catastrophic — attacker with TMK derive capability can decrypt all in-flight keystroke content during the compromise window. Retained ciphertext remains safe only until the attacker can also call derive. | On compromise: customer notified within 24h. TMK rotation triggers cluster-wide PE-DEK re-wrap (automatic, no endpoint restart required). |
| 2 | **Agent enrollment bootstrap token** (Vault AppRole Secret ID) | 15 minutes, single use | Admin API requests a fresh Secret ID per enrollment | None (single-use) | Minimal — allows one rogue endpoint to enroll if intercepted before use | None under normal ops |
| 3 | **Agent client certificate** | 14 days | Automated via `ServerMessage.RotateCert` on the existing gRPC stream | 48 hours (old cert valid until rotation propagates) | One endpoint worth of access to the gateway; rate-limited, cannot impersonate other endpoints | None |
| 4 | **Server TLS certificates** (gateway, admin API, DLP, update, live view) | 90 days | `vault-agent` template watches lease, writes renewed cert, signals service with SIGHUP | 14 days before expiry | TLS termination for one service; replaced before expiry; old cert remains technically valid until expiry | None |
| 5 | **Tenant CA** | 3 years | Air-gapped ceremony per `pki-bootstrap.md` §3 | 12 months dual-sign period | Catastrophic PKI-wide | Schedule ceremony attendance; customer security officer required on-site |
| 6 | **Root CA** | 10 years | Air-gapped ceremony | 12 months dual-sign period | Full PKI rebuild | Schedule ceremony attendance |
| 7 | **JWT signing keys** (admin console auth — access tokens) | 90 days | Ed25519 key rotation; new kid in JWKS; old kid trusted for 24h after rotation to drain in-flight tokens | 24 hours | Admin console session forgery; all admin tokens must be re-issued | None under normal ops; admins re-login on rotation day |
| 8 | **JWT signing keys** (refresh tokens) | 90 days, rotated with #7 | Same as #7 | 24 hours | Admin session continuity forged | Same |
| 9 | **Postgres role passwords** — application roles (`app_admin_api`, `app_gateway`, etc.) | 24 hours (Vault dynamic secret) | Vault database secrets engine; service fetches fresh creds each lease cycle via `vault-agent` template | 5 minutes (lease overlap) | One service's Postgres access for at most 24h | None |
| 10 | **Postgres superuser password** | Sealed; used only in break-glass | Manual rotation after every break-glass use | N/A | Full DB takeover including audit log forgery (detectable via checkpoints, see `admin-audit-immutability.md` §9) | Custodians re-seal after rotation |
| 11 | **NATS credentials** (service users: gateway, dlp, admin-api, audit, writer workers) | 90 days | NATS account-based auth; credentials rotated via Vault KV delivered to services | 7 days | One service's NATS access | None |
| 12 | **NATS operator signing key** | 1 year | Manual rotation; new operator signing key signs new accounts; old accounts migrated | 30 days | Full NATS trust fabric | Brief restart of NATS service during migration |
| 13 | **MinIO access keys** (per service: gateway-writer, dlp-reader, admin-reader, backup-writer) | 90 days | MinIO admin API rotation triggered by a systemd timer reading a Vault-managed schedule | 1 hour | Object store R/W per scope | None |
| 14 | **MinIO root access key** | Sealed; break-glass only | Manual | N/A | Full object store | Custodians re-seal |
| 15 | **Project code signing key** (Ed25519, Vault transit `transit/keys/project/release-signing`) | 1 year | Manual rotation on calendar date; new key bundled in next agent release | 6 months grace (old key trusted for N+1 major version) | Supply chain — attacker with the key plus the EV code-signing key could push updates; inner signature stopped without it | Next release includes new pubkey; no customer action |
| 16 | **EV code-signing certificate** | 1-3 years (CA-defined) | Renewal via Sectigo; new Yubikey loaded with new cert | 30 days | SmartScreen reputation + outer signature; not sufficient alone to push updates due to inner signature | None |
| 17 | **Control-plane signing key** (policy push, pin update, live-view control, DLP rule bundle, checkpoint signing) | 1 year | Manual rotation; signed key rollover message broadcast to agents | 6 months grace | Policy push + pin update + live-view start messages — attacker could alter agent policy and start live view. Detected by per-endpoint audit and abnormal-rate alerts. | None |
| 18 | **Vault unseal shares (Shamir)** | Lifetime of the Vault instance | Re-sharding via `vault operator rekey` | Hours (online rekey) | Unseal capability; 3-of-5 threshold required | Custodians re-seal envelopes |
| 19 | **Vault break-glass admin token** | 1 year; revoked after any use | Issued fresh after each use | N/A | Full Vault admin | Custodian re-seal |
| 20 | **Live view room tokens** (LiveKit JWT) | Session duration, max 1 hour | Minted per session from the LiveKit API secret; never reused | None — ephemeral | One live view session | None |
| 21 | **LiveKit API secret** | 90 days | Rotation via LiveKit admin API; delivered via Vault KV | 1 hour | Ability to mint arbitrary live view tokens; agents still require a signed `LiveViewStart` from the control-plane signing key to actually start publishing | None |
| 22 | **DLP AppRole Secret ID** | Rotated on every DLP restart (systemd credential) | `ExecStartPre=` hook calls provisioning token | None | DLP's Vault token; bounded by Vault policy to transit operations | None |
| 23 | **Audit signing checkpoint key** | Same as control-plane signing key (#17) | Rotated with #17 | 6 months | Checkpoint signature forgery (detectable if #17 is also uncompromised) | None |
| 24 | **Backup encryption key (age recipient)** | 1 year | Key rotation via Vault KV; old key held for historical restores | 1 year overlap | Backup confidentiality | Customer holds the private key offline |
| 25 | **LDAP bind credentials** (if LDAP auth enabled) | 90 days | Rotation via customer AD; Personel updates via admin console config | Minutes | LDAP read access for the service account | Customer IT performs AD rotation |

## 2. Rotation Procedures — Automated

Rotations marked "automated" in §1 are executed by `vault-agent` sidecars and systemd timers. The operational contract:

- Every service that consumes a rotated secret reloads on the signal written by `vault-agent` (SIGHUP or file-watch).
- Reload must not drop in-flight connections. Services implement a connection drain window of 30 seconds.
- If reload fails, `vault-agent` emits `vault.renewal.failed` to NATS `ops.v1.alerts`. Alerting fires to on-call within 60 seconds.
- If reload fails AND the cert is within 24h of expiry, the alert escalates to priority 0.

## 3. Rotation Procedures — Manual

### 3.1 TMK rotation (annual or emergency)

1. Open a ticket referencing this runbook section and the reason.
2. Notify compliance-auditor (TMK rotation is KVKK-relevant).
3. Execute:
   ```bash
   vault write -f transit/keys/tenant/<tenant_id>/tmk/rotate
   ```
4. Verify the new version:
   ```bash
   vault read transit/keys/tenant/<tenant_id>/tmk
   ```
5. Trigger DLP re-wrap job for all active endpoints (`personel-dlp-tools rewrap --tenant <id> --from-version <N-1> --to-version <N>`). The job iterates `keystroke_keys`, unwraps with the old version, rewraps with the new, and updates the row. Runs in the DLP service context — the only identity with derive access.
6. Monitor `dlp.rewrap.progress` metric. For 500 endpoints, the job completes in under 10 minutes.
7. After the rewrap confirms 100% completion and the grace period (30 days) has elapsed, schedule old version for destruction by lowering `min_decryption_version`:
   ```bash
   vault write transit/keys/tenant/<tenant_id>/tmk/config min_decryption_version=<N>
   ```
8. Audit: confirm `vault.policy.changed` and `admin.breakglass.*` entries present if break-glass was used; confirm Vault audit device logged the rotate call.

### 3.2 Control-plane signing key rotation (annual)

1. Generate new Ed25519 key in Vault:
   ```bash
   vault write -f transit/keys/project/control-signing/rotate
   ```
2. Fetch the new public key, embed in the next agent release alongside the previous pubkey (trust both).
3. Publish a signed `KeyRotationNotice` control message to agents that lists the new kid as active.
4. Monitor: all agents must ack within 7 days. Stragglers are flagged.
5. After one major version has rolled out with the new key baked in AND 6 months have passed, remove the old key from the trust bundle in the subsequent release.
6. Audit: `release.signing_key_rotated` event.

### 3.3 JWT signing key rotation (90 days)

1. Generate new RSA/Ed25519 key, assign a new `kid`.
2. Publish new `kid` in the JWKS endpoint alongside the current `kid`.
3. Switch the Admin API to sign with the new `kid`.
4. After the access-token TTL (15 min) has passed, all in-flight tokens have been refreshed or expired.
5. Keep the old `kid` in JWKS for 24h for any late validators.
6. Remove the old `kid` from JWKS.

### 3.4 Break-glass tokens

After every use of a break-glass token (Vault, Postgres superuser, MinIO root), issue a new token, re-seal in tamper-evident envelopes, update the custodian log. Old tokens are revoked explicitly; do not rely on TTL expiry.

## 4. Rotation SLOs

| Category | SLO |
|---|---|
| Scheduled rotations (automated) | 100% success within 48h of scheduled time |
| Scheduled rotations (manual) | Completed within the calendar window |
| Emergency rotations (compromise) | Initiated within 1h of confirmed incident, completed within 4h for keys; full cluster propagation within 24h |
| Rotation failures detected-to-resolved | <4h for P1, <24h for P2 |

## 5. Verification

A nightly job `personel-secret-audit` iterates each entry in the table and verifies:

- Current version/lease age does not exceed the declared lifetime.
- Automated rotations have run on schedule.
- No Vault tokens older than their max TTL are still in use.
- Custodian log has a recent entry for every break-glass token.

Failures are reported as `secret.rotation.overdue` with severity scaling by how overdue and how blast-radius-heavy the secret is.

## 6. Handoffs

- **devops-engineer**: owns the rotation automation, `vault-agent` configs, the nightly audit job, and alerting.
- **backend-developer**: ensures services implement clean reload on secret rotation signals without dropping connections.
- **compliance-auditor**: reviews rotation evidence during KVKK audit preparation.
