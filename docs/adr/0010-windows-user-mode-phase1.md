# ADR 0010 — Windows User-Mode Only in Phase 1

**Status**: Accepted

## Context

UAM agents have historically shipped minifilter drivers, WFP callout drivers, and other kernel components for file-system visibility, tamper resistance, and network control. Kernel code is powerful but carries enormous cost: driver signing (Microsoft WHQL), crash-risk on customer machines, longer release cycles, deeper security review, and a painful debugging loop. For a Phase 1 pilot with 500 endpoints in Turkey, the cost is not justified.

## Decision

**Phase 1 and Phase 2 ship user-mode only.** The agent uses:

- **ETW (Event Tracing for Windows)** for process start/stop, file events, DNS, TLS SNI, Kernel-Process provider, Microsoft-Windows-Kernel-File, Microsoft-Windows-DNS-Client.
- **Win32 APIs** (`GetForegroundWindow`, `SetWinEventHook`, `GetWindowText`, `GetLastInputInfo`, `GetProcessImageFileName`, etc.).
- **WFP (Windows Filtering Platform) user-mode** for network flow summarization and optional block actions.
- **DXGI Desktop Duplication** for screen capture.
- **WMI** for USB device events.
- **Clipboard listener** via the standard clipboard sequence API.
- **DPAPI + TPM** for key sealing at rest.

A **kernel minifilter driver** is explicitly deferred to **Phase 3**. An extension seam (`collectors::kernel_bridge`) is reserved so Phase 3 can add the driver without architectural rework.

## Consequences

- Faster Phase 1 delivery, no WHQL bottleneck, no kernel crash exposure on pilot customers.
- Some file-system events visible only to minifilter (e.g., pre-write blocking, certain rename patterns) are unavailable — the file collector observes post-hoc via ETW. Acceptable for UAM use cases.
- Anti-tamper is weaker than kernel-assisted competitors; we compensate with watchdog process, service ACLs, and DPAPI+TPM-sealed secrets. See `docs/security/anti-tamper.md`.
- WFP user-mode cannot do in-place packet modification; we can only summarize and optionally block by process-connection. Acceptable.
- No need for SV2 EV code-signing certificate for a driver; standard EV code-signing is enough.
- Phase 3 minifilter will slot in behind the collector trait, not replace it.

## Alternatives Considered

- **Ship a minifilter in Phase 1**: rejected — timeline and risk.
- **Ship a WFP callout driver in Phase 1**: rejected — subset of the same burden.
- **Use a commercial UAM SDK with a bundled driver**: rejected — vendor lock-in, cost, and it defeats the point of owning the stack.
- **PowerShell-based collector**: laughable at scale; rejected.
