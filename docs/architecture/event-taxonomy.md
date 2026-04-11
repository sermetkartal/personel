# Event Taxonomy

> Language: English. Status: Phase 1 canonical list. Downstream agents must treat this file as the single source of truth for event names and classifications.

## Classification Key

- **Retention**: `hot` = ClickHouse hot tier (≤ 30 d), `warm` = warm tier (≤ 180 d), `cold` = object store archive (≤ retention max), `purge` = never long-term retained.
- **PII (KVKK)**:
  - `NONE` — no personal data
  - `IDENTIFIER` — technical identifier linkable to person (EndpointId, username)
  - `BEHAVIORAL` — personal behavioral data (app use, window titles)
  - `CONTENT` — content of communication/work product (file names, URLs, clipboard)
  - `SENSITIVE` — özel nitelikli / high-sensitivity (keystroke content, screen pixels)

## Events

| # | Event Name | Freq (per endpoint/day) | Size (avg) | Retention | PII |
|---|---|---|---|---|---|
| 1 | `process.start` | 300 | 280 B | hot→warm | BEHAVIORAL |
| 2 | `process.stop` | 300 | 220 B | hot→warm | BEHAVIORAL |
| 3 | `process.foreground_change` | 800 | 260 B | hot→warm | BEHAVIORAL |
| 4 | `window.title_changed` | 1 500 | 420 B | hot→warm | CONTENT |
| 5 | `window.focus_lost` | 800 | 180 B | hot | BEHAVIORAL |
| 6 | `session.idle_start` | 40 | 160 B | hot→warm | BEHAVIORAL |
| 7 | `session.idle_end` | 40 | 160 B | hot→warm | BEHAVIORAL |
| 8 | `session.lock` | 8 | 140 B | hot→warm | IDENTIFIER |
| 9 | `session.unlock` | 8 | 140 B | hot→warm | IDENTIFIER |
| 10 | `screenshot.captured` | 60 | 200 B (metadata; image in MinIO) | warm→cold | SENSITIVE |
| 11 | `screenclip.captured` | 4 | 260 B (metadata; video in MinIO) | warm→cold | SENSITIVE |
| 12 | `file.created` | 400 | 360 B | hot→warm | CONTENT |
| 13 | `file.read` | 1 200 | 340 B | hot | CONTENT |
| 14 | `file.written` | 600 | 360 B | hot→warm | CONTENT |
| 15 | `file.deleted` | 80 | 340 B | hot→warm | CONTENT |
| 16 | `file.renamed` | 60 | 440 B | hot→warm | CONTENT |
| 17 | `file.copied` | 40 | 420 B | hot→warm | CONTENT |
| 18 | `clipboard.metadata` | 200 | 200 B | hot→warm | BEHAVIORAL |
| 19 | `clipboard.content_encrypted` | 200 | 600 B avg (blob in MinIO) | cold | SENSITIVE |
| 20 | `print.job_submitted` | 20 | 320 B | hot→warm | CONTENT |
| 21 | `usb.device_attached` | 4 | 300 B | hot→warm | IDENTIFIER |
| 22 | `usb.device_removed` | 4 | 220 B | hot→warm | IDENTIFIER |
| 23 | `usb.mass_storage_policy_block` | 1 | 300 B | warm | IDENTIFIER |
| 24 | `network.flow_summary` | 3 000 | 280 B | hot | CONTENT |
| 25 | `network.dns_query` | 1 500 | 200 B | hot | CONTENT |
| 26 | `network.tls_sni` | 2 000 | 240 B | hot | CONTENT |
| 27 | `keystroke.window_stats` | 1 000 | 180 B (counts only) | hot→warm | BEHAVIORAL |
| 28 | `keystroke.content_encrypted` | 200 | 900 B (ciphertext blob) | cold | SENSITIVE |
| 29 | `app.blocked_by_policy` | 3 | 300 B | warm | BEHAVIORAL |
| 30 | `web.blocked_by_policy` | 5 | 320 B | warm | CONTENT |
| 31 | `agent.health_heartbeat` | 288 (5 min) | 220 B | hot | NONE |
| 32 | `agent.policy_applied` | 4 | 260 B | warm | NONE |
| 33 | `agent.update_installed` | 0.1 | 280 B | warm | NONE |
| 34 | `agent.tamper_detected` | 0.01 | 340 B | cold | IDENTIFIER |
| 35 | `live_view.started` | 0.05 | 260 B | cold (audit) | IDENTIFIER |
| 36 | `live_view.stopped` | 0.05 | 260 B | cold (audit) | IDENTIFIER |

