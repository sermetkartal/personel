-- Reverse of 0025_evidence_items. Note that the WORM objects themselves
-- cannot be deleted by the down migration — they remain in the audit-worm
-- bucket until their Compliance mode retention period (5 years) expires.
-- This is intentional per ADR 0014 and ADR 0023. Only the Postgres index
-- of evidence is removed.

DROP POLICY IF EXISTS evidence_items_tenant_isolation ON evidence_items;
DROP INDEX IF EXISTS evidence_items_referenced_audit_ids_gin;
DROP INDEX IF EXISTS evidence_items_tenant_kind_time;
DROP INDEX IF EXISTS evidence_items_tenant_period_control;
DROP TABLE IF EXISTS evidence_items;
