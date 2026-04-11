# Runbook — Admin Audit Log Immutability

> Language: English. Audience: backend-developer, postgres-pro, compliance-auditor, security on-call. Scope: the tamper-evident hash-chained audit log that records every privileged admin action. Companion to `docs/architecture/c4-container.md` (Audit Log Service row) and `docs/security/threat-model.md` Flow 3.

## 1. Design Constraints

- Single Postgres database; no separate ledger technology in Phase 1.
- Append-only semantics enforced by schema, grants, and a stored procedure.
- Tamper-evidence (not tamper-prevention) via SHA-256 hash chain plus daily external checkpoint.
- Legal defensibility for KVKK/VERBİS audits and Turkish court evidence.
- No dependency on specific Postgres versions beyond 14+.

## 2. Schema

```sql
CREATE SCHEMA audit;

CREATE TABLE audit.audit_log (
    id         BIGSERIAL PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor      TEXT        NOT NULL,        -- user id, service identity, or "system"
    actor_ip   INET,
    actor_ua   TEXT,
    tenant_id  UUID        NOT NULL,
    action     TEXT        NOT NULL,        -- enumerated; see §7
    target     TEXT        NOT NULL,        -- resource identifier; e.g. endpoint id, employee id
    details    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    prev_hash  BYTEA       NOT NULL,
    hash       BYTEA       NOT NULL,
    CONSTRAINT audit_log_hash_len CHECK (octet_length(hash) = 32 AND octet_length(prev_hash) = 32)
);

CREATE INDEX audit_log_ts        ON audit.audit_log (ts);
CREATE INDEX audit_log_actor_ts  ON audit.audit_log (actor, ts);
CREATE INDEX audit_log_tenant_ts ON audit.audit_log (tenant_id, ts);
CREATE INDEX audit_log_action_ts ON audit.audit_log (action, ts);

CREATE TABLE audit.audit_checkpoint (
    day          DATE PRIMARY KEY,
    last_id      BIGINT NOT NULL,
    last_hash    BYTEA NOT NULL,
    entry_count  BIGINT NOT NULL,
    verified_at  TIMESTAMPTZ NOT NULL,
    verifier     TEXT NOT NULL
);
```

## 3. Hash Computation

```
hash = SHA-256(
    id_be64        ||
    ts_unix_nanos_be64 ||
    len(actor)_be32 || actor_bytes ||
    len(tenant_id)_be32 || tenant_id_bytes_16 ||
    len(action)_be32 || action_bytes ||
    len(target)_be32 || target_bytes ||
    len(details_canon)_be32 || details_canon_bytes ||
    prev_hash_32
)
```

- `details_canon` is the canonical JSON encoding of the `details` JSONB (keys sorted lexicographically, no whitespace, UTF-8).
- `id_be64` is the assigned BIGSERIAL after INSERT.
- `ts_unix_nanos_be64` uses microsecond precision from Postgres extracted as nanoseconds for forward compatibility.
- `prev_hash_32` for the first row (id=1) is 32 bytes of 0x00 (the "genesis hash").

The canonical encoder is implemented in a single Go package `internal/audit/canon` to guarantee byte-exact reproducibility across the writer and the verifier.

## 4. Write Flow

Writes go through a stored procedure that computes the hash server-side and enforces chain continuity. Application code has INSERT privilege only through this procedure; direct INSERT to `audit.audit_log` is revoked from the app role.

