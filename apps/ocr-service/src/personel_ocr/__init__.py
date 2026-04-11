"""
personel_ocr — OCR service for Personel Platform Phase 2.8.

Screenshot text extraction with Turkish/English language support and
KVKK-compliant PII redaction (TCKN, IBAN, credit card, phone, email).

KVKK note: OCR is OFF by default (module-state: disabled). The enricher
only forwards screenshots here when:
  1. Module state for 'ocr' is 'enabled' (tenant opt-in via ceremony).
  2. The screenshot is NOT sensitive-flagged.
  3. The source app is NOT in the tenant's OCR exclude list.

See docs/architecture/phase-2-scope.md §B.3 and docs/compliance/kvkk-framework.md §6.
"""
