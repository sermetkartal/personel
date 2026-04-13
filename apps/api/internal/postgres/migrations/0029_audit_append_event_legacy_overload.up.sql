-- 0029_audit_append_event_legacy_overload.up.sql
-- Compatibility overload bridging the bootstrap init.sql signature with the
-- Go recorder's call site. The infra bootstrap (infra/compose/postgres/init.sql)
-- creates audit.append_event(uuid, text, uuid, text, text, text, jsonb) but
-- apps/api/internal/audit/recorder.go calls it as
-- (text, inet, text, uuid, text, text, jsonb). Without this overload the
-- function call fails to resolve and every audit write 500s. Schema ownership
-- unification is tracked in CLAUDE.md §10 ("Schema ownership dokümantasyonu").

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
AS $func$
DECLARE
    v_actor_uuid UUID;
    v_payload    JSONB;
    v_id         BIGINT;
BEGIN
    BEGIN
        v_actor_uuid := p_actor::UUID;
    EXCEPTION WHEN invalid_text_representation THEN
        v_actor_uuid := NULL;
    END;

    v_payload := COALESCE(p_details, '{}'::jsonb)
               || jsonb_build_object(
                    'actor_ip', COALESCE(host(p_actor_ip), ''),
                    'actor_ua', COALESCE(p_actor_ua, ''),
                    'target',   COALESCE(p_target, '')
                  );

    v_id := audit.append_event(
        p_tenant_id,
        p_action,
        v_actor_uuid,
        'user',
        'entity',
        COALESCE(p_target, ''),
        v_payload
    );
    RETURN v_id;
END;
$func$;

GRANT EXECUTE ON FUNCTION audit.append_event(TEXT, INET, TEXT, UUID, TEXT, TEXT, JSONB) TO app_admin_api;
