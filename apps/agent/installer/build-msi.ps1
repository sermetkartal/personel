#Requires -Version 5.1
<#
.SYNOPSIS
    Builds the Personel Agent MSI installer.

.DESCRIPTION
    1. Compiles the Rust workspace in release mode for x86_64-pc-windows-msvc.
    2. Invokes the WiX 4 toolset to produce personel-agent.msi.

.PARAMETER Version
    Product version (semver). Defaults to the version in Cargo.toml.

.PARAMETER GatewayUrl
    Optional: embed a default GATEWAY_URL in the MSI (overridable at install
    time via msiexec property). Leave empty for pure GPO deployments.

.EXAMPLE
    # Standard build (GPO supplies GATEWAY_URL at install time):
    .\build-msi.ps1

    # Build with a baked-in default gateway:
    .\build-msi.ps1 -GatewayUrl "https://personel-gw.acme.com:8443"

    # Silent GPO install from the produced MSI:
    msiexec /i personel-agent.msi /qn `
        GATEWAY_URL="https://personel-gw.acme.com:8443" `
        TENANT_TOKEN="<jwt-from-admin-console>"
#>
[CmdletBinding()]
param(
    [string]$Version      = "",
    [string]$GatewayUrl   = "",
    [string]$OutputDir    = "$PSScriptRoot\dist"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Resolve paths ─────────────────────────────────────────────────────────────

$RepoRoot    = Resolve-Path "$PSScriptRoot\..\..\.."
$AgentRoot   = Resolve-Path "$PSScriptRoot\.."
$WixManifest = "$PSScriptRoot\wix\main.wxs"
$MsiOutput   = "$OutputDir\personel-agent.msi"

# ── Determine version ──────────────────────────────────────────────────────────

if (-not $Version) {
    # Parse version from workspace Cargo.toml
    $CargoToml = Get-Content "$AgentRoot\Cargo.toml" -Raw
    if ($CargoToml -match 'version\s*=\s*"(\d+\.\d+\.\d+)"') {
        $Version = $Matches[1]
    } else {
        # Fallback to agents crate Cargo.toml
        $AgentCargo = Get-Content "$AgentRoot\crates\personel-agent\Cargo.toml" -Raw
        if ($AgentCargo -match 'version\s*=\s*"(\d+\.\d+\.\d+)"') {
            $Version = $Matches[1]
        } else {
            $Version = "0.1.0"
        }
    }
}
Write-Host "Building Personel Agent v$Version" -ForegroundColor Cyan

# ── Step 1: Cargo build ────────────────────────────────────────────────────────

Write-Host "`n[1/2] cargo build --release --target x86_64-pc-windows-msvc" -ForegroundColor Yellow

Push-Location $AgentRoot
try {
    & cargo build --release --target x86_64-pc-windows-msvc
    if ($LASTEXITCODE -ne 0) {
        throw "cargo build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$ReleaseDir = "$AgentRoot\target\x86_64-pc-windows-msvc\release"

# Verify expected binaries exist.
@("personel-agent.exe", "personel-watchdog.exe", "enroll.exe") | ForEach-Object {
    $Bin = "$ReleaseDir\$_"
    if (-not (Test-Path $Bin)) {
        throw "Expected binary not found after build: $Bin"
    }
}

# ── Step 2: WiX build ─────────────────────────────────────────────────────────

Write-Host "`n[2/2] wix build → $MsiOutput" -ForegroundColor Yellow

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$WixArgs = @(
    "build",
    $WixManifest,
    "-d", "ProductVersion=$Version",
    "-o", $MsiOutput
)

if ($GatewayUrl) {
    $WixArgs += @("-d", "GATEWAY_URL=$GatewayUrl")
}

& wix @WixArgs
if ($LASTEXITCODE -ne 0) {
    throw "wix build failed with exit code $LASTEXITCODE"
}

Write-Host "`nBuild complete: $MsiOutput" -ForegroundColor Green
Write-Host @"

GPO silent install:
  msiexec /i personel-agent.msi /qn \`
      GATEWAY_URL="https://your-gateway:8443" \`
      TENANT_TOKEN="<jwt-from-admin-console>"
"@ -ForegroundColor Cyan
