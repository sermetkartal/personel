# =============================================================================
# Personel Platform — HashiCorp Vault Configuration
# Storage: integrated Raft (single node for Phase 1)
# Per vault-setup.md §2
# =============================================================================

ui            = true
disable_mlock = false

# ---------------------------------------------------------------------------
# Listener — TLS 1.3 only
# ---------------------------------------------------------------------------
listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_cert_file      = "/vault/tls/vault.crt"
  tls_key_file       = "/vault/tls/vault.key"
  tls_ca_file        = "/vault/tls/tenant_ca.crt"
  tls_min_version    = "tls13"

  # AppRole login does not require client certs (clients use secret_id)
  tls_require_and_verify_client_cert = false

  # Only accept connections from the Docker internal bridge
  # (host firewall enforces this at the OS level too)
  x_forwarded_for_authorized_addrs = "172.16.0.0/12"
}

# ---------------------------------------------------------------------------
# Storage — Raft (integrated)
# ---------------------------------------------------------------------------
storage "raft" {
  path    = "/vault/data"
  node_id = "personel-vault-1"

  # Three-node raft cluster for Phase-1-exit (document both replicas here)
  # retry_join {
  #   leader_api_addr = "https://vault-2.personel.internal:8200"
  # }
  # retry_join {
  #   leader_api_addr = "https://vault-3.personel.internal:8200"
  # }

  # Performance
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
  prometheus_retention_time = "24h"
  disable_hostname          = true
  unauthenticated_metrics_access = false
}

# ---------------------------------------------------------------------------
# Audit
# Enabled post-init via `vault audit enable` in bootstrap.sh.
# Defined here as documentation; the vault operator init script activates.
# ---------------------------------------------------------------------------
# audit "file" {
#   file_path = "/vault/data/audit.log"
#   log_raw   = false
#   hmac_accessor = true
# }
# audit "syslog" {
#   tag = "vault-audit"
#   facility = "LOCAL6"
# }

# ---------------------------------------------------------------------------
# Log level
# ---------------------------------------------------------------------------
log_level = "INFO"
log_format = "json"

# ---------------------------------------------------------------------------
# Seal — Shamir (default for Phase 1 on-prem)
# Auto-unseal via cloud KMS or HSM deferred to Phase 2.
# Every restart requires 3-of-5 Shamir shares from custodians.
# ---------------------------------------------------------------------------
# seal "pkcs11" { ... }  # Phase 2 option for regulated customers
