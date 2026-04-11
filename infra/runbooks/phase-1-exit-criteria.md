# Phase 1 Exit Criteria — Validation Guide

> TR: Bu belge, Faz 1 çıkış kriterlerinin nasıl doğrulanacağını açıklar.
> EN: This document explains how to validate Phase 1 exit criteria.

Per `docs/architecture/mvp-scope.md` §Exit Criteria.

---

## #17 — ClickHouse Replication Plan Validated

**Acceptance Test:**

```bash
sudo /opt/personel/infra/scripts/staging-replication-rig.sh --validate
```

**What "Validated" Means:**

1. Kill replica 1 during ingest → ingest continues on replica 2 with <500ms write pause
2. Restart replica 1 → catches up within 5 minutes
3. Read traffic failover transparent to Admin API (connection retry works)
4. TTL drops work correctly under replication (run OPTIMIZE FINAL; verify count drops)
5. Backup restore rebuilds a dead replica from MinIO in <1 hour for 100 GB of data

**Acceptance evidence:**
- `staging-replication-rig.sh --validate` output showing PASS for all 5 tests
- Manual backup restore drill log showing <1h restore time for 100 GB
- DBA sign-off that migration procedure is rehearsed twice

**Blocking condition:** No paying customer (beyond the pilot) may be onboarded until criterion #17 is cleared.

---

## #9 — Keystroke Isolation (Red Team)

**Test:** Independent red team attempts to decrypt keystroke content as an admin user.

**Evidence required:**
- Red team report confirming admin plane has no path to plaintext keystrokes
- Vault policy files at install match committed versions
- DLP container image matches release manifest SHA-256
- seccomp and AppArmor profiles match infra/compose/dlp/ versions

---

## #10 — Live View Governance

**Test:** Dual-control is enforced; 100% of sessions have hash-chained audit entries.

**Evidence required:**
```bash
# Verify every live_view_request.state='ended' has a matching audit entry
docker exec personel-postgres psql -U postgres -d personel -c "
SELECT lvr.id, ae.id AS audit_id
FROM core.live_view_requests lvr
LEFT JOIN audit.audit_events ae
  ON ae.entity_id = lvr.id::text AND ae.event_type LIKE 'live_view.%'
WHERE lvr.state = 'ended' AND ae.id IS NULL;"
# Must return 0 rows
```

---

## #20 — DSR SLA Timer

**Test:** Synthetic DSR ticket transitions open → at_risk → overdue.

```bash
# Insert a DSR with backdated created_at
docker exec personel-postgres psql -U postgres -d personel -c "
INSERT INTO core.dsr_requests (tenant_id, employee_email, request_type, justification, created_at)
VALUES (
  '${PERSONEL_TENANT_ID}'::UUID,
  'test-dsr@example.com',
  'access',
  'Test DSR for exit criterion #20',
  now() - INTERVAL '21 days'
);"

# Run DSR SLA check manually
docker exec personel-postgres psql -U postgres -d personel -c "
UPDATE core.dsr_requests SET state = 'at_risk', updated_at = now()
WHERE state = 'open' AND now() >= (created_at + INTERVAL '20 days') AND now() < sla_deadline;"

# Verify at_risk
docker exec personel-postgres psql -U postgres -d personel -c "
SELECT id, state, created_at, sla_deadline FROM core.dsr_requests WHERE employee_email = 'test-dsr@example.com';"
```

**Expected:** state = 'at_risk', Prometheus alert fires, DPO dashboard shows badge.

---

## #8 — Server Uptime ≥ 99.5%

**Measurement:** Grafana dashboard `personel-capacity` → service availability panel over 14-day pilot window.

```
Uptime % = (1 - total_down_minutes / (14 * 24 * 60)) * 100
Target:   ≥ 99.5% = ≤ 100.8 minutes downtime per 14 days
```
