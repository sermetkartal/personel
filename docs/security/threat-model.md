# Threat Model — STRIDE

> Language: English. Scope: Phase 1 data flows that matter most.

STRIDE = Spoofing, Tampering, Repudiation, Information disclosure, Denial of service, Elevation of privilege.

## Flow 1 — Agent ↔ Ingest Gateway

| Threat | Scenario | Mitigation |
|---|---|---|
| **S**poofing | Attacker runs a rogue agent impersonating a real endpoint | mTLS with per-endpoint client cert; enrollment bound to hardware fingerprint; cert pinning of tenant CA on both sides |
| **T**ampering | Man-in-the-middle alters event batches in transit | TLS 1.3 integrity; optional batch HMAC (`AgentMessage.EventBatch.batch_hmac`) |
| **R**epudiation | Endpoint denies having sent an event | Server stores `received_at`, `seq`, cert serial, and hash of batch in audit index |
| **I**nfo disclosure | Traffic capture reveals event content | TLS 1.3 with forward secrecy; screenshots/keystroke content go as encrypted blobs even before TLS |
| **D**oS | Compromised agents flood gateway | Per-endpoint rate limits on gateway; adaptive backpressure; NATS publish quotas; connection caps per source IP |
| **E**oP | Rogue cert issued by compromised Vault role | Narrow Vault PKI policy; short-lived certs; OCSP + short TTL; deny-list cache; monitoring of cert issuance rate |

## Flow 2 — Admin ↔ Admin API

| Threat | Scenario | Mitigation |
|---|---|---|
| **S** | Stolen session cookie | Secure, HttpOnly, SameSite=strict cookies; IP+UA binding optional; 30-min idle timeout |
| **T** | CSRF against state-changing endpoints | Double-submit token + SameSite=strict; origin check |
| **R** | Admin denies making a policy change | Every write goes through Audit Service hash chain; UI shows audit entry id on success |
| **I** | Admin browses data outside their scope | RBAC at API; tenant scoping enforced at query time; row-level security in Postgres |
| **D** | Brute force login | Rate limit per account + per IP; LDAP lockout propagation; CAPTCHA after N failures |
| **E** | Privilege escalation via API param tampering | Server-side role check on every endpoint; never trust client role claims |

## Flow 3 — Admin ↔ Live View (WebRTC)

| Threat | Scenario | Mitigation |
|---|---|---|
| **S** | Admin spoofs HR approval | HR approval requires distinct user session; server enforces `approver != requester`; dual control |
| **T** | Attacker edits live-view audit record | Hash-chained append-only audit; role has INSERT+SELECT only; nightly integrity verifier |
| **R** | Admin denies viewing a session | LiveKit token is bound to session id; both the mint and the join are audited; session_id travels in agent event `live_view.started` too |
| **I** | Unauthorized viewer joins room | LiveKit JWT scoped to room, short TTL, admin token is view-only; agent token is publish-only |
| **D** | Repeated live-view requests exhaust HR | Per-requester rate limit; daily cap; alerts on abnormal request rate |
| **E** | Admin bypasses approval gate via direct API call | State machine enforced server-side; `APPROVED` state cannot be set without valid HR approval record; proto-level validation |

## Flow 4 — Updater ↔ Agent

| Threat | Scenario | Mitigation |
|---|---|---|
| **S** | Attacker serves a fake update mirror | mTLS agent cert required to fetch; manifest signed with Ed25519 by release signing key |
| **T** | Artifact modified in transit or at rest | SHA-256 in manifest; manifest signature; subresource hashes for file pieces |
| **R** | Release team denies pushing a bad version | Signed release pipeline; release ledger; signing is done by a HSM-backed key with 2-of-3 approval |
| **I** | Binary contains secrets | Release pipeline scrubs secrets; SBOM published; reproducible builds target |
| **D** | Mass rollout breaks fleet | Canary cohorts + automated rollback + health gates |
| **E** | Agent runs a malicious binary as LocalSystem | Signature verification happens before any exec; watchdog holds the swap transaction; TPM-sealed pin of expected publisher cert |

## Flow 5 — DLP Service Boundary

| Threat | Scenario | Mitigation |
|---|---|---|
| **S** | Another service pretends to be DLP to derive DSEK | Vault AppRole `dlp-service` secret id delivered via systemd credentials to the DLP process only; per-restart rotation |
| **T** | Attacker alters DLP rules to exfiltrate via match metadata | Rule set is versioned, audited, requires DPO approval; match metadata schema is fixed and narrow (no arbitrary fields) |
| **R** | DLP denies having seen a match | Every match emitted on NATS + audited; Vault audit device records each TMK derive call |
| **I** | Raw keystrokes leak from DLP via core dump, swap, or log | Core dumps disabled; seccomp filter; memguard for plaintext; logging redaction; ptrace disabled |
| **D** | Overwhelm DLP with encrypted blobs | Batched processing with backpressure; per-endpoint quotas; alerting on queue depth |
| **E** | Attacker with host access reads DSEK from memory | Keep DSEK lifetime short (regen every 24h and on restart); run DLP on a dedicated host where possible; host hardening (SELinux/AppArmor) |

## Flow 6 — Keystroke Key Hierarchy

