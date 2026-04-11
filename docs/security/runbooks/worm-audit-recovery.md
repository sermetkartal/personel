# Runbook — WORM Audit Chain Divergence Recovery

> Language: English. Audience: security on-call, devops-engineer, compliance-auditor, DPO.
> Scope: forensic response when `personel-worm-verifier.service` detects that the Postgres
> audit chain head does not match the MinIO Object Lock checkpoint for the same day.
>
> Companion: `docs/security/runbooks/admin-audit-immutability.md`, `docs/adr/0014-worm-audit-sink.md`

---

## 1. What This Runbook Covers

The nightly WORM verifier (`personel-worm-verifier.timer`, 04:00 local) reads yesterday's checkpoint from the `audit-worm` MinIO bucket and compares its `last_hash` to the live Postgres `audit.audit_log` chain tail for the same day.

A **WORMDivergenceError** means one of two things happened after the WORM checkpoint was written at 03:30:

1. **Tampering (most serious)**: A Postgres superuser recomputed the hash chain after the checkpoint was written. The WORM object reflects a state that no longer matches Postgres.
2. **WORM write failure followed by a legitimate event** (less serious, but still requires investigation): The 03:30 verifier wrote the WORM checkpoint at time T, then a legitimate audit event was appended between T and 04:00 before the cross-check ran. The verifier logic excludes events after 03:30 from the daily checkpoint range, so this should not happen — but network partitions or race conditions could theoretically produce it.

**Both cases are P0 incidents**. Do not attempt to determine which case applies until all evidence is preserved.

---

## 2. Alert Triggering

The divergence alert fires when any of the following occur:

- `personel_worm_verify_last_success == 0` (Prometheus alert rule `WormAuditVerifyFailed`)
- `personel-worm-verifier.service` exits with status 2 (WORMDivergenceError) or status 1 (infrastructure error)
- Manually confirmed by running: `docker compose exec -T api /personel-api audit worm-verify --tenant-id <UUID> --day <YYYY-MM-DD>`

---

## 3. Immediate Response (< 15 minutes)

### 3.1 Preserve the Postgres dump

Before any other action, snapshot the current state of Postgres. Do NOT restart the API or DBA processes.

```bash
# On the Personel host, as root
pg_dump -h localhost -U postgres -t audit.audit_log \
  --format=custom --compress=9 \
  -f /var/lib/personel/forensics/audit_log_$(date +%Y%m%dT%H%M%S).dump personel
```

This creates a timestamped forensic snapshot that can be compared to the WORM object.

### 3.2 Lock the database (optional, operator judgement)

If you suspect active tampering is in progress, lock the audit table to prevent further writes while you investigate:

```sql
-- Connect as postgres superuser (break-glass credential)
BEGIN;
LOCK TABLE audit.audit_log IN EXCLUSIVE MODE;
-- Leave this transaction open until §4 is complete or you determine it is safe to continue.
```

Locking the table blocks all admin API operations that write audit events. Notify the DPO and IT Security before doing this in production.

### 3.3 Page the DPO and security on-call

Immediately notify:
- DPO (`${DPO_EMAIL}`)
- Security on-call (PagerDuty / Slack `#security-incidents`)
- Customer IT Security contact (if this is a customer-facing incident)

Use the following incident title: `[P0] Audit Chain Divergence Detected — tenant <UUID> day <YYYY-MM-DD>`

---

## 4. Evidence Collection

### 4.1 Retrieve the WORM object

```bash
# Using the audit-sink credentials (read-only to audit-worm)
mc alias set forensic http://localhost:9000 \
  "${MINIO_AUDIT_SINK_ACCESS_KEY}" "${MINIO_AUDIT_SINK_SECRET_KEY}"

mc cp forensic/audit-worm/checkpoints/<TENANT_ID>/<YYYY-MM-DD>.json \
  /var/lib/personel/forensics/worm_checkpoint_<YYYY-MM-DD>.json

# Verify object integrity — MinIO returns an ETag (MD5 of the object) that
# you can compare against a local MD5 of the downloaded file.
mc stat forensic/audit-worm/checkpoints/<TENANT_ID>/<YYYY-MM-DD>.json
md5sum /var/lib/personel/forensics/worm_checkpoint_<YYYY-MM-DD>.json
```

