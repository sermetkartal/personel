# ADR 0015 — macOS Endpoint Agent Architecture

**Status**: Proposed for Phase 2. Not implemented.
**Deciders**: microservices-architect (Phase 2 planner); to be ratified by rust-engineer + security-engineer at Phase 2 kickoff.
**Related**: ADR 0002 (Rust for agent), ADR 0010 (Windows user-mode Phase 1), ADR 0016 (Linux agent, sibling), ADR 0009/0013 (keystroke encryption + DLP off-by-default).

## Context

Phase 2 ships a macOS endpoint agent with feature parity to the Windows agent for the Phase 1 collector set (process, file, window focus, screenshot, clipboard metadata, keystroke metadata + encrypted content, network flow summaries, DNS/TLS SNI, USB events, print jobs, idle/active, live view). macOS fundamentally differs from Windows in three areas that shape agent architecture:

1. **Apple deprecated kernel extensions on Apple Silicon.** System Extensions (DriverKit, Endpoint Security, Network Extension) replace KEXTs. No equivalent to a WFP callout in user mode; Apple's user-mode Network Extension API is the only supported path.
2. **TCC (Transparency, Consent, Control) enforces per-capability user consent.** Accessibility, Screen Recording, Input Monitoring, Full Disk Access, and Network Extension approvals are **per-app, per-user, one-time-with-revocation**. The user (not an admin, not a policy) grants them. Declaring them in the `.app` Info.plist and entitlements is necessary but not sufficient.
3. **Notarization and hardened runtime are mandatory for Gatekeeper and MDM distribution.** An un-notarized agent cannot be deployed at scale; a notarized agent that lacks hardened runtime entitlements cannot perform TCC-requiring operations.

Phase 1 chose user-mode-only for Windows (ADR 0010). macOS has no user-mode / kernel-mode dichotomy in the same sense; Endpoint Security Framework (ES) is a user-space daemon that subscribes to a kernel event stream via a System Extension. ES is the supported path. DTrace is a debugging tool, not a production telemetry source. Hybrid designs (ES + periodic polling) are the only practical approach.

## Decision

### Core collector model: Endpoint Security Framework (ES)

Use **Endpoint Security Framework** (first-class since macOS 10.15, stable and production-ready on macOS 12+) for process, file, and authentication events.

- Delivery mechanism: a **System Extension** (`com.apple.endpoint-security` type) embedded in the agent `.app` bundle.
- Event subscription set: `ES_EVENT_TYPE_NOTIFY_EXEC`, `NOTIFY_EXIT`, `NOTIFY_FORK`, `NOTIFY_OPEN`, `NOTIFY_CLOSE`, `NOTIFY_RENAME`, `NOTIFY_UNLINK`, `NOTIFY_WRITE`, `NOTIFY_CREATE`, `NOTIFY_MOUNT`, `NOTIFY_UNMOUNT`, `NOTIFY_IOKIT_OPEN` (USB), `NOTIFY_LOGIN`, `NOTIFY_LOGOUT`, `NOTIFY_SCREENSHARING_ATTACH` (live view abuse detection).
- **No AUTH events**: we do not take blocking decisions in ES because AUTH events have a hard deadline and any latency kills the user's session. The Personel policy engine already acts on enforcement downstream (URL block, USB block) via separate mechanisms.
- ES daemon written in Swift or Objective-C thin shim over the C `EndpointSecurity.framework` API, bridged to the Rust agent core via a local UNIX domain socket with a Cap'n Proto or Protobuf framing. Rust cannot directly link `EndpointSecurity.framework` at the time of writing; the Swift shim is an architectural fence, not a limitation.

**Rejected: DTrace.** DTrace requires disabling SIP (System Integrity Protection) on modern macOS for full scope, which is a non-starter for enterprise deployment. Even with SIP on, DTrace provides limited event coverage compared to ES and is meant for debugging.

**Rejected: Hybrid (ES + DTrace).** No gain over ES alone in production; adds operational complexity.

### Screen capture: ScreenCaptureKit

Use **ScreenCaptureKit** (introduced macOS 12.3). Minimum supported OS: **macOS 13 Ventura** (the first macOS where ScreenCaptureKit is stable and the APIs are non-deprecated).

