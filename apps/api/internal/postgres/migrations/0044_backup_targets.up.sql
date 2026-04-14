-- Migration 0044: backup_targets — per-tenant backup destination catalog.
--
-- Wave 9 Sprint 3A backend. Tenants can configure N backup destinations;
-- the backup-cron process picks them up at run time and writes to each.
-- Per-target retention overrides the tenant-level retention_policy
-- (migration 0043) for audit-friendly defence-in-depth — a customer may
-- choose to keep in-site backups for 7 days while retaining an off-site
-- S3 archive for 5 years.
--
-- Supported kinds (allowlist enforced at application layer):
--
--   in_site_local       — same host / NAS mount
--   offsite_s3          — AWS S3 or S3-compatible (not MinIO peer)
--   offsite_azure       — Azure Blob
--   offsite_gcs         — Google Cloud Storage
--   offsite_sftp        — SFTP server
--   offsite_nfs         — NFS mount (DR site)
--   offsite_minio_peer  — peer MinIO cluster mirroring
--
-- config_encrypted is the Vault transit ciphertext of the kind-specific
-- config JSON (bucket, credentials, endpoints, etc). config_key_version
-- records the Vault key version so rotation can retire old targets
-- without losing access.

CREATE TABLE IF NOT EXISTS backup_targets (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL,
    config_encrypted   BYTEA NOT NULL,
    config_key_version INT  NOT NULL,
    enabled            BOOLEAN NOT NULL DEFAULT true,
    retention_days     INT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_backup_targets_tenant
    ON backup_targets (tenant_id);

ALTER TABLE backup_targets ENABLE ROW LEVEL SECURITY;

CREATE POLICY backup_targets_tenant_isolation ON backup_targets
    USING (tenant_id = current_setting('personel.tenant_id', true)::uuid);

COMMENT ON TABLE backup_targets IS
    'Backup hedefi kataloğu — her tenant N hedef tanımlayabilir, cron hepsine yazar';
COMMENT ON COLUMN backup_targets.kind IS
    'in_site_local | offsite_s3 | offsite_azure | offsite_gcs | offsite_sftp | offsite_nfs | offsite_minio_peer';
COMMENT ON COLUMN backup_targets.retention_days IS
    'NULL = tenants.retention_policy devral. Non-NULL = bu hedefe özel override.';
