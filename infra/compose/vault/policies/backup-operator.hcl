# =============================================================================
# Vault Policy: backup-operator
# Allows nightly backup job to take a Vault Raft snapshot.
# Per vault-setup.md §7 and backup.sh
# =============================================================================

# Raft snapshot — the only operation backup needs from Vault
path "sys/storage/raft/snapshot" {
  capabilities = ["read"]
}

# Health check (for pre-backup validation)
path "sys/health" {
  capabilities = ["read"]
}

path "sys/leader" {
  capabilities = ["read"]
}

# ============================================================
# EXPLICIT DENY BLOCK
# ============================================================

path "transit/*"  { capabilities = ["deny"] }
path "pki/*"      { capabilities = ["deny"] }
path "kv/*"       { capabilities = ["deny"] }
path "auth/*"     { capabilities = ["deny"] }
path "sys/auth/*" { capabilities = ["deny"] }