```sql
CREATE OR REPLACE FUNCTION audit.append_event(
    p_actor     TEXT,
    p_actor_ip  INET,
    p_actor_ua  TEXT,
    p_tenant_id UUID,
    p_action    TEXT,
    p_target    TEXT,
    p_details   JSONB
) RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
    v_prev_hash BYTEA;
    v_new_id    BIGINT;
    v_new_hash  BYTEA;
    v_canon     BYTEA;
    v_ts        TIMESTAMPTZ := clock_timestamp();
BEGIN
    -- Lock the tail so concurrent appends serialize.
    PERFORM pg_advisory_xact_lock(hashtextextended('audit.audit_log', 0));

    SELECT hash INTO v_prev_hash
    FROM audit.audit_log
    ORDER BY id DESC
    LIMIT 1;

    IF v_prev_hash IS NULL THEN
        v_prev_hash := decode('0000000000000000000000000000000000000000000000000000000000000000', 'hex');
    END IF;

    -- Reserve the id so the hash can incorporate it.
    v_new_id := nextval('audit.audit_log_id_seq');

    v_canon := audit.canon_details(p_details);
    v_new_hash := audit.compute_hash(
        v_new_id, v_ts, p_actor, p_tenant_id, p_action, p_target, v_canon, v_prev_hash
    );

    INSERT INTO audit.audit_log
        (id, ts, actor, actor_ip, actor_ua, tenant_id, action, target, details, prev_hash, hash)
    VALUES
        (v_new_id, v_ts, p_actor, p_actor_ip, p_actor_ua, p_tenant_id, p_action, p_target, p_details, v_prev_hash, v_new_hash);

    RETURN v_new_id;
END;
$$;
```

Supporting functions `audit.canon_details(jsonb) returns bytea` and `audit.compute_hash(...)` are implemented either as pure PL/pgSQL or (preferred) as a PL/Rust or `pg_tle` extension to match the Go writer's canonical encoding byte-for-byte.

## 5. Grants and Revocations

```sql
-- Roles.
CREATE ROLE audit_writer NOLOGIN;
CREATE ROLE audit_reader NOLOGIN;
CREATE ROLE app_admin_api LOGIN PASSWORD '...'; -- managed by Vault dynamic secrets
CREATE ROLE app_audit_reader LOGIN PASSWORD '...';

-- audit_writer can call the stored procedure, nothing else.
GRANT USAGE ON SCHEMA audit TO audit_writer;
GRANT EXECUTE ON FUNCTION audit.append_event(TEXT, INET, TEXT, UUID, TEXT, TEXT, JSONB) TO audit_writer;

-- Explicit revocations on the table.
REVOKE ALL ON audit.audit_log FROM audit_writer;
REVOKE ALL ON audit.audit_log FROM PUBLIC;

-- audit_reader has SELECT only.
GRANT USAGE ON SCHEMA audit TO audit_reader;
GRANT SELECT ON audit.audit_log TO audit_reader;
GRANT SELECT ON audit.audit_checkpoint TO audit_reader;

-- Application roles inherit.
GRANT audit_writer TO app_admin_api;
GRANT audit_reader TO app_audit_reader;

-- Deny UPDATE and DELETE at the Postgres level for non-superusers.
-- Belt-and-braces: a row-level trigger rejecting UPDATE/DELETE regardless of the role.
CREATE OR REPLACE FUNCTION audit.reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit.audit_log is append-only';
END;
$$;

CREATE TRIGGER trg_audit_log_no_update
    BEFORE UPDATE OR DELETE ON audit.audit_log
    FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();
```

## 6. Verification Jobs

### 6.1 Nightly full-chain verifier

A systemd timer (`personel-audit-verify.timer`) runs at 02:30 local time. The verifier:

1. SELECTs every row from `audit.audit_log` in id order. For large datasets, batches of 50k rows.
2. Recomputes each row's hash using the same canonical encoder as the stored procedure.
3. Asserts `row.hash == computed_hash`.
4. Asserts `row.prev_hash == previous_row.hash`.
5. On success, writes an entry into `audit.audit_checkpoint` for yesterday's date: `last_id`, `last_hash`, `entry_count`, `verified_at=now()`, `verifier=hostname()`.
6. Also writes the checkpoint to an external tamper-evident store (see §6.2).
7. On failure, emits `audit.chain_broken` alert priority 0, halts further verifier runs until resolved, and opens an incident per `incident-response-playbook.md` §3/4 depending on scope.

### 6.2 External checkpoint sink

Two options, customer picks one at install time:

