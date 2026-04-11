# =============================================================================
# Vault Policy: admin-audit
# Read-only access to Vault audit device for DPO/legal review tooling.
# Per vault-setup.md §4.4
# =============================================================================

# Read audit device list (which devices are enabled)
path "sys/audit" {
  capabilities = ["read"]
}

# Compute HMAC for audit log hash verification
path "sys/audit-hash/*" {
  capabilities = ["read", "update"]
}

# Read Vault node status (not sensitive)
path "sys/health" {
  capabilities = ["read"]
}

path "sys/leader" {
  capabilities = ["read"]
}

# ============================================================
# EXPLICIT DENY BLOCK
# ============================================================

# No PKI — cannot issue certs
path "pki/*" {
  capabilities = ["deny"]
}

# No transit — cannot derive or use keys
path "transit/*" {
  capabilities = ["deny"]
}

# No KV writes
path "kv/*" {
  capabilities = ["deny"]
}

# No auth management
path "auth/*" {
  capabilities = ["deny"]
}
