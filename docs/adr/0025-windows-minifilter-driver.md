# ADR 0025 — Windows Minifilter Driver (Forensic-Grade DLP)

**Status**: Accepted (Phase 3)
**Amends**: ADR 0010 (Windows Agent — User-Mode Only, Phase 1–2). Phase 3 elevates the Windows agent to include an optional kernel-mode minifilter driver component for customers requiring forensic-grade file-level DLP.
**Related**: ADR 0013 (DLP Disabled by Default), ADR 0009 (Keystroke Content Encryption)

## Context

Phase 1 and Phase 2 shipped the Windows agent entirely in user mode, per ADR 0010, using ETW (Event Tracing for Windows), GDI screen capture, and Windows API hooks reachable from user mode. This posture kept deployment simple (no driver signing, no WHQL, no BSOD risk) and was sufficient to pass Phase 1 exit criteria.

However, high-end forensic RFPs (banking fraud investigations, IP theft cases, regulator-mandated DLP) gate on **file-system-level visibility** that user mode cannot reliably provide:

- Arbitrary-application file writes to removable media (visible at ETW with delay, unreliable under load).
- Encrypted container creation and mount (e.g., VeraCrypt) — only visible at file system filter level.
- Alternate Data Streams (ADS) — user-mode collectors cannot reliably detect ADS writes.
- Pre-write file content inspection (DLP-policy enforcement at write time, not after the fact).
- File attribute changes that evade ETW audit.

Segment 1 competitors (Veriato, Teramind Enterprise) use minifilter drivers for exactly this purpose. Phase 3 adds a minifilter to the Windows agent as an **optional component** enabled for customers whose DPIA authorizes it.

Constraints:

- Kernel-mode code has BSOD potential; stability is non-negotiable.
- WHQL signing is required for deployment to modern Windows (Windows 11 22H2+). WHQL is slow and expensive.
- ADR 0013 DLP-off-by-default posture is preserved; driver installation does not mean DLP is active — DLP still requires the opt-in ceremony.
- KVKK m.6 özel nitelikli veri risk is re-evaluated: a driver can observe more than user-mode code, so DPIA must be amended.

## Decision

### 1. Driver type: File System Filter Driver (minifilter, FLT model)

A **file system minifilter driver** using the Filter Manager (`fltmgr.sys`) framework is added to the Windows agent. The minifilter:

- Registers for `IRP_MJ_CREATE`, `IRP_MJ_WRITE`, `IRP_MJ_SET_INFORMATION`, `IRP_MJ_CLEANUP`, and `IRP_MJ_CLOSE` pre- and post-operations.
- Filters only on configured path patterns and file extensions (no blanket filtering).
- Does NOT log file contents (that would cross the ADR 0009 keystroke-content-encryption boundary differently and risk m.6 violations).
- Emits metadata events (path, process ID, operation, timestamp) to the user-mode agent via a shared `FilterSendMessage` channel.

Alternatives rejected:
- **Legacy file system filter drivers** (non-minifilter) — deprecated by Microsoft; WHQL path much harder.
- **Kernel-mode callbacks only** (no filter driver) — insufficient coverage of file-system events.
- **Network minifilter (WFP callout driver)** — orthogonal concern; may be added later for network DLP, separate ADR.

### 2. Build and sign toolchain

- **Windows Driver Kit (WDK)** 10 (current, matched to Visual Studio 2022).
- **INF file** for driver installation.
- **EV Code Signing Certificate** for the driver binary (mandatory for driver submission to Microsoft).
- **Microsoft Partner Center** account for WHQL attestation signing and dashboard submission.
- **Hardware Lab Kit (HLK)** test passes for the driver's target Windows versions (Win10 21H2, Win11 22H2, Win11 23H2, Server 2022).
- **Dual signing**: the driver is EV-signed AND WHQL-attested; deployment uses the WHQL-attested version for production.

Estimated WHQL signing calendar: 4–8 weeks from first HLK submission to dashboard-signed driver, independent of development effort. Retries for failed tests extend this.

### 3. Integration with existing user-mode agent

The minifilter is a **separate component**, not a monolith with the agent:

```
┌───────────────────────────────────────────────┐
│ User mode (ring 3)                            │
│  ┌──────────────────────────────────────┐     │
│  │ personel-agent.exe (Rust)            │     │
│  │  - collectors, queue, policy         │     │
│  │  - opens handle to \\.\PersonelFlt   │     │
│  └──────────────────────────────────────┘     │
│                    │                          │
│                    │ DeviceIoControl + FltSendMessage
│                    ▼                          │
├───────────────────────────────────────────────┤
│ Kernel mode (ring 0)                          │
│  ┌──────────────────────────────────────┐     │
│  │ personel-flt.sys (C, WDK)            │     │
│  │  - PreCreate/PreWrite callbacks      │     │
│  │  - Metadata event ring buffer        │     │
│  │  - FilterSendMessage to user mode    │     │
│  └──────────────────────────────────────┘     │
└───────────────────────────────────────────────┘
```

