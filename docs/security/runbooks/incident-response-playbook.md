# Runbook — Incident Response Playbook

> Language: English (Turkish customer notification templates in §8). Audience: security on-call, devops-engineer, DPO. Scope: top 8 incident classes for Personel Phase 1 on-prem deployments.

## Shared Conventions

- **Severity**: P0 = active data exposure or service outage; P1 = credible compromise, no confirmed data loss; P2 = anomaly or suspected incident.
- **First responder** is whoever acknowledges the alert first, unless an incident commander is appointed.
- **Every incident** gets a ticket in the customer's incident tracking channel with id format `PER-INC-<yyyymmdd>-<n>`.
- **Every incident** ends with a post-incident review written into `docs/incidents/<id>.md` using the template in §9.
- **Evidence preservation**: before touching a host, snapshot it (VM snapshot, EBS, or `dd` image). Never skip this.
- **Communications discipline**: during P0/P1, speak only on the incident channel. No email, no slack DMs that bypass the channel.

---

## 1. Suspected Agent Compromise on an Endpoint

**Detection signals**
- Clustered `agent.tamper_detected` events from one endpoint_id.
- `pki.cert.issued` from Vault where the CSR hardware fingerprint does not match the recorded endpoint row.
- Gateway rate-limiter trips for one agent cert.
- `agent.update_rollback_failed`.
- Heartbeat gap followed by reconnection from a different source IP that does not match the endpoint's historical range.

**Immediate containment (within 15 minutes)**
1. Publish the endpoint's cert serial to `pki.v1.revoke` — the gateway deny-list picks it up within 5 minutes cluster-wide.
2. Call `admin.endpoint.revoke` from the admin console, which also writes `endpoint.revoked` to the audit log.
3. Rotate the endpoint's PE-DEK via the DLP rekey job (even if PE-DEK compromise is not confirmed; it is cheap and bounds forward risk).
4. If live view was active from this endpoint in the last hour, flag the affected sessions as potentially tainted in the audit log.

**Investigation**
1. Pull the last 24h of events from ClickHouse filtered by endpoint_id; look for anomalies.
2. Pull the last 24h of gateway TLS session logs for this endpoint's cert serial.
3. Examine the local queue eviction counter trend; attackers sometimes trigger evictions to drop tamper events before upload.
4. If possible, collect the endpoint machine's forensic image via the customer's IR team.

**Recovery**
1. Re-enroll the endpoint after the customer's IT has cleaned or reimaged the host. Enrollment token is fresh (single-use).
2. New agent cert issued; old cert serial stays on the deny-list permanently.
3. Retrospective: compare the compromised endpoint's activity window with any `screenshot.viewed` or `live_view.started` events to assess what data the attacker may have generated.

**Customer communication**
Internal only unless the compromise is confirmed to have exposed personal data of third parties, in which case escalate to §8.

---

## 2. Gateway Compromise

**Detection signals**
- Unusual outbound connections from gateway host.
- Gateway process emitting anomalous Vault calls (read via Vault audit device).
- Unexpected `CsrSubmit` activity.
- OS-level EDR alert on the gateway host.

**Immediate containment (within 15 minutes)**
1. Cut gateway from Vault: revoke the gateway's Vault token (`vault token revoke -accessor <acc>`).
2. Cut gateway from NATS: disable the gateway NATS user.
3. Stop the gateway service: `systemctl stop personel-gateway.service`.
4. Bring the standby gateway online if one exists; otherwise accept the downtime (agents queue locally, retention unaffected).
5. Snapshot the compromised host.

**Investigation**
1. Review Vault audit device for all gateway-originated calls in the last 72h.
2. Compare gateway process image hash against the known-good release build.
3. Check for lateral movement: did the gateway's Vault token attempt any denied paths (transit derive, kv/crypto/*)? The policy denies those but attempts themselves are evidence.
4. Review gateway TLS session logs for unusual client patterns.

**Recovery**
1. Rebuild the gateway host from a known-good image.
2. Re-issue gateway server certificate with a fresh key.
3. Re-issue gateway Vault AppRole Secret ID.
4. Restart gateway and verify agent reconnects at expected rate.

**Blast radius assessment**
- Gateway never sees decrypted keystroke content (ciphertext only). Keystroke confidentiality is preserved.
- Gateway could forge `EventBatch` records into NATS; downstream analytics may contain injected false events. Audit log queries during the compromise window are suspect.