**Distinct event types: 36** (exceeds the ≥25 requirement).

Per-endpoint aggregate raw volume estimate: ~4.6 MB/day uncompressed metadata (excluding screenshots/video/keystroke blobs which land in MinIO). At 10 000 endpoints this is ~46 GB/day metadata plus ~0.5–1 TB/day binary (screenshots/video depending on cadence and compression).

## JSON Schema Sketches

All events share an envelope:

```json
{
  "event_id": "ulid",
  "event_type": "window.title_changed",
  "schema_version": 1,
  "tenant_id": "uuid",
  "endpoint_id": "uuid",
  "user_sid": "S-1-5-21-...",
  "occurred_at": "RFC3339Nano",
  "received_at": "RFC3339Nano (server-stamped)",
  "agent_version": "1.0.3",
  "seq": 1234567,
  "payload": { }
}
```

### Selected payloads

`process.start`
```json
{
  "pid": 12345,
  "parent_pid": 1024,
  "image_path": "C:\\Program Files\\...\\chrome.exe",
  "image_sha256": "hex",
  "command_line_hash": "hex",
  "signer": "Google LLC",
  "integrity_level": "medium"
}
```

`window.title_changed`
```json
{
  "pid": 12345,
  "hwnd": 1180442,
  "title": "Monthly Report - Excel",
  "exe_name": "EXCEL.EXE",
  "duration_ms_in_previous": 18230
}
```

`file.written`
```json
{
  "path": "C:\\Users\\x\\Documents\\notes.docx",
  "pid": 2220,
  "bytes_delta": 4096,
  "sha256_after": "hex|null",
  "is_removable_target": false
}
```

`usb.device_attached`
```json
{
  "vid": "0x0781",
  "pid": "0x5581",
  "serial": "hex-hashed",
  "device_class": "mass_storage",
  "vendor_name": "SanDisk"
}
```

`network.flow_summary`
```json
{
  "pid": 5120,
  "exe_name": "chrome.exe",
  "dest_ip": "142.250.xx.xx",
  "dest_port": 443,
  "protocol": "tcp",
  "bytes_out": 12043,
  "bytes_in": 48112,
  "flow_start": "RFC3339Nano",
  "flow_end": "RFC3339Nano"
}
```

`keystroke.window_stats` (metadata only — counts, never content)
```json
{
  "hwnd": 1180442,
  "exe_name": "EXCEL.EXE",
  "keystroke_count": 482,
  "backspace_count": 14,
  "paste_count": 2,
  "window_duration_ms": 183400
}
```

`keystroke.content_encrypted`
```json
{
  "hwnd": 1180442,
  "exe_name": "EXCEL.EXE",
  "ciphertext_ref": "minio://keystroke-blobs/tenant/endpoint/2026/04/10/ulid.bin",
  "dek_wrap_ref": "vault://transit/keys/dlp-edk-v1",
  "nonce_b64": "...",
  "aad": { "endpoint_id": "...", "seq": 123 },
  "byte_len": 812
}
```

`screenshot.captured`
```json
{
  "blob_ref": "minio://screenshots/tenant/endpoint/2026/04/10/ulid.webp",
  "width": 2560,
  "height": 1440,
  "monitor_index": 0,
  "foreground_exe": "chrome.exe",
  "capture_reason": "interval|event|on_demand",
  "blur_applied": false,
  "sha256": "hex"
}
```

`live_view.started`
```json
{
  "session_id": "ulid",
  "requested_by": "user_id",
  "approved_by": "user_id_hr",
  "reason_code": "investigation_ticket_id",
  "livekit_room": "lv-<tenantid>-<ulid>",
  "audit_chain_head": "hex"
}
```

`agent.tamper_detected`
```json
{
  "check_name": "registry_key_acl|service_state|binary_hash|debugger",
  "severity": "low|medium|high",
  "details_hash": "hex"
}
```

## Retention Matrix Pointer

Concrete per-class retention periods and KVKK article references live in `data-retention-matrix.md`. Event authors must update both files in lock-step.
