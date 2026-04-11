DROP POLICY IF EXISTS mobile_push_tokens_tenant_isolation ON mobile_push_tokens;
DROP INDEX IF EXISTS mobile_push_tokens_user_active;
DROP INDEX IF EXISTS mobile_push_tokens_tenant_id;
DROP TABLE IF EXISTS mobile_push_tokens;