**Customer communication**
Internal if no confirmed data exposure; escalate to §8 if ClickHouse data was read or the DLP stream was observed.

---

## 3. Admin Account Compromise

**Detection signals**
- Impossible-travel login (geo IP jump).
- Multiple `screenshot.viewed` or `live_view.requested` entries in a short window from one actor.
- `live_view.requested` where approver was the same person (rejected by state machine, but attempts are signals).
- LDAP bind failures followed by a success from a new IP.

**Immediate containment**
1. Disable the admin account (`user.disabled`).
2. Invalidate all JWTs for the actor (revoke refresh tokens; flip `token_version` column so any existing access tokens fail on next validation).
3. Force password + MFA re-enrollment.
4. Review the audit log for every action this actor took in the last 30 days. The hash chain ensures retroactive tampering is detectable.

**Investigation**
1. Enumerate all `screenshot.viewed`, `screenclip.viewed`, `live_view.*`, `export.*`, `employee.*` actions by this actor.
2. Identify affected employees.
3. Check for privilege escalation: did the account's role change recently? Who approved it?

**Recovery**
1. Restore account only after the customer confirms control.
2. If the customer's AD was the root cause, coordinate with customer IT.

**Customer communication**
Internal. Escalate to §8 if the compromised admin viewed or exported employee personal data.

---

## 4. Vault Compromise (Worst Case)

**Detection signals**
- Vault audit device shows unexpected token creation or policy changes.
- Vault raft storage modified outside of normal write patterns.
- Break-glass token used without a matching incident ticket.
- Host EDR alert on the Vault host.

**Immediate containment (within 30 minutes)**
1. **Seal Vault**: `vault operator seal`. This is drastic; it stops all cert issuance, all DLP decrypts, and all Vault-dependent operations. Accept the downtime.
2. Snapshot the Vault host for forensics.
3. Convene the incident bridge: security-engineer, customer security officer, DPO.

**Investigation**
1. Pull the Vault audit device log from the earliest suspicious activity. The audit log is signed and stored both on the Vault host and off-host via filebeat (see `vault-setup.md` §6).
2. Identify every secret that may have been exposed: enumerate `transit/derive` calls, `pki/sign` calls, and `kv/*` reads.
3. Determine whether the root key material is compromised. If so, the entire PKI must be rebuilt.

**Recovery (full rebuild)**
1. Stand up a fresh Vault instance per `vault-setup.md` on a new host.
2. Perform a new air-gapped PKI ceremony per `pki-bootstrap.md`. **New root CA.**
3. Re-enroll every endpoint. For 500 endpoints this takes 4-8 hours with coordination from customer IT; endpoints are offline until re-enrolled.
4. Rotate **every** secret in `secret-rotation.md`.
5. Rotate TMK by generating a fresh one in the new Vault. Old keystroke blobs wrapped under the old TMK are **cryptographically destroyed** — they cannot be decrypted. This is acceptable because a Vault compromise already compromised that material's confidentiality guarantees.
6. Wipe and reinstall the compromised Vault host.

**Blast radius assessment**
- Assume the worst: any keystroke blob in transit during the compromise window was decryptable by the attacker.
- Historical blobs remain safe only if the attacker did not exfiltrate TMK derive results for them. Vault audit shows which derive calls were made.
- The admin audit log chain remains verifiable if the external checkpoints are intact (see `admin-audit-immutability.md` §6.2).

**Customer communication**
Mandatory escalation to §8. Vault compromise is a reportable KVKK incident if personal data confidentiality was affected.

---

## 5. DLP Service Compromise

**Detection signals**
- Unusual `transit/derive` or `transit/decrypt` call rate in Vault audit device.
- `dlp.v1.match` events with malformed payloads or schema violations.
- Break-glass SSH to DLP host without a matching ticket.
- DLP process memory footprint anomalous.
- AppArmor / seccomp denials appearing in host audit log.

**Immediate containment (within 10 minutes)**
1. Revoke the DLP service's Vault token.
2. Revoke the DLP service's NATS publish permissions.
3. `systemctl stop personel-dlp.service`.
4. Snapshot the DLP host.

**Investigation**
1. Vault audit: enumerate every `transit/decrypt` call during the compromise window. Each call corresponds to one PE-DEK unwrap, which corresponds to one or more keystroke blobs.
2. Map decrypted blobs to affected endpoints and affected employees.
3. Check whether `dlp.v1.match` events during the window carry anomalous fields — an attacker with DLP code execution could try to smuggle plaintext via match metadata.
4. Check the pattern rule bundle: was a rule recently activated that differs from the intended bundle?

