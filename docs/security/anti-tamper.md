# Anti-Tamper Strategy — Windows User-Mode Agent

> Language: English. Scope: Phase 1 (user-mode only). Phase 3 minifilter-assisted techniques are noted but not designed here.

## Threat Model for Tampering

We defend primarily against **the monitored employee** attempting to disable, bypass, or silence the agent. We do **not** claim to defend against an attacker with `SeDebugPrivilege` and a motivated hour (that is Phase 3). The Phase 1 bar:

- Casual stop via Services.msc / Task Manager: detected and recovered within 10 s.
- Registry ACL tamper: detected within 60 s.
- Binary replacement at rest: detected on next self-check (≤ 5 min).
- Debugger attach: detected and reported as tamper event.
- Network isolation attempt (hosts file, firewall rule): agent reports last-known state locally and escalates health-degraded on reconnect.

## Techniques

### 1. Dual-Process Watchdog

- Two services: `personel-agent` (main) and `personel-agent-watchdog` (minimal).
- Each watches the other via named pipe heartbeats. If main dies, watchdog restarts it. If watchdog dies, main restarts it via SCM.
- Both services have `SERVICE_SID_TYPE_UNRESTRICTED` and restrict ACLs on their service configuration so that a standard admin cannot simply `sc delete` without leaving a trace; any deletion is treated as tamper on next boot.

Phase 1-achievable. Phase 3 minifilter can additionally refuse process termination signals.

### 2. Registry Protection

- Service configuration under `HKLM\SYSTEM\CurrentControlSet\Services\personel-agent` has ACLs tightened to `NT AUTHORITY\SYSTEM` full, `BUILTIN\Administrators` read.
- A periodic checker (every 30 s) reads the current DACL and compares to the baked-in expected SDDL. Mismatch → `agent.tamper_detected` event.
- The agent's configuration and cert storage keys are under DPAPI machine scope and additionally under a protected registry subkey checked on the same cadence.

Phase 1-achievable. Phase 3 driver can enforce "self-protection" (PatchGuard-style) for these keys.

### 3. Binary Integrity Self-Check

- On startup and every 5 minutes, the agent re-reads its own PE file from disk, computes SHA-256, and compares to a value signed by the release key and stored next to the binary in a signed manifest.
- On mismatch, tamper event is emitted; the watchdog refuses to restart a tampered binary and asks the server for a known-good version via the update channel.

Phase 1-achievable.

### 4. Anti-Debug

- `IsDebuggerPresent()` and `CheckRemoteDebuggerPresent()` checks.
- `NtQueryInformationProcess(ProcessDebugPort)` and `ProcessDebugFlags` checks.
- Timing checks: measure `QueryPerformanceCounter` deltas across specific instruction windows to detect stepping.
- None of these are blockers (we do not crash on debugger detect); they emit `agent.tamper_detected` with `check_name="debugger"`.

Phase 1-achievable. None of these stop a determined reverser; they raise cost and generate evidence.

### 5. String and Symbol Obfuscation

- Sensitive constants (URLs, pin values, signing key ids) are stored XOR-masked with a compile-time key and decoded at the point of use.
- No symbols in release builds (`strip`).
- No `.pdb` shipped.

Phase 1-achievable. Not a serious barrier; deters casual `strings` inspection.

### 6. Certificate and Connection Pinning

- Outbound connections to the gateway verify the tenant CA SPKI pin; any mismatch refuses to connect and logs tamper.
- Pin values are baked in and rotatable via signed `PinUpdate` messages (see `mtls-pki.md`).
- Resolver cache TTLs short; hosts file override is detected by cross-checking DoH resolution when available.

Phase 1-achievable.

### 7. TPM / DPAPI Key Sealing

- PE-DEK and agent private key are stored as DPAPI-protected blobs.
- Where a TPM is present, protectors are bound to TPM (Bitlocker-style) so blobs are unusable on a different host or after firmware reset.
- Key unseal happens lazily and the key stays in memory in a `Zeroize`-wrapped type.

Phase 1-achievable.

### 8. Process Token Hardening

- Agent runs as `LocalSystem` but drops unnecessary privileges at startup via `AdjustTokenPrivileges`.
- Agent enables DEP, CFG, ASLR (`/DYNAMICBASE`, `/HIGHENTROPYVA`, `/GUARD:CF` at build time).
- Enables arbitrary code guard (`ProcessDynamicCodePolicy`) and child-process-disallow (`ProcessChildProcessPolicy`) via `UpdateProcThreadAttribute` where possible — requires careful testing with ETW consumption.

Phase 1 partial (some mitigations interact with ETW); fully in Phase 2.

### 9. Log Tampering Resistance

- Local agent logs are rotated with per-file HMAC, key sealed to DPAPI. Tampered logs fail HMAC on read and are reported.
- Local SQLite queue DB is encrypted at rest; modifications by a third party produce decrypt failures that the agent reports and then recreates the DB with a critical tamper event.

Phase 1-achievable.

### 10. Service Dependency on Watchdog

- Windows SCM `DependOnService` set so that stopping `personel-agent` requires coordinated stop of the watchdog. Stopping the watchdog is itself a tamper event.

Phase 1-achievable.

### 11. Optional: Driver-Assisted (Phase 3)

- A minifilter driver will protect the agent process from termination (`ObRegisterCallbacks` for process/thread handle filtering), protect its files from deletion, and protect its registry keys from modification.
- User-mode Phase 1 cannot achieve this level; the compensating controls above are the ceiling.

## What We Explicitly Do NOT Do

- We do not hide the agent from Task Manager / Process Explorer. Transparency doctrine requires the agent to be visible under its correct publisher name.
- We do not inject into other processes for anti-tamper purposes.
- We do not hook kernel syscalls.
- We do not disable Windows Defender, AMSI, or any security product.
- We do not tamper with the hosts file ourselves.

## Telemetry

Every tamper detection emits:
- `agent.tamper_detected { check_name, severity, details_hash }`
- Priority 0 (critical), never evicted from the local queue.
- Uploaded with next connect even if other categories are dropped.

## Runbook Hooks

When tamper events cluster on a single endpoint → Admin API raises an alert with suggested actions (remote lock, investigation, HR notification). When tamper events cluster across a tenant → product-level incident (possible version regression or policy error).

## Related: Server-Side Detection of a Silenced Agent

Endpoint-side anti-tamper can slow a determined employee with local admin rights but cannot fully defeat them. The other half of the defense is server-side heartbeat monitoring, gap classification, and DPO alerting — modeled as **Flow 7 — Employee-Initiated Agent Disable** in `docs/security/threat-model.md`. That flow ensures an agent going silent produces audit entries (`endpoint.state_transition` with classified gap reason), dashboard visibility (`offline_extended` badge), and optional auto-quarantine. Silence-gap data is also written into the periodic destruction report so that the legal record honestly reflects when monitoring was and was not active.
