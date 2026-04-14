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

## Faz 6–9 Genişletilmiş STRIDE Matrisi (2026-04-13, Faz 12 #124)

> Scope: Phase 1 base + Phase 2/3/6/9 yeni endpoint'ler. Her satır: per-endpoint
> saldırgan hedefi, STRIDE kategorisi, mevcut mitigation, artık risk, tespit
> yolu, müdahale prosedürü.

### Flow 8 — Search API (OpenSearch full-text)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Tenant veri sızıntısı via query injection | I | Farklı tenant'ın audit kayıtlarını gör | Server-side RLS query rewriting; tenant_id pinned in search context; query AST validation reject unknown fields | Düşük | `personel_search_cross_tenant_attempt` metric + audit log | Incident response; erişim reviewer |
| NLQ prompt injection → over-scoped query | T,E | "Show all employees' DLP matches" via LLM re-query | Query template whitelist; LLM output scanned for disallowed fields | Orta | `search.llm_refusal` rate | LLM refusal retraining |
| DoS via expensive regex / wildcard | D | Kaynak tüketimi | Query cost estimator; per-tenant rate limit 60/min; circuit breaker | Düşük | Latency SLO breach | Rate limit bump; auto-block |

### Flow 9 — DLQ Replay API

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Replay abuse: tekrar tekrar replay → duplicate audit kayıt | R,T | Audit inflation, false evidence | Replay idempotency key; audit `pipeline.replay_requested` + `replay_completed` hash chain entries | Düşük | `replay_rate_per_actor` metric | Throttle per actor; DPO review |
| Stale payload replay → integrity drift | T | Old policy state reapplied | Replay bundles pinned to schema version; version mismatch → reject | Düşük | Schema drift alert | Manuel DPO evaluation |
| Privilege bypass — Auditor replays DPO-only events | E | Role confusion | Replay endpoint RBAC: `pipeline_replay` permission → DPO only | Düşük | RBAC denial metric | SOC review |

### Flow 10 — API Key / Service-to-Service Auth

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| API key exfiltration (log leak, env var dump) | I | Persistent access | Keys stored Vault-sealed; rotated 90d; log scrubber; secret scanning pre-commit | Orta | Gitleaks CI + Vault audit `read` count anomaly | Key revoke + rotate + audit |
| Stolen key → lateral pivot | E | Tenant escalation | Per-key tenant binding; per-key scope list; IP allowlist optional | Orta | Unusual IP / geo metric | Revoke + MFA challenge operator |
| Key brute force | D,E | Guess key | 256-bit entropy + per-IP rate limit + exponential backoff | Çok düşük | Rate limit metric | Auto-block |

### Flow 11 — Audit Log Streaming (WebSocket)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| WebSocket hijack via CORS bypass | S,I | Live audit tail okuma | Origin strict check; ws token per-session short-lived; server-side RBAC on subscribe | Düşük | CORS denial metric | Block IP; session kill |
| DoS — 1000 WS connections | D | Kaynak tüketim | Per-user connection cap 5; total cap 100; heartbeat timeout | Düşük | Connection count metric | Cap enforcement |
| Event filter bypass → cross-tenant peek | I,E | Başka tenant'ın audit'ini gör | Filter parsed + rewritten server-side; tenant_id locked | Düşük | Filter rewrite failure log | SOC review |

### Flow 12 — ClickHouse Aggregation API

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Query-based info leak (narrow-GROUP-BY attack) | I | Individual employee identification in aggregated view | k-anonymity enforce: GROUP BY result count < 5 → reject; DP noise Phase 2 | Orta | Query rejection metric | Query refinement guide |
| Expensive cross-join DoS | D | CH cluster overload | Query cost estimator; max execution time 30s; per-tenant memory cap | Düşük | `ch_query_aborted` counter | Auto-kill |
| SQL injection via filter param | T,I,E | DB-level access | Parameterized queries only; koanf validation; no string concat | Düşük | WAF log + CH query log | Incident response |

### Flow 13 — LLM ML Classifier (apps/ml-classifier)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Prompt injection via window title → mis-categorize | T | Hide malicious app as "productive" | Output schema strict JSON; confidence threshold 0.70; regex fallback cross-check | Orta | Disagreement rate metric | Reclassification queue |
| Jailbreak → LLM returns PII | I | Data exfiltration | Input pre-scrub (redaction); output post-scrub (regex filter); sandboxed model container | Orta | PII-in-output regex hit | Container kill + retrain |
| Adversarial input → classifier DoS (very long title) | D | Stall classifier | Input length cap 512 chars; timeout 50ms; graceful fallback to regex | Düşük | Timeout metric | Rate limit |
| Model poisoning via malicious update | T | Change classification behavior | Model file SHA + Ed25519 sig check at load; offline review cycle | Düşük | Boot signature check failure | Rollback |

