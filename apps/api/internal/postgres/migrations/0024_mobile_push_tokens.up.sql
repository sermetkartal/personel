-- Migration 0024: mobile_push_tokens table for Phase 2.4/2.9 mobile admin app.
-- Stores FCM (Android) and APNs (iOS) tokens per user+device so the
-- mobile-bff can route push notifications.
--
-- ADR 0019 Push Privacy: the token itself is sensitive (can be used to
-- impersonate the device endpoint with FCM/APNs). Stored pgcrypto-sealed
-- with the app_api_secret key; application code never reads it directly.
-- The sha256 prefix hash is kept in cleartext for observability.

CREATE TABLE IF NOT EXISTS mobile_push_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    device_id       TEXT NOT NULL,               -- stable per-device from expo-device
    platform        TEXT NOT NULL CHECK (platform IN ('ios', 'android')),
    token_ciphertext BYTEA NOT NULL,             -- pgcrypto-sealed, never logged
    token_hash      TEXT NOT NULL,                -- sha256 prefix 16 for observability
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ,                  -- soft-delete; FCM/APNs rejects use natural expiry
    UNIQUE (user_id, device_id)
);

CREATE INDEX IF NOT EXISTS mobile_push_tokens_tenant_id
    ON mobile_push_tokens (tenant_id);

CREATE INDEX IF NOT EXISTS mobile_push_tokens_user_active
    ON mobile_push_tokens (user_id)
    WHERE revoked_at IS NULL;

-- Enable row-level security so a compromised app_api role can only see
-- tokens for the current session's tenant. Matches the pattern already
-- used on tenants, users, endpoints in earlier migrations.
ALTER TABLE mobile_push_tokens ENABLE ROW LEVEL SECURITY;

CREATE POLICY mobile_push_tokens_tenant_isolation ON mobile_push_tokens
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

COMMENT ON TABLE mobile_push_tokens IS 'FCM/APNs tokens for mobile admin app (Phase 2.9)';
COMMENT ON COLUMN mobile_push_tokens.token_ciphertext IS 'pgcrypto-sealed; decrypt only in the push dispatcher';
COMMENT ON COLUMN mobile_push_tokens.token_hash IS 'sha256 prefix 16; safe for logging';
