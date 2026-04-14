-- Migration 0042: tenants.ca_mode + tenants.ca_config — production CA mode.
--
-- Wave 9 Sprint 3A settings. Each tenant picks one of three CA modes at
-- deployment time:
--
--   letsencrypt — automatic DNS-01 from Let's Encrypt; ca_config carries
--                 { dns_provider, email, ... }
--   internal    — Vault PKI root we already ship with (default, ca_config
--                 is an empty JSON object)
--   commercial  — customer-provided commercial cert chain; ca_config
--                 carries { csr_key, cert_chain_key } pointing at MinIO
--                 object keys where the signed chain lives
--
-- ca_config is JSONB so the shape can evolve per mode without migrations.
-- The application layer validates the shape against the chosen mode.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS ca_mode   TEXT NOT NULL DEFAULT 'internal',
    ADD COLUMN IF NOT EXISTS ca_config JSONB;

-- Defensive check constraint — the only legal values are the three
-- supported modes. Any future addition requires a migration AND code
-- in internal/settings to handle the new shape.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM information_schema.check_constraints
         WHERE constraint_name = 'tenants_ca_mode_check'
    ) THEN
        ALTER TABLE tenants
          ADD CONSTRAINT tenants_ca_mode_check
          CHECK (ca_mode IN ('letsencrypt', 'internal', 'commercial'));
    END IF;
END $$;

COMMENT ON COLUMN tenants.ca_mode IS
    'Üretim sertifika CA modu — letsencrypt | internal | commercial (default internal)';
COMMENT ON COLUMN tenants.ca_config IS
    'Mode-bağımlı JSON config: LE için dns_provider+email, commercial için MinIO key refs';
