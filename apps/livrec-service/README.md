# livrec-service — Live View Recording Service

**ADR 0019 — Phase 2.8 Implementation**

---

## KVKK Uyarısı (Zorunlu)

> **Canlı izleme kayıtları sadece DPO onayı + İK çift kontrol ile geri oynatılabilir. Kayıtlar MinIO'da LVMK ile şifrelenir; anahtarlar TMK'dan tamamen bağımsızdır. 30 günlük TTL; legal hold bayrağı ile uzatılabilir. ADR 0019 uyarınca varsayılan olarak devre dışıdır.**

---

## Overview

`livrec-service` receives encrypted WebM chunks from the LiveKit egress shim
during live view sessions, stores them in MinIO under a per-session AES-256-GCM
encrypted format, and exposes dual-control SSE playback.

Key properties from ADR 0019:

- **Per-session toggle**: recording is NOT enabled by default for any session.
  Admin must explicitly request recording AND HR must separately approve it.
- **Independent key hierarchy**: LVMK (Live View Master Key) in Vault transit,
  separate from TMK. Zero cross-contamination with keystroke crypto.
- **Dual-control playback**: same HR approval flow as session initiation.
- **30-day retention** with legal-hold override.
- **DPO-only forensic export** with chain-of-custody signed ZIP package.
- **No download button**: playback via SSE streaming only; browser decrypts
  with WebCrypto API (DEK delivered via SSE, held in memory only).

## Architecture

```
LiveKit Egress Shim
      |
      | POST /v1/record/chunk (internal bearer)
      v
livrec-service
  |-- upload/handler.go     — receives encrypted WebM chunks
  |-- upload/session.go     — RecordingSession state machine
  |-- crypto/lvmk.go        — LVMK derivation via Vault transit
  |-- crypto/envelope.go    — AES-256-GCM per-chunk encryption
  |-- storage/minio.go      — MinIO client (bucket: live-view-recordings)
  |-- playback/handler.go   — SSE stream (GET /v1/record/{id}/stream)
  |-- playback/approval_gate.go — dual-control check via Admin API
  |-- playback/dek_delivery.go  — one-time DEK unwrap + SSE delivery
  |-- retention/ttl.go      — daily TTL job
  |-- export/forensic.go    — DPO ZIP export
  |-- audit/recorder.go     — async forwarding to Admin API /v1/internal/audit/livrec
```

## API

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/record/chunk` | Ingest encrypted WebM chunk |
| GET | `/v1/record/{session_id}/stream` | SSE playback (dual-control gated) |
| POST | `/v1/record/{session_id}/export` | DPO-only forensic ZIP export |
| GET | `/healthz` | Liveness |
| GET | `/readyz` | Readiness |
| GET | `/metrics` | Prometheus |

## Key Hierarchy

```
Vault: transit/keys/lvmk-<tenant_id>   (LVMK — one per tenant)
  |
  +-> per-session DEK (HKDF derive, context = session_id || tenant_id || "lv-session-dek-v1")
        - held in livrec-service memory only during session
        - wrapped form stored in Postgres: live_view_recordings.dek_wrap
        - unwrapped for playback/export after dual-control approval
