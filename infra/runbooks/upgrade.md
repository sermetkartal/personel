# Upgrade Runbook

> TR: Platform yükseltme prosedürü.
> EN: Platform upgrade procedure.

## Pre-Upgrade Checklist

- [ ] Take a manual backup: `sudo ./backup.sh`
- [ ] Check current health: `tests/smoke.sh`
- [ ] Read the CHANGELOG for the new version
- [ ] Verify no DSR SLA at-risk tickets are open

## Upgrade Command

```bash
# Single service upgrade
sudo /opt/personel/infra/upgrade.sh --version 0.2.0 --service api

# Full stack upgrade (ordered, health-gated)
sudo /opt/personel/infra/upgrade.sh --version 0.2.0
```

## Rollback

```bash
# Automatic rollback triggers on health check failure.
# Manual rollback:
sudo /opt/personel/infra/upgrade.sh --rollback
```

## Data Service Upgrades (Postgres, ClickHouse)

Data service upgrades require a migration plan. Do not upgrade data services with `upgrade.sh`. Follow the specific migration runbook for each.

For ClickHouse Phase 1 exit (MergeTree → ReplicatedMergeTree):
```bash
sudo /opt/personel/infra/scripts/staging-replication-rig.sh --validate
```
