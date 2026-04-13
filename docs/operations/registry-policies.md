# Personel Agent — Registry Policy Reference

Flat lookup table for every HKLM registry key the Personel agent reads (or will read) at runtime. Companion to [gpo-deployment.md](./gpo-deployment.md).

## Key locations

The agent reads two roots:

| Root | Purpose | Writer |
|---|---|---|
| `HKLM\SOFTWARE\Personel\Agent\Config` | Install-time seed values | MSI installer (`main.wxs`) |
| `HKLM\SOFTWARE\Policies\Personel\Agent` | Group Policy overrides | GPO / ADMX |

**Precedence**: values under `SOFTWARE\Policies\` override values under `SOFTWARE\Personel\` at the next agent restart or policy refresh. The `Policies` subtree is also the default ACL-locked GPO surface — end users cannot modify it without local admin rights.

## Status legend

| Status | Meaning |
|---|---|
| **WIRED** | Agent reads this value at runtime today. Safe to set, takes effect on next service restart. |
| **DEFINED** | Policy surface exists via ADMX but agent does not yet read it. Setting it is safe and future-proof; Phase 5 wiring sprint will activate it. |

## Enrollment keys

| Value name | Type | Default | Valid values | Faz / Feature | Status |
|---|---|---|---|---|---|
| `EnrollmentToken` | REG_SZ | (none) | base64url JWT, ≤4096 chars | Faz 1 enrollment | DEFINED |
| `GatewayUrl` | REG_SZ | compiled-in | `https://host:port`, ≤512 chars | Faz 1 gateway connect | WIRED (via install-time path) / DEFINED (via Policies path) |
| `DataDirectory` | REG_SZ | `%PROGRAMDATA%\Personel\agent` | absolute Windows path | Faz 1 data path | DEFINED |

## Operations keys

| Value name | Type | Default | Valid values | Faz / Feature | Status |
|---|---|---|---|---|---|
| `HeartbeatIntervalSeconds` | REG_DWORD | 30 | 5..3600 | Faz 1 gateway heartbeat / Flow 7 | DEFINED |
| `ScreenshotIntervalSeconds` | REG_DWORD | 300 | 30..7200 | Faz 3 #21 base capture cadence | DEFINED |
| `ScreenshotQuality` | REG_DWORD | 75 | 1..100 | Faz 3 #24 WebP encoding | DEFINED |
| `EnableScreenshotOnIdleFastTick` | REG_DWORD | 1 | 0, 1 | Faz 3 #22 adaptive frequency | DEFINED |
| `AutoUpdateCheckInterval` | REG_DWORD | 21600 | 300..604800 | Faz 4 #36 auto-update checker | DEFINED |
| `AutoUpdateChannel` | REG_SZ | `stable` | `stable`, `beta`, `dev` | Faz 4 #36 update channel | DEFINED |
| `DiagnosticLogLevel` | REG_SZ | `info` | `error`, `warn`, `info`, `debug` | Faz 1 tracing-subscriber | DEFINED |

## Privacy / DLP keys

| Value name | Type | Default | Valid values | Faz / Feature | Status |
|---|---|---|---|---|---|
| `ExcludedAppsAdditional` | REG_MULTI_SZ | (empty) | process base names, one per line | Faz 3 #23 additive sensitivity list | DEFINED |
| `DLPOptInAcknowledged` | REG_DWORD | 0 | 0, 1 | ADR 0013 runtime gate for clipboard_content_redacted | DEFINED |

## Install-time keys (legacy `SOFTWARE\Personel\Agent\Config`)

These are written by the MSI installer and consumed by `enroll.exe` on first run. They are NOT Group Policy keys — they are the seed values used when no GPO is in effect.

| Value name | Type | Source | Purpose |
|---|---|---|---|
| `GatewayUrl` | REG_SZ | `GATEWAY_URL` msiexec property | Initial gateway endpoint for `enroll.exe` |
| `TenantToken` | REG_SZ | `TENANT_TOKEN` msiexec property (`Hidden="yes"`) | One-time enrollment JWT for `enroll.exe` |

**Note on TenantToken**: the install-time `TenantToken` value is single-use. After a successful enrollment, `enroll.exe` should zero it out. If the GPO `EnrollmentToken` policy is wired in Phase 5, it will supersede the install-time `TenantToken`.

## Quick reference — PowerShell snippets

**Check all Personel policy values on the local machine**:
```powershell
Get-ItemProperty -Path "HKLM:\SOFTWARE\Policies\Personel\Agent" -ErrorAction SilentlyContinue
```

**Check install-time seed values**:
```powershell
Get-ItemProperty -Path "HKLM:\SOFTWARE\Personel\Agent\Config" -ErrorAction SilentlyContinue
```

**Set a value manually (for testing; prefer GPO in production)**:
```powershell
New-Item -Path "HKLM:\SOFTWARE\Policies\Personel\Agent" -Force | Out-Null
Set-ItemProperty -Path "HKLM:\SOFTWARE\Policies\Personel\Agent" `
                 -Name "DiagnosticLogLevel" -Value "debug" -Type String
Restart-Service PersonelAgent
```

**Clear all Personel GPO values (rollback)**:
```powershell
Remove-Item -Path "HKLM:\SOFTWARE\Policies\Personel\Agent" -Recurse -Force
Restart-Service PersonelAgent
```

## See also

- [gpo-deployment.md](./gpo-deployment.md) — full GPO deployment runbook (TR)
- `apps/agent/installer/admx/personel-agent.admx` — ADMX source
- `apps/agent/installer/admx/en-US/personel-agent.adml` — English strings
- `apps/agent/installer/admx/tr-TR/personel-agent.adml` — Turkish strings
- `apps/agent/installer/wix/main.wxs` — MSI installer (install-time registry writes)
- `apps/agent/crates/personel-agent/src/config.rs` — `AgentConfig` struct; currently does NOT read Policies subtree (DEFINED → WIRED migration tracked for Phase 5)

---

*Version 1.0 — Faz 4 Wave 3 #38. When a policy transitions from DEFINED to WIRED, update the Status column in this file in the same commit as the agent code change.*
