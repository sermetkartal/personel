# personel-os-linux

Linux OS abstraction layer for the Personel endpoint agent. This crate is the
Phase 2.1 scaffold introduced by ADR 0016 and mirrors the public surface of
`personel-os` (Windows) against Linux APIs.

## Scope

| Module | Linux mechanism | Phase 2.2 status |
|--------|----------------|------------------|
| `input` | `/dev/input/event*` via libinput | Stub |
| `capture` | X11 XShm + Wayland portal ScreenCast | Stub |
| `file_events` | fanotify (CAP_SYS_ADMIN) / inotify fallback | Stub |
| `ebpf::process` | `sched_process_exec/exit/fork` tracepoints | Stub |
| `ebpf::network` | `tcp_connect/close`, `udp_sendmsg` kprobes | Stub |
| `window_title` | X11 `_NET_ACTIVE_WINDOW` / Wayland (unavailable) | Stub |
| `systemd` | sd_notify, journal logger | Stub |
| `keystore` | libsecret / KWallet / file fallback | Stub |

**Phase 2.1 is a scaffold.** All functions return
`Err(AgentError::Unsupported { os: "linux", component: "…" })`. Real
implementations arrive in Phase 2.2.

## Cross-platform compilation

The crate compiles on Linux, macOS, and Windows:

- On `target_os = "linux"`: the real module tree is compiled. All functions
  return `Unsupported` in Phase 2.1, with doc comments describing the intended
  implementation.
- On other platforms: `src/stub/mod.rs` provides the same public API. This
  allows `cargo check -p personel-os-linux` to pass on macOS developer
  machines and Windows CI runners without any `#[cfg]` branching at the call
  site.

## X11 vs Wayland

### Window title

| Session | Availability | Reason |
|---------|-------------|--------|
| X11 | Phase 2.2 | `_NET_ACTIVE_WINDOW` + `_NET_WM_NAME` via `x11rb` |
| Wayland | Permanently unavailable | Wayland security model |

The Wayland compositor architecture deliberately prevents any process from
reading another process's window title. `WaylandAdapter::active_window()`
always returns `Err(AgentError::Unsupported { os: "wayland", component: "window_title" })`.
This is not a Phase 2.1 limitation — it is the correct long-term behaviour.

Customers who require window title collection must use X11 sessions (the
majority of enterprise Linux fleets as of 2026). This is documented in the
admin console as a capability flag and framed as a compliance improvement in
KVKK documentation (Wayland users enjoy stronger OS-level keystroke privacy).

### Screen capture

| Session | Mechanism | User consent |
|---------|-----------|-------------|
| X11 | XShm / `XGetImage` fallback | None (background, below compositor) |
| Wayland | `org.freedesktop.portal.ScreenCast` | Per-session dialog (can be persisted) |

## fanotify and `CAP_SYS_ADMIN`

`fanotify` mount-mark mode requires `CAP_SYS_ADMIN`. The agent is not run as
root; the systemd unit grants this capability narrowly:

```ini
[Service]
AmbientCapabilities=CAP_SYS_ADMIN
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_ADMIN
NoNewPrivileges=yes
```

If the capability is absent, `file_events::check_fanotify_capability()` returns
`CapabilityStatus::Absent` and the collector degrades to inotify mode. The
admin console surfaces this as a degraded endpoint capability.

## eBPF / CO-RE

BPF C source files live under `bpf/` (empty in Phase 2.1; see `bpf/README.md`).
`build.rs` detects `clang` and `bpftool` in `PATH` and emits a warning if
either is absent. In Phase 2.2 it will invoke `libbpf-cargo::SkeletonBuilder`.

Minimum supported kernel versions: Ubuntu 22.04 (5.15), RHEL 9 (5.14),
Debian 12 (6.1), Fedora 37 (6.x). See ADR 0016 §Consequences.

## Feature flags

| Flag | Effect |
|------|--------|
| `x11` | Enables `x11rb` dependency for X11 adapter (Linux only) |
| `wayland` | Enables `wayland-client` for Wayland adapter (Linux only) |

Neither flag is enabled by default; Phase 2.2 will set the appropriate default
based on build target.

## Phase 2.2 roadmap

1. Implement `input` via `libinput` path context + tokio `AsyncFd` poll loop.
2. Implement `window_title::X11Adapter` with `x11rb` `_NET_ACTIVE_WINDOW`.
3. Implement `capture::X11Adapter` with MIT-SHM extension.
4. Implement `capture::WaylandAdapter` with `xdg-portal` `ScreenCast`.
5. Add `bpf/process.bpf.c` and `bpf/network.bpf.c`; wire up `build.rs`
   skeleton builder; replace `ProcessCollector::load` / `NetworkCollector::load`
   stub bodies.
6. Implement `file_events` fanotify path; inotify fallback.
7. Implement `systemd` `sd_notify` socket protocol.
8. Implement `keystore` via `secret-service` crate.