The WORM object's `last_hash` is the authoritative record of what the chain looked like at 03:30. If Postgres now shows a different hash for the same id range, the chain was modified after the checkpoint.

### 4.2 Identify the divergence point

Run the on-demand verifier to find the first row that no longer matches the WORM checkpoint's chain:

```bash
docker compose exec -T api /personel-api audit verify \
  --tenant-id <UUID> \
  --range 1-<WORM_LAST_ID> \
  --verbose
```

This will report the exact `id` at which the recomputed hash diverges from the stored hash. The rows before this `id` are still intact; the rows at and after it may be forged.

### 4.3 Query Postgres audit logs for superuser activity

```sql
-- Check for recent superuser logins (requires pg_log enabled)
SELECT usename, application_name, backend_start, query_start
FROM pg_stat_activity
WHERE usesuper = true;

-- Check if any maintenance session recently ran
SELECT log_time, user_name, application_name, message
FROM pg_csvlog  -- or equivalent log table if pg_log is configured
WHERE log_time > now() - interval '12 hours'
  AND message ILIKE '%DISABLE TRIGGER%'
ORDER BY log_time DESC;
```

Also check the Vault audit log for any break-glass token usage:

```bash
docker compose exec -T vault vault audit-log list
```

---

## 5. Containment

### 5.1 If tampering is confirmed

1. **Freeze the affected tenant**: disable all admin API write operations for the tenant. This prevents the attacker from further modifying audit history while the investigation is open.
2. **Revoke the Postgres superuser session**: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usesuper = true AND pid <> pg_backend_pid();`
3. **Rotate the Postgres superuser password** using the break-glass ritual in `docs/security/runbooks/vault-setup.md §Break-Glass`.
4. **Rotate all Vault tokens** for the compromised session.
5. **File a personal data breach notification** under KVKK Article 12(5): the DPO must notify the KVKK Kurulu within 72 hours if the breach involves personal data.

### 5.2 If WORM write failure is confirmed (no tampering)

1. The WORM object for the affected day does not exist, so there was no convergence — the cross-check fired because yesterday's chain write to MinIO failed.
2. Manually write a late WORM checkpoint using the CLI:
   ```bash
   docker compose exec -T api /personel-api audit worm-backfill \
     --tenant-id <UUID> --day <YYYY-MM-DD>
   ```
3. Re-run the cross-validation to confirm it now passes.
4. Investigate why the initial WORM write failed (network partition, MinIO unavailability, credential rotation gap).

---

## 6. Legal Evidence Pack

If the incident requires regulatory disclosure, compile the following evidence pack (see `docs/security/runbooks/admin-audit-immutability.md §8.1` for the full template):

1. The Postgres forensic dump from §3.1.
2. The WORM object downloaded in §4.1 (with WORM object metadata showing `RetainUntilDate`).
3. The divergence report from §4.2 showing the first diverging `id`.
4. Vault audit logs for the affected time window.
5. A signed attestation from the DPO or security-engineer describing the timeline.

The WORM object's `RetainUntilDate` field in its S3 object metadata proves that the object was written before the manipulation occurred and could not have been modified after creation. This is the primary forensic evidence.

---

## 7. Post-Incident Review

After the incident is resolved:

1. Update `docs/compliance/hukuki-riskler-ve-azaltimlar.md` with a post-incident entry.
2. Trigger a blameless post-mortem within 5 business days.
3. Review whether the checkpoint interval should be shortened (currently daily at 03:30). Shorter intervals reduce the tamper window.
4. Consider enabling Postgres write-ahead log (WAL) archiving to an append-only sink for forensic completeness.

---

## 8. Contacts

| Role | Contact |
|---|---|
| Security on-call | PagerDuty `personel-security` |
| DPO | `${DPO_EMAIL}` |
| DPO (secondary) | `${DPO_SECONDARY_EMAIL}` |
| Compliance auditor | via DPO |
| MinIO admin | devops-engineer (on-call) |
| Postgres DBA | postgres-pro (escalate via devops) |
| Legal / KVKK counsel | via DPO |

---

*Last updated: 2026-04-10. Owner: devops-engineer. Review cadence: annually or after any P0 incident.*
