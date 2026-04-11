# Troubleshooting Guide

> TR: Yaygın sorunlar ve çözümleri.
> EN: Common issues and resolutions.

## Vault Sealed After Restart

```bash
# Check status
docker exec personel-vault vault status -tls-skip-verify

# Unseal
sudo /opt/personel/infra/scripts/vault-unseal.sh
```

**Root cause:** Vault requires manual 3-of-5 Shamir unseal after any restart. This is by design for Phase 1 (auto-unseal deferred to Phase 2).

## Service Unhealthy

```bash
# Check status
docker compose -f /opt/personel/infra/compose/docker-compose.yaml ps

# View logs
docker compose -f /opt/personel/infra/compose/docker-compose.yaml logs --tail=100 SERVICE_NAME

# Restart a service
docker compose -f /opt/personel/infra/compose/docker-compose.yaml restart SERVICE_NAME
```

## Disk Full

```bash
# Check disk usage
df -h /var/lib/personel

# Check per-service usage
du -sh /var/lib/personel/*

# Force ClickHouse TTL merge
docker exec personel-clickhouse clickhouse-client -q "OPTIMIZE TABLE personel.events FINAL"

# Check MinIO usage
docker exec personel-minio mc du personel/
```

## DLP Not Processing (keystroke blobs accumulating)

```bash
# Check DLP container
docker logs personel-dlp --tail=50

# Check NATS backlog
curl -s http://localhost:8222/jsz | python3 -m json.tool | grep -A2 '"EVENTS"'

# Restart DLP (new Vault AppRole Secret ID is issued on restart)
docker compose -f /opt/personel/infra/compose/docker-compose.yaml restart dlp
```

## Audit Chain Verification Failed

```bash
# Run verifier
sudo /opt/personel/infra/scripts/verify-audit-chain.sh

# Check which rows fail
docker exec personel-postgres psql -U postgres -d personel -c "
SELECT id, seq, prev_hash,
       LAG(row_hash) OVER (PARTITION BY tenant_id ORDER BY seq) AS expected_prev
FROM audit.audit_events
ORDER BY seq DESC LIMIT 20;"
```

**CRITICAL:** Do not delete any rows. Escalate to security team.

## Agent Not Connecting

1. Check gateway is running: `docker compose ps gateway`
2. Verify gateway cert is valid: `openssl s_client -connect SERVER:9443 -showcerts 2>/dev/null | openssl x509 -noout -dates`
3. Check agent cert serial is not on deny-list: query `core.cert_deny_list`
4. Check Vault is unsealed (gateway needs Vault to validate certs)

## Certificate Expiry Warning

```bash
# Check all cert expiries
for cert in /etc/personel/tls/*.crt; do
  echo "=== $cert ==="; openssl x509 -enddate -noout -in "$cert"; done

# Trigger renewal check
sudo /opt/personel/infra/scripts/rotate-secrets.sh --certs-only
```
