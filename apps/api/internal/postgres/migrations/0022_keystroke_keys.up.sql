-- Migration 0022: Per-endpoint PE-DEK (wrapped) storage.
-- ADR 0013 Amendment A2: when DLP is opted-in on a deployment with already-enrolled
-- endpoints, dlp-bootstrap-keys generates fresh PE-DEKs and stores the wrapped blobs
-- here. Delivery to agents happens on next stream open via the sealed enrollment channel.

CREATE TABLE IF NOT EXISTS keystroke_keys (
    endpoint_id     UUID        NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    tenant_id       UUID        NOT NULL,
    -- Vault transit-derived PE-DEK wrapped with DSEK. Binary (base64 in application layer).
    wrapped_pe_dek  BYTEA       NOT NULL,
    -- Key version label from Vault (e.g. "v1"). Used for rotation detection.
    key_version     TEXT        NOT NULL DEFAULT 'v1',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at    TIMESTAMPTZ,           -- set when agent confirms receipt on stream open
    PRIMARY KEY (endpoint_id)
);

CREATE INDEX IF NOT EXISTS idx_keystroke_keys_tenant ON keystroke_keys (tenant_id);
