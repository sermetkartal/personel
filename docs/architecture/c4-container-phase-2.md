# C4 Level 2 — Container Diagram (Phase 2)

> Language: English. Scope: Phase 2 on-prem deployment.
> Status: PLANNING — extends `c4-container.md` (Phase 1). Phase 2 containers are marked **NEW**; arrows to/from new containers are dashed in the Mermaid diagram.

## Containers (Phase 1 + Phase 2 delta)

Containers inherited unchanged from `c4-container.md` are listed here only by name. See the Phase 1 document for their full descriptions.

### Phase 1 containers (unchanged in Phase 2)

Endpoint Agent (Rust, now Windows + macOS + Linux), Ingest Gateway, Admin API, Admin Console, Transparency Portal, DLP Service (still off by default per ADR 0013), Policy Engine, Live View Broker (LiveKit SFU), Update Service, Audit Log Service, Event Bus (NATS JetStream), Time-Series Store (ClickHouse), Metadata Store (PostgreSQL 16), Object Store (MinIO), Search (OpenSearch), Secrets / PKI / KMS (Vault), Reverse Proxy (Caddy).

### Phase 2 NEW containers

| Container | Technology | Responsibility | Notes |
|---|---|---|---|
| **ml-classifier** | Go | Batches events, calls `llama-server` for category inference, writes enriched stream | New NATS subject `events.enriched.*`; can fall back to Phase 1 rule-based classifier on model outage. ADR 0017. |
| **llama-server** | llama.cpp (C++) | Local LLM inference (Llama 3.2 3B q4_k_m by default) | Sidecar to `ml-classifier`. **Network-isolated: `net_ml` Docker network has no default route, no egress** — enforced at install time by preflight. ADR 0017. |
| **ocr-service** | Go + Tesseract | Reads new screenshot events from NATS, extracts Turkish + English text, writes to `ocr_texts` Postgres table | Encrypted at rest (pgcrypto + Vault-wrapped tenant key). Off by default at tenant level (opt-in pattern mirroring ADR 0013). Phase 2 scope §B.3. |
| **hris-connector** | Go | Per-tenant HRIS sync: hourly poll + webhook receiver. Pluggable adapter registry (BambooHR, Logo Tiger in Phase 2 initial ship; others as Phase 2.5+). | No direct Vault TMK/LVMK access. Credentials in Vault KV only. ADR 0018. |
| **siem-exporter** | Go | Emits OCSF-schema events and audit entries to customer SIEM via webhook / Splunk HEC / Sentinel DCR / ECS | Outbound only, no inbound. Per-tenant config. Phase 2 scope §B.9. |
| **mobile-bff** | Go | Narrow on-call backend for React Native admin app. Sanitizes push notification payloads, holds APNs/FCM credentials, translates a subset of Admin API endpoints | Stateless. Separate Keycloak client. Phase 2 scope §B.8. |
| **uba-service** | Python (FastAPI + scikit-learn + PyTorch) | Isolation forest + LSTM on ClickHouse time series, emits anomaly flags, dispute ingestion | Runs as a **read-only ClickHouse consumer**; writes anomaly flags only to its own Postgres table. Phase 2 scope §B.5. |
| **live-view-recorder** | Go | Receives LiveKit egress stream, chunk-encrypts under session DEK derived from LVMK, writes to MinIO `live-view-recordings` bucket. | Holds derived DEKs in memory only for session duration. Has a **separate Vault AppRole** (`live-view-recorder`) distinct from all other AppRoles. ADR 0019. |

## Network segmentation

Phase 1 has one internal Docker network (`net_internal`) plus the gateway-facing `net_edge`. Phase 2 introduces two additional segments:

- **`net_ml`** — `ml-classifier` + `llama-server`. No default route. Egress blocked at `iptables` level by `preflight.sh`. Only one ingress: `ml-classifier` listens on a NATS-side interface for job intake. This guarantees that even if `llama-server` is compromised, it cannot exfiltrate data.
- **`net_hris`** — `hris-connector` only. Needs egress to HRIS providers on specific hostnames (BambooHR `*.bamboohr.com`, customer Logo Tiger base URL). Egress is restricted by hostname via a local egress proxy (Squid in a sidecar) with an allowlist. No wildcard egress.

`siem-exporter` also needs egress but uses the customer's existing SIEM path; it joins `net_internal` and relies on the customer's firewall policy for its SIEM egress.

## Mermaid — C4 Container (Phase 2)

