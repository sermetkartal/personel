# Runbook — HashiCorp Vault Deployment

> Language: English. Audience: devops-engineer, security on-call. Scope: single-tenant on-prem Vault for Personel Phase 1. Companion to `pki-bootstrap.md` and `key-hierarchy.md`.

## 1. Versions and Topology

- **Vault**: 1.15.x (OSS). We do NOT use Enterprise for Phase 1; auto-unseal with Transit/HSM is a Phase 2 discussion.
- **Storage backend**: integrated Raft. Single node for MVP. Three-node raft cluster documented here for the production upgrade path but not mandatory for pilot.
- **Deployment**: Docker Compose managed under a systemd-supervised `personel-stack.service`. Volume pinned to an encrypted LUKS partition on the Vault host.
- **Placement**: Vault container on the same host as gateway is **not** permitted. Vault must run on its own host OR at minimum in a dedicated Compose profile with host-level firewalling, because a gateway RCE must not trivially expose Vault's raft data directory.

## 2. Docker Compose Fragment

```yaml
# docker-compose.vault.yml — deployed on the Vault host only.
services:
  vault:
    image: hashicorp/vault:1.15.6
    container_name: personel-vault
    restart: unless-stopped
    cap_add:
      - IPC_LOCK
    user: "100:1000"  # vault:vault
    ports:
      - "127.0.0.1:8200:8200"
    volumes:
      - /var/lib/personel/vault/data:/vault/data:rw
      - /etc/personel/vault/config.hcl:/vault/config/config.hcl:ro
      - /etc/personel/vault/tls:/vault/tls:ro
    command: ["vault", "server", "-config=/vault/config/config.hcl"]
    environment:
      VAULT_ADDR: "https://127.0.0.1:8200"
    healthcheck:
      test: ["CMD", "vault", "status", "-address=https://127.0.0.1:8200"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 60s
```

`/etc/personel/vault/config.hcl`:

```hcl
ui = true
disable_mlock = false

listener "tcp" {
  address            = "0.0.0.0:8200"
  tls_cert_file      = "/vault/tls/vault.crt"
  tls_key_file       = "/vault/tls/vault.key"
  tls_client_ca_file = "/vault/tls/tenant_ca.crt"
  tls_min_version    = "tls13"
  tls_require_and_verify_client_cert = false  # see §5 — AppRole login is unauth+secret_id
}

storage "raft" {
  path    = "/vault/data"
  node_id = "personel-vault-1"
}

api_addr     = "https://vault.internal:8200"
cluster_addr = "https://vault.internal:8201"

telemetry {
  prometheus_retention_time = "24h"
  disable_hostname          = true
}

audit {
  # Configured post-init via `vault audit enable`
}
```

Host firewall (nftables) restricts inbound 8200/tcp to the internal Docker bridge network and to the admin jump host only.

## 3. Initialization and Unseal

### 3.1 Initial Unseal (Shamir 3-of-5)

```bash
docker exec -it personel-vault vault operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -format=json > /tmp/vault-init.json
```

Handle the output with the same tamper-evident envelope procedure as the PKI Shamir shares (see `pki-bootstrap.md` §3.2). Custodians MUST be different from the PKI root custodians where possible (separation of duties). The initial root token is revoked after §4 completes; custodians retain only unseal shares, not the root token.

**Auto-unseal tradeoffs considered:**

