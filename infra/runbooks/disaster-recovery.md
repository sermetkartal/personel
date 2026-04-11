# Disaster Recovery Runbook

> TR: Felaket kurtarma prosedürleri.
> EN: Disaster recovery procedures.

## RTO / RPO Targets

| Scenario | RTO (max downtime) | RPO (max data loss) |
|---|---|---|
| Single service failure | <5 min (auto-restart) | 0 |
| Host OS crash | <30 min | 24h (last backup) |
| Ransomware | <4 hours | 24h |
| Vault compromise | <8 hours | 24h |
| Full host loss | <4 hours | 24h |

## Full Host Restore

1. Provision new host meeting requirements
2. Copy install files
3. Restore backup: `sudo ./restore.sh --backup-dir /path/to/backup`
4. Unseal Vault: `scripts/vault-unseal.sh`
5. Verify: `tests/smoke.sh`

## Vault Disaster Recovery

1. Stop all services: `docker compose down`
2. Rebuild Vault container on clean host
3. Restore raft snapshot: `./restore.sh --service vault`
4. Unseal with 3 Shamir shares
5. Verify all services can authenticate to Vault

## Agent Data Loss (48h offline buffer)

Agents have a 48-hour local SQLite queue. Events are not lost if the server is unreachable for <48 hours. On server restore, agents will drain their queues automatically.

## Complete Reference

See: `docs/security/runbooks/incident-response-playbook.md §7 (Ransomware)`