### Flow 14 — DSR Erasure (KVKK m.11/g)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| DSR abuse — fake request for someone else | S,R | Wipe victim's data | Employee identity verified via Keycloak OIDC + 2nd factor; DPO review gate | Düşük | DPO queue + audit trail | DPO reject |
| Over-erasure — wipe beyond scope | I,T | Collateral data loss | Scoped erasure: per employee_id only; audit log preserved (legal basis m.10) | Düşük | Erasure audit delta | Restore from backup |
| Crypto-shred bypass — old backup still contains | I | Post-erasure recovery possible | PITR + backup retention aligned with DSR SLA; backup re-encryption on tenant key rotation | Orta | Backup scan | Delete backup batch |

### Flow 15 — Policy Editor (visual SensitivityGuard)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Malicious policy push → over-collection | E,T | Capture more than allowed (e.g., enable keystroke content) | ADR 0013 invariant: server-side validator rejects `dlp_enabled=false AND keystroke.content_enabled=true`; signing happens after validation | Düşük | Validator metric | Reject + audit |
| Policy signing key abuse | E | Forge policy | Ed25519 signing via Vault transit; DPO + Admin dual control for prod push | Düşük | Vault audit | Key rotation |
| Policy diff UI XSS | T,E | Inject code into admin browser | React strict escaping; CSP header nonce-based | Düşük | CSP violation report | Patch |

### Flow 16 — Live View HR Approval

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Approver collusion — requester + approver same person / colluding pair | S,R,E | Bypass dual control | `approver != requester` enforced server-side; pattern detection on repeated pair anomaly | Orta | Pair frequency metric | Random audit sample; DPO review |
| Approval expiry race — approve → revoke race → viewer already in room | D,E | Extended view | Short-lived JWT (60s); server-side session termination on revoke | Düşük | Race log | Token TTL bump |
| Replay approval — same approval token reused | R | Multiple sessions one approval | Token one-time use; audit-bound to session_id | Düşük | Token reuse metric | Reject |

### Flow 17 — Bulk Endpoint Operations (mass wipe / deactivate)

| Threat | STRIDE | Attacker Goal | Mitigation | Residual | Detection | Response |
|---|---|---|---|---|---|---|
| Mass wipe attack — admin nukes all endpoints | D,E | Organization sabotage | Bulk op requires `bulk_operation` permission (Admin only); 2-step confirmation; cooldown 5min between bulk ops; target count cap 500 | Yüksek (insider) | Bulk op audit entry + executive alert | Executive review; possibly revert via backup |
| Bulk enroll → rogue fleet | S,E | Shadow fleet for surveillance | Bulk enroll rate limit 50/hour; per-batch audit; DPO review bulk > 100 | Orta | Enrollment rate anomaly | DPO notification |
| Partial bulk failure leaks error = endpoint existence oracle | I | Enumerate endpoint IDs | Error responses generic ("operation failed"); detailed only in server log | Düşük | Error response shape test | Response unification |

---

## Kill Chain Diyagramları / Kill Chain Diagrams

### KC1 — Agent Enrollment MITM

```
[Attacker at employee network tap]
         │
         │ intercepts enroll token in transit
         │ (HTTP over employee's corp LAN)
         ▼
[Replay enroll token from attacker machine]
         │
         │ calls /v1/agent-enroll with valid token + own CSR
         ▼
[Admin API signs CSR → attacker gets valid agent cert]
         │
         ▼
[Attacker establishes mTLS as fake endpoint]
         │
         └── Mitigation chain:
             1. Enroll token served over TLS 1.3 (mitigates network tap)
             2. Token single-use + 15-min TTL (mitigates delayed replay)
             3. Hardware fingerprint bind check at first Hello (CSR hw_fp
                must match enroll ceremony record)
             4. Gateway cert pinning (agent must also trust gateway)
             5. Anomaly: duplicate hw_fingerprint from different IPs →
                revoke + alert
```

### KC2 — Insider Admin Threat

