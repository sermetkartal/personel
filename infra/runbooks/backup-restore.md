# Backup and Restore Runbook

> TR: Yedekleme ve geri yükleme prosedürleri.
> EN: Backup and restore procedures.

## Backup Schedule

| Type | Schedule | Retention | Script |
|---|---|---|---|
| Daily full | 02:00 local | 7 days | `backup.sh` |
| Weekly full | Sunday 02:00 | 4 weeks | `backup.sh` (auto) |
| Vault snapshot | Daily | 7 days | Included in daily |

## Running a Manual Backup

```bash
sudo /opt/personel/infra/backup.sh
```

## Restore

```bash
# List available backups
sudo /opt/personel/infra/restore.sh --list

# Restore from specific backup (interactive confirmation required)
sudo /opt/personel/infra/restore.sh \
  --backup-dir /var/lib/personel/backups/daily/20260410T020000Z

# Restore single service
sudo /opt/personel/infra/restore.sh \
  --backup-dir /path/to/backup \
  --service postgres
```

## Backup Round-Trip Test

```bash
sudo /opt/personel/infra/tests/backup-restore.sh
```

Target: backup completes in <2 hours, restore completes in <2 hours.

## Encryption

Backups are encrypted with GPG AES-256 symmetric encryption using `BACKUP_GPG_PASSPHRASE` from `.env`.
The passphrase must be stored securely offline (not in the server's `.env` file in plaintext for long-term storage — consider a password manager or HSM for the passphrase).

## Vault Snapshot

Vault raft snapshots are included in the daily backup. Note: restoring a Vault snapshot requires the Shamir unseal ritual afterward.