- ScreenCaptureKit vs legacy CGDisplayStream: legacy is deprecated on macOS 14+ and removed in macOS 15 beta. Not an option.
- ScreenCaptureKit supports display filtering (exclude specific windows — we use this for the `screenshot_exclude_apps` Phase 1 policy), frame rate control, and cursor inclusion. It requires **Screen Recording TCC permission**, which the user must grant through System Settings → Privacy & Security → Screen Recording.
- For live view, ScreenCaptureKit's `SCStream` delivers CMSampleBuffer directly to a video encoder; we encode H.264 via VideoToolbox and feed LiveKit WebRTC, mirroring the Windows DXGI → LiveKit pipeline.

### Keystroke capture: IOHIDManager (Input Monitoring TCC)

Use **IOHIDManager** (a user-space API that sits on top of the HID device driver stack) to observe key event counts. Requires **Input Monitoring TCC permission**, not Accessibility (Accessibility gives you the event content directly; Input Monitoring gives raw HID).

- Keystroke metadata collector (counts, not content): IOHIDManager + filter by key-down events only. No content reconstruction.
- Keystroke content collector (Phase 1 design: encrypted at endpoint, decrypted only by DLP service when enabled): on macOS this requires **Accessibility TCC permission** because IOHIDManager alone does not produce Unicode characters, only HID usage codes. Given ADR 0013 makes this off-by-default, the Accessibility grant is only requested if the tenant opts into DLP. In the default off-state, the agent **does not request Accessibility at all**, preserving the least-privilege posture.
- The policy engine on macOS must branch: if `dlp_enabled=false` → request only Input Monitoring; if `dlp_enabled=true` (post-ceremony) → request Accessibility additionally. This branching is OS-specific glue; the cross-platform collector trait is unchanged.

### File system: FSEvents + ES duplication

Use **FSEvents** (filesystem event stream API) for high-volume file activity, specifically for user directories (`~/Documents`, `~/Downloads`, `~/Desktop`), because ES event volume is very high on system-wide file subscription. FSEvents uses the kernel FSEvents daemon and provides coalesced, path-scoped events cheaply.

- For sensitive paths (e.g., `/private`, `/System/Volumes/Data/System`), ES is used because FSEvents does not see them reliably.
- Dual-source design: FSEvents = user-space fast path, ES = kernel-level coverage for privileged paths. Dedup is by `(inode, timestamp, event_type)` in the Rust core.

### Network: Network Extension (Content Filter + Transparent Proxy)

Use the **Network Extension** framework, specifically the `NEFilterDataProvider` for traffic inspection and `NEDNSProxyProvider` for DNS observation.

- TCC requires **Full Disk Access** for the parent app and a separate System Extension approval for the Network Extension. Both are one-time.
- No packet modification; observation only. Summary events are produced: `(process, remote_host, remote_port, bytes_sent, bytes_received, duration)`.
- DNS proxy is optional and only enabled if the policy allows DNS observation; some environments prefer to rely on corporate DNS instead.

### Code signing, notarization, hardened runtime

- **Developer ID Application certificate** (Apple Developer Enterprise Program) — a separate class from Mac App Store certificates. Acquired in Phase 2 kickoff week.
- **Notarization**: every release artifact submitted to Apple's notary service. Stapled with `stapler` before distribution.
- **Hardened runtime**: enabled with the following entitlements:
  - `com.apple.security.cs.allow-jit` — **disabled** (not needed; security win)
  - `com.apple.security.cs.disable-library-validation` — **disabled** (no third-party loadable modules)
  - `com.apple.developer.endpoint-security.client` — **required** (for ES; requires manual approval from Apple)
  - `com.apple.developer.networking.networkextension` — **required** (requires manual approval)
  - `com.apple.developer.system-extension.install` — **required**
- Apple manually reviews entitlement requests for ES and Network Extension — Phase 2 kickoff must file these applications in week 1 because approval takes 2–4 weeks historically.

### Installer and deployment

- **`.pkg` installer** built via `productbuild`, signed with **Developer ID Installer certificate**.
- **Postinstall script** (signed with the same certificate) that:
  1. Copies `Personel.app` to `/Applications`.
  2. Loads the System Extension (`systemextensionsctl install`).
  3. Registers the launch daemon (`/Library/LaunchDaemons/com.personel.agent.plist`) for unattended boot-time start.
  4. **Does not** bypass TCC prompts (impossible on modern macOS; we only pre-stage what we can).