- **Append-only file on a dedicated audit server**: the checkpoint is written as a newline-delimited JSON record to `/var/lib/personel/audit-checkpoints/<year>.ndjson`, on an ext4 filesystem mounted with `chattr +a` (append-only) at the inode level. Only root can remove the append attribute, and root access is subject to the break-glass ritual.
- **S3 Object Lock (compliance mode)**: if the customer operates a MinIO or AWS S3 bucket with Object Lock in compliance retention mode, checkpoints are uploaded as individual objects. Compliance mode prevents even the root account from deleting objects before the retention period expires.

Each checkpoint record is also signed with the control signing key (same Ed25519 key used for manifests and policy bundles). A signed checkpoint's integrity can be proven even if Postgres is fully compromised, provided the signing key is not.

### 6.3 On-demand verifier

Operators and auditors can run `personel-audit-verify --range <id_start>-<id_end>` to re-verify an arbitrary range. Used during incident investigation and by compliance-auditor on evidence requests.

## 7. Mandatory Audit Actions

Every listed action MUST produce exactly one `audit.append_event` call. A CI lint enforces this via a test that replays all Admin API handlers and asserts a stub audit writer was invoked.

| Action enum | Triggered by | Target |
|---|---|---|
| `admin.login.success` | Admin login | user id |
| `admin.login.failed` | Failed login | attempted username |
| `admin.login.locked` | Account lockout | user id |
| `admin.logout` | Admin logout | user id |
| `admin.session.refreshed` | Session refresh | session id |
| `admin.password.changed` | Password change | user id |
| `admin.mfa.enrolled` | MFA enrollment | user id |
| `admin.mfa.disabled` | MFA disabled | user id |
| `user.created` | Create user | user id |
| `user.role_changed` | Role change | user id |
| `user.disabled` | User disable | user id |
| `user.deleted` | User delete | user id |
| `policy.created` | Policy create | policy id |
| `policy.updated` | Policy update | policy id |
| `policy.deleted` | Policy delete | policy id |
| `policy.pushed` | Policy push to endpoint cohort | cohort descriptor |
| `endpoint.enrolled` | New endpoint enrollment | endpoint id |
| `endpoint.revoked` | Endpoint cert revoked | endpoint id |
| `endpoint.deleted` | Endpoint removed | endpoint id |
| `screenshot.viewed` | Admin viewed an employee screenshot | screenshot id |
| `screenshot.exported` | Admin exported screenshot | screenshot id |
| `screenclip.viewed` | Admin viewed a screen clip | clip id |
| `file_event.viewed` | Admin viewed file event details | event id |
| `network_event.viewed` | Admin viewed network flow details | event id |
| `live_view.requested` | Admin requested live view | session id |
| `live_view.approved` | HR approved live view | session id |
| `live_view.denied` | HR denied live view | session id |
| `live_view.started` | Live view session started | session id |
| `live_view.stopped` | Live view session stopped | session id |
| `live_view.terminated_by_hr` | HR emergency stop | session id |
| `live_view.terminated_by_dpo` | DPO emergency stop | session id |
| `dlp.rule.drafted` | DLP rule drafted | rule id |
| `dlp.rule.approved` | DLP rule approved (dual control) | rule id |
| `dlp.rule.signed` | Rule bundle signed | bundle id |
| `dlp.rule.activated` | Rule bundle activated in DLP | bundle id |
| `dlp.match.viewed` | Admin viewed a DLP match record | match id |
| `employee.created` | Employee onboarded | employee id |
| `employee.updated` | Employee metadata updated | employee id |
| `employee.deleted` | Employee deleted | employee id |
| `retention.policy.changed` | Retention matrix updated | category |
| `retention.purge.executed` | Retention purge ran | category+range |
| `export.requested` | Data export requested | request id |
| `export.generated` | Export produced | export id |
| `export.downloaded` | Export downloaded | export id |
| `admin.breakglass.dlp_host.opened` | Break-glass SSH to DLP | host id |
| `admin.breakglass.dlp_host.closed` | Break-glass session ended | host id |
| `admin.breakglass.vault.opened` | Break-glass token use | token accessor |
| `vault.policy.changed` | Vault policy file update | policy name |
| `pki.cert.issued` | Vault PKI issuance | cert serial |
| `pki.cert.revoked` | Cert revoked | cert serial |
| `release.signing_key_rotated` | Release signing key rotation | key id |
| `release.published` | Update manifest published | manifest id |
| `release.canary_advanced` | Canary cohort widened | manifest id + cohort |
| `release.rolled_back` | Release rolled back | manifest id |
| `audit.chain_verified` | Verifier ran successfully | date |
| `audit.chain_broken` | Verifier failed | date + break point |

