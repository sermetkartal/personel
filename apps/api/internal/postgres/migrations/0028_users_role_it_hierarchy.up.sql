-- Migration 0028: add it_operator + it_manager to users.role check constraint.
--
-- The IT hierarchy refactor (refactor(rbac): IT department owns live view
-- authority) added two new roles at the Go RBAC layer but the database
-- check constraint was not updated. Any INSERT with role='it_operator' or
-- 'it_manager' was rejected with users_role_check violation.
--
-- This migration drops the old constraint and recreates it with the full
-- role list, matching apps/api/internal/auth/rbac.go Role constants.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (
    role = ANY (ARRAY[
        'admin'::text,
        'manager'::text,
        'hr'::text,
        'dpo'::text,
        'investigator'::text,
        'auditor'::text,
        'employee'::text,
        'it_operator'::text,
        'it_manager'::text
    ])
);
