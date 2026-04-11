# Personel Windows Endpoint Agent

Rust workspace for the Personel endpoint agent. Windows-only for Phase 1.

## Build (Windows, MSVC toolchain required)

```powershell
# Install the toolchain pin
rustup override set 1.75

# Build all crates (debug)
cargo build --workspace

# Build release binary
cargo build --release -p personel-agent

# Run tests (library crates only — safe on macOS/Linux)
cargo test --workspace --exclude personel-os
```

## Cross-check on macOS/Linux (library crates only)

```bash
# cargo check for cross-platform library crates
cargo check -p personel-core -p personel-crypto -p personel-queue \
            -p personel-policy -p personel-transport -p personel-proto
```

`personel-os` and `personel-agent` (main binary) require the Windows SDK
and will not `cargo check` on non-Windows. The stub module in `personel-os`
handles the non-Windows surface for all other crates.

## Workspace layout

| Crate | Status | Notes |
|-------|--------|-------|
| `personel-core` | Complete | Types, errors, IDs, clock |
| `personel-proto` | Complete | tonic-build codegen |
| `personel-crypto` | Complete | AES-GCM, X25519, HKDF, DPAPI |
| `personel-queue` | Complete | SQLCipher, enqueue/dequeue/ack/evict |
| `personel-policy` | Complete | Policy cache, glob eval, hot reload |
| `personel-collectors` | Trait complete; `idle` working; rest stub | |
| `personel-transport` | TLS + backoff complete; stream stub | |
| `personel-os` | Windows impls partial; stubs complete | unsafe here only |
| `personel-agent` | Service lifecycle wired; transport stub | |
| `personel-watchdog` | Process monitor loop working | |
| `personel-updater` | Manifest verify complete; apply stub | |
| `personel-livestream` | Interface only | LiveKit SDK TBD |

## Proto codegen

Proto files are read from `../../../../proto/personel/v1/` (monorepo root
relative to `apps/agent/`). On a fresh checkout run `cargo build -p personel-proto`
first to generate the bindings before building dependent crates.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PERSONEL_LOG` | `info` | tracing filter |
| `PERSONEL_DATA_DIR` | `C:\ProgramData\Personel\agent` | data root |
