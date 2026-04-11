# =============================================================================
# Vault Policy: gateway-service
# Gateway may sign agent certs for rotation and renew its own server cert.
# Per vault-setup.md §4.3
# =============================================================================

# Sign agent client certs during stream-based rotation (RotateCert flow)
path "pki/tenant/+/agents/sign/endpoint" {
  capabilities = ["create", "update"]
}

# Renew its own server TLS cert
path "pki/tenant/+/servers/issue/gateway" {
  capabilities = ["create", "update"]
}

# Read the PKI deny-list (cert serials to reject at mTLS handshake)
path "kv/data/pki/deny-list" {
  capabilities = ["read"]
}

# Read the root CA public cert for chain validation
path "pki/tenant/+/ca/pem" {
  capabilities = ["read"]
}
path "pki/tenant/+/ca_chain" {
  capabilities = ["read"]
}

# ============================================================
# EXPLICIT DENY BLOCK
# ============================================================

# Gateway MUST NOT have any key derive capability
path "transit/*" {
  capabilities = ["deny"]
}

# No other KV access
path "kv/data/crypto/*" {
  capabilities = ["deny"]
}

path "sys/*" {
  capabilities = ["deny"]
}
