# ADR 0016 — Linux Endpoint Agent Architecture

**Status**: Proposed for Phase 2. Not implemented.
**Deciders**: microservices-architect (Phase 2 planner); to be ratified by rust-engineer + security-engineer at Phase 2 kickoff.
**Related**: ADR 0002, ADR 0010, ADR 0015 (macOS sibling), ADR 0009/0013.

## Context

Phase 2 ships a Linux endpoint agent with feature parity to Windows (ADR 0010) and macOS (ADR 0015) for the Phase 1 collector set. Linux differs from both in key ways:

- No analogue to TCC — privilege is root/capability-based, not per-app consent.
- Multiple display servers (X11, Wayland) with fundamentally different screen capture models.
- Wide distribution fragmentation (kernel version, libc, init system, packaging).
- Explicit kernel-level instrumentation (eBPF) is first-class and does not require a kernel module if CO-RE is used.
- A strong cultural norm among Linux desktop users that agents should not be invasive; a visible kernel module destroys trust immediately.

**Explicit rejection: kernel module.** Personel will not ship any custom kernel module on Linux. Reason: (a) out-of-tree modules break across kernel versions (every 6–12 weeks on mainstream distros), (b) signature requirements for Secure Boot add a painful release pipeline, (c) a kernel module is a single upgrade away from a system-wide crash that costs customer trust. eBPF with CO-RE (Compile Once, Run Everywhere) gives us kernel-level instrumentation without shipping kernel code.

## Decision

### Core collector model: fanotify + eBPF

Use a **dual-instrumentation model**:

- **fanotify** for filesystem events in user-accessible mount points.
  - `FAN_OPEN`, `FAN_CLOSE_WRITE`, `FAN_MODIFY`, `FAN_MOVE_SELF`, `FAN_OPEN_EXEC`.
  - Requires **`CAP_SYS_ADMIN`** for mark types that cover the whole mount (`FAN_MARK_MOUNT`). Documented in install guide; the agent runs under a dedicated systemd unit with `AmbientCapabilities=CAP_SYS_ADMIN` narrowly, not root.
  - **Fallback to inotify** if `CAP_SYS_ADMIN` cannot be granted (e.g., certain hardened hosts): inotify covers `IN_OPEN/CLOSE/MODIFY/MOVED_FROM/MOVED_TO` but is inode-based, has watch-descriptor limits, and misses filesystems mounted after watch creation. This is a **degraded mode** and the admin console surfaces it as such. It is acceptable for Phase 2 launch but not the default.
- **eBPF (CO-RE compiled)** for process lifecycle and network.
  - Process: `sched_process_exec`, `sched_process_exit`, `sched_process_fork` tracepoints.
  - Network: `sock_ops` or `kprobe:tcp_connect` / `kprobe:tcp_close` for TCP flow summaries. For UDP, `kprobe:udp_sendmsg` / `udp_recvmsg` with aggregation (not per-packet) to stay within CPU budget.
  - DNS: parse from `sock_ops`-observed TCP/UDP payloads for port 53, or alternatively use `tcpdump`-style via the eBPF `XDP` layer at ingress. Phase 2 starts with socket-level parsing; XDP is Phase 3 if needed.
  - CO-RE via libbpf + BTF (kernel BTF required; supported on Ubuntu 22.04+, RHEL 9+, Debian 12+, Fedora 37+; for older kernels we ship **precompiled** BPF bytecode as a fallback but prefer CO-RE).
- **bpftrace** is used only as a development/calibration aid in QA, never shipped to customers.

The Rust agent loads eBPF programs via the `libbpf-rs` crate. This is a well-maintained crate and our preferred binding.

### Display: X11 vs Wayland

Different capture models.

**X11 (still dominant on enterprise Linux as of 2026).**
- Screen capture: `XGetImage` via `libxcb` or `XCB-shm` for efficient shared-memory capture. Proven, fast, ~1% CPU at 1Hz on a standard desktop.
- Window focus: `_NET_ACTIVE_WINDOW` property on the root window, observed via `XCB_PROPERTY_NOTIFY`.
- Window title: `_NET_WM_NAME` or `WM_NAME` on the focused window.
- Accessibility for keystroke content (only if DLP enabled): `XRecord` extension — intrusive and visible to any `xinput list` command, which is acceptable because DLP-enabled mode is an explicit ceremony.

**Wayland.**
- Fundamentally different: applications cannot read other applications' buffers by design. This is a privacy feature we respect.
- Screen capture requires the `org.freedesktop.portal.ScreenCast` D-Bus interface, which invokes a user-visible consent prompt **every session** by default (configurable to "remember" on GNOME / KDE). The user is in control.
- Window focus/title: no standard API on Wayland. GNOME Shell exposes an extension API; KDE has its own. We ship two adapters: `wayland-gnome` and `wayland-kde`, both installed as the user's login session supplementary processes.
- Keystroke metadata (counts): `libinput` via `/dev/input/event*` — works on both X11 and Wayland because it is below the display server.
- Keystroke content (DLP-enabled only): Wayland does not offer a global key event stream, by design. On Wayland, DLP-enabled mode is **unavailable** and the agent reports this capability as false. Customers who require DLP on Linux use X11 sessions. Documented as a KVKK-framing improvement (Wayland offers stronger OS-level keystroke privacy; if customer DPO wants to strengthen their m.6 story, recommend Wayland).

The agent detects display server via `WAYLAND_DISPLAY` / `DISPLAY` environment and the `loginctl show-session` output at startup, then loads the appropriate collector variant.

