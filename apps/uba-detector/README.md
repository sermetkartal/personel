# UBA Detector — User Behavior Analytics Service

Personel Platform Phase 2.6 — Insider Threat Anomaly Detection

## Overview

The UBA Detector is an advisory-only anomaly scoring service. It reads event
time series from ClickHouse materialized views, computes per-user anomaly
scores using Isolation Forest (Phase 2.6) and (Phase 2.7, deferred) LSTM, and
exposes a REST API for the Admin Console and DPO workflows.

**KVKK m.11/g — Critical Constraint:** All outputs are ADVISORY ONLY. This
service NEVER triggers automated disciplinary action or access restriction.
Scores are presented to human reviewers (DPO / Investigator) who make all
final decisions. Every API response includes a mandatory Turkish disclaimer.

## Architecture

```
ClickHouse (read-only)
  └── 5 materialized views (user_hourly_activity, user_app_diversity,
      user_off_hours_ratio, user_data_egress, user_policy_violations)
          │
          ▼
    features.py  ─── 7 features extracted from DataFrames
          │
          ▼
    model.py     ─── IsolationForestDetector (sklearn wrapper)
          │
          ▼
    scoring.py   ─── Anomaly score + contributing features + tier
          │
          ▼
    routes.py    ─── FastAPI REST API (read from uba_scores in ClickHouse)
```

## Features (Phase 2.6 — 7 features)

| # | Feature | Description |
|---|---------|-------------|
| 1 | off_hours_activity | Ratio of events outside Turkish business hours (UTC+3 Mon-Fri 08:00-18:00) |
| 2 | app_diversity | Count of distinct applications used in window |
| 3 | data_egress_volume | Bytes: file writes + clipboard content + network outbound |
| 4 | screenshot_rate | Screenshots per hour averaged |
| 5 | file_access_rate | File read events per hour |
| 6 | policy_violation_count | App/web block events + DLP matches |
| 7 | new_host_ratio | Fraction of network hosts not seen in prior 30 days |

## Risk Tiers

| Tier | Score Range | Action |
|------|-------------|--------|
| normal | < 0.30 | No action required |
| watch | 0.30 – 0.70 | DPO may review |
| investigate | > 0.70 | DPO review recommended (advisory) |

Scores > 0.9 trigger an advisory notification to the DPO audit feed.

## API Endpoints

```
GET  /v1/uba/users/top-anomalous?limit=20&window=7d
GET  /v1/uba/users/{user_id}/score?window=7d
GET  /v1/uba/users/{user_id}/timeline?days=30
POST /v1/uba/recompute?user_id=X   (DPO-only; audited upstream)
GET  /healthz
GET  /readyz
GET  /metrics
```

## KVKK Disclaimer (required in every response)

> Bu skor yalnızca inceleme önceliklendirmesi için üretilmiştir. Otomatik
> disiplin veya erişim kısıtlaması tetiklemez.

## Development

```bash
# Install
pip install -e ".[dev]"

# Run tests
pytest

# Lint
ruff check src/ tests/

# Start dev server
uvicorn personel_uba.main:app --reload --port 8080
```

## Docker

```bash
# Build
docker build -t personel/uba-detector:latest .

# Run with uba profile (off by default)
cd infra/compose
docker compose --profile uba up -d uba-detector
```

## ClickHouse Materialized Views

Run migration once before service start:

```bash
clickhouse-client --queries-file migrations/0001_uba_materialized_views.sql
```

DDL for individual views is in `materialized_views/`.

## Phase 2.7 Deferred

- LSTM temporal anomaly detection (placeholder class in `model.py`)
- Real customer baseline training pipeline
- ClickHouse write role for `uba_scores` table (needs DBA provisioning)
- Audit recorder integration from Python (needs backend team API endpoint)
