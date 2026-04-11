-- =============================================================================
-- Personel Platform — PostgreSQL Bootstrap Schema
-- =============================================================================
-- Executed once by docker-entrypoint-initdb.d on first container start.
-- Idempotent: uses CREATE ... IF NOT EXISTS throughout.
-- Role model:
--   app_admin_api  — full CRUD on application tables, INSERT-only on audit_events
--   app_gateway    — INSERT on ingest tables, SELECT on keystroke_keys
--   app_dlp_ro     — SELECT on keystroke_keys ONLY (no other tables)
--   app_keycloak   — separate schema, full CRUD
-- =============================================================================

\set ON_ERROR_STOP on

-- ---------------------------------------------------------------------------
-- Extensions
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";       -- for ILIKE full-text on smaller columns
CREATE EXTENSION IF NOT EXISTS "btree_gin";     -- GIN index support for JSONB
CREATE EXTENSION IF NOT EXISTS "pg_cron";       -- scheduled maintenance jobs (retention, DSR SLA)

-- ---------------------------------------------------------------------------
-- Application roles (passwords are overridden at runtime by Vault dynamic secrets)
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_admin_api') THEN
    CREATE ROLE app_admin_api WITH LOGIN PASSWORD 'PLACEHOLDER_VAULT_WILL_ROTATE';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_gateway') THEN
    CREATE ROLE app_gateway WITH LOGIN PASSWORD 'PLACEHOLDER_VAULT_WILL_ROTATE';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_dlp_ro') THEN
    CREATE ROLE app_dlp_ro WITH LOGIN PASSWORD 'PLACEHOLDER_VAULT_WILL_ROTATE';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_keycloak') THEN
    CREATE ROLE app_keycloak WITH LOGIN PASSWORD 'PLACEHOLDER_VAULT_WILL_ROTATE';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'backup_operator') THEN
    CREATE ROLE backup_operator WITH LOGIN REPLICATION PASSWORD 'PLACEHOLDER_VAULT_WILL_ROTATE';
  END IF;
END $$;

-- ---------------------------------------------------------------------------
-- Schemas
-- ---------------------------------------------------------------------------
CREATE SCHEMA IF NOT EXISTS core    AUTHORIZATION app_admin_api;
CREATE SCHEMA IF NOT EXISTS audit   AUTHORIZATION app_admin_api;
CREATE SCHEMA IF NOT EXISTS keycloak AUTHORIZATION app_keycloak;

-- ---------------------------------------------------------------------------
-- CORE TABLES
-- ---------------------------------------------------------------------------

