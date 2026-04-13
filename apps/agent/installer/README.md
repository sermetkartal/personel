# Personel Agent — MSI Installer

## Build Requirements

| Tool | Minimum Version | Notes |
|------|----------------|-------|
| Rust toolchain | 1.75 stable | `rustup target add x86_64-pc-windows-msvc` |
| WiX Toolset | 4.x | `winget install WiX.WiX` or from wixtoolset.org |
| PowerShell | 5.1 | Windows built-in |
| Windows SDK | 10.0.19041+ | For heat.exe (optional, not yet used) |

## Build

```powershell
# From the installer directory:
.\build-msi.ps1

# With a baked-in default gateway URL:
.\build-msi.ps1 -GatewayUrl "https://personel-gw.acme.com:8443"
```

Output: `installer\dist\personel-agent.msi`

## Silent Install — Interactive (msiexec)

```cmd
msiexec /i personel-agent.msi /qn ^
    GATEWAY_URL="https://personel-gw.acme.com:8443" ^
    TENANT_TOKEN="<jwt-from-admin-console>"
```

## Silent Install — GPO (Group Policy Object)

### Machine Software Policy (recommended for fleet deployments)

1. Copy `personel-agent.msi` to a UNC share accessible by computer accounts:
   `\\fileserver\software\personel\personel-agent.msi`

2. Create a GPO and link it to the target OU:
   **Computer Configuration → Software Settings → Software Installation**
   → New → Package → select the MSI → **Assigned**

3. Supply MSI properties via a GPO startup script
   (`Computer Configuration → Windows Settings → Scripts → Startup`):

   ```powershell
   Start-Process msiexec -ArgumentList @(
       "/i", "\\fileserver\software\personel\personel-agent.msi",
       "/qn",
       "GATEWAY_URL=https://personel-gw.acme.com:8443",
       "TENANT_TOKEN=eyJhbGci..."
   ) -Wait
   ```

### MSI Public Properties

| Property | Required | Description |
|----------|----------|-------------|
| `GATEWAY_URL` | Yes | Gateway gRPC endpoint (`https://host:8443`) |
| `TENANT_TOKEN` | Yes | One-time enrollment JWT from admin console |

`TENANT_TOKEN` is marked `Secure="yes"` and `Hidden="yes"` — it does not
appear in Windows Installer logs (`.log` files).

## Upgrade

The installer uses `MajorUpgrade` with a stable `UpgradeCode`. Deploying a
newer MSI over an existing installation performs a major-upgrade in a single
`msiexec` call (stops services, replaces binaries, restarts services).

## Uninstall

```cmd
msiexec /x personel-agent.msi /qn
```

Or via Add/Remove Programs → "Personel Agent".

The uninstaller stops and removes both `PersonelAgent` and
`PersonelWatchdog` services.

## Firewall

The installer adds a Windows Firewall outbound exception for TCP 8443
(gRPC mTLS to the gateway). The exception is scoped to any profile (domain,
private, public) and removed on uninstall.

## Registry keys written

```
HKLM\SOFTWARE\Personel\Agent\Config
  GatewayUrl   REG_SZ   <GATEWAY_URL property>
  TenantToken  REG_SZ   <TENANT_TOKEN property>
```

The `enroll.exe` custom action reads these keys during deferred execution to
provision the PE-DEK and register the endpoint.

## Code signing (production builds)

For production releases, binaries and the MSI must be Authenticode-signed.
See [`docs/operations/code-signing.md`](../../../docs/operations/code-signing.md)
for the full operator runbook (cert procurement, GitHub Actions secret setup,
local signing via `sign-binaries.ps1`, rotation procedure).
