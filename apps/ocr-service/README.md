# Personel OCR Service

Phase 2.8 — Screenshot text extraction with KVKK-compliant PII redaction.

## Purpose

Receives screenshot blobs from the Personel enricher and returns extracted
text.  Used to index screenshot content into OpenSearch for forensic search
and DLP coverage of screen-only leaks.

## KVKK Default-Off Invariant

OCR is **disabled by default** at the tenant level, following the same
opt-in ceremony pattern as DLP (ADR 0013).  The enricher only forwards
screenshots here when:

1. Module state for `ocr` is `enabled` (tenant DPO opted in).
2. The screenshot is NOT sensitive-flagged.
3. The source app is NOT in the tenant's OCR exclude list.

The response text is **always redacted** — TCKN, IBAN, and credit card
numbers are replaced with `[TCKN]`, `[IBAN]`, `[CREDIT_CARD]` before
JSON encoding.  This is a hard invariant enforced in `redaction.py`.

## Quick Start (Development)

```bash
# Install dependencies (without paddleocr to keep it light)
pip install -e ".[dev]"

# Run with Tesseract installed locally
PERSONEL_OCR_DEFAULT_ENGINE=tesseract uvicorn personel_ocr.main:app --reload

# Run tests (no Tesseract binary required — engines are mocked)
pytest tests/
```

## API

```
POST /v1/extract         Extract text from a single screenshot (base64)
POST /v1/extract/batch   Extract text from multiple screenshots
GET  /healthz            Liveness probe
GET  /readyz             Readiness probe (ready | degraded)
GET  /metrics            Prometheus metrics
GET  /docs               Swagger UI
```

## Configuration

All settings use the `PERSONEL_OCR_` prefix:

| Variable | Default | Description |
|---|---|---|
| `PERSONEL_OCR_BIND_HOST` | `0.0.0.0` | Bind address |
| `PERSONEL_OCR_BIND_PORT` | `8080` | Bind port |
| `PERSONEL_OCR_DEFAULT_ENGINE` | `tesseract` | `tesseract` / `paddle` / `auto` |
| `PERSONEL_OCR_DEFAULT_LANGUAGES` | `tr,en` | Comma-separated ISO-639-1 codes |
| `PERSONEL_OCR_CONFIDENCE_THRESHOLD` | `0.30` | Drop words below this confidence |
| `PERSONEL_OCR_BATCH_MAX_ITEMS` | `16` | Max screenshots per batch request |
| `PERSONEL_OCR_LOG_LEVEL` | `info` | `debug` / `info` / `warning` / `error` |
| `PERSONEL_OCR_PREPROCESS_GRAYSCALE` | `true` | Grayscale conversion |
| `PERSONEL_OCR_PREPROCESS_AUTOCONTRAST` | `true` | Auto-contrast |
| `PERSONEL_OCR_PREPROCESS_THRESHOLD` | `false` | Binary threshold |

## Docker Compose Integration

The service is off by default (profile: `ocr`):

```bash
# Start with OCR enabled
docker compose --profile ocr up -d ocr-service
```

The `module-state` API controls runtime enablement separately from container
presence.  Even when the container is running, the enricher checks
`GET /v1/system/module-state?module=ocr` before forwarding screenshots.

## Architecture

```
ExtractRequest
     │
     ▼
pipeline.py
  ├─ base64 decode
  ├─ preprocess.py  (grayscale, autocontrast, optional threshold)
  ├─ engines/tesseract.py or engines/paddle.py
  ├─ postprocess.py (confidence filter, unicode normalize, whitespace)
  └─ redaction.py   (TCKN, IBAN, credit card, phone, email)
     │
     ▼
ExtractResponse (text already redacted)
```

## PII Redaction Patterns

| Pattern | Tag | Algorithm |
|---|---|---|
| Turkish National ID (TCKN) | `[TCKN]` | 11-digit + official checksum |
| Turkish IBAN | `[IBAN]` | TR + 24 digits + ISO 13616 mod-97 |
| Credit card | `[CREDIT_CARD]` | 13-19 digits + Luhn |
| Turkish phone | `[PHONE]` | +90 / 0 prefix + 10 digits |
| Email | `[EMAIL]` | RFC 5322 simplified |

## File Layout

```
apps/ocr-service/
├── pyproject.toml
├── Dockerfile
├── README.md
├── docker-entrypoint.sh
├── src/personel_ocr/
│   ├── main.py         FastAPI app factory + lifespan
│   ├── config.py       pydantic-settings (PERSONEL_OCR_ prefix)
│   ├── routes.py       POST /v1/extract, /healthz, /readyz, /metrics
│   ├── schemas.py      pydantic v2 models
│   ├── pipeline.py     full pipeline coordinator
│   ├── preprocess.py   PIL image normalisation
│   ├── postprocess.py  confidence filter + whitespace cleanup
│   ├── redaction.py    PII pattern matching + redaction
│   ├── metrics.py      Prometheus counters/histograms
│   ├── logging.py      structlog JSON + request-id middleware
│   └── engines/
│       ├── base.py     OCREngine ABC
│       ├── tesseract.py  pytesseract wrapper (deferred import)
│       └── paddle.py   paddleocr wrapper (deferred import, optional)
├── tests/
│   ├── conftest.py
│   ├── test_redaction.py
│   ├── test_preprocess.py
│   └── test_routes.py
├── lang/README.md      Tesseract language pack installation guide
└── bench/README.md     Benchmark methodology
```