-- Tenants (single-tenant for Phase 1; multi-tenant code path exists but unused)
CREATE TABLE IF NOT EXISTS core.tenants (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,
  kvkk_config   JSONB NOT NULL DEFAULT '{}',
  verbis_id     TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users
CREATE TABLE IF NOT EXISTS core.users (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id     UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  email         TEXT NOT NULL,
  display_name  TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin','hr','dpo','viewer','employee')),
  status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','deleted')),
  token_version INTEGER NOT NULL DEFAULT 0,  -- bump to invalidate all JWTs
  external_id   TEXT,                        -- LDAP/AD objectGUID
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(tenant_id, email)
);
CREATE INDEX IF NOT EXISTS users_tenant_idx ON core.users(tenant_id);
CREATE INDEX IF NOT EXISTS users_external_idx ON core.users(external_id) WHERE external_id IS NOT NULL;

-- Endpoints (enrolled Windows agents)
CREATE TABLE IF NOT EXISTS core.endpoints (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id       UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  hostname        TEXT NOT NULL,
  user_sid        TEXT,
  hw_fingerprint  TEXT NOT NULL,
  cert_serial     TEXT,
  status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked','offline','maintenance')),
  last_seen_at    TIMESTAMPTZ,
  agent_version   TEXT,
  os_version      TEXT,
  pe_dek_version  INTEGER NOT NULL DEFAULT 1,
  tmk_version     INTEGER NOT NULL DEFAULT 1,
  enrollment_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(tenant_id, hw_fingerprint)
);
CREATE INDEX IF NOT EXISTS endpoints_tenant_idx ON core.endpoints(tenant_id);
CREATE INDEX IF NOT EXISTS endpoints_cert_serial_idx ON core.endpoints(cert_serial) WHERE cert_serial IS NOT NULL;
CREATE INDEX IF NOT EXISTS endpoints_status_idx ON core.endpoints(status);

-- Keystroke keys (DLP-managed; wrapped DEKs only — never plaintext)
-- SECURITY: app_dlp_ro has SELECT only. app_admin_api has NO access to this table.
CREATE TABLE IF NOT EXISTS core.keystroke_keys (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  endpoint_id   UUID NOT NULL REFERENCES core.endpoints(id) ON DELETE RESTRICT,
  tenant_id     UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  wrapped_dek   BYTEA NOT NULL,      -- AES-256-GCM ciphertext of PE-DEK, wrapped by DSEK via Vault transit
  nonce         BYTEA NOT NULL,      -- 96-bit GCM nonce
  dek_version   INTEGER NOT NULL DEFAULT 1,
  tmk_version   INTEGER NOT NULL DEFAULT 1,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(endpoint_id, dek_version)
);
-- Deliberately NO index on wrapped_dek — no query path should filter on key material
CREATE INDEX IF NOT EXISTS keystroke_keys_endpoint_idx ON core.keystroke_keys(endpoint_id);

-- Policies
CREATE TABLE IF NOT EXISTS core.policies (
  id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id     UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  name          TEXT NOT NULL,
  version       INTEGER NOT NULL DEFAULT 1,
  status        TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
  bundle        JSONB NOT NULL DEFAULT '{}',  -- compiled policy bundle
  bundle_sig    TEXT,                          -- Ed25519 signature over bundle (hex)
  signing_kid   TEXT,
  created_by    UUID REFERENCES core.users(id),
  approved_by   UUID REFERENCES core.users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(tenant_id, name, version)
);

-- Live-view requests (dual-control state machine)
CREATE TABLE IF NOT EXISTS core.live_view_requests (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id       UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  endpoint_id     UUID NOT NULL REFERENCES core.endpoints(id) ON DELETE RESTRICT,
  requested_by    UUID NOT NULL REFERENCES core.users(id),
  approved_by     UUID REFERENCES core.users(id),
  state           TEXT NOT NULL DEFAULT 'pending'
                  CHECK (state IN ('pending','approved','active','ended','rejected','expired')),
  livekit_room    TEXT,
  livekit_token   TEXT,                        -- short-lived, cleared on end
  reason          TEXT NOT NULL,
  started_at      TIMESTAMPTZ,
  ended_at        TIMESTAMPTZ,
  audit_chain_ref TEXT,                        -- reference to audit chain entry
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS lvr_tenant_idx ON core.live_view_requests(tenant_id);
CREATE INDEX IF NOT EXISTS lvr_state_idx ON core.live_view_requests(state);
CREATE INDEX IF NOT EXISTS lvr_endpoint_idx ON core.live_view_requests(endpoint_id);

-- KVKK m.11 Data Subject Requests (DSR)
CREATE TABLE IF NOT EXISTS core.dsr_requests (
  id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id             UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  employee_user_id      UUID REFERENCES core.users(id),
  employee_email        TEXT,                  -- for non-enrolled employees
  request_type          TEXT NOT NULL
                        CHECK (request_type IN ('access','rectify','erase','object','restrict','portability')),
  scope_json            JSONB NOT NULL DEFAULT '{}',
  state                 TEXT NOT NULL DEFAULT 'open'
                        CHECK (state IN ('open','at_risk','overdue','extended','responded','rejected','closed')),
  justification         TEXT NOT NULL,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  sla_deadline          TIMESTAMPTZ NOT NULL GENERATED ALWAYS AS (created_at + INTERVAL '30 days') STORED,
  extended_deadline     TIMESTAMPTZ,
  assigned_to           UUID REFERENCES core.users(id),
  response_artifact_ref TEXT,                  -- MinIO path to signed PDF
  audit_chain_ref       TEXT,
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dsr_tenant_state_idx ON core.dsr_requests(tenant_id, state);
CREATE INDEX IF NOT EXISTS dsr_deadline_idx ON core.dsr_requests(sla_deadline) WHERE state IN ('open','at_risk');

-- Legal holds
CREATE TABLE IF NOT EXISTS core.legal_holds (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tenant_id       UUID NOT NULL REFERENCES core.tenants(id) ON DELETE RESTRICT,
  placed_by       UUID NOT NULL REFERENCES core.users(id),
  released_by     UUID REFERENCES core.users(id),
  reason_code     TEXT NOT NULL,
  ticket_id       TEXT NOT NULL,
  justification   TEXT NOT NULL,
  scope_json      JSONB NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','released')),
  expires_at      TIMESTAMPTZ NOT NULL,
  placed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  released_at     TIMESTAMPTZ,
  release_reason  TEXT,
  audit_chain_ref TEXT,
  CONSTRAINT hold_duration_max_2y CHECK (expires_at <= placed_at + INTERVAL '2 years')
);
CREATE INDEX IF NOT EXISTS legal_holds_tenant_idx ON core.legal_holds(tenant_id, status);

-- PKI cert deny-list (replicated to Vault KV on update)
CREATE TABLE IF NOT EXISTS core.cert_deny_list (
  serial      TEXT PRIMARY KEY,
  endpoint_id UUID REFERENCES core.endpoints(id),
  revoked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  reason      TEXT NOT NULL
);

-- ---------------------------------------------------------------------------
-- AUDIT SCHEMA — Append-Only, Hash-Chained
-- ---------------------------------------------------------------------------

-- audit_events: the append-only hash-chained journal.
-- GRANT: app_admin_api may INSERT; no UPDATE or DELETE for any role.
-- Trigger enforces append-only at DB level.
CREATE TABLE IF NOT EXISTS audit.audit_events (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   UUID NOT NULL,
  event_type  TEXT NOT NULL,       -- e.g. 'live_view.started', 'dsr.submitted', 'admin.login'
  actor_id    UUID,                -- user or service identity; NULL for system events
  actor_type  TEXT NOT NULL DEFAULT 'user' CHECK (actor_type IN ('user','service','system')),
  entity_type TEXT,                -- 'endpoint','user','policy', etc.
  entity_id   TEXT,
  payload     JSONB NOT NULL DEFAULT '{}',
  prev_hash   TEXT,                -- SHA-256 of previous row's hash (chain link)
  row_hash    TEXT,                -- SHA-256 of this row's canonical fields
  signed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  seq         BIGINT NOT NULL      -- monotonically increasing within tenant
) PARTITION BY RANGE (signed_at);

-- Create monthly partitions for current year (extend in upgrade.sh)
CREATE TABLE IF NOT EXISTS audit.audit_events_2026_q1
  PARTITION OF audit.audit_events
  FOR VALUES FROM ('2026-01-01') TO ('2026-04-01');
CREATE TABLE IF NOT EXISTS audit.audit_events_2026_q2
  PARTITION OF audit.audit_events
  FOR VALUES FROM ('2026-04-01') TO ('2026-07-01');
CREATE TABLE IF NOT EXISTS audit.audit_events_2026_q3
  PARTITION OF audit.audit_events
  FOR VALUES FROM ('2026-07-01') TO ('2026-10-01');
CREATE TABLE IF NOT EXISTS audit.audit_events_2026_q4
  PARTITION OF audit.audit_events
  FOR VALUES FROM ('2026-10-01') TO ('2027-01-01');

CREATE INDEX IF NOT EXISTS audit_tenant_time_idx ON audit.audit_events(tenant_id, signed_at DESC);
CREATE INDEX IF NOT EXISTS audit_event_type_idx ON audit.audit_events(event_type, signed_at DESC);
CREATE INDEX IF NOT EXISTS audit_actor_idx ON audit.audit_events(actor_id) WHERE actor_id IS NOT NULL;

-- Append-only enforcement trigger
CREATE OR REPLACE FUNCTION audit.deny_audit_modification()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only; UPDATE and DELETE are forbidden';
END;
$$;

DROP TRIGGER IF EXISTS audit_no_update ON audit.audit_events;
CREATE TRIGGER audit_no_update
  BEFORE UPDATE ON audit.audit_events
  FOR EACH ROW EXECUTE FUNCTION audit.deny_audit_modification();

DROP TRIGGER IF EXISTS audit_no_delete ON audit.audit_events;
CREATE TRIGGER audit_no_delete
  BEFORE DELETE ON audit.audit_events
  FOR EACH ROW EXECUTE FUNCTION audit.deny_audit_modification();

-- audit.append_event — the only safe insert path; computes hash chain.
CREATE OR REPLACE FUNCTION audit.append_event(
  p_tenant_id   UUID,
  p_event_type  TEXT,
  p_actor_id    UUID,
  p_actor_type  TEXT,
  p_entity_type TEXT,
  p_entity_id   TEXT,
  p_payload     JSONB
)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER AS $$
DECLARE
  v_prev_hash TEXT;
  v_prev_seq  BIGINT;
  v_row_hash  TEXT;
  v_id        BIGINT;
BEGIN
  -- Get the last chain link for this tenant
  SELECT row_hash, seq
  INTO v_prev_hash, v_prev_seq
  FROM audit.audit_events
  WHERE tenant_id = p_tenant_id
  ORDER BY seq DESC
  LIMIT 1;

  IF v_prev_seq IS NULL THEN
    v_prev_seq  := 0;
    v_prev_hash := encode(digest(p_tenant_id::text || 'genesis', 'sha256'), 'hex');
  END IF;

  -- Compute row hash over deterministic canonical fields
  v_row_hash := encode(digest(
    p_tenant_id::text ||
    p_event_type ||
    COALESCE(p_actor_id::text, '') ||
    p_actor_type ||
    COALESCE(p_entity_type, '') ||
    COALESCE(p_entity_id, '') ||
    p_payload::text ||
    v_prev_hash ||
    (v_prev_seq + 1)::text,
    'sha256'
  ), 'hex');

  INSERT INTO audit.audit_events(
    tenant_id, event_type, actor_id, actor_type,
    entity_type, entity_id, payload,
    prev_hash, row_hash, seq
  ) VALUES (
    p_tenant_id, p_event_type, p_actor_id, p_actor_type,
    p_entity_type, p_entity_id, p_payload,
    v_prev_hash, v_row_hash, v_prev_seq + 1
  ) RETURNING id INTO v_id;

  RETURN v_id;
END;
$$;

-- Audit checkpoints table (external checkpoint signatures written here)
CREATE TABLE IF NOT EXISTS audit.checkpoints (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     UUID NOT NULL,
  checkpoint_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seq      BIGINT NOT NULL,
  last_hash     TEXT NOT NULL,
  signature     TEXT NOT NULL,   -- Ed25519 signature (control signing key) over last_seq||last_hash
  signing_kid   TEXT NOT NULL
);

-- ---------------------------------------------------------------------------
-- ROLE GRANTS
-- ---------------------------------------------------------------------------

-- app_admin_api: CRUD on core, INSERT-only on audit_events (via function), no keystroke_keys
GRANT USAGE ON SCHEMA core TO app_admin_api;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA core TO app_admin_api;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA core TO app_admin_api;
REVOKE ALL ON core.keystroke_keys FROM app_admin_api;  -- explicitly forbidden

GRANT USAGE ON SCHEMA audit TO app_admin_api;
GRANT SELECT ON audit.audit_events TO app_admin_api;   -- read for DPO queries
GRANT SELECT, INSERT ON audit.checkpoints TO app_admin_api;
GRANT EXECUTE ON FUNCTION audit.append_event TO app_admin_api;

-- app_gateway: limited ingest access
GRANT USAGE ON SCHEMA core TO app_gateway;
GRANT INSERT ON core.endpoints TO app_gateway;
GRANT SELECT ON core.endpoints TO app_gateway;
GRANT SELECT ON core.keystroke_keys TO app_gateway;   -- only to read pe_dek_version/tmk_version
GRANT SELECT ON core.policies TO app_gateway;
GRANT SELECT ON core.cert_deny_list TO app_gateway;
GRANT INSERT ON core.cert_deny_list TO app_gateway;
GRANT USAGE ON SCHEMA audit TO app_gateway;
GRANT EXECUTE ON FUNCTION audit.append_event TO app_gateway;

-- app_dlp_ro: ONLY keystroke_keys SELECT
GRANT USAGE ON SCHEMA core TO app_dlp_ro;
GRANT SELECT ON core.keystroke_keys TO app_dlp_ro;
-- All other core tables: no access

-- app_keycloak: separate schema
GRANT ALL ON SCHEMA keycloak TO app_keycloak;

-- ---------------------------------------------------------------------------
-- ROW LEVEL SECURITY (multi-tenant readiness)
-- ---------------------------------------------------------------------------
ALTER TABLE core.tenants         ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.users           ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.endpoints       ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.policies        ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.live_view_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.dsr_requests    ENABLE ROW LEVEL SECURITY;
ALTER TABLE core.legal_holds     ENABLE ROW LEVEL SECURITY;

-- Phase 1: single-tenant, so RLS policies just enforce tenant_id matches
-- The application sets the session variable set_config('app.tenant_id', ...) on connection.
CREATE POLICY tenant_isolation ON core.users
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
CREATE POLICY tenant_isolation ON core.endpoints
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
CREATE POLICY tenant_isolation ON core.policies
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
CREATE POLICY tenant_isolation ON core.live_view_requests
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
CREATE POLICY tenant_isolation ON core.dsr_requests
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
CREATE POLICY tenant_isolation ON core.legal_holds
  USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

-- ---------------------------------------------------------------------------
-- PG_CRON JOBS — Retention and DSR SLA
-- ---------------------------------------------------------------------------

-- DSR SLA timer: open -> at_risk at day 20, open/at_risk -> overdue at day 30
SELECT cron.schedule(
  'personel-dsr-sla-check',
  '0 8 * * *',  -- daily at 08:00 UTC
  $$
    UPDATE core.dsr_requests
    SET state = 'overdue', updated_at = now()
    WHERE state IN ('open', 'at_risk')
      AND now() >= sla_deadline;

    UPDATE core.dsr_requests
    SET state = 'at_risk', updated_at = now()
    WHERE state = 'open'
      AND now() >= (created_at + INTERVAL '20 days')
      AND now() < sla_deadline;
  $$
) ON CONFLICT DO NOTHING;

-- Expire live view sessions that were never ended (safety net, max 2 hours)
SELECT cron.schedule(
  'personel-lv-expire',
  '*/15 * * * *',
  $$
    UPDATE core.live_view_requests
    SET state = 'ended', ended_at = now(), updated_at = now()
    WHERE state = 'active'
      AND started_at < now() - INTERVAL '2 hours';
  $$
) ON CONFLICT DO NOTHING;

-- Expire legal holds past their expiry date
SELECT cron.schedule(
  'personel-legal-hold-expire',
  '0 1 * * *',
  $$
    UPDATE core.legal_holds
    SET status = 'released', released_at = now()
    WHERE status = 'active'
      AND expires_at < now();
  $$
) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- INITIAL SEED: default tenant placeholder (replaced by install.sh)
-- ---------------------------------------------------------------------------
INSERT INTO core.tenants(id, name, slug, kvkk_config)
VALUES (
  '00000000-0000-0000-0000-000000000001'::UUID,
  'CHANGEME_TENANT_NAME',
  'default',
  '{"verbis_registered": false, "data_controller": "CHANGEME_COMPANY_NAME"}'
)
ON CONFLICT(slug) DO NOTHING;

-- ---------------------------------------------------------------------------
-- COMMENTS (documentation as code)
-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.keystroke_keys IS
  'Contains only WRAPPED (ciphertext) PE-DEKs. Accessible only to app_dlp_ro. '
  'Admins have no SELECT grant. This is the cryptographic boundary between the '
  'admin plane and the DLP plane per key-hierarchy.md.';

COMMENT ON TABLE audit.audit_events IS
  'Append-only hash-chained audit journal. UPDATE and DELETE are blocked by trigger. '
  'All writes must go through audit.append_event(). Partitioned by quarter for '
  'efficient retention management.';

COMMENT ON FUNCTION audit.append_event IS
  'The sole write path for the audit chain. Computes SHA-256 hash chain over '
  'previous row and inserts the new event atomically. SECURITY DEFINER to allow '
  'application roles to insert without direct INSERT privilege on the table.';
