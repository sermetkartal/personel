-- 0032_endpoint_commands.down.sql

DROP INDEX IF EXISTS endpoint_commands_endpoint;
DROP INDEX IF EXISTS endpoint_commands_tenant_state;
DROP TABLE IF EXISTS endpoint_commands;
