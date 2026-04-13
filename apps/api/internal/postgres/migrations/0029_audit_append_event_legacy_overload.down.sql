-- 0029_audit_append_event_legacy_overload.down.sql
DROP FUNCTION IF EXISTS audit.append_event(TEXT, INET, TEXT, UUID, TEXT, TEXT, JSONB);
