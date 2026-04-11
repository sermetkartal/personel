# =============================================================================
# Vault Policy: agent-enrollment
# Consumed by Admin API during a single endpoint enrollment.
# TTL 15 min, single-use Secret ID.
# Per vault-setup.md §4.1
# =============================================================================

# Sign agent client certificate (enrollment CSR)
path "pki/tenant/+/agents/sign-verbatim/endpoint" {
  capabilities = ["create", "update"]
}

# Read agent issuance role metadata
path "pki/tenant/+/agents/roles/endpoint" {
  capabilities = ["read"]
}

# Read enrollment invite metadata (one key per token, consumed on use)
path "kv/data/enrollment-invites/{{identity.entity.aliases.auth_approle_*.metadata.invite_id}}" {
  capabilities = ["read"]
}

# Read root CA for chain delivery to enrolling endpoint
path "pki/tenant/+/ca/pem" {
  capabilities = ["read"]
}

# Explicit deny on all key material — this role MUST NOT touch keys
path "transit/*"        { capabilities = ["deny"] }
path "kv/data/crypto/*" { capabilities = ["deny"] }
path "sys/*"            { capabilities = ["deny"] }
path "auth/*"           { capabilities = ["deny"] }
