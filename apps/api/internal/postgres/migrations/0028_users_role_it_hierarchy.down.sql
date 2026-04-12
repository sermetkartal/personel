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
