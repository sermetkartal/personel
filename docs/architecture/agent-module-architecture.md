# Endpoint Agent — Internal Module Architecture

> Language: English. Target: rust-engineer specialist agent.

## Runtime and Crate Choices

- **Runtime**: `tokio` multi-threaded. Rationale: best async ecosystem, good Windows I/O support, strong gRPC via `tonic`.
- **gRPC**: `tonic` with `rustls` (no OpenSSL).
- **TLS**: `rustls` + `rustls-native-certs` for trust store interop; custom pin verifier.
- **Local queue DB**: `rusqlite` + `r2d2` pool or `sqlx` with sqlite — decision deferred to rust-engineer; **SQLite is fixed**.
- **Serialization**: `prost` for wire proto, `serde` for local-only config.
- **Crypto**: `ring` for AES-GCM / HKDF; `x25519-dalek` for enrollment sealing.
- **Windows APIs**: `windows` crate (official Microsoft bindings).
- **ETW**: `ferrisetw` or direct `windows::Win32::System::Diagnostics::Etw` bindings.
- **Capture**: DXGI desktop duplication via `windows-capture` crate.
- **Logging**: `tracing` + `tracing-subscriber`; never logs PII; rotating file sink.
- **Anti-tamper zeroization**: `zeroize`.

## Process Topology

Two Windows services:

1. **`personel-agent`** (`LocalSystem`) — main collector and uploader.
2. **`personel-agent-watchdog`** (`LocalSystem`) — tiny process whose only job is to supervise and restart the main agent, detect tampering, and self-heal. See `docs/security/anti-tamper.md`.

The two processes communicate over a named pipe protected by a per-boot key.

## Main Agent Module Tree

```
personel-agent/
├── bin/
│   └── service_main.rs              // Windows service entry; installs crash handler
├── src/
│   ├── lib.rs
│   ├── bootstrap/                   // Enrollment, cert load, key load
│   ├── config/                      // Merged: baked defaults + local file + pushed policy
│   ├── runtime/                     // tokio runtime setup, panic capture
│   ├── ipc/                         // Named pipe to watchdog; command surface
│   ├── transport/
│   │   ├── grpc_client.rs           // tonic stream client, reconnect, backoff
│   │   ├── pin_verifier.rs          // Custom SPKI pinning
│   │   └── message_router.rs        // ServerMessage dispatcher
│   ├── queue/
│   │   ├── sqlite.rs                // Encrypted SQLite (SQLCipher or page-level AES)
│   │   ├── schema.rs                // See below
│   │   └── uploader.rs              // Batch, ack, retention in-queue
│   ├── collectors/                  // Each implements the Collector trait
│   │   ├── mod.rs                   // trait + registry + lifecycle
│   │   ├── process_etw.rs
│   │   ├── window_focus.rs
│   │   ├── screenshot.rs
│   │   ├── screenclip.rs
│   │   ├── file_etw.rs
│   │   ├── clipboard.rs
│   │   ├── print.rs
│   │   ├── usb_wmi.rs
│   │   ├── network_wfp.rs
│   │   ├── keystroke_meta.rs
│   │   ├── keystroke_content.rs     // encrypts at source via PE-DEK
│   │   └── idle.rs
│   ├── policy/
│   │   ├── engine.rs                // Evaluates rules (blocklists, intervals, flags)
│   │   ├── enforcers/
│   │   │   ├── app_block.rs
│   │   │   ├── web_block.rs
│   │   │   └── usb_block.rs
│   │   ├── sensitivity_guard.rs     // KVKK m.6: exclude_apps, title regex, host globs, sensitive-flag router
│   │   └── apply.rs                 // Atomic policy swap
│   ├── live_view/
│   │   ├── control.rs               // Receives signed start/stop
│   │   ├── capture_pipeline.rs      // DXGI → encode → LiveKit publish
│   │   └── token_verifier.rs
│   ├── updater/
│   │   ├── pull.rs                  // Fetch signed artifact
│   │   ├── verify.rs                // Signature + hash
│   │   └── apply.rs                 // Staged restart via watchdog
│   ├── crypto/
│   │   ├── pe_dek.rs                // PE-DEK load, zeroize, AES-GCM
│   │   └── sealing.rs               // DPAPI + TPM wrappers
│   └── telemetry/
│       ├── metrics.rs               // Self metrics (CPU, RAM, queue depth)
│       └── health.rs                // Health report to server
```

## Collector Trait

```rust
// Conceptual — do not paste as implementation.
#[async_trait]
pub trait Collector: Send + Sync {
    fn name(&self) -> &'static str;
    fn event_types(&self) -> &'static [&'static str];
    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle>;
    async fn reload_policy(&self, policy: &PolicyBundle) -> Result<()>;
    async fn stop(&self) -> Result<()>;
    fn health(&self) -> HealthSnapshot;
}

pub struct CollectorCtx {
    pub queue: Arc<QueueWriter>,
    pub clock: Arc<dyn Clock>,
    pub pe_dek: Option<Arc<SecretKey>>, // only for keystroke_content
    pub policy: Arc<PolicyView>,
    pub tenant_id: Uuid,
    pub endpoint_id: Uuid,
}
```

Constraints:
- A collector MUST NOT call the transport directly. It enqueues into the local queue only.
- A collector MUST respect a global backpressure signal (queue nearly full → drop-lowest-priority sampling for its class).
- A collector MUST zeroize any intermediate buffers carrying plaintext sensitive content.

## Local SQLite Queue Schema

SQLite database is encrypted (SQLCipher preferred; fallback: agent-managed AES-GCM page encryption). WAL mode. Size cap enforced.

