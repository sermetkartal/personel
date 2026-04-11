# personel-ml — ML Category Classifier

Phase 2.3 service. Classifies employee app/URL activity into `work | personal | distraction | unknown` using a local Llama 3.2 3B Instruct model via llama.cpp. No cloud egress.

Architecture decision: ADR 0017.

## Quick start (development, no GPU)

```sh
cd apps/ml-classifier

# Install with uv (recommended)
uv sync --all-groups

# Or with pip
pip install -e ".[dev]"

# Run tests (no model file needed — FallbackClassifier only)
pytest tests/

# Start the service (falls back to rule-based classifier if no model)
PERSONEL_ML_SKIP_MODEL_DOWNLOAD=true python -m personel_ml.main
```

Service listens on http://localhost:8080. Swagger UI at http://localhost:8080/docs.

## Configuration

All settings use the `PERSONEL_ML_` prefix.

| Variable | Default | Description |
|---|---|---|
| `PERSONEL_ML_BIND_HOST` | `0.0.0.0` | Bind address |
| `PERSONEL_ML_BIND_PORT` | `8080` | Bind port |
| `PERSONEL_ML_MODEL_PATH` | `/models/llama-3.2-3b.Q4_K_M.gguf` | GGUF model file path |
| `PERSONEL_ML_MODEL_VERSION` | `llama-3.2-3b-q4_k_m` | Version tag in API responses |
| `PERSONEL_ML_N_THREADS` | `0` (auto) | llama.cpp CPU threads |
| `PERSONEL_ML_N_CTX` | `2048` | Context window tokens |
| `PERSONEL_ML_N_GPU_LAYERS` | `0` | GPU layers (0 = CPU only) |
| `PERSONEL_ML_CONFIDENCE_THRESHOLD` | `0.70` | Below this → `unknown` |
| `PERSONEL_ML_LOG_LEVEL` | `info` | Structlog level |
| `PERSONEL_ML_SKIP_MODEL_DOWNLOAD` | `false` | Skip model download on first start |

## API

### POST /v1/classify

```json
// Request
{"app_name": "chrome.exe", "window_title": "Stack Overflow", "url": "stackoverflow.com"}

// Response
{"category": "work", "confidence": 0.92, "backend": "llama", "model_version": "llama-3.2-3b-q4_k_m"}
```

### POST /v1/classify/batch

```json
// Request
{"items": [{"app_name": "Excel"}, {"app_name": "YouTube", "url": "youtube.com"}]}

// Response
{"results": [...], "total": 2, "latency_ms": 87.3}
```

### GET /healthz

Always `{"status": "ok"}` if the process is alive.

### GET /readyz

```json
{"status": "ready", "backend": "llama", "model_loaded": true, "model_version": "llama-3.2-3b-q4_k_m"}
// or in degraded mode:
{"status": "degraded", "backend": "fallback", "model_loaded": true, "model_version": "fallback"}
```

### GET /metrics

Prometheus text format.

## Fallback mode

If the GGUF model fails to load (missing file, OOM, corrupt), the service starts in degraded mode using the rule-based `FallbackClassifier`. Every response carries `"backend": "fallback"`. HTTP 200 is always returned — never 503. The `/readyz` endpoint reports `"status": "degraded"` so monitoring can alert on sustained fallback rates.

## KVKK note

This service only accepts `app_name`, `window_title`, and `url` (hostname only). It never receives keystroke content, OCR text, or screenshot data. This is enforced structurally: the `ClassifyItem` schema has no such fields.

## Docker

```sh
# Build
docker build -t personel/ml-classifier:0.1.0 .

# Run (CPU only, no model)
docker run --rm -p 8080:8080 \
  -e PERSONEL_ML_SKIP_MODEL_DOWNLOAD=true \
  personel/ml-classifier:0.1.0

# Run with pre-staged model volume
docker run --rm -p 8080:8080 \
  -v /var/lib/personel/ml-models:/models:ro \
  personel/ml-classifier:0.1.0
```

## Model

Default: Llama 3.2 3B Instruct, quantized AWQ 4-bit, GGUF q4_k_m format.

On first container start, `docker-entrypoint.sh` checks whether the model file
is present in the `/models` volume. If absent, it downloads from Hugging Face
(~2 GB). For air-gapped deployments, pre-stage the file via `install.sh` using
the signed tar.zst bundle.

Alternative models (drop-in, config change only): Mistral 7B Instruct, Qwen 2.5 3B Instruct.

## Development

```sh
# Lint
ruff check src/ tests/
ruff format --check src/ tests/

# Type check (optional, no strict mode yet)
pyright src/

# Run only fast tests (no fixtures)
pytest tests/ -k "not test_all_fixture_examples"
```