```

LVMK has NO relationship to TMK (`transit/keys/tenant/*/tmk`). The Vault policy
`infra/compose/vault/policies/livrec-service.hcl` enforces explicit deny on
all TMK paths.

## Enabling (Production)

livrec-service is off by default (`profiles: [livrec]` in docker-compose).

To enable:
1. DPO signs the opt-in form (to be created in Phase 3)
2. Run `infra/scripts/livrec-enable.sh` (to be authored in Phase 3):
   - Creates `transit/keys/lvmk-<tenant_id>` in Vault
   - Issues a single-use Secret ID for the `live-view-recorder` AppRole
   - Starts the container: `docker compose --profile livrec up -d livrec-service`
3. Verify LVMK isolation: `infra/scripts/vault-audit-check.sh livrec`

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `VAULT_ADDR` | Yes | Vault server address |
| `VAULT_ROLE_ID` | Yes | live-view-recorder AppRole role ID |
| `VAULT_SECRET_ID` | Yes | live-view-recorder AppRole secret ID |
| `VAULT_CACERT` | No | Path to Vault CA certificate |
| `VAULT_LVMK_PATH` | No | Defaults to `transit/derive/lvmk` |
| `MINIO_ENDPOINT` | Yes | MinIO endpoint |
| `MINIO_ACCESS_KEY` | Yes | MinIO access key (PutObject-only account) |
| `MINIO_SECRET_KEY` | Yes | MinIO secret key |
| `MINIO_BUCKET` | No | Defaults to `live-view-recordings` |
| `ADMIN_API_BASE_URL` | Yes | Base URL of Admin API |
| `ADMIN_API_INTERNAL_TOKEN` | Yes | Internal service-to-service bearer token |
| `LIVREC_RETENTION_DAYS` | No | Default 30 |
| `LOG_LEVEL` | No | Default `info` |

## Development Status (Phase 2.8)

**Fully implemented:**
- AES-256-GCM chunk encryption/decryption with proper random nonce generation
- LVMK Vault transit derive + wrap/unwrap DEK lifecycle
- MinIO client with bucket auto-creation
- Chunk upload handler with monotonic index enforcement
- RecordingSession state machine (waiting → recording → completed → archived)
- SSE playback handler with explicit flush after each chunk
- Dual-control approval gate (calls Admin API)
- One-time DEK delivery via SSE
- TTL scheduler with legal-hold skip
- DPO-only forensic export (ZIP with manifest + chain.json + signature.sig)
- Async audit forwarder (non-blocking; failures do not break recording)
- chi router, middleware, error helpers

**Scaffolded (Phase 3):**
- Postgres wiring (recording metadata persistence, session expiry queries)
- Real legal_hold query (`PostgresLegalHoldChecker`)
- `infra/scripts/livrec-enable.sh` opt-in ceremony
- Integration test bodies (structure present, t.Skip)
- `pubkey.pem` retrieval for offline signature verification

## Open Questions for Phase 3 Browser-Side WebCrypto Implementor

1. **DEK format in SSE**: The `event: dek` data field carries the base64-encoded
   plaintext DEK. The browser must import it as a raw AES-256-GCM key via
   `crypto.subtle.importKey("raw", ...)`. Confirm the browser can do this before
   the SSE connection is fully open (first event buffering risk).

2. **Nonce extraction**: Each chunk stored on disk is `[12-byte nonce][ciphertext+tag]`.
   The browser receives the base64-encoded wire format. It must extract the first
   16 bytes as nonce (note: 12 bytes nonce + GCM appends 16-byte tag, but Go's
   `gcm.Seal` fuses tag into ciphertext — so wire format is `[12][ciphertext||tag]`).
   Confirm the JS WebCrypto decrypt call uses `{name: "AES-GCM", iv: nonce}` with
   the correct 12-byte IV slice.

3. **AAD for authentication**: Each chunk is encrypted with AAD =
   `"livrec-chunk:" + session_id + ":" + big-endian uint64 chunk_index`.
   The browser WebCrypto `AES-GCM` decrypt call must supply the same AAD or
   decryption will fail with `DOMException: OperationError`. The SSE chunk event
   data carries `{chunk_index} {base64_ciphertext}` — the browser must reconstruct
   the AAD from the session_id (available from the page URL or a page-load API call)
   and the chunk_index from the SSE event.

4. **DEK lifecycle in the browser**: The DEK must be held only in a
   `CryptoKey` object (non-extractable if possible). On page unload
   (`beforeunload`/`visibilitychange`) the reference must be cleared. Confirm
   that `importKey(..., ["decrypt"])` without `"extractable": true` prevents
   the key from being read out via `exportKey`.

5. **Seek support**: Current SSE stream delivers all chunks in order from start
   to end. Seeking requires a protocol extension (e.g. a `?start_chunk=N` query
   param). ADR 0019 notes that 1 MiB chunks balance seek latency; the Phase 3
   protocol must define how the browser requests a partial stream.
