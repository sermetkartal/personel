-- Migration 0020: DLP state table (single-row, upserted by dlp-enable.sh and dlp-disable.sh).
-- ADR 0013: DLP is disabled by default; state transitions are written by infra scripts,
-- not by the API. The API only reads this table.

CREATE TABLE IF NOT EXISTS dlp_state (
    id              BOOL PRIMARY KEY DEFAULT TRUE CHECK (id = TRUE), -- enforces single row
    state           TEXT NOT NULL DEFAULT 'disabled'
                        CHECK (state IN ('disabled', 'enabling', 'enabled', 'disabling', 'error')),
    enabled_at      TIMESTAMPTZ,
    enabled_by      TEXT,
    ceremony_form_hash TEXT,       -- SHA-256 hex of the signed opt-in PDF
    last_audit_event_id TEXT,
    message         TEXT NOT NULL DEFAULT 'DLP varsayılan olarak kapalıdır.'
);

-- Seed the single default row.
INSERT INTO dlp_state (id, state, message)
VALUES (TRUE, 'disabled', 'DLP varsayılan olarak kapalıdır.')
ON CONFLICT (id) DO NOTHING;
