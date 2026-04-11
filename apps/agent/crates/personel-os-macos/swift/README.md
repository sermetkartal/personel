# Swift ES Helper — Phase 2.4 Placeholder

This directory will contain the Swift source for the **Personel ES Helper** System Extension
process that bridges Endpoint Security Framework events to the Rust agent core.

## Why a Swift shim?

Rust cannot directly link `EndpointSecurity.framework` at this time:
- Existing unofficial FFI bindings lag the ES API revision cycle.
- The Swift shim is a small, stable boundary (~2 k LOC estimated).
- The architectural decision is recorded in **ADR 0015** §"ES daemon written in Swift".

## Planned structure (Phase 2.4)

```
swift/
├── PersonelESHelper/
│   ├── PersonelESHelper.swift       — System Extension lifecycle entry point
│   ├── ESClient.swift               — ESClient subscription + event loop
│   ├── EventSerializer.swift        — Protobuf / Cap'n Proto frame serialiser
│   ├── UDSServer.swift              — UNIX domain socket writer
│   └── Info.plist                   — NSExtensionPointIdentifier = com.apple.endpoint-security
├── PersonelESHelper.entitlements    — com.apple.developer.endpoint-security.client = true
└── Package.swift                    — Swift Package Manager manifest
```

## Event subscription set (ADR 0015)

Subscribe to these NOTIFY events only (no AUTH events — no blocking decisions):

| ES Event Type                        | Collector use         |
|--------------------------------------|-----------------------|
| `ES_EVENT_TYPE_NOTIFY_EXEC`          | Process start         |
| `ES_EVENT_TYPE_NOTIFY_EXIT`          | Process end           |
| `ES_EVENT_TYPE_NOTIFY_FORK`          | Process fork          |
| `ES_EVENT_TYPE_NOTIFY_OPEN`          | File access           |
| `ES_EVENT_TYPE_NOTIFY_CLOSE`         | File write-close      |
| `ES_EVENT_TYPE_NOTIFY_RENAME`        | File rename           |
| `ES_EVENT_TYPE_NOTIFY_UNLINK`        | File delete           |
| `ES_EVENT_TYPE_NOTIFY_WRITE`         | File write            |
| `ES_EVENT_TYPE_NOTIFY_CREATE`        | File create           |
| `ES_EVENT_TYPE_NOTIFY_MOUNT`         | Volume mount          |
| `ES_EVENT_TYPE_NOTIFY_UNMOUNT`       | Volume unmount        |
| `ES_EVENT_TYPE_NOTIFY_IOKIT_OPEN`    | USB device open       |
| `ES_EVENT_TYPE_NOTIFY_LOGIN`         | User login            |
| `ES_EVENT_TYPE_NOTIFY_LOGOUT`        | User logout           |
| `ES_EVENT_TYPE_NOTIFY_SCREENSHARING_ATTACH` | Screen sharing abuse detection |

## IPC channel

The helper writes length-prefixed protobuf frames to a UNIX domain socket at
`/var/run/personel-es.sock`. The Rust agent's `es_bridge::EsBridgeClient` reads
from this socket and deserialises into `EsEvent` structs.

Frame format (little-endian):

```
┌──────────────────┬────────────────────────────┐
│  u32 frame_len   │  protobuf EsEventProto ...  │
└──────────────────┴────────────────────────────┘
```

## Entitlement approval

The `com.apple.developer.endpoint-security.client` entitlement requires manual
approval from Apple. This application must be filed in **Phase 2 kickoff week**
(historical review time: 2–4 weeks per ADR 0015 §"Consequences / Risks").

## Build script integration

`personel-os-macos/build.rs` will invoke `swiftc` to compile this directory when:
- `CARGO_CFG_TARGET_OS == "macos"`
- The swift source tree has changed (`cargo:rerun-if-changed=swift/`)

The compiled helper binary is placed in the Xcode-less build output alongside
the Rust agent binary for `.app` bundle packaging.
