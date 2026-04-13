#Requires -Version 5.1
<#
.SYNOPSIS
    Signs Personel Agent binaries + MSI with an Authenticode code-signing cert.

.DESCRIPTION
    Reads CODE_SIGNING_CERT_PFX (path) and CODE_SIGNING_CERT_PASSWORD env vars.
    If both are set, signs:
      - target\x86_64-pc-windows-msvc\release\personel-agent.exe
      - target\x86_64-pc-windows-msvc\release\personel-agent-watchdog.exe
      - target\x86_64-pc-windows-msvc\release\enroll.exe
      - installer\dist\personel-agent.msi (if present)
    Then verifies each signature via signtool verify /pa.

    If env vars are NOT set, prints a clear configuration hint and exits 0
    (so it can be chained after build-msi.ps1 in dev workflows without
    breaking unsigned local builds).

    The signtool invocation matches .github/workflows/build-agent.yml exactly,
    so CI and local builds produce byte-identical signatures (modulo timestamp).

.PARAMETER PfxPath
    Override CODE_SIGNING_CERT_PFX env var.

.PARAMETER PfxPassword
    Override CODE_SIGNING_CERT_PASSWORD env var.

.PARAMETER TimestampUrl
    RFC 3161 timestamp authority. Default: http://timestamp.sectigo.com

.PARAMETER ReleaseDir
    Cargo release output directory. Default: ..\target\x86_64-pc-windows-msvc\release

.PARAMETER MsiPath
    MSI to sign. Default: dist\personel-agent.msi (relative to script dir).

.EXAMPLE
    # Set env vars then call:
    $env:CODE_SIGNING_CERT_PFX = "C:\secrets\personel-codesign.pfx"
    $env:CODE_SIGNING_CERT_PASSWORD = "<password>"
    .\sign-binaries.ps1

.EXAMPLE
    # Pass directly:
    .\sign-binaries.ps1 -PfxPath C:\secrets\personel.pfx -PfxPassword "..."
#>
[CmdletBinding()]
param(
    [string]$PfxPath      = $env:CODE_SIGNING_CERT_PFX,
    [string]$PfxPassword  = $env:CODE_SIGNING_CERT_PASSWORD,
    [string]$TimestampUrl = "http://timestamp.sectigo.com",
    [string]$ReleaseDir   = "$PSScriptRoot\..\target\x86_64-pc-windows-msvc\release",
    [string]$MsiPath      = "$PSScriptRoot\dist\personel-agent.msi"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Configuration check ──────────────────────────────────────────────────────

if ([string]::IsNullOrWhiteSpace($PfxPath) -or [string]::IsNullOrWhiteSpace($PfxPassword)) {
    Write-Host ""
    Write-Host "Code signing not configured — skipping." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  To enable signing, set both environment variables:" -ForegroundColor Yellow
    Write-Host "    `$env:CODE_SIGNING_CERT_PFX      = 'C:\path\to\codesign.pfx'" -ForegroundColor Yellow
    Write-Host "    `$env:CODE_SIGNING_CERT_PASSWORD = '<pfx-password>'" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  Or add them to your local .env file (do NOT commit)." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  Reference runbook: docs/operations/code-signing.md" -ForegroundColor Yellow
    Write-Host ""
    exit 0
}

if (-not (Test-Path $PfxPath)) {
    throw "PFX file not found at: $PfxPath"
}

# ── Locate signtool.exe ──────────────────────────────────────────────────────

function Find-SignTool {
    # 1. PATH
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    # 2. Standard Windows SDK locations (highest version wins)
    $bases = @(
        "C:\Program Files (x86)\Windows Kits\10\bin",
        "C:\Program Files\Windows Kits\10\bin"
    )
    foreach ($base in $bases) {
        if (-not (Test-Path $base)) { continue }
        $candidates = Get-ChildItem -Path $base -Directory -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -match '^10\.' } |
            Sort-Object Name -Descending
        foreach ($c in $candidates) {
            $tool = Join-Path $c.FullName "x64\signtool.exe"
            if (Test-Path $tool) { return $tool }
        }
    }
    throw "signtool.exe not found. Install the Windows 10/11 SDK or add signtool to PATH."
}

$SignTool = Find-SignTool
Write-Host "signtool: $SignTool" -ForegroundColor Cyan
Write-Host "PFX:      $PfxPath" -ForegroundColor Cyan
Write-Host "TSA:      $TimestampUrl" -ForegroundColor Cyan
Write-Host ""

# ── Resolve files to sign ────────────────────────────────────────────────────

if (-not (Test-Path $ReleaseDir)) {
    throw "Release directory not found: $ReleaseDir (run build-msi.ps1 first)"
}

$ResolvedReleaseDir = (Resolve-Path $ReleaseDir).Path
$BinNames = @("personel-agent.exe", "personel-agent-watchdog.exe", "enroll.exe")
$BinPaths = @()
foreach ($name in $BinNames) {
    $p = Join-Path $ResolvedReleaseDir $name
    if (-not (Test-Path $p)) {
        throw "Expected binary not found: $p"
    }
    $BinPaths += $p
}

$SignTargets = $BinPaths
if (Test-Path $MsiPath) {
    $SignTargets += (Resolve-Path $MsiPath).Path
} else {
    Write-Host "MSI not found at $MsiPath — skipping MSI signing (binaries only)." -ForegroundColor Yellow
}

# ── Sign ─────────────────────────────────────────────────────────────────────

Write-Host "Signing $($SignTargets.Count) file(s)..." -ForegroundColor Yellow
$signArgs = @(
    "sign",
    "/f", $PfxPath,
    "/p", $PfxPassword,
    "/tr", $TimestampUrl,
    "/td", "sha256",
    "/fd", "sha256",
    "/d",  "Personel Agent",
    "/du", "https://github.com/sermetkartal/personel"
) + $SignTargets

& $SignTool @signArgs
if ($LASTEXITCODE -ne 0) {
    throw "signtool sign failed with exit code $LASTEXITCODE"
}

# ── Verify ───────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "Verifying signatures..." -ForegroundColor Yellow
$verifyArgs = @("verify", "/pa", "/v") + $SignTargets
& $SignTool @verifyArgs
if ($LASTEXITCODE -ne 0) {
    throw "signtool verify failed with exit code $LASTEXITCODE"
}

Write-Host ""
Write-Host "All artifacts signed + verified successfully." -ForegroundColor Green
foreach ($t in $SignTargets) {
    Write-Host "  signed: $t" -ForegroundColor Green
}
