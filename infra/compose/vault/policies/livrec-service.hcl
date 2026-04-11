# =============================================================================
# Vault Policy: livrec-service (live-view-recorder AppRole)
# =============================================================================
# ADR 0019 §Key hierarchy — this policy is the cryptographic fence that keeps
# LVMK completely separate from TMK.
#
# Changes to this policy require:
#   - A linked ADR update (0019 or successor)
#   - Review from security-engineer AND compliance-auditor
#   - CI-enforced diff review (CODEOWNERS)
#
# Identity: livrec-service authenticates via the "live-view-recorder" AppRole.
# The AppRole Secret ID is issued ONLY when a tenant explicitly enables live
# view recording (infra/scripts/livrec-enable.sh — to be authored in Phase 3).
# Until then, no Secret ID is issued and no LVMK derivation is possible.
# This mirrors the ADR 0013 DLP opt-in pattern exactly.
# =============================================================================

# ---------------------------------------------------------------------------
# LVMK — derive per-session DEKs (one transit key per tenant, path lvmk-<tid>)
# ---------------------------------------------------------------------------

# Derive per-session recording DEK from LVMK using HKDF context derivation.
# This is the ONLY operation granting livrec-service cryptographic capability
# over live view recordings.
path "transit/derive/lvmk-+" {
  capabilities = ["update"]
}

# Read LVMK metadata (version, rotation timestamp) — no key material returned.
path "transit/keys/lvmk-+" {
  capabilities = ["read"]
}

# Wrap (encrypt) a session DEK under LVMK for Postgres storage.
path "transit/encrypt/lvmk-+" {
  capabilities = ["update"]
}

# Unwrap (decrypt) a stored DEK wrap back to plaintext for playback/export.
path "transit/decrypt/lvmk-+" {
  capabilities = ["update"]
}

# ---------------------------------------------------------------------------
# Control-plane signing — for forensic export manifests (ADR 0019 §DPO export)
# ---------------------------------------------------------------------------

# Sign export manifests with the control-plane Ed25519 key.
# This is the same signing key used by the Admin API for policy signatures.
path "transit/sign/control-plane-signer" {
  capabilities = ["update"]
}

# ---------------------------------------------------------------------------
# EXPLICIT DENY BLOCK — do not remove or comment out these lines.
# Zero cross-contamination with TMK (keystroke encryption keys).
# ---------------------------------------------------------------------------

# CRITICAL: No access to keystroke TMK paths — ever.
# A single erroneous grant here would cross-contaminate the LVMK and TMK
# security boundaries defined in ADR 0009 and ADR 0019.
path "transit/keys/tenant/+/tmk" {
  capabilities = ["deny"]
}
path "transit/derive/tenant/+/tmk" {
  capabilities = ["deny"]
}
path "transit/encrypt/tenant/+/tmk" {
  capabilities = ["deny"]
}
path "transit/decrypt/tenant/+/tmk" {
  capabilities = ["deny"]
}

# No raw key export.
path "transit/export/*" {
  capabilities = ["deny"]
}

# No key rotation or reconfiguration (only Vault operators via break-glass).
path "transit/keys/*/rotate" {
  capabilities = ["deny"]
}
path "transit/keys/*/config" {
  capabilities = ["deny"]
}

# No LVMK trim (only automated post-destruction cleanup via admin script).
path "transit/keys/lvmk-+/trim" {
  capabilities = ["deny"]
}

# No PKI access.
path "pki/*" {
  capabilities = ["deny"]
}

# No KV secrets.
path "kv/*" {
  capabilities = ["deny"]
}

# No system operations.
path "sys/*" {
  capabilities = ["deny"]
}

# No auth management.
path "auth/*" {
  capabilities = ["deny"]
}

# No access to HR approval flow — livrec calls the Admin API for approval
# checks; it never touches the Vault paths that the Admin API uses for
# session tokens or HR workflow state.
path "secret/liveview/*" {
  capabilities = ["deny"]
}
