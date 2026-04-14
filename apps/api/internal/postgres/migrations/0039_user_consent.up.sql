-- Migration 0039: user_consent table — per-user KVKK açık rıza (explicit consent).
--
-- ADR 0013 A-series: DLP keystroke content capture requires explicit
-- opt-in "açık rıza" from each affected employee. This table records
-- every signed/revoked consent as a first-class row so that (a) the
-- DLP engine can gate capture on live consent state and (b) KVKK
-- DSR fulfilment (m.11/a) can produce a complete consent history for
-- any subject without scanning the audit log.
--
-- consent_type is a free-form string rather than an enum because
-- KVKK Art. 5-2/h ("açık rıza") applies across many future feature
-- opt-ins (live view recording, high-frequency screen capture,
-- cross-department transfer, ...). The application layer validates
-- the allowed set so a typo does not silently write a new consent
-- category.
--
-- Once signed_at is set, revoked_at may later be filled — but the
-- row itself is never deleted. Auditors need to see consent timelines
-- including revocations. The UNIQUE (user_id, consent_type) constraint
-- enforces that there is exactly one tracking row per user+type;
-- re-consenting after a revoke updates signed_at and clears
-- revoked_at on the same row.

CREATE TABLE IF NOT EXISTS user_consent (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    consent_type    TEXT NOT NULL,
    signed_at       TIMESTAMPTZ,
    document_key    TEXT,
    document_sha256 TEXT,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, consent_type)
);

CREATE INDEX IF NOT EXISTS idx_user_consent_tenant
    ON user_consent (tenant_id);

-- Partial index for fast "does this user currently have a live consent
-- of type X?" queries from the DLP engine. Rows with revoked_at IS NOT
-- NULL are excluded; signed_at IS NOT NULL ensures we only index active
-- consents.
CREATE INDEX IF NOT EXISTS idx_user_consent_active
    ON user_consent (tenant_id, consent_type, user_id)
    WHERE signed_at IS NOT NULL AND revoked_at IS NULL;

-- Row-level security — same pattern as mobile_push_tokens (migration 0024)
-- and evidence_items (migration 0025). The app role sets
-- personel.tenant_id per session in the middleware; RLS prevents
-- cross-tenant reads even if a handler forgets to filter.
ALTER TABLE user_consent ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_consent_tenant_isolation ON user_consent
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

COMMENT ON TABLE user_consent IS
    'KVKK açık rıza kayıtları — signed/revoked timeline per user per consent_type';
COMMENT ON COLUMN user_consent.consent_type IS
    'Serbest metin: dlp | live_view_recording | screen_capture_high_freq | cross_department_transfer | ...';
COMMENT ON COLUMN user_consent.signed_at IS
    'Rıza imzalama zamanı — NULL ise henüz imzalanmamış (placeholder row)';
COMMENT ON COLUMN user_consent.revoked_at IS
    'Rıza geri çekme zamanı — NULL ise aktif; NOT NULL ise revoke edilmiş';
COMMENT ON COLUMN user_consent.document_key IS
    'MinIO object key: kvkk/{tenant_id}/consent/{user_id}/{ulid}.pdf';
COMMENT ON COLUMN user_consent.document_sha256 IS
    'İmzalı rıza belgesinin SHA-256 hex hash bütünlük kontrolü';