`details` JSONB holds the specifics. The enumeration lives in `internal/audit/actions.go` and is covered by a compile-time exhaustiveness check.

## 8. Legal Defensibility

The audit log is designed to be quotable in a Turkish court as evidence of what happened (or did not happen) on the platform.

### 8.1 Evidence pack

On subpoena or KVKK Kurul request, compliance-auditor produces:

1. A Postgres dump of `audit.audit_log` filtered to the relevant date range.
2. The corresponding `audit.audit_checkpoint` rows.
3. The matching checkpoint file records from the external sink (§6.2), with Ed25519 signatures.
4. A signed attestation from security-engineer describing:
   - The schema and write flow (by reference to this runbook).
   - The hash computation spec (§3).
   - The verifier output for the range.
   - A comparison of `audit.audit_checkpoint.last_hash` against the corresponding external checkpoint's `last_hash`.
5. The control signing public key, so the court-appointed expert can independently verify the external checkpoint signatures.
6. A copy of this runbook with a cryptographic hash matching the git tag under which the running system was deployed.

### 8.2 Expert witness procedure

The expert re-runs the verifier on the dumped data, confirms the chain verifies, compares to the external checkpoints, and signs an affidavit. Because the verifier is deterministic and the canonical encoder is specified, an independent implementation can be audited.

## 9. Honest Tamper-Evidence Limits

We do NOT claim tamper-prevention. A DBA or attacker with Postgres superuser access can:

- Forge rows by recomputing the chain forward. This is detectable only if the forged range has NOT been checkpointed yet.
- Destroy the entire log by `DROP TABLE`. Detectable immediately by loss of the writer stored procedure and by the next verifier run.
- Rewrite the log but not the external checkpoint. The next verifier run will find a mismatch between Postgres chain and the last external checkpoint hash, at which point the forgery is provable.

Mitigations:

1. **Separate audit DB**: audit runs on its own Postgres instance (`personel-audit-db`), physically separated where possible, with its own DBA credentials. Application DBAs do not have superuser on the audit DB.
2. **Shortest possible checkpoint interval**: daily is the maximum acceptable; for hostile environments, checkpoint hourly.
3. **External sink must be truly append-only**: chattr +a with custody of root access to the audit host, or S3 Object Lock compliance mode. A chattr bit that any root can remove is weaker than Object Lock.
4. **No superuser in normal ops**: Postgres superuser password is sealed in the break-glass envelope. Day-to-day operations use the `app_admin_api` role only.
5. **Backup encryption**: `pg_basebackup` to an encrypted volume. An attacker with offline backup access can read history but cannot forge future entries without re-injecting into the live DB.

## 10. Operational Integration

- `audit.append_event` is called from every Admin API handler via a middleware wrapper. Handlers that do not call it fail a CI test.
- `audit_writer` role credentials are dynamic secrets issued by Vault; they rotate every 24h. See `secret-rotation.md` §2.
- Verifier timer, checkpoint writer, and alert path are owned by devops-engineer.
- Schema migrations for `audit.*` require two reviewers: backend-developer and security-engineer.

## 11. Handoffs

- **backend-developer**: implements `audit.append_event`, the middleware wrapper, canonical encoder, verifier CLI, and the exhaustive action enumeration with CI checks.
- **postgres-pro**: owns schema, triggers, grants, and the `audit.canon_details` / `audit.compute_hash` implementation (preferably as an extension for performance and byte-exact match).
- **devops-engineer**: owns the verifier timer, external checkpoint sink, and alert plumbing.
- **compliance-auditor**: owns the evidence pack template and the expert-witness affidavit boilerplate.
