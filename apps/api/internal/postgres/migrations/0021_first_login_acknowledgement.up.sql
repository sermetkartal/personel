-- Migration 0021: First-login acknowledgement for KVKK m.10 compliance.
-- Records the moment an employee explicitly acknowledged the transparency disclosure
-- (clicks "Anladım" on the first-login modal in the transparency portal).
-- Per calisan-bilgilendirme-akisi.md Aşama 5.

CREATE TABLE IF NOT EXISTS first_login_acknowledgements (
    user_id             UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    aydinlatma_version  TEXT        NOT NULL DEFAULT '1.0',
    acknowledged_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    audit_id            TEXT        NOT NULL,
    locale              TEXT        NOT NULL DEFAULT 'tr',
    PRIMARY KEY (user_id, aydinlatma_version)
);

CREATE INDEX IF NOT EXISTS idx_first_login_ack_user ON first_login_acknowledgements (user_id);