### Input: `/dev/input/event*` permission model

- The agent unit requires membership in the `input` group (or explicit `AmbientCapabilities=CAP_DAC_READ_SEARCH`). Installer adds `personel-agent` user to `input`.
- Raw input events are used for keystroke count collection and mouse activity (idle/active detection). No content.

### Clipboard and USB

- Clipboard: X11 `XCB_SELECTION_NOTIFY` for X11, Wayland `wl_data_device` via the `wayland-client` crate for Wayland. Metadata only by default (type + size + source app); content only if DLP enabled, per the Phase 1 contract.
- USB: `udev` monitor via the `libudev-rs` crate. Provides device insertion/removal events with vendor/product IDs and serial.

### Print jobs

- CUPS D-Bus notifications via `libcups`. Provides job submission events with metadata (user, printer, page count, document title).
- No content capture of print jobs; metadata only.

### Distribution format and packaging

- **DEB** for Debian 12, Ubuntu 22.04, 24.04 (`debhelper` based build).
- **RPM** for RHEL 9, Rocky 9, Alma 9, Fedora 38+.
- **Flatpak** is considered for Flatpak-first installations; rejected because Flatpak sandboxing conflicts with `CAP_SYS_ADMIN` and raw `/dev/input` access. Flatpak is not a viable packaging for a UAM agent.
- **Snap** is not supported. Snap's strict confinement blocks the capabilities we need, and classic confinement is deprecated.
- **AppImage** is not supported. Not a good fit for a service.
- Packages are signed with a GPG key; public key distributed via Personel's customer portal.
- Reproducible builds: same pipeline as Rust agent on Windows (deterministic Cargo build). Linux side benefits from `rustc` reproducibility guarantees more easily.

### systemd service model

- **`personel-agent.service`** — main agent process. `Type=notify` for readiness, `Restart=on-failure`, `AmbientCapabilities=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_ADMIN`, `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=read-only`, `PrivateTmp=yes`, `SystemCallFilter=@system-service`.
- **`personel-watchdog.service`** — sibling process, `Requires=personel-agent.service` with `Restart=always`. Same watchdog semantics as Windows ADR 0010.
- **`personel-agent.socket`** — UNIX domain socket for local IPC (e.g., admin CLI status queries).
- User session components (Wayland window/title collector) run as `systemd --user` units: `personel-session.service`.
- **No kernel module** and therefore no DKMS or kernel post-install hooks.

### Keystroke content encryption (DLP, off by default)

Same per-endpoint PE-DEK model as Phase 1 (ADR 0009). The encryption primitives and key storage (secret-service D-Bus interface for the user session, falling back to a file with `chmod 600` owned by `personel-agent`) are platform-independent from the Rust core's perspective.

### Enrollment and mTLS

Unchanged from Phase 1 design; the `personel-transport` crate is cross-platform and needs no Linux-specific work.

## Consequences

### Positive

- No kernel module — no DKMS, no Secure Boot signing, no upgrade breakage.
- eBPF CO-RE gives us kernel-level visibility that is portable across distros.
- fanotify + eBPF duality is a well-trodden path used by commercial Linux EDR products, so the primitives are battle-tested.
- Wayland-first consent model strengthens our privacy story for Linux-heavy customers.
- systemd unit is restrictive (`NoNewPrivileges`, `ProtectSystem=strict`), which gives us a defense-in-depth posture beyond Windows.

### Negative / Risks

- **Distribution fragmentation cost**: five primary distros × two display servers × two init scenarios (though we only support systemd) = at least 10 QA permutations. The 2-person QA team will stretch here.
- **Wayland keystroke content gap**: DLP-enabled mode is not supported on Wayland. Sales may push back on this. Our position is that Wayland is the privacy-preferred option; customers who truly need DLP should use X11 sessions (acceptable as most Linux enterprise fleets still do).
- **Kernel BTF requirement**: CO-RE requires BTF, which is shipped in newer kernels but not all. Ubuntu 22.04 has it; older RHEL 8 does not. Minimum supported kernels: 5.15 (Ubuntu 22.04), 5.14 (RHEL 9). Phase 2 does not target Ubuntu 20.04 or RHEL 8.
- **Privilege management is operator burden**: customers who run hardened systems (e.g., SELinux enforcing, AppArmor) may need additional policy exceptions. Runbook provides SELinux policy (`.te` file) and AppArmor profile.

## Alternatives Considered

- **Kernel module**: rejected (see context).
- **LD_PRELOAD for syscall interception**: rejected (crashes statically linked binaries, Go binaries, visible to any user with `ldd`, defeats hardened-runtime philosophy).
- **auditd as sole source**: rejected (event schema is fragile across kernel versions, very high volume by default, poor filtering primitives; eBPF does the same job better).
- **`/proc` polling**: rejected (race conditions, high CPU at meaningful polling frequencies, no delete/create events).
- **Falco** as the engine: Falco is excellent for security-event detection but is tuned for rule matching, not raw telemetry collection; using Falco would force us to write our own abstraction layer anyway.
- **Linux-only DLP using `strace`/`ptrace`**: rejected (ptrace is extremely visible to the target user and slows processes by 10-100x; unacceptable UX).
- **Flatpak**: rejected (sandboxing conflicts).
- **Snap**: rejected (classic confinement deprecation).

## Related

- `docs/adr/0015-macos-agent-architecture.md`
- `docs/architecture/phase-2-scope.md` §B.2
- `docs/security/anti-tamper.md` (Linux section to be added in Phase 2)
