-- Down: re-creates the original constraint without it_operator / it_manager.
-- WARNING: if any users rows already have role='it_operator' or 'it_manager',
-- ADD CONSTRAINT will fail with check constraint violation. This is
-- intentional data-loss protection — operator must DELETE or UPDATE those
-- rows before running the down migration. Do NOT auto-cascade delete them;
-- RBAC history matters for audit.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (
    role = ANY (ARRAY[
        'admin'::text,
        'manager'::text,
        'hr'::text,
        'dpo'::text,
        'investigator'::text,
        'auditor'::text,
        'employee'::text
    ])
);