Communication:
- User mode opens `\\.\PersonelFlt` via `FilterConnectCommunicationPort`.
- Driver emits events via `FltSendMessage`; user mode reads via `FilterGetMessage`.
- Policy updates (path filters) are pushed from user mode via `FilterSendMessage` in the reverse direction.
- IPC is one-way at a time (not full duplex on a single connection) but two ports are used.

### 4. Test and development workflow

- **Windows test signing mode** (`bcdedit /set testsigning on`) for development workstations.
- **Hyper-V VMs with a checked-build Windows** for driver debugging via kernel debugger (KD over serial or net).
- **Static analysis**: Driver Verifier + Code Analysis + SAL annotations + BinSkim.
- **Runtime analysis**: Application Verifier, Driver Verifier in "standard" + "special pool" mode before every release.
- **Fuzz testing**: crafted malformed I/O against the filter callback to detect driver crashes. Fault injection via Driver Verifier force-faults.
- **Soak testing**: 72-hour continuous write/read load on test machines before each release candidate.

### 5. Deployment via MSI installer

- The Personel MSI installer is extended to optionally install the driver via INF + `pnputil /add-driver`.
- Driver installation requires **admin rights and a reboot** (there is no way to install a minifilter driver without reboot in the supported model).
- Uninstall removes the driver via `pnputil /delete-driver` and removes the registered minifilter altitude, then requires reboot.
- **Driver is NOT enabled by default**; installation requires the customer's explicit opt-in via MSI install parameter (`PERSONEL_INSTALL_DRIVER=1`) AND DLP ceremony (ADR 0013 still gates actual DLP behavior).

### 6. Rollback story

Rollback = `pnputil /delete-driver personel-flt.inf` + reboot. No graceful unload on a file system minifilter is safe in the general case (risk of leaving volumes with stale filter altitude).

**Operational guarantee**: if the driver causes issues, the agent service is architected to **continue operating in user-mode-only fallback** even if the driver refuses to load. The agent detects driver absence via `FilterConnectCommunicationPort` failure and logs the fallback state.

**Emergency remote unload**: via admin console "disable driver" action → signed policy → agent stops opening the filter handle → next reboot the driver can be uninstalled via automated pipeline.

### 7. KVKK m.6 DPIA amendment

Because the driver can observe file-level events that user mode cannot:

- New DPIA amendment required per customer enabling the driver.
- Transparency portal updated with an "Advanced DLP monitoring" explainer.
- Driver-observed events are classified into the same sensitivity taxonomy as ETW-observed events; no special new category is created.
- The driver **does not capture file contents** by default; policy-matched samples may be captured only if the DLP opt-in ceremony has been completed AND a specific content-capture policy is signed by the customer DPO (this is the strongest ceremony in the product).

### 8. Tamper resistance

- Driver is marked as PPL (Protected Process Light) eligible to resist user-mode tamper; actual PPL requires Microsoft ELAM registration which is reserved for future.
- Driver is registered as **not-unloadable** via `FLTFL_REGISTRATION_DO_NOT_SUPPORT_SERVICE_STOP` — admin cannot `sc stop personel-flt` without reboot.
- Uninstall requires signed policy + admin rights + reboot.

## Consequences

### Positive

- Unblocks forensic-grade DLP RFPs where minifilter is a hard requirement.
- File-level visibility is an architectural differentiator vs Teramind-level competitors.
- Driver stays optional — customers who don't need it don't install it, preserving Phase 1/2 operational simplicity.

### Negative

- BSOD risk in customer environments; test burden is significantly higher than user-mode code.
- WHQL signing process adds calendar delay independent of code readiness.
- Deployment always requires reboot; cannot be rolled out without maintenance window.
- Support matrix grows: each Windows version + each customer antivirus combination is a potential compatibility issue.
- KVKK m.6 DPIA amendment required for each customer enabling it (slower sales motion).

### ADR 0010 impact

ADR 0010 said "Windows agent user-mode only Phase 1–2, minifilter Phase 3". This ADR is the Phase 3 elevation it foretold. ADR 0010's core assertion — that user-mode is sufficient for Phase 1/2 — is preserved; the minifilter is strictly additive for Phase 3 customers who need it.

### ADR 0013 impact

ADR 0013 DLP-off-by-default is preserved. Installing the driver is orthogonal to enabling DLP. A customer can install the driver (for forensic visibility metadata) without ever enabling content-inspection DLP.

## Alternatives Considered

- **Stay user-mode only** — rejected; caps Phase 3 enterprise segment.
- **Rent a third-party minifilter (OEM a vendor)** — rejected; trust boundary violation and cost.
- **WFP callout driver only (network)** — insufficient; does not address file-system requirement.
- **Kernel-mode component written in Rust** — ecosystem for Rust-in-Windows-kernel exists but is not mature enough for production Phase 3. C + WDK remains the blessed path.
- **Skip WHQL, use EV signing only** — rejected; modern Windows enforces WHQL for driver deployment in many scenarios and enterprise MDM flows.

## Cross-references

- ADR 0010 — user-mode-only posture, Phase 1/2 (amended by this ADR for Phase 3)
- ADR 0013 — DLP off-by-default (preserved)
- `docs/architecture/phase-3-roadmap.md` — Phase 3.7 parallel workstream
- `docs/compliance/dpia-sablonu.md` — to receive a driver amendment
