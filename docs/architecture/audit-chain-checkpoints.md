# Audit Hash Chain Checkpoints

> Language: English. Status: Authoritative. Closes Gap 11 from the Phase 0 revision round. Cross-references the security-engineer runbook `docs/security/runbooks/admin-audit-immutability.md`.

## Problem

The audit log is an append-only, hash-chained table in PostgreSQL (see `docs/architecture/live-view-protocol.md` §Audit Hash Chain and `docs/architecture/bounded-contexts.md` §Cross-Cutting Concerns). Over a 5-year retention horizon and tens of thousands of events per tenant per day, the chain will grow to tens of millions of records. Naive full-chain verification is O(n) and becomes operationally impractical at year 2 and beyond. It is also too slow to run inline on user-facing queries.

## Decision

Introduce **daily signed checkpoints** over the audit chain, stored in two places for defense in depth, and verified incrementally.

### Checkpoint Record

```
audit_checkpoints
-----------------
id              bigint primary key
tenant_id       uuid
period_start    timestamptz   -- usually 00:00 local
period_end      timestamptz   -- usually 23:59:59.999999 local
first_seq       bigint        -- first audit_records.seq covered
last_seq        bigint        -- last audit_records.seq covered
chain_head_hash bytea         -- audit_records.this_hash at last_seq
record_count    bigint
created_at      timestamptz
signed_by       text          -- key id of the control-plane signing key
signature       bytea         -- Ed25519 over canonical(period_start||period_end||first_seq||last_seq||chain_head_hash||record_count)
external_ref    text          -- optional: pointer to the external append-only sink record
```

One row per tenant per day. Multiple rows per day are permitted for on-demand checkpointing (e.g., before a backup).

### Daily Job

A scheduled job (`audit-service` systemd timer, 02:15 local) per tenant:

1. Load the previous checkpoint (`last_seq_prev`).
2. Walk `audit_records` from `last_seq_prev + 1` to `MAX(seq)` at the moment of checkpoint start, recomputing `this_hash` from each record and the rolling previous hash. (This re-reads data; the chain itself is not recomputed in production, just verified.)
3. Compare recomputed `this_hash` at the final record to the stored `this_hash`. Any mismatch is a P0 incident → halt the job, freeze the tenant's audit writes, alert DPO + security team.
4. On success, compute the signature over the canonical struct and `INSERT` the checkpoint record.
5. Push the signed checkpoint to the **external append-only sink**.

### External Append-Only Sink

A second copy of every checkpoint is pushed to an external sink that the Personel admin plane cannot rewrite. Phase 1 options (customer picks at install):

- **Profile A — Local WORM volume**: a write-once medium on the customer side (e.g., a dedicated NFS export mounted read-only after each write, or a tape archive fed by a nightly job).
- **Profile B — Customer SIEM**: Syslog/CEF forwarder with receipt acknowledgement from the SIEM.
- **Profile C — Customer object store with object-lock**: S3-compatible bucket with object-lock enabled, separate IAM credentials; Personel's admin plane does not hold the delete-capable role.

All three profiles produce a pointer written back to `audit_checkpoints.external_ref`. If the external sink is unreachable for more than 24 hours, the checkpoint job still runs (local copy is not blocked), but an alert fires and the gap is later reconciled.

### Verification at Read Time

For the common case ("is the audit history intact for the last 30 days?"), verification now needs only to walk from the most recent checkpoint forward, not from seq=1. This is O(1 day of records) instead of O(all records).

