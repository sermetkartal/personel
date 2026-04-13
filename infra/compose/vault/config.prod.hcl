# =============================================================================
# Personel Platform — HashiCorp Vault PRODUCTION Configuration
# =============================================================================
#
# This is the production-hardened companion to config.hcl (the dev shortcut
# config that keeps `disable_mlock = true` and 1-of-1 Shamir for fast bring-up).
#
# DIFFERENCES FROM config.hcl
#   - disable_mlock = false (real swap protection; requires CAP_IPC_LOCK on the
#     container or running on a host with swap fully disabled)
#   - explicit `seal "shamir" {}` stanza (3-of-5 ceremony — see bootstrap-prod.sh)
#   - HSM seal stanza scaffolded (commented) for Phase 2 hardware migration
#   - cluster_addr / api_addr point at the production hostname
#   - Raft node_id = "personel-vault-prod" (so a prod node never rejoins a dev
#     cluster by accident)
#
# DEPLOYMENT NOTES
#   1. The container running Vault MUST have:
#        cap_add: [IPC_LOCK]
#      in docker-compose so the kernel allows mlock(). Without IPC_LOCK plus
#      `disable_mlock = false`, Vault refuses to start.
#
#   2. Swap on the host SHOULD also be disabled (`swapoff -a` + remove
#      /etc/fstab entry) for true at-rest secret protection. The IPC_LOCK
#      capability is the in-process guarantee; swapoff is the kernel guarantee.
#
#   3. This file is a TEMPLATE for production. The operator copies it to
#      /etc/personel/vault/config.hcl on the prod host and DOES NOT
#      symlink it back into the repo working tree. The dev config.hcl
#      remains the source of truth for the on-box bring-up.
#
# REFERENCE
#   docs/operations/vault-prod-migration.md
#   docs/security/runbooks/vault-setup.md
# =============================================================================

ui            = true
disable_mlock = false

# ---------------------------------------------------------------------------
# Listener — TLS 1.2+ (1.3 preferred but 1.2 retained for older Go clients)
# ---------------------------------------------------------------------------
listener "tcp" {
  address            = "0.0.0.0:8200"
  tls_cert_file      = "/etc/personel/tls/vault.crt"
  tls_key_file       = "/etc/personel/tls/vault.key"
  tls_ca_file        = "/etc/personel/tls/tenant_ca.crt"
  tls_min_version    = "tls12"
  tls_disable        = false

  # AppRole login does not require client certs (clients use secret_id).
  # mTLS pinning happens at the application layer (gateway, API).
  tls_require_and_verify_client_cert = false

  # Trust X-Forwarded-For only from the docker bridge network.
  x_forwarded_for_authorized_addrs = "172.16.0.0/12"

  # Production cipher-suite hardening
  tls_cipher_suites = "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
}

# ---------------------------------------------------------------------------
# Storage — Raft (integrated)
# ---------------------------------------------------------------------------
storage "raft" {
  path    = "/vault/data"
  node_id = "personel-vault-prod"

  # Three-node raft cluster scaffolded for HA. Phase 1 is single-node; the
  # retry_join lines come live when nodes 2 and 3 are provisioned per
  # vault-prod-migration.md §6.
  # retry_join {
  #   leader_api_addr = "https://vault-2.personel.internal:8200"
  # }
  # retry_join {
  #   leader_api_addr = "https://vault-3.personel.internal:8200"
  # }

  performance_multiplier = 1
}

# ---------------------------------------------------------------------------
# Cluster and API addressing
# ---------------------------------------------------------------------------
api_addr     = "https://vault.personel.internal:8200"
cluster_addr = "https://vault.personel.internal:8201"

# ---------------------------------------------------------------------------
# Telemetry — Prometheus
# ---------------------------------------------------------------------------
telemetry {
  prometheus_retention_time      = "24h"
  disable_hostname               = true
  unauthenticated_metrics_access = false
}

# ---------------------------------------------------------------------------
# Audit — file + syslog. Both are enabled post-init via bootstrap-prod.sh.
# Do NOT uncomment these stanzas; audit devices live in Vault's metadata,
# not in the config file.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Log level
# ---------------------------------------------------------------------------
log_level  = "INFO"
log_format = "json"

# ---------------------------------------------------------------------------
# Seal — Shamir 3-of-5 (production default)
#
# The empty stanza is REQUIRED — it forces Vault to use the Shamir seal even
# if the binary embeds an alternate default. Operators should not remove it
# without coordinating with the security-engineer team.
# ---------------------------------------------------------------------------
seal "shamir" {}

# ---------------------------------------------------------------------------
# ASPIRATIONAL: HSM auto-unseal — Phase 2 hardware migration target.
# Uncomment exactly one of the following blocks AFTER:
#   1. The HSM is provisioned and the unseal key is created
#   2. docs/operations/vault-prod-migration.md §8 (HSM cutover) is executed
#   3. The Shamir → HSM seal migration command has been run on a test cluster
#
# seal "pkcs11" {
#   lib            = "/usr/lib/softhsm/libsofthsm2.so"
#   slot           = "0"
#   pin            = "file:///etc/personel/hsm.pin"
#   key_label      = "personel-vault-unseal"
#   hmac_key_label = "personel-vault-hmac"
#   generate_key   = "false"
# }
#
# seal "awskms" {
#   region     = "eu-central-1"
#   kms_key_id = "alias/personel-vault-unseal"
# }
# ---------------------------------------------------------------------------
