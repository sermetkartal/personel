# Incident Response Runbook

> TR: Bu runbook, `docs/security/runbooks/incident-response-playbook.md` dosyasını referans alır.
> EN: This runbook references `docs/security/runbooks/incident-response-playbook.md`.

## Quick Reference

| Incident | Severity | First Action |
|---|---|---|
| Vault sealed | P0 | `scripts/vault-unseal.sh` |
| DLP offline >15 min | P1 | Check logs, restart |
| Audit chain broken | P0 | Do NOT delete files; escalate |
| Agent heartbeat cluster | P1 | Check `pki.v1.revoke` |
| DSR SLA overdue | P0 (compliance) | DPO notified automatically |

## KVKK Data Breach (72-hour rule)

Per KVKK Article 12(5):
1. Detect incident → open `PER-INC-YYYYMMDD-N` ticket
2. Within 24 hours → notify customer DPO (template in `incident-response-playbook.md §8.2`)
3. Within 72 hours → customer files with KVKK Kurul
4. Within 7 days → full technical report

**Forensic bundle export:**
```bash
sudo /opt/personel/infra/scripts/export-forensic-bundle.sh \
  --incident-id PER-INC-20260410-1 \
  --since 2026-04-01 \
  --until 2026-04-10
```

## Full Playbook

See: `docs/security/runbooks/incident-response-playbook.md`
