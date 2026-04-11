-- 004_audit_log.up.sql
-- Hash-chained append-only audit log.
-- Schema matches admin-audit-immutability.md §2 exactly.

CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS audit.audit_log (
    id         BIGSERIAL PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor      TEXT        NOT NULL,
    actor_ip   INET,
    actor_ua   TEXT,
    tenant_id  UUID        NOT NULL,
    action     TEXT        NOT NULL,
    target     TEXT        NOT NULL,
    details    JSONB       NOT NULL DEFAULT '{}',
    prev_hash  BYTEA       NOT NULL,
    hash       BYTEA       NOT NULL,
    CONSTRAINT audit_log_hash_len CHECK (octet_length(hash) = 32 AND octet_length(prev_hash) = 32)
);

CREATE INDEX IF NOT EXISTS audit_log_ts        ON audit.audit_log (ts);
CREATE INDEX IF NOT EXISTS audit_log_actor_ts  ON audit.audit_log (actor, ts);
CREATE INDEX IF NOT EXISTS audit_log_tenant_ts ON audit.audit_log (tenant_id, ts);
CREATE INDEX IF NOT EXISTS audit_log_action_ts ON audit.audit_log (action, ts);

CREATE TABLE IF NOT EXISTS audit.audit_checkpoint (
    id           BIGSERIAL,
    tenant_id    UUID  NOT NULL,
    day          DATE  NOT NULL,
    last_id      BIGINT NOT NULL,
    last_hash    BYTEA NOT NULL,
    entry_count  BIGINT NOT NULL,
    verified_at  TIMESTAMPTZ NOT NULL,
    verifier     TEXT NOT NULL,
    signature    BYTEA,
    PRIMARY KEY (tenant_id, day)
);

-- Append-only trigger: reject UPDATE and DELETE.
CREATE OR REPLACE FUNCTION audit.reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit.audit_log is append-only';
END;
$$;

CREATE TRIGGER trg_audit_log_no_update
    BEFORE UPDATE OR DELETE ON audit.audit_log
    FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();

-- The append_event stored procedure implements the hash chain.
-- The Go side calls: SELECT audit.append_event($1, $2, $3, $4, $5, $6, $7)
CREATE OR REPLACE FUNCTION audit.append_event(
    p_actor     TEXT,
    p_actor_ip  INET,
    p_actor_ua  TEXT,
    p_tenant_id UUID,
    p_action    TEXT,
    p_target    TEXT,
    p_details   JSONB
) RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
    v_prev_hash BYTEA;
    v_new_id    BIGINT;
    v_new_hash  BYTEA;
    v_ts        TIMESTAMPTZ := clock_timestamp();
    v_canon     BYTEA;
BEGIN
    -- Serialize concurrent appends.
    PERFORM pg_advisory_xact_lock(hashtextextended('audit.audit_log', 0));

    SELECT hash INTO v_prev_hash
    FROM audit.audit_log
    ORDER BY id DESC
    LIMIT 1;

    IF v_prev_hash IS NULL THEN
        v_prev_hash := decode(repeat('00', 32), 'hex');
    END IF;

    v_new_id := nextval('audit.audit_log_id_seq');

    -- Canonical encoding: id_be64 || ts_nanos_be64 || len(actor)_be32 || actor ||
    --                     len(tenant_id)_be32 || tenant_id || len(action)_be32 || action ||
    --                     len(target)_be32 || target || len(details_canon)_be32 || details_canon || prev_hash
    v_canon :=
        int8send(v_new_id) ||
        int8send(EXTRACT(EPOCH FROM v_ts)::bigint * 1000000000) ||
        int4send(length(p_actor::bytea)) || p_actor::bytea ||
        int4send(length(p_tenant_id::text::bytea)) || p_tenant_id::text::bytea ||
        int4send(length(p_action::bytea)) || p_action::bytea ||
        int4send(length(p_target::bytea)) || p_target::bytea ||
        int4send(length(p_details::text::bytea)) || p_details::text::bytea ||
        v_prev_hash;

    v_new_hash := digest(v_canon, 'sha256');

    INSERT INTO audit.audit_log
        (id, ts, actor, actor_ip, actor_ua, tenant_id, action, target, details, prev_hash, hash)
    VALUES
        (v_new_id, v_ts, p_actor, p_actor_ip, p_actor_ua, p_tenant_id, p_action, p_target, p_details, v_prev_hash, v_new_hash);

    RETURN v_new_id;
END;
$$;

-- Roles (created if not exists — idempotent for re-runs).
DO $$ BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_writer') THEN
        CREATE ROLE audit_writer NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_reader') THEN
        CREATE ROLE audit_reader NOLOGIN;
    END IF;
END $$;

GRANT USAGE ON SCHEMA audit TO audit_writer;
GRANT EXECUTE ON FUNCTION audit.append_event(TEXT, INET, TEXT, UUID, TEXT, TEXT, JSONB) TO audit_writer;
REVOKE ALL ON audit.audit_log FROM audit_writer;
REVOKE ALL ON audit.audit_log FROM PUBLIC;

GRANT USAGE ON SCHEMA audit TO audit_reader;
GRANT SELECT ON audit.audit_log TO audit_reader;
GRANT SELECT ON audit.audit_checkpoint TO audit_reader;
