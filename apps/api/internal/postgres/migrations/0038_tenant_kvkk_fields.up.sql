-- Migration 0038: tenants.kvkk_* fields — VERBİS, Aydınlatma, DPA, DPIA metadata.
--
-- Wave 9 Sprint 2A backend for the KVKK compliance console section.
-- Every field here corresponds to a single form input in the console pages
-- at /tr/kvkk/{verbis,aydinlatma,dpa,dpia}. Uploaded document content itself
-- lives in MinIO — these columns store only metadata (object key + SHA256
-- integrity hash + signature timestamps). The actual DPA/DPIA PDFs are
-- retrievable from MinIO under the kvkk/{tenant_id}/{kind}/{ulid}.pdf
-- key scheme.
--
-- Rationale for column placement on the tenants table (rather than a new
-- kvkk_tenant_compliance sidecar table): each tenant has exactly one
-- VERBİS registration, one current aydınlatma metni, one signed DPA, and
-- one active DPIA. A 1:1 relationship is cleaner as a column set than a
-- sidecar row that would always exist. Historical versions — when the
-- aydınlatma text changes or a new DPA is signed — are captured implicitly
-- via aydinlatma_version (monotonic counter) and through the audit log
-- (hash-chained mutations with before/after state captured in Details).

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS verbis_registration_number TEXT,
    ADD COLUMN IF NOT EXISTS verbis_registered_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS aydinlatma_markdown        TEXT,
    ADD COLUMN IF NOT EXISTS aydinlatma_published_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS aydinlatma_version         INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS dpa_signed_at              TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dpa_document_key           TEXT,
    ADD COLUMN IF NOT EXISTS dpa_document_sha256        TEXT,
    ADD COLUMN IF NOT EXISTS dpa_signatories            JSONB,
    ADD COLUMN IF NOT EXISTS dpia_amendment_key         TEXT,
    ADD COLUMN IF NOT EXISTS dpia_amendment_sha256      TEXT,
    ADD COLUMN IF NOT EXISTS dpia_completed_at          TIMESTAMPTZ;

COMMENT ON COLUMN tenants.verbis_registration_number IS
    'VERBİS (Veri Sorumluları Sicil Bilgi Sistemi) kayıt numarası — KVKK m.16';
COMMENT ON COLUMN tenants.verbis_registered_at IS
    'VERBİS kayıt tescil tarihi';
COMMENT ON COLUMN tenants.aydinlatma_markdown IS
    'Çalışan aydınlatma metni (KVKK m.10) — Markdown gövdesi';
COMMENT ON COLUMN tenants.aydinlatma_published_at IS
    'Yürürlükteki aydınlatma metninin yayımlanma tarihi';
COMMENT ON COLUMN tenants.aydinlatma_version IS
    'Yayım sayacı — her publish çağrısı bir artırır, 0 = hiç yayımlanmadı';
COMMENT ON COLUMN tenants.dpa_signed_at IS
    'Data Processing Addendum imza tarihi';
COMMENT ON COLUMN tenants.dpa_document_key IS
    'MinIO object key: kvkk/{tenant_id}/dpa/{ulid}.pdf';
COMMENT ON COLUMN tenants.dpa_document_sha256 IS
    'DPA PDF içeriğinin SHA-256 hex hash — bütünlük doğrulama için';
COMMENT ON COLUMN tenants.dpa_signatories IS
    'Imzalayan taraflar — [{name, role, organization, signed_at}] JSON array';
COMMENT ON COLUMN tenants.dpia_amendment_key IS
    'DPIA amendment PDF MinIO object key';
COMMENT ON COLUMN tenants.dpia_amendment_sha256 IS
    'DPIA amendment PDF SHA-256 hex hash';
COMMENT ON COLUMN tenants.dpia_completed_at IS
    'DPIA tamamlanma tarihi (ADR 0013 DLP opt-in öncesi zorunlu)';
