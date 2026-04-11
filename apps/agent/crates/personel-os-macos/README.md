# personel-os-macos

macOS OS-abstraction layer for the Personel endpoint agent. This crate is the macOS counterpart
to `personel-os` (Windows) and provides the same collector API surface backed by Apple system
frameworks.

## Status

**Phase 2.1 scaffold** — type-correct stubs. All public APIs compile on macOS, Linux, and Windows.
Real implementations follow in Phases 2.2–2.4.

## Module map

| Module               | macOS framework                              | Phase 2.1 | Full impl |
|----------------------|----------------------------------------------|-----------|-----------|
| `input`              | IOHIDManager / NSWorkspace                   | stub      | 2.3       |
| `capture`            | ScreenCaptureKit                             | stub      | 2.3       |
| `file_events`        | FSEvents + ES Framework (via es_bridge)      | stub      | 2.3/2.4   |
| `network_extension`  | Network Extension (NEFilterDataProvider)     | stub      | 2.4       |
| `tcc`                | IOKit / AXIsProcessTrusted / Security.fw     | stub      | 2.2       |
| `es_bridge`          | UDS bridge to Swift ES helper process        | stub      | 2.4       |
| `service`            | launchd plist generation + SIGTERM bridge    | partial   | 2.2       |
| `keystore`           | Keychain (Security.framework SecItem)        | partial   | 2.2       |

`service::generate_launch_daemon_plist` and all type declarations are fully
implemented in Phase 2.1 regardless of platform.

## Cross-platform compilation

The crate compiles cleanly on macOS, Linux, and Windows:

```bash
# Linux / macOS
cargo check -p personel-os-macos

# Windows cross-check (from Linux/macOS host — types only, no link)
cargo check -p personel-os-macos --target x86_64-pc-windows-msvc
```

On non-macOS platforms every API call returns
`Err(AgentError::Unsupported { os: "<os>", component: "..." })`.
The `stub/mod.rs` module is the non-macOS implementation.

## macOS-gated dependencies

```toml
[target.'cfg(target_os = "macos")'.dependencies]
cocoa             = "0.25"
core-foundation   = "0.9"
security-framework = "2"
objc              = "0.2"
```

These are not compiled on Linux or Windows.

## Safety policy

This crate is the only Phase 2 crate allowed to use `unsafe` code (Objective-C /
CoreFoundation FFI). Every `unsafe` block carries a `// SAFETY:` comment. All
other Phase 2 crates declare `#![deny(unsafe_code)]`.

## Phase roadmap

| Phase | What changes                                                                 |
|-------|------------------------------------------------------------------------------|
| 2.1   | This crate — scaffold, stubs, `service` plist generator, `keystore` types.   |
| 2.2   | Wire into `personel-agent` via OS facade; real `tcc`, `service` (SIGTERM),   |
|       | `keystore` (Keychain round-trip) validated on macOS CI.                      |
| 2.3   | Real `input` (IOHIDManager), `capture` (ScreenCaptureKit), `file_events`     |
|       | (FSEvents path). TCC permission check + degradation reporting.               |
| 2.4   | Swift ES helper process (`swift/PersonelESHelper/`); real `es_bridge` UDS    |
|       | framing; real `network_extension` (NEFilterDataProvider System Extension).   |

## Open questions for Phase 2.2 integrator

See also the inline `TODO` comments in each module.

1. **Keychain item accessibility attribute**: `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
   is used for the PE-DEK. Confirm this matches the key-hierarchy threat model in
   `docs/architecture/key-hierarchy.md` before Phase 2.2 ratification.

2. **`is_launchd_context` heuristic**: `getppid() == 1` works on macOS but is technically
   not a documented API contract. Consider whether the `--service` CLI flag (mirroring
   the Windows `personel-os` approach) is more robust for Phase 2.2.

3. **`capture.rs` non-macOS cfg branch**: the `#[cfg(not(target_os = "macos"))]` block in
   `ScCapture::capture_frame` uses `std::env!("CARGO_CFG_TARGET_OS")` which is a build-time
   constant, not a runtime value. Verify the error message is clear enough for
   the Phase 2.2 integrator; replace with the `stub` dispatch pattern if preferred.

4. **`security_framework::passwords` API surface**: `set_generic_password` /
   `get_generic_password` / `delete_generic_password` were confirmed present in
   `security-framework 2.x`. Verify the exact error code for `errSecItemNotFound`
   (`-25300`) is stable across macOS 13/14/15 before Phase 2.2.

5. **ES helper process identity / code signing**: the Swift ES helper requires the
   `com.apple.developer.endpoint-security.client` entitlement, which Apple must
   manually approve. File the application in Phase 2 kickoff week 1 (see ADR 0015
   §"Risks"). Approval takes 2–4 weeks historically.

6. **`TccStatus::Unknown` vs `Err`**: `tcc::check_permission` is designed to return
   `Ok(TccStatus::Unknown)` rather than `Err` when the OS returns an unexpected
   code, so the collector degrades gracefully. Confirm this design with the Phase 2.2
   collector integration author.