| Mode | Pros | Cons | Decision |
|---|---|---|---|
| Shamir manual | No external dependency, auditable, works air-gapped | Human-in-the-loop on every restart; 3 custodians must respond within SLO | **Chosen for MVP.** Vault restarts are rare (Compose `restart: unless-stopped`) and a restart window justifies the custodian ritual. |
| Transit auto-unseal via peer Vault | Survives restarts unattended | Requires a second Vault; circular trust problem for single-host on-prem | Rejected for Phase 1. |
| Cloud KMS auto-unseal (AWS/GCP/Azure) | Unattended | Violates on-prem-only constraint | Rejected. |
| HSM auto-unseal (PKCS#11) | Best of both worlds | Requires Vault Enterprise license | Deferred to Phase 2 if a regulated customer requires it. |

**Operational consequence**: any Vault restart blocks gateway, DLP, Admin API cert renewals until 3 custodians re-enter shares. Retention DB queries and ClickHouse ingest continue (they cache credentials via `vault-agent`). Rehearse the unseal drill monthly.

### 3.2 Unseal

```bash
for i in 1 2 3; do
  read -s -p "Share $i: " SHARE
  echo
  docker exec -i personel-vault vault operator unseal "$SHARE"
done
```

### 3.3 Post-Init Lockdown

```bash
export VAULT_TOKEN=$(jq -r .root_token /tmp/vault-init.json)
vault audit enable file file_path=/vault/data/audit.log
vault audit enable syslog tag="vault-audit" facility="AUTH"

# Harden root token: create a break-glass admin role and revoke root.
vault policy write break-glass /etc/personel/vault/policies/break-glass.hcl
vault token create -policy=break-glass -ttl=8760h -orphan -format=json > /tmp/break-glass.json
# Seal break-glass token in an envelope with custodians (same procedure as Shamir).

vault token revoke "$VAULT_TOKEN"
shred -uz /tmp/vault-init.json
```

From this point on, normal operations use narrowly-scoped tokens issued via AppRole or Kubernetes-style short-lived tokens. The root token is gone.

## 4. Policy Files

All policies live in `/etc/personel/vault/policies/` and are version-controlled in the Personel monorepo under `deploy/vault/policies/`.

### 4.1 `agent-enrollment.hcl`

```hcl
# Consumed by Admin API during a single enrollment.
# TTL 15 min, max 15 min, single-use Secret ID.
path "pki/tenant/+/agents/sign-verbatim/endpoint" {
  capabilities = ["create", "update"]
}

path "pki/tenant/+/agents/roles/endpoint" {
  capabilities = ["read"]
}

# Read the enrollment-bound KV with the per-endpoint invite metadata (one key per token).
path "kv/data/enrollment-invites/{{identity.entity.aliases.auth_approle_*.metadata.invite_id}}" {
  capabilities = ["read"]
}

# Explicitly DENY everything else, including any transit or kv root path.
path "transit/*"        { capabilities = ["deny"] }
path "kv/data/crypto/*" { capabilities = ["deny"] }
path "sys/*"            { capabilities = ["deny"] }
```

### 4.2 `dlp-service.hcl`

```hcl
# The only identity allowed to derive DSEK. Cannot export, cannot read raw TMK.
path "transit/keys/tenant/+/tmk" {
  capabilities = ["read"]   # metadata only (version, rotation ts); no key material
}

path "transit/derive/tenant/+/tmk" {
  capabilities = ["update"]
}

path "transit/decrypt/tenant/+/tmk" {
  capabilities = ["update"]  # for unwrapping PE-DEK wraps previously made with transit encrypt
}

path "transit/encrypt/tenant/+/tmk" {
  capabilities = ["update"]  # only used during enrollment to wrap a fresh PE-DEK
}

# Explicitly deny export of any key material.
path "transit/export/*"       { capabilities = ["deny"] }
path "transit/keys/*/rotate"  { capabilities = ["deny"] }
path "transit/keys/*/config"  { capabilities = ["deny"] }
path "sys/*"                  { capabilities = ["deny"] }
path "pki/*"                  { capabilities = ["deny"] }
path "kv/*"                   { capabilities = ["deny"] }
path "auth/*"                 { capabilities = ["deny"] }
```

**Hard rule**: this policy is the cryptographic fence behind the "admins cannot read keystrokes" claim. Any change to this file requires a ticket linked to an ADR update, review from security-engineer AND compliance-auditor, and CI-enforced diff review. See `dlp-service-isolation.md` §8.

**ADR 0013 amendment — No Secret ID at install time**. The `dlp-service` policy above is created during install so that it is visible and audit-reviewable, and the corresponding AppRole is created (see §5.1 below) — but the **Secret ID for this role is NOT issued during install**. The AppRole exists with no active Secret ID; without one, no login is possible, no Vault token is minted, no `transit/derive/tenant/+/tmk` call can succeed. This is the default-off posture mandated by ADR 0013.

Secret ID issuance happens exclusively via `infra/scripts/dlp-enable.sh`, which is run only after the customer has executed the opt-in ceremony documented in ADR 0013 §Opt-In Ceremony. The script verifies the signed opt-in form at `/var/lib/personel/dlp/opt-in-signed.pdf`, issues a single-use Secret ID, writes a `dlp.enabled` hash-chained audit event with the form's SHA-256, starts the `personel-dlp` container via `docker compose --profile dlp up -d`, and validates end-to-end processing. Opt-out via `infra/scripts/dlp-disable.sh` revokes the Secret ID and stops the container.

**Install-time verification (must be part of exit criterion #18)**: after a fresh install, the operator runs `vault list auth/approle/role/dlp-service/secret-id` and confirms the list is empty. The Vault audit device likewise shows zero `auth/approle/login` events for the `dlp-service` role and zero `transit/derive/tenant/*/tmk` events.

### 4.3 `gateway-service.hcl`

```hcl
# Gateway may issue short-lived agent certs for rotation (via CsrSubmit flow)
# and may read its own server cert for renewal. No keystroke key access.
path "pki/tenant/+/agents/sign/endpoint" {
  capabilities = ["create", "update"]
}

path "pki/tenant/+/servers/issue/gateway" {
  capabilities = ["create", "update"]
}

# Read the deny-list maintained by Admin API.
path "kv/data/pki/deny-list" {
  capabilities = ["read"]
}

# Deny everything else.
path "transit/*" { capabilities = ["deny"] }
path "sys/*"     { capabilities = ["deny"] }
```

### 4.4 `admin-audit.hcl`

```hcl
# Audit log reader role used by DPO and legal review tooling.
# Vault's own audit device is the source; this policy reads it via file mount.
# NOTE: the Personel admin_audit Postgres log is a SEPARATE system — see
# `admin-audit-immutability.md`. This policy governs the Vault-internal audit.

path "sys/audit"            { capabilities = ["read"] }
path "sys/audit-hash/*"     { capabilities = ["read", "update"] }

# Explicit denies — no write to any data path.
path "pki/*"     { capabilities = ["deny"] }
path "transit/*" { capabilities = ["deny"] }
path "kv/*"      { capabilities = ["deny"] }
path "auth/*"    { capabilities = ["deny"] }
```

### 4.5 `break-glass.hcl`

```hcl
# Sealed in tamper-evident envelope. Used only for disaster recovery.
path "*" {
  capabilities = ["create", "read", "update", "delete", "list", "sudo"]
}
```

Every use of this token MUST trigger a post-incident review, documented via `incident-response-playbook.md`.

## 5. Auth Methods

### 5.1 AppRole

```bash
vault auth enable approle

# Create roles for each service identity.
vault write auth/approle/role/agent-enrollment    token_policies=agent-enrollment    secret_id_num_uses=1  secret_id_ttl=15m  token_ttl=15m  token_max_ttl=15m
vault write auth/approle/role/dlp-service         token_policies=dlp-service         secret_id_num_uses=1  secret_id_ttl=24h  token_ttl=1h   token_max_ttl=24h
vault write auth/approle/role/gateway-service     token_policies=gateway-service     secret_id_num_uses=0  secret_id_ttl=24h  token_ttl=1h   token_max_ttl=24h
vault write auth/approle/role/admin-api           token_policies=admin-api           secret_id_num_uses=0  secret_id_ttl=24h  token_ttl=1h   token_max_ttl=24h
```

**ADR 0013 — `dlp-service` AppRole exists, Secret ID does not**. The `vault write auth/approle/role/dlp-service ...` command above creates the **role** only. Notice the new `secret_id_num_uses=1` (single-use Secret ID, issued on opt-in). The `vault write auth/approle/role/dlp-service/secret-id` command is **deliberately not run at install time**. The installer prints:

```
NOTICE: DLP is DISABLED by default (ADR 0013).
        The dlp-service Vault AppRole has been created but no Secret ID is issued.
        To enable DLP, complete the opt-in ceremony and run infra/scripts/dlp-enable.sh
        Reference: docs/adr/0013-dlp-disabled-by-default.md
```

Secret IDs for the other four roles are delivered to their services via **systemd credentials** (`LoadCredential=`), NOT via environment variables or bind-mounted files. systemd rotates the in-memory secret on service restart. The `dlp-service` Secret ID is delivered the same way, but only after `dlp-enable.sh` issues it — at which point the `personel-dlp.service` systemd unit is started and picks up the credential.

### 5.2 Transit and KV Engines

```bash
vault secrets enable -path=transit transit
vault write -f transit/keys/tenant/<tenant_id>/tmk type=aes256-gcm96 derived=true exportable=false allow_plaintext_backup=false

vault secrets enable -path=kv -version=2 kv
vault kv put kv/pki/deny-list serials="[]"
```

**TMK versioning and rotation:**

```bash
# Manual rotation (annual, per key-hierarchy.md §8).
vault write -f transit/keys/tenant/<tenant_id>/tmk/rotate

# Configure auto-rotation as belt-and-braces.
vault write transit/keys/tenant/<tenant_id>/tmk/config \
  min_decryption_version=1 \
  min_encryption_version=0 \
  deletion_allowed=false \
  auto_rotate_period=8760h
```

Old versions are retained for decrypt-only until all blobs wrapped under them expire per retention. A scheduled job (described in `secret-rotation.md` §3) enumerates TMK versions and lowers `min_decryption_version` once the last blob is purged; at that point the old TMK version becomes cryptographically dead (destruction = key destruction pattern from `key-hierarchy.md` §9).

## 6. Audit Device

```bash
vault audit enable -path=file file file_path=/vault/data/audit.log log_raw=false hmac_accessor=true
vault audit enable -path=syslog syslog tag="vault-audit" facility="LOCAL6"
```

- `log_raw=false` — request bodies are HMAC'd, not stored in plaintext. Required: leaking the audit file must not reveal any secret material.
- Log rotation: `logrotate.d/personel-vault` rotates daily with `copytruncate`, keeps 90 days, signs each rotated file with the audit checkpoint key (see `admin-audit-immutability.md` §4).
- The audit log is shipped to the Audit DB via filebeat with a deny-all inbound policy on the Audit DB host.

## 7. Backup and DR

- `vault operator raft snapshot save /var/lib/personel/backups/vault/snapshot-<ts>.snap` — runs nightly via systemd timer.
- Snapshot is encrypted with age using a recipient key held in the customer's backup vault (offline).
- Restore procedure and break-glass ritual documented in `incident-response-playbook.md` §4 (Vault compromise / DR).

## 8. Rotation Job Summary

| Item | Period | Mechanism |
|---|---|---|
| TMK | 8760h (1y) | `auto_rotate_period` on transit key + manual on compromise |
| AppRole Secret IDs | 24h | systemd timer calls Admin API → Vault rotation script |
| Vault TLS cert | 90d | `vault-agent` template |
| Vault audit log rotation | daily | logrotate + checkpoint signing |
| Raft snapshot | daily | systemd timer |
| Unseal drill | 30d | on-call rehearsal; logged to audit DB |

## 9. Handoffs

- **devops-engineer**: owns the Docker Compose fragment, systemd unit, logrotate config, snapshot timer.
- **backend-developer**: Admin API integrates with `agent-enrollment` AppRole; must request Secret ID per enrollment and never cache.
- **compliance-auditor**: owns the Shamir envelope ritual, break-glass token custody, unseal drill log.