```mermaid
flowchart LR
    subgraph Endpoint["Endpoint (Windows + macOS + Linux)"]
        Agent["Endpoint Agent (Rust)"]
    end

    subgraph Mobile["Admin On-Call"]
        MA["Mobile Admin App (RN/Expo)"]
    end

    subgraph Edge["DMZ / Edge"]
        RP["Reverse Proxy (Caddy)"]
        GW["Ingest Gateway"]
        LK["LiveKit SFU"]
        UP["Update Service"]
    end

    subgraph App["Application Layer"]
        API["Admin API"]
        POL["Policy Engine"]
        DLP["DLP Service (off by default)"]
        AUD["Audit Log Service"]
        CON["Admin Console"]
        POR["Transparency Portal"]
        OCR["ocr-service (NEW, off by default)"]:::new
        HRIS["hris-connector (NEW)"]:::new
        SIEM["siem-exporter (NEW)"]:::new
        BFF["mobile-bff (NEW)"]:::new
        UBA["uba-service (NEW)"]:::new
        LVR["live-view-recorder (NEW)"]:::new
    end

    subgraph MLNet["net_ml (isolated, no egress)"]
        MLC["ml-classifier (NEW)"]:::new
        LLM["llama-server (NEW)"]:::new
    end

    subgraph HRISNet["net_hris (egress allowlist only)"]
        HRISOut["HRIS egress proxy"]:::new
    end

    subgraph Data["Data Layer"]
        NATS[("NATS JetStream")]
        CH[("ClickHouse")]
        PG[("PostgreSQL")]
        MIN[("MinIO")]
        OS[("OpenSearch")]
        VAULT[("Vault / KMS")]
    end

    Agent -- mTLS gRPC --> GW
    Agent -- WebRTC --> LK
    Agent -- HTTPS --> UP

    GW --> NATS
    NATS --> CH
    NATS --> DLP
    NATS --> OS
    NATS --> AUD
    NATS -. events.to_classify .-> MLC:::newline
    NATS -. screenshots.new .-> OCR:::newline
    NATS -. audit + events .-> SIEM:::newline

    MLC -. localhost HTTP .-> LLM
    MLC -. events.enriched .-> NATS

    OCR --> PG
    OCR --> MIN

    UBA -. read .-> CH
    UBA --> PG

    API --> PG
    API --> CH
    API --> OS
    API --> NATS
    API --> AUD
    API --> VAULT
    API --> MIN
    API -. calls .-> HRIS

    HRIS --> PG
    HRIS --> VAULT
    HRIS -. egress allowlist .-> HRISOut

    SIEM -. customer firewall .-> Customer["Customer SIEM"]

    LK -. egress .-> LVR
    LVR --> MIN
    LVR --> VAULT
    LVR --> PG

    POL --> PG
    POL --> NATS
    DLP --> VAULT
    DLP --> MIN

    BFF --> API
    BFF -. APNs/FCM .-> Push["Apple/Google push"]
    MA -- HTTPS+biometric --> RP
    RP --> BFF

    CON --> RP
    POR --> RP
    RP --> API
    RP --> LK

    classDef new fill:#fff4c2,stroke:#b07c00,stroke-width:2px;
    classDef newline stroke-dasharray: 5 5;
```

## Arrow notes (Phase 2 delta only)

1. **NATS → ml-classifier** (dashed): new subject `events.to_classify.*` populated by the enricher when classification is enabled. The enricher strips sensitive fields before publishing; classification receives `(app_name, window_title, url, lang_hint)` only.
2. **NATS → ocr-service** (dashed): new subject `screenshots.new` for newly persisted screenshot objects. OCR consumes the object key, reads from MinIO, processes, writes text to Postgres.
3. **NATS → siem-exporter** (dashed): the exporter durable-subscribes to `events.enriched.*` and `audit.*`. It transforms to OCSF and pushes to the customer SIEM on an adapter-specific cadence.
4. **Admin API → hris-connector** (dashed): the API calls into the connector for on-demand sync triggers and for the `/v1/hris/status` read endpoint. The connector is authoritative for cron state.
5. **LiveKit → live-view-recorder**: LiveKit egress writes to the `live-view-recorder` container as a local destination when the session's `recording=true` flag is set. LVR encrypts chunks and uploads to MinIO.
6. **uba-service → ClickHouse (read)**: UBA has a read-only ClickHouse role. It never writes to ClickHouse; its outputs go to Postgres (`uba_flags`).
7. **mobile-bff → Apple/Google push**: push notifications with sanitized payloads (ticket IDs only, no PII). This is the only Phase 2 egress from the application layer to external cloud (ignoring `siem-exporter` customer SIEM and `hris-connector` allowlist egress).

## Placement rules (Phase 2 additions)

- `llama-server` **must** run in `net_ml` with no default gateway. Preflight fails installation if the network has egress.
- `live-view-recorder` has its own Vault AppRole `live-view-recorder`. No other container holds a Secret ID for it.
- `hris-connector` credentials live in Vault KV only (`secret/hris/<adapter>/<tenant>`). No `.env`, no mounted files, no in-process cache beyond a single operation.
- `mobile-bff` is the **only** container in the stack that initiates outbound HTTPS to consumer cloud services (Apple APNs, Google FCM). This is declared in the threat model update and monitored.
- `uba-service` is **read-only against ClickHouse**. Its ClickHouse role has `SELECT` only on specific event tables; no `INSERT/ALTER/DROP`.
- `ocr-service` inherits the DLP-like opt-in pattern: Compose profile `ocr` must be explicitly activated; the default stack does not include it.

## Data flows (Phase 2 additions, summary)

1. **ML classification**: Agent → Gateway → NATS `events.raw.*` → Enricher (strips sensitive) → NATS `events.to_classify.*` → ml-classifier → llama-server → ml-classifier → NATS `events.enriched.*` → ClickHouse writer.
2. **OCR**: screenshot capture → Gateway → NATS → MinIO writer → NATS `screenshots.new` → ocr-service → Tesseract → Postgres `ocr_texts` (encrypted).
3. **HRIS sync**: cron timer in hris-connector → adapter.ListEmployees / WatchChanges → hris-connector reconciles against Postgres users table → writes audit entries via Admin API gRPC → updates users, endpoints.
4. **SIEM export**: NATS `events.enriched.*` + `audit.*` → siem-exporter OCSF transform → customer SIEM endpoint (push or pull per adapter).
5. **Mobile alert**: backend event → mobile-bff → APNs/FCM with ticket ID → device wakes → biometric → fetches detail from mobile-bff (which proxies to Admin API).
6. **UBA flag**: nightly job reads ClickHouse last-24-hour window → isolation forest + LSTM → writes flags to Postgres `uba_flags` → Admin API surfaces in Investigator dashboard.
7. **Live view recording**: session start with `recording=true` → LiveKit egress → live-view-recorder → session DEK derived from LVMK → chunk-encrypt → MinIO upload → Postgres `live_view_recordings` row → audit chain `live_view.recording_started` + `live_view.recording_ended`.

---

*End of c4-container-phase-2.md v0.1*