- **MDM deployment**: ship an MDM configuration profile (`.mobileconfig`) for Jamf/Intune/Mosyle that **pre-approves** the System Extension and PPPC (Privacy Preferences Policy Control) for the non-user-interactive grants. PPPC can pre-grant: Full Disk Access, System Extension, Network Extension, Endpoint Security. It cannot pre-grant Screen Recording, Accessibility, or Input Monitoring — those always require the user.
- Customer ops runbook documents the two deployment modes:
  - **Managed (MDM)**: push the config profile, user only needs to grant Screen Recording + Input Monitoring + (if DLP enabled) Accessibility. One visible prompt flow, documented with screenshots in the transparency portal.
  - **Unmanaged**: user runs the `.pkg` and is walked through a guided first-run flow by the agent itself (an `onboarding` window that explains each permission with a KVKK reference).

### User-visible consent model and KVKK m.10 framing

The macOS TCC model is an **asset** for the KVKK m.10 aydınlatma argument:

- The OS itself prompts the user and records consent. This is independent corroboration that the user was informed and consented at the OS level.
- The transparency portal surfaces which TCC grants are held and which are not. If the user revokes Screen Recording, the portal immediately shows "Ekran kaydı için izniniz kaldırıldı — yöneticinize bildirin".
- KVKK m.5/2-f legitimate interest still applies; TCC does not replace organizational consent, it layers on top of it.
- **Degradation rules**: if a required TCC grant is missing, the affected collector is disabled (not the whole agent). The agent reports its **capability state** to the gateway so the admin console shows "this endpoint has no screen capture — 3 days" as an alert.

## Consequences

### Positive

- Modern, Apple-sanctioned architecture. No KEXTs, no deprecated APIs, no SIP disablement.
- OS-enforced consent layer strengthens KVKK m.10 defense.
- Feature parity with Windows agent for Phase 1 collector set.
- MDM-deployable with pre-approved profile; reasonable operational burden.
- Swift shim is small (<2k LOC estimated) and does not change the Rust-dominant agent codebase.

### Negative / Risks

- **Apple entitlement approval is a dependency.** ES and Network Extension entitlements require a manual Apple review that can take weeks. Phase 2 kickoff must file immediately.
- **macOS version fragmentation.** macOS 13 minimum excludes ~15% of enterprise devices still on Big Sur / Monterey as of early 2026. Customers with older fleets need to upgrade first. Documented as a prerequisite in the Phase 2 install runbook.
- **Swift ↔ Rust boundary.** A local socket between the ES daemon and the Rust agent core adds a failure mode (socket dropped → daemon respawn). Mitigated by a watchdog similar to the Windows watchdog.
- **User-grantable TCC means gaps in coverage are normal.** The admin console must treat gaps gracefully, not as errors.
- **Apple Silicon code signing** requires `arm64` and `x86_64` universal binaries; our Rust toolchain must cross-compile. This is solved but adds CI burden.

## Alternatives Considered

- **DTrace + launchd periodic polling**: rejected (SIP limitations, stale data, no file event semantics).
- **LD_PRELOAD / DYLD_INSERT_LIBRARIES**: rejected (blocked by hardened runtime on notarized apps; incompatible with SIP).
- **Kernel extension (legacy KEXT)**: rejected (deprecated, no Apple Silicon support, no notarization).
- **Swift-native agent (no Rust)**: rejected (diverges from Windows/Linux codebase; loses cross-platform collector trait).
- **Cross-compile Rust directly against `EndpointSecurity.framework`**: evaluated; Rust FFI bindings exist but are unstable and lag ES API revisions. The Swift shim is simpler and more maintainable.
- **PaddleOCR on endpoint for screenshot text**: out of scope (OCR runs server-side; see B.3 in `phase-2-scope.md`).

## Related

- `docs/adr/0002-rust-for-agent.md`
- `docs/adr/0010-windows-user-mode-phase1.md`
- `docs/adr/0016-linux-agent-architecture.md`
- `docs/architecture/phase-2-scope.md` §B.1
- `docs/compliance/kvkk-framework.md` §5 (m.10 aydınlatma)