**Recovery**
1. Rebuild the DLP host from a known-good image.
2. Issue a fresh Vault AppRole Secret ID (delivered via systemd credential, not stored).
3. Rotate TMK (§3.1 in `secret-rotation.md`).
4. Rotate all PE-DEKs cluster-wide.
5. Restart DLP on the clean host; verify new pattern rule bundle signature before activation.

**Blast radius assessment**
- Confirmed: keystroke content decrypted during the compromise window. All affected employees must be identified.
- TMK itself is **not** exposed — Vault policy denies export. The attacker could only decrypt on demand during the time they held the DLP Vault token.
- After token revocation, no further decrypts are possible even with offline wrapped DEK copies.

**Customer communication**
Mandatory escalation to §8. DLP compromise almost certainly triggers KVKK notification because keystroke content is özel nitelikli veri.

---

## 6. Stolen Signing Key (Code Signing or Project Signing)

**Detection signals**
- Physical theft or loss of the EV code-signing Yubikey.
- Vault audit device shows `transit/sign/project/release-signing` calls outside the release window, or from unexpected tokens.
- Unauthorized manifests detected at the Update Service.

**Immediate containment (within 1 hour)**
1. Revoke the code-signing certificate via the issuing CA (Sectigo portal for EV).
2. In Vault, destroy or re-key the project signing key: `vault write -f transit/keys/project/release-signing/rotate`.
3. Publish a signed "key-compromise" advisory manifest that pins all agents to the current known-good version, signed with the new key (agents trust the new key only after they've received a release embedding it — chicken-and-egg, see below).
4. Pause the canary advancement pipeline.

**Bootstrap paradox**
If the old key is the only key agents trust and it is compromised, the new key cannot reach agents via the normal signed channel. Recovery requires:

1. Emergency release built and signed with the current (old, compromised) key that embeds the new key's public key in its trust bundle. This is acceptable because the old key is still technically valid to the agent until an update replaces it, and the emergency release is reviewed by all three release approvers.
2. Canary that release at 100% weight with forced installation (override canary gating) — an audited exception to normal rollout policy.
3. Once every endpoint has the new trust bundle, the old key is no longer trusted.

**Investigation**
1. Review all releases signed during the window the key was unaccounted for.
2. Re-verify the SHA-256 of every deployed agent binary against the known-good hash. Any mismatch is a P0 supply-chain incident.
3. If a malicious release was signed, identify which endpoints installed it.

**Recovery**
- See bootstrap paradox above.
- If a malicious binary was deployed, the affected endpoints must be considered compromised and handled per §1.

**Customer communication**
P0 mandatory escalation. Most customers will freeze updates until the new trust chain is propagated.

---

## 7. Ransomware on Server Host

**Detection signals**
- Mass file encryption events on the server host (detectable via host EDR).
- Services failing en masse with I/O errors.
- Ransom note found on filesystem.
- Backup integrity check fails.

**Immediate containment (within 15 minutes)**
1. Isolate the host from the network. Do not power off — memory forensics may be needed.
2. Disable the NATS JetStream consumer for the affected services to prevent poisoned data from propagating.
3. Snapshot the host state.

**Investigation**
1. Identify ransomware family via signature matching (customer's EDR or incident response vendor).
2. Determine initial vector (phishing, exposed RDP, vulnerable service).
3. Enumerate which Personel data stores were encrypted: Postgres, ClickHouse, MinIO, NATS JetStream data.

**Recovery**
1. Do NOT pay ransom. Customer decision, but Personel's recommendation is documented.
2. Rebuild affected hosts from clean OS images.
3. Restore data stores from the most recent verified backup:
   - Postgres: `pg_basebackup` restore from the encrypted backup volume.
   - ClickHouse: `clickhouse-backup restore`.
   - MinIO: bucket mirror from backup bucket.
   - NATS JetStream: stream recovery from stored snapshots if available; otherwise in-flight events are lost but buffered on agents.
4. Verify the audit log hash chain from the restored Postgres is intact (see `admin-audit-immutability.md` §6).
5. Re-enroll Vault from its raft snapshot (see `vault-setup.md` §7). The unseal Shamir ritual is required.

**Blast radius assessment**
- Data loss bounded by the time since last backup (nightly cadence → up to 24h loss).
- Confidentiality impact depends on whether the attacker exfiltrated data before encrypting. Assume yes for worst-case planning.

**Customer communication**
P0. Escalate to §8 if any personal data is confirmed or suspected to have been exfiltrated.

---

## 8. KVKK Reportable Data Breach (72-hour Kurul Notification)

KVKK Article 12(5) requires data controllers to notify the KVKK Kurul "en kısa sürede" (as soon as possible) of breaches, interpreted as 72 hours by Kurul guidance aligned with GDPR. Because Personel is an on-prem product, the customer is the data controller; Personel is the data processor. Our contractual obligation is to notify the customer **within 24 hours** of breach detection to give them time to make their own Kurul filing.

### 8.1 Trigger conditions

Any of the following triggers the §8 flow:

- Confirmed exposure of personal data of an identifiable individual to an unauthorized party.
- Confirmed decryption or potential decryption of keystroke content by anyone other than the DLP service in its normal role.
- Confirmed exfiltration of screenshots, screen clips, or live view recordings.
- Ransomware incident where exfiltration cannot be ruled out.
- Vault or DLP compromise.

### 8.2 Within 24 hours — notify the customer

Turkish template (hand-edited per incident):

```
Sayın [müşteri yetkili],

[Tarih/Saat] itibarıyla Personel platformunuzu etkileyen güvenlik olayı
tespit edilmiştir. Olayın bilinen kapsamı ve alınan aksiyonlar aşağıdadır.

Olay kimliği: [PER-INC-yyyymmdd-n]
Tespit tarihi ve saati: [UTC+3]
Olay sınıfı: [Vault Uzlaşması / DLP Uzlaşması / Yönetici Hesabı Ele Geçirilmesi / ...]
Etkilenen veri kategorileri: [ekran görüntüsü / klavye içeriği / kullanıcı hesabı / ...]
Etkilenmesi muhtemel kişi sayısı (tahmini): [n]
Alınan ilk müdahale adımları:
  - [Uygulanmış containment adımları]
  - [Kriptografik rotasyonlar]
  - [Servis durdurma / izolasyon]
Devam eden çalışmalar: [inceleme, kök neden, toparlama]

KVKK Madde 12(5) kapsamında veri sorumlusu sıfatıyla Kurul'a bildirim
yükümlülüğünüz bulunduğu için bu bilgileri mümkün olan en kısa sürede
iletiyoruz. KVKK Kurul bildirim formunun doldurulmasında teknik destek
için [DPO iletişim] ile görüşebilirsiniz.

Güvenlik ekibimiz [tarih/saat]'te detaylı rapor sunacaktır.

Saygılarımızla,
Personel Güvenlik Ekibi
```

### 8.3 Within 72 hours — customer files with Kurul

Support the customer in producing the KVKK notification form (Kurul's standard form). Provide:
- Technical incident description translated to plain Turkish.
- Data categories and approximate record counts affected.
- Containment and remediation actions taken.
- Contact point at Personel for Kurul follow-up.

### 8.4 Within 7 days — detailed report

Deliver a full post-incident report to the customer containing:
- Timeline.
- Root cause analysis.
- Affected data categories with confirmed vs. potential exposure.
- List of affected employees (customer's responsibility to identify from tenant data; Personel helps with queries).
- Remediation actions and verification evidence.
- Preventive measures adopted.

### 8.5 Within 30 days — lessons learned and product changes

See §9.

---

## 9. Post-Incident Review Template

`docs/incidents/PER-INC-yyyymmdd-n.md`:

```markdown
# PER-INC-<id> — <short title>

**Severity**: P0 | P1 | P2
**Detected**: <utc>
**Declared**: <utc>
**Contained**: <utc>
**Resolved**: <utc>
**Incident commander**: <name>

## Summary
<one paragraph>

## Timeline
- hh:mm UTC — <event>
- ...

## Detection
<how we found out, what signals fired, what we missed>

## Containment
<what we did first, effectiveness, delays>

## Investigation
<what we found, evidence, forensic notes>

## Recovery
<how we restored normal operation, time to recover>

## Impact
- Data exposed: <category / count / confidence>
- Systems affected: <list>
- Customer communications: <timeline>

## Root cause
<technical + process causes>

## What went well
- ...

## What went wrong
- ...

## Action items
| Owner | Action | Due | Status |
|---|---|---|---|

## Follow-up reviews
- 30 days: <date>
- 90 days: <date>
```

Action items from the post-incident review MUST be tracked to closure. Closure review happens at the scheduled 30- and 90-day points.