```
[Malicious admin with valid console access]
         │
         │ wants to: read keystroke content of specific employee
         ▼
[Attempt 1: direct API call /v1/keystroke/content]
         │
         │ ← Blocked: no such endpoint exists (Phase 1 invariant)
         ▼
[Attempt 2: enable DLP via UI to gain decryption path]
         │
         │ ← Blocked: UI has no enable button (ADR 0013)
         │ ← dlp-enable.sh requires DPO + IT-Sec + Legal signatures
         ▼
[Attempt 3: impersonate DLP service AppRole]
         │
         │ ← Blocked: Secret ID provisioning requires ceremony
         │ ← Vault audit logs every secret_id read attempt
         ▼
[Attempt 4: modify policy to capture more]
         │
         │ ← Blocked: policy validator rejects invariant violation
         │ ← Policy signing requires dual control
         ▼
[Attempt 5: direct DB query on ciphertext]
         │
         │ ← ciphertext present, but no PE-DEK exists in default state
         │ ← Vault transit/derive never called, mathematical impossibility
         ▼
[Residual: read screen captures within legal scope (audited)]
         │
         └── All access logged in audit hash chain + Evidence Locker
             CC6.1; DPO can verify post-hoc; employee can request via
             transparency portal
```

### KC3 — DSR Erasure Abuse

```
[Internal attacker wants to wipe colleague's data]
         │
         ▼
[Submit fake DSR on behalf of victim]
         │
         │ ← Blocked: DSR submission requires victim's own OIDC auth +
         │   challenge (Keycloak), attacker can't forge
         ▼
[Attacker obtains victim's password / cookie]
         │
         ▼
[Fake DSR submitted as victim]
         │
         │ ← DPO review stage: DPO verifies identity, may require
         │   additional proof (ID scan, HR confirmation)
         │ ← 7-day cooling period for erasure requests > N records
         ▼
[DPO approves under duress / compromised]
         │
         │ ← Audit log: every DPO action signed, hash-chained
         │ ← Evidence Locker P7.1: erasure bundle kept
         │ ← Ops: PITR backup retention 30 days — recovery possible
         ▼
[Detection: unusual DSR pattern / victim complaint]
         │
         ▼
[Response: restore from PITR; rotate compromised credentials;
 post-mortem + DPO role review]
```

### KC4 — Pipeline Replay Abuse

```
[Attacker with Auditor role (read-only)]
         │
         │ attempts: replay old policy to reinstate less-strict rules
         ▼
[Call /v1/pipeline/replay with bundle_id]
         │
         │ ← Blocked: replay requires `pipeline_replay` permission
         │   (DPO-only)
         ▼
[Attacker with DPO role (legitimate)]
         │
         │ replays old DSR-erasure event to "undo" the erasure
         ▼
[Replay handler: validates bundle signature + schema version]
         │
         │ ← DSR erasure is idempotent: replay is a no-op if target
         │   already erased
         │ ← Replay itself audited (replay.requested + replay.completed)
         ▼
[Residual: attacker can replay non-idempotent events]
         │
         │ Mitigation: all events MUST be designed idempotent or
         │ carry replay-fence (Phase 7 #73 schema versioning)
         ▼
[Detection: replay-per-actor metric > threshold]
```

### KC5 — Policy Push Tampering

```
[Attacker with Admin role wants malicious policy deployed]
         │
         ▼
[Edit policy JSON to include keystroke.content_enabled=true]
         │
         │ ← Blocked: server validator rejects ADR 0013 invariant
         │   violation BEFORE signing
         ▼
[Bypass validator by direct DB write]
         │
         │ ← Blocked: policy_versions table append-only; INSERT only
         │ ← Gateway fetches latest row AND verifies signature
         │   against Control Plane Root Key
         │ ← Direct DB write has no valid signature
         ▼
[Forge signature via stolen signing key]
         │
         │ ← Signing key lives in Vault transit, exportable=false
         │ ← Vault audit logs every sign call
         │ ← Key rotation on suspicion
         ▼
[Attack cost: must compromise Vault + stay under audit radar]
         │
         ▼
[Detection: Vault transit sign rate anomaly;
           Control Plane Key usage without approved change ticket]
         │
         ▼
[Response: rotate Control Plane Key + re-sign all valid artifacts +
           revoke old signature version]
```

---

## Residual Risks Flagged

1. Endpoint compromise is out of scope: if a single endpoint is fully compromised before enrollment, its PE-DEK and live content are lost for that endpoint. This is a design boundary, not a bug.
2. Customer operators with full host access can eventually extract anything that touches memory on that host. We raise the cost with DLP isolation but cannot reduce it to zero on a single-tenant on-prem box.
3. Phase 1 user-mode agent has limited tamper resistance against an admin user on the endpoint itself (not the target threat; the employee is the subject, not the adversary).
