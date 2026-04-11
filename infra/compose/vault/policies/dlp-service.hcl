# =============================================================================
# Vault Policy: dlp-service
# =============================================================================
# CRITICAL SECURITY POLICY
# This policy is the cryptographic fence behind the "admins cannot read
# keystrokes" product guarantee. Any change requires:
#   - A linked ADR update
#   - Review from security-engineer AND compliance-auditor
#   - CI-enforced diff review (enforced by .github/CODEOWNERS)
# Per vault-setup.md §4.2 and dlp-service-isolation.md §4
# =============================================================================

# Read TMK metadata (version, rotation timestamp) — no key material returned
path "transit/keys/tenant/+/tmk" {
  capabilities = ["read"]
}

# Derive DSEK from TMK (HKDF-SHA256 context-based derivation)
# This is the ONLY operation that gives DLP its decryption capability
path "transit/derive/tenant/+/tmk" {
  capabilities = ["update"]
}

# Unwrap PE-DEK (decrypt wrapped_dek from keystroke_keys table)
path "transit/decrypt/tenant/+/tmk" {
  capabilities = ["update"]
}

# Wrap new PE-DEK during agent enrollment
path "transit/encrypt/tenant/+/tmk" {
  capabilities = ["update"]
}

# Read current TMK version for version handshake validation
path "transit/keys/tenant/+/tmk/trim" {
  capabilities = ["deny"]  # explicitly block trimming
}

# ============================================================
# EXPLICIT DENY BLOCK — do not remove these lines
# ============================================================

# Never export raw key material
path "transit/export/*" {
  capabilities = ["deny"]
}

# Never rotate or reconfigure keys (only admins should do this, never DLP)
path "transit/keys/*/rotate" {
  capabilities = ["deny"]
}
path "transit/keys/*/config" {
  capabilities = ["deny"]
}

# No access to PKI (cert issuance)
path "pki/*" {
  capabilities = ["deny"]
}

# No access to secrets other than its own transit operations
path "kv/*" {
  capabilities = ["deny"]
}

# No system operations
path "sys/*" {
  capabilities = ["deny"]
}

# No auth management
path "auth/*" {
  capabilities = ["deny"]
}
