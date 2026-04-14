-- 0035_tickets.down.sql — reverse item #184.

DROP POLICY IF EXISTS tickets_tenant_isolation ON tickets;
DROP TABLE IF EXISTS tickets;