For long-range proofs ("is everything since 2026-01-01 intact?"), verification walks the chain of checkpoints (which is itself a hash chain by virtue of `chain_head_hash` being the head of the previous checkpoint's coverage), validates each checkpoint's signature, and samples records. The cost is O(number of checkpoints) signature verifications plus O(sampled records).

### Integrity Monitor

A separate nightly monitor (distinct from the checkpoint job; runs at 03:15) performs a **random sample full verification**: pick N random records per tenant per day, walk from the nearest checkpoint forward, confirm the walked hash matches the stored hash. Any deviation is P0.

### Key Hierarchy for Checkpoint Signing

Checkpoint signatures use a dedicated key:

- **Control-plane signing key**: Ed25519, held in Vault transit under `transit/keys/control-plane-signing`. Distinct from the release signing key and the policy signing key. Distinct from LVMK and TMK. Rotated yearly with grace overlap.
- **Signer identity**: `audit-service` Vault AppRole; no other service can sign.
- **Verifier identity**: public key is embedded in the Admin Console and the Transparency Portal read path. Rotations are delivered via a signed pinset update.

### Cross-Context References

- **Audit & Compliance** context (see `bounded-contexts.md`) owns the checkpoint aggregate. It is listed as a cross-cutting concern because every other context consumes it as a proof-of-integrity.
- **Crypto / Key Management** context issues and rotates the signing key.
- **Transparency** context exposes (for the DPO) a checkpoint-viewer that renders the most recent N checkpoints, their signatures' validity, and any integrity alerts.

## Consequences

- Verification cost is bounded and practical at 5-year retention.
- A malicious admin that manages to `UPDATE` audit records must also forge all subsequent checkpoint signatures, forge the external sink's record, and remain undetected by the random-sample monitor. The attack surface is narrow and detectable.
- One additional Vault key to manage (the control-plane signing key). Operational overhead is small.
- If the checkpoint job fails silently, chain integrity can degrade undetected; we mitigate with monitoring alarms on job runtime, job freshness (`MAX(created_at)` per tenant), and random-sample monitor.

## WORM Sink — MinIO Object Lock (Phase 1 Polish)

> Added in Phase 1 Polish sprint. Resolves the open concern from `docs/security/security-architecture-decisions.md` SD-8 and `CLAUDE.md §10`.

The "Profile C — Customer object store with object-lock" from the original external sink design is now the **default and mandatory** Phase 1 sink, implemented using the on-stack MinIO instance rather than relying on the customer to provide a separate bucket.

### Why MinIO Object Lock over the other Profile options

The `docs/adr/0014-worm-audit-sink.md` ADR documents the full rationale. In summary:

- Profile A (WORM volume) and chattr+a are software-enforced; root can bypass them.
- Profile B (Customer SIEM) requires the customer to operate and secure a SIEM, creating a dependency outside Personel's control.
- MinIO Object Lock Compliance Mode is a protocol-level guarantee. Even the MinIO root account cannot delete or modify a Compliance-locked object before its retention period expires.

### Architecture Addition

```
audit.audit_log (Postgres)
        │
        │ nightly at 03:30 (personel-audit-verifier.timer)
        ▼
audit.audit_checkpoint (Postgres)
        │
        │ same job writes WORM checkpoint
        ▼
audit-worm bucket (MinIO — Object Lock COMPLIANCE, 5-year retention)
        │
        │ nightly at 04:00 (personel-worm-verifier.timer)
        │ reads back WORM object and compares LastHash
        ▼
Divergence? → WORMDivergenceError → P0 alert → worm-audit-recovery.md
```

### Key Properties of the WORM Sink

- Bucket: `audit-worm`, created at install time by `infra/compose/minio/worm-bucket-init.sh`
- Object Lock mode: `COMPLIANCE` (no bypass even for root)
- Default retention: 1826 days (5 years + 1 day buffer)
- Object key scheme: `checkpoints/{tenant_id}/{YYYY-MM-DD}.json`
- Service account: `audit-sink` (PutObject + GetObject only; no DeleteObject)
- Implementation: `apps/api/internal/audit/worm.go` (`WORMSink` type)
- Cross-validation: `apps/api/internal/audit/verifier.go` (`CrossValidateWORM`)
- Systemd: `infra/systemd/personel-worm-verifier.service` + `.timer`
- Recovery runbook: `docs/security/runbooks/worm-audit-recovery.md`

### Consequences for Tamper-Evidence Claim

With the WORM sink in place, the tamper-evidence guarantee is strengthened:

- A DBA who disables Postgres triggers and rewrites history will produce a Postgres chain whose head hash differs from the WORM checkpoint written at 03:30. The 04:00 cross-validation will detect this within 24 hours.
- The WORM object's `RetainUntilDate` in S3 object metadata provides independently verifiable proof of when the checkpoint was written. No party with access to the Personel system can alter this.
- Legal defensibility: the WORM object + its S3 metadata + the divergence report constitutes forensic evidence admissible in KVKK proceedings. See `docs/security/runbooks/admin-audit-immutability.md §8` for the evidence pack template.

## Related

- `docs/security/runbooks/admin-audit-immutability.md`
- `docs/security/runbooks/worm-audit-recovery.md` (new — Phase 1 Polish)
- `docs/adr/0014-worm-audit-sink.md` (new — Phase 1 Polish)
- `docs/architecture/live-view-protocol.md` §Audit Hash Chain
- `docs/architecture/bounded-contexts.md` §Cross-Cutting Concerns
- `docs/architecture/data-retention-matrix.md` (5-year retention for audit entries)