| Threat | Scenario | Mitigation |
|---|---|---|
| **S** | Admin API impersonates DLP to derive TMK | Vault policy binds TMK derive to `dlp-service` AppRole identity; Admin API has no such policy |
| **T** | Wrapped DEK rows tampered in Postgres | Row includes `version` + is covered by audit; DLP refuses to use rows whose wrap fails GCM auth tag |
| **R** | DLP denies using a specific TMK version | Vault audit device logs all derive calls; PE-DEK wrap rows persist the TMK version id used |
| **I** | Offline Postgres dump leaks wrapped DEKs | Wrapped DEKs are worthless without TMK; backups additionally encrypted; pg_basebackup to encrypted volume |
| **D** | Vault unavailable → DLP stops decrypting | DLP holds DSEK in memory across Vault blips; alerts on Vault connectivity; retention means blobs can be decrypted later; no loss |
| **E** | Operator reads Vault raft snapshot | Vault storage encrypted with seal key (Shamir 3-of-5); root token inaccessible in normal ops; break-glass procedure audited |

## Flow 7 — Employee-Initiated Agent Disable

**Added during Phase 0 revision round (Gap 10).** This flow models an employee with local administrator rights on their own machine deliberately disabling the Personel agent (stopping the service, killing the watchdog, blocking network egress, uninstalling). The legal-defense concern: "we monitored you" is weaker if the agent was silently offline for hours or days without anyone noticing.

| Threat | Scenario | Mitigation |
|---|---|---|
| **S**poofing | Employee copies agent identity to another machine to appear "online from elsewhere" | mTLS cert bound to hardware fingerprint; cert reuse on a different host is detected at `Hello` and flagged as `agent.tamper_detected` with severity HIGH |
| **T**ampering | Employee patches the agent binary to stop emitting events but keep the connection alive | Binary integrity self-check against signed manifest every 5 min; mismatch → forced update via watchdog; watchdog refuses to restart tampered binary; last successful heartbeat age is an independent monitoring signal that a patched agent cannot fake without also impersonating the watchdog |
| **R**epudiation | Employee claims "my agent was broken, not disabled on purpose" | Server heartbeat monitor distinguishes "clean offline" (last heartbeat was healthy and predictable gap like sleep/shutdown) from "dirty offline" (heartbeats stop mid-stream; queue depth high but no upload); dirty offline is audit-logged with full context and timestamps |
| **I**nfo disclosure | Not applicable to this flow |  |
| **D**oS | Employee disables the agent to avoid being monitored; audit record is sparse | Server-side heartbeat monitor on gateway: stream-close without graceful `Bye` → audit entry `agent.disappeared`. Gap duration tracked. Alerts below |
| **E**oP | Employee uses local admin to grant themselves SYSTEM and modify the agent's service config | SCM ACLs tightened (see `anti-tamper.md` §2); ACL changes detected within 60 s and reported as tamper; uninstall flows outside operator scripts are treated as suspicious |

### Detection and Response Behaviors

Concrete server-side behaviors implementing this flow (Phase 1 scope):

1. **Gateway heartbeat monitor**: per-endpoint timer. Expected heartbeat cadence is ~30 s (health) + ~12 s (TCP keepalive). After **3 missed expected heartbeats** (approximately 90 s), the endpoint transitions to `degraded`. After **5 minutes**, transitions to `offline`. After **2 hours**, transitions to `offline_extended`.
2. **Audit entries** on every transition: `endpoint.state_transition { from, to, last_seen, gap_classification }` where `gap_classification` is one of `graceful_shutdown`, `suspected_suspend`, `unreachable`, `disappeared_unexpectedly`. The classification uses the last `Heartbeat` + the presence/absence of a graceful `Bye`.
3. **Admin dashboard**: `/admin/endpoints?state=offline_extended` query with a badge count; widget on the main dashboard shows "Endpoints silent > 2 h" count.
4. **DPO notification**: endpoints silent beyond the policy-configured threshold (default 4 h during business hours; 24 h otherwise) generate a `dpo.endpoint_silence_alert` that lands in the DPO dashboard.
5. **Auto-quarantine policy (optional, default OFF)**: customer can configure "after N hours of unexpected offline, auto-quarantine the user's network access via a webhook to the corporate NAC." This is OFF by default because it is intrusive; it is enabled only when the customer has a formal disciplinary process aligned with KVKK accountability.
6. **"Silence gap" field in reports**: the standard DPO destruction/compliance report enumerates silence gaps per endpoint so that any legal argument about monitored content is accompanied by an honest record of when monitoring was and was not active.

### Legal Defensibility Note

KVKK m.10 aydınlatma ilkesi implies the employee must know what is being processed about them. By extension, documenting gaps in processing (when the agent was not running) is part of honest record-keeping. Flow 7's artifacts support both sides:

- When the employer needs to demonstrate a period of monitoring, silence gaps are visible and their cause is classified.
- When the employee contests a disciplinary action, silence gaps let them argue "no evidence was collected during period X."

The cross-reference is `docs/security/anti-tamper.md` which links back to this flow.

## Residual Risks Flagged

1. Endpoint compromise is out of scope: if a single endpoint is fully compromised before enrollment, its PE-DEK and live content are lost for that endpoint. This is a design boundary, not a bug.
2. Customer operators with full host access can eventually extract anything that touches memory on that host. We raise the cost with DLP isolation but cannot reduce it to zero on a single-tenant on-prem box.
3. Phase 1 user-mode agent has limited tamper resistance against an admin user on the endpoint itself (not the target threat; the employee is the subject, not the adversary).
