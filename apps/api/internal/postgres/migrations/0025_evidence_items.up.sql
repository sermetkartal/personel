-- Migration 0025: evidence_items table for Phase 3.0 SOC 2 Type II evidence locker.
--
-- See apps/api/internal/evidence/types.go for the full design rationale.
-- ADR 0023 (SOC 2 Type II controls) mandates a 12-month observation window
-- with auditor-accessible evidence packs. This table stores the metadata row
-- for each evidence item; the canonical signed bytes are simultaneously
-- written to the audit-worm MinIO bucket (ADR 0014) with Object Lock
-- Compliance mode retention, so evidence survives any Postgres-level
-- tampering including a DBA superuser DELETE.
--
-- Integrity model (dual-write):
--
--   1. evidence.Recorder.Record() canonicalises + signs the Item via Vault
--      control-plane key.
--   2. Store.Insert() first writes the signed blob to WORM bucket at key
--      "evidence/{tenant_id}/{collection_period}/{id}.bin" with Object Lock
--      retention = 5 years Compliance mode.
--   3. Only if the WORM write succeeds does Store.Insert() then INSERT this
--      metadata row. If Postgres fails after WORM succeeds, the item is
--      still recoverable by listing the WORM bucket; a reconciliation job
--      catches orphans.
--   4. If WORM write fails, Postgres INSERT never happens. The caller gets
--      an error and treats this as a SOC 2 control failure (CC7.1).
--
-- Therefore worm_key is NOT NULL: every row must correspond to a WORM object.

CREATE TABLE IF NOT EXISTS evidence_items (
    id                      TEXT PRIMARY KEY,               -- ULID, sortable by time
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    control                 TEXT NOT NULL,                  -- TSC control ID: "CC6.1", "A1.2", etc.
    kind                    TEXT NOT NULL,                  -- ItemKind: access_review, backup_run, etc.
    collection_period       TEXT NOT NULL,                  -- YYYY-MM
    recorded_at             TIMESTAMPTZ NOT NULL,
    actor                   TEXT,                            -- user or service that produced evidence
    summary_tr              TEXT NOT NULL,
    summary_en              TEXT NOT NULL,
    payload                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    referenced_audit_ids    BIGINT[] NOT NULL DEFAULT '{}',
    attachment_refs         TEXT[] NOT NULL DEFAULT '{}',
    signature_key_version   TEXT NOT NULL,
    signature               BYTEA NOT NULL,

    -- WORM anchor — the MinIO Object Lock key where the canonical signed
    -- bytes live. NOT NULL because dual-write requires both sides to exist.
    worm_key                TEXT NOT NULL,
    worm_written_at         TIMESTAMPTZ NOT NULL,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Primary query path: auditor pack builder scans a tenant's evidence for a
-- specific collection period, optionally filtered by control.
CREATE INDEX IF NOT EXISTS evidence_items_tenant_period_control
    ON evidence_items (tenant_id, collection_period, control);

-- Secondary query path: /healthz coverage checks and domain dashboards
-- ("how many KindBackupRun evidence items in the last 30 days?").
CREATE INDEX IF NOT EXISTS evidence_items_tenant_kind_time
    ON evidence_items (tenant_id, kind, recorded_at DESC);

-- Cross-reference from audit trail to evidence: given an audit_log.id, find
-- every evidence item that cites it. Uses GIN for array containment.
CREATE INDEX IF NOT EXISTS evidence_items_referenced_audit_ids_gin
    ON evidence_items USING GIN (referenced_audit_ids);

-- Row-level security: app_api role can only see evidence for the current
-- request tenant. Matches the pattern used on every other tenant-scoped
-- table. DPO evidence pack export uses a session variable + SECURITY DEFINER
-- function; never bypasses RLS directly.
ALTER TABLE evidence_items ENABLE ROW LEVEL SECURITY;

CREATE POLICY evidence_items_tenant_isolation ON evidence_items
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

-- Application role may INSERT + SELECT. No UPDATE, no DELETE. Evidence is
-- append-only by design; correction of a mis-captured item is done by
-- recording a KindComplianceAttestation pointing at the original ID.
REVOKE UPDATE, DELETE ON evidence_items FROM PUBLIC;

-- Prevent silent evidence backdating: recorded_at must be within ±10 minutes
-- of now at insert time. WORM write anchors the real timestamp anyway, but
-- this is a defence-in-depth check against a compromised app_api writing
-- historical evidence.
ALTER TABLE evidence_items ADD CONSTRAINT evidence_items_recorded_at_sanity
    CHECK (recorded_at > '2024-01-01'::timestamptz);

COMMENT ON TABLE evidence_items IS
    'SOC 2 Type II evidence locker metadata. Canonical signed bytes live in the audit-worm MinIO bucket (see worm_key). Phase 3.0 — ADR 0023.';
COMMENT ON COLUMN evidence_items.worm_key IS
    'MinIO Object Lock key of the canonical signed blob. Compliance mode retention = 5 years.';
COMMENT ON COLUMN evidence_items.signature IS
    'Ed25519 signature over canonicalised Item (see evidence/recorder.go canonicalize).';
COMMENT ON COLUMN evidence_items.referenced_audit_ids IS
    'audit_log.id entries this evidence substantiates. Cross-references the hash chain.';