```sql
CREATE TABLE event_queue (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type   TEXT NOT NULL,
    priority     INTEGER NOT NULL,       -- 0=critical (tamper), 1=high, 2=normal, 3=low
    occurred_at  INTEGER NOT NULL,       -- unix nanos
    enqueued_at  INTEGER NOT NULL,
    payload_pb   BLOB NOT NULL,          -- prost-encoded events.v1.Event
    size_bytes   INTEGER NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    batch_id     INTEGER,                -- set when picked up by uploader
    status       INTEGER NOT NULL        -- 0=pending, 1=in_flight, 2=acked
);
CREATE INDEX idx_queue_status_priority ON event_queue(status, priority, id);
CREATE INDEX idx_queue_batch ON event_queue(batch_id);

CREATE TABLE blob_queue (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT NOT NULL,          -- screenshot, screenclip, keystroke_content, clipboard_content
    local_path   TEXT NOT NULL,          -- sealed file on disk
    size_bytes   INTEGER NOT NULL,
    sha256       BLOB NOT NULL,
    linked_event_id INTEGER,
    status       INTEGER NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    enqueued_at  INTEGER NOT NULL
);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value BLOB
);  -- stores watermarks, last seq, etc.
```

Eviction policy when size cap hit:
1. Drop low-priority events first (status=pending, priority=3).
2. Then drop oldest normal events.
3. Never drop priority=0 (tamper) or `live_view.*` events.
4. Dropping emits an internal `queue.evicted` event.

## IPC: Main ↔ Watchdog

Named pipe `\\.\pipe\personel-agent-ipc`. Length-prefixed proto frames. Message set:

- `Heartbeat { seq, ts, self_metrics }`
- `RequestRestart { reason }`
- `UpdateReady { artifact_path, signature }`
- `TamperAlert { check, severity }`
- `ShutdownAck {}`

If watchdog misses 3 heartbeats (15 s), it force-kills and restarts main. If main requests update, watchdog performs the swap+restart.

## Policy Engine

- Policy bundle is a protobuf message (`proto/personel/v1/policy.proto`) signed by the policy-signing key.
- Agent verifies signature before applying.
- Apply is atomic: new bundle is written to `policy.new`, validated, fsync'd, then renamed over `policy.active`; collectors are notified via `reload_policy()`.
- Rollback: previous bundle retained as `policy.prev` for one cycle.

### Sensitivity Guard (KVKK m.6)

The `sensitivity_guard` module implements the `SensitivityGuard` policy fields (added Phase 0 revision round — Gap 1). It operates as a cross-collector decision point:

1. **`exclude_apps` (suppression)** — consulted by `screenshot` and `screenclip` collectors before capture. If the foreground exe glob-matches the list, capture is suppressed entirely and a `screenshot.suppressed_by_sensitivity` metric is incremented (no event is emitted to avoid leaking the fact that a suppressed capture was taken).
2. **`window_title_sensitive_regex` (flagging)** — consulted by `window_focus` and by `keystroke_meta`/`keystroke_content` when emitting events for a window whose title matches. Matched events get a `sensitive = true` tag in the protobuf metadata.
3. **`sensitive_host_globs` (flagging)** — cross-referenced by `window_focus` when the active browser window's host (best-effort parsed from title or from a cached last-DNS-query for the foreground pid) matches. Tagged events are routed to the sensitive bucket by the ingest pipeline.
4. **`sensitive_retention_days_override`** — informational only on the agent; the server-side retention job enforces it.
5. **`auto_flag_on_m6_dlp_match`** — pure server-side (DLP service reads the policy and applies the flag when emitting `dlp.match`); the agent does not evaluate this field.

The guard runs in O(1) for exe globs (AhoCorasick over lowercased paths) and O(k) for title regexes, where k is kept small (≤ 32 regexes) by policy validation at push time.

## Key Version Handshake (reference)

On stream open the agent populates `AgentMessage.Hello.pe_dek_version` and `AgentMessage.Hello.tmk_version` from the local sealed key blob. If the gateway refuses the stream with a `RotateCert` reason of `rekey`, the bootstrap module runs the re-enrollment flow. Full protocol in `docs/architecture/key-hierarchy.md` §Key Version Handshake.

## Updater

1. Periodic poll (12 h default; policy-driven) or on-stream `update.notify` message.
2. Fetch signed manifest from Update Service over HTTPS (mTLS using agent cert).
3. Verify Ed25519 signature against baked-in root signing key.
4. Download artifact (resumable).
5. Verify manifest hash + subresource hashes.
6. Stage to `update/pending/`.
7. Tell watchdog; watchdog stops main, swaps binary, starts main, reports success back over IPC.
8. Main pings server `agent.update_installed`; on N minutes without health, server triggers rollback.

## Anti-Tamper Hooks

See `docs/security/anti-tamper.md`. Agent exposes:
- Periodic self-hash check (PE image integrity).
- Registry ACL monitor.
- Service state monitor.
- Debugger detection loop (never blocking critical paths).

## Self-Metrics

Sampled every 30 s; emitted as `agent.health_heartbeat`:
- CPU% (process), RSS, queue depth, blob queue depth, drops since last report, upload RTT p95, last successful upload, policy version, updater state.

## Build Matrix

- `x86_64-pc-windows-msvc` — primary
- `aarch64-pc-windows-msvc` — best effort, Phase 2 for ARM Windows laptops

No cross-compile from macOS/Linux for release builds; CI uses Windows runners with MSVC.

## Extension Point for Phase 3 (Minifilter Driver)

A `collectors::kernel_bridge` module stub is reserved. It will connect to the future minifilter via `FilterConnectCommunicationPort`. Until then, the module returns `NotImplemented` and no code paths depend on it. File system collector `file_etw.rs` remains the Phase 1 implementation and will become a fallback in Phase 3.
