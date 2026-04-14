ALTER TABLE tenants
    DROP COLUMN IF EXISTS verbis_registration_number,
    DROP COLUMN IF EXISTS verbis_registered_at,
    DROP COLUMN IF EXISTS aydinlatma_markdown,
    DROP COLUMN IF EXISTS aydinlatma_published_at,
    DROP COLUMN IF EXISTS aydinlatma_version,
    DROP COLUMN IF EXISTS dpa_signed_at,
    DROP COLUMN IF EXISTS dpa_document_key,
    DROP COLUMN IF EXISTS dpa_document_sha256,
    DROP COLUMN IF EXISTS dpa_signatories,
    DROP COLUMN IF EXISTS dpia_amendment_key,
    DROP COLUMN IF EXISTS dpia_amendment_sha256,
    DROP COLUMN IF EXISTS dpia_completed_at;
