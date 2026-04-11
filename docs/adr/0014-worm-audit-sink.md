# ADR 0014 — WORM Audit Sink: MinIO S3 Object Lock (Compliance Mode)

> Status: **Accepted** — 2026-04-10
> Deciders: devops-engineer, security-engineer, compliance-auditor
> Supersedes: nothing; extends the external checkpoint design in `docs/architecture/audit-chain-checkpoints.md`

---

## Context

The Personel audit log (`audit.audit_log`) is append-only at the PostgreSQL application level: the `audit_writer` role has `EXECUTE` on `audit.append_event` only; direct `INSERT/UPDATE/DELETE` is revoked. A row-level trigger blocks mutations for non-superusers.

However, a DBA with PostgreSQL superuser can execute:

```sql
ALTER TABLE audit.audit_log DISABLE TRIGGER ALL;
UPDATE audit.audit_log SET hash = ..., details = ... WHERE id = ...;
ALTER TABLE audit.audit_log ENABLE TRIGGER ALL;
```

This recomputes the chain forward from the tampered row and leaves no Postgres-level trace. The forgery is detectable only if an external checkpoint was written before the tampered range — and only if that checkpoint lives in a sink the DBA cannot also rewrite.

Legal review for the KVKK pilot is blocked until this is mitigated. The mitigation must be:

1. **Technically unbypassable** — not just policy-controlled
2. **Legally recognised** — accepted by Turkish data-protection jurisprudence and comparable to "WORM storage"
3. **Operationally simple** — does not add a new vendor, operator skill, or infrastructure layer
4. **Retention-capable** — must enforce the 5-year KVKK audit retention without operator action

---

## Decision

Implement **MinIO Object Lock in Compliance Mode** as the WORM audit sink (Option C from the design brief).

The audit verifier writes each daily hash-chain checkpoint batch as an immutable S3 object to a dedicated MinIO bucket (`audit-worm`) with:

- Object Lock enabled at bucket creation (cannot be enabled retroactively on an existing bucket)
- Retention mode: **COMPLIANCE** (the `s3:BypassGovernanceRetention` action is not available in compliance mode, even for the root/admin user)
- Retention period: **1826 days** (5 years + 1 leap-year day) matching KVKK Article 7 minimum for audit records subject to regulatory inquiry
- A dedicated service account (`audit-sink`) with `s3:PutObject` and `s3:GetObject` only; no `s3:DeleteObject`, no `s3:PutObjectRetention`, no bucket-level operations
- The MinIO root account does NOT hold delete capability for objects within their retention window (compliance mode guarantee)

---

## Alternatives Considered

### Option A — OpenSearch Write-Once Index

OpenSearch is already in the stack and already indexes audit events for search. A dedicated `personel-audit-worm` index with ILM `rollover → read_only` would provide a second copy.

Rejected because:
- OpenSearch `read_only` is an index setting, not an object-level lock. A superuser on the OpenSearch node can toggle `index.blocks.write` off. This is a software-enforced barrier, not a hardware/protocol guarantee.
- `cluster:admin/reindex` revocation helps but is fragile; OpenSearch's permission model changes across minor versions.
- No legal precedent for "WORM storage" in Turkish or international compliance frameworks refers to OpenSearch ILM — it specifically refers to Object Lock or equivalent write-once media.
- Does not simplify the operator skill requirement; OpenSearch index management is non-trivial.

### Option B — chattr +a Append-Only File

An ext4 filesystem with `chattr +a` enforced at mount time provides append-only semantics.

Rejected because:
- `chattr +a` can be cleared by root (`chattr -a`). Root access to the audit host would allow an attacker to remove the attribute, truncate the file, and re-add the attribute.
- `docs/security/runbooks/admin-audit-immutability.md` §9.3 already notes "A chattr bit that any root can remove is weaker than Object Lock."
- Does not scale across distributed or replicated deployments without additional complexity (dedicated NFS export, tape archive).
- Rotation semantics require careful implementation to avoid a window where files are writable.
- Not legally recognised as WORM storage in any compliance framework reviewed.

---

## Consequences

### Positive

- **Protocol-level immutability**: MinIO compliance mode prevents object deletion or version modification until the retention period expires. This guarantee is implemented in the S3 Object Lock spec (RFC, AWS S3 API documentation) and is accepted as WORM evidence in SOC 2, ISO 27001, SEC 17a-4, and by extension KVKK auditors who recognise WORM storage as tamper-evident.
- **No new vendor**: MinIO is already in the Personel stack. No new license, no new container image, no new operational skill beyond what the devops-engineer already maintains.
- **Automatic enforcement**: retention periods are enforced by MinIO without any operator action. There is no "forget to rotate" failure mode.
- **Legal defensibility**: a compliance-mode locked object can be presented to a Turkish court or KVKK Kurul as evidence. The chain of custody proof is: (a) the object was written at time T, (b) its content is identical to the checkpoint hash, (c) no one could have deleted or modified it.
- **Nightly cross-validation**: the WORM verifier (`personel-worm-verifier.service`) reads back objects from the WORM bucket and compares checkpoint hashes against Postgres. Divergence is a P0 alert.

### Negative / Risks

- **MinIO single point of failure**: if the MinIO data directory is lost (disk failure without replication), WORM objects are lost too. Mitigation: MinIO WORM is a secondary sink; Postgres remains the primary audit store. Loss of WORM objects does not lose audit history, but it weakens the tamper-evidence claim. RAID and backup of the MinIO data volume is required.
- **5-year bucket accumulation**: audit-worm objects cannot be deleted before retention expires. At 500 endpoints × estimated 2KB per checkpoint object × 365 days/year × 5 years ≈ 1.8 GB. Negligible on a 16 TB host. For high-volume deployments (10k endpoints, frequent checkpointing), size grows linearly but remains manageable.
- **Object Lock requires MinIO ≥ RELEASE.2021-01-05T05-22-38Z** (the version in the stack is well beyond this).
- **Bucket cannot have Object Lock disabled retroactively**: the `audit-worm` bucket must be created with Object Lock enabled from the start. The `worm-bucket-init.sh` script is idempotent for the creation step but cannot retrofit Object Lock onto an existing bucket.

---

## Implementation

- `infra/compose/minio/worm-bucket-init.sh` — idempotent bucket creation script
- `infra/compose/docker-compose.yaml` — `minio-init` service updated to also run `worm-bucket-init.sh`
- `apps/api/internal/audit/worm.go` — Go implementation of the `WORMSink` interface
- `apps/api/internal/audit/verifier.go` — updated to cross-validate Postgres chain vs WORM objects
- `infra/systemd/personel-worm-verifier.service` + `.timer` — nightly WORM verification
- `docs/security/runbooks/worm-audit-recovery.md` — incident response runbook

---

## Related

- `docs/architecture/audit-chain-checkpoints.md` (updated to include WORM sink)
- `docs/security/runbooks/admin-audit-immutability.md` §9.3
- `docs/security/security-architecture-decisions.md` SD-8
- `docs/adr/0008-audit-log-postgres-hash-chain.md` (if it exists; reference to the baseline decision)
