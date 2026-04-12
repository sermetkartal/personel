#Requires -Version 5.1
#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Windows build ortamını hazırlar: Rust + WiX 4 + Visual Studio Build Tools.

.DESCRIPTION
    Bu script Windows makinesinde Personel Agent MSI üretmek için gerekli
    tüm araçları kurar. Bir kez çalıştırılır, sonraki build'ler sadece
    build-msi.ps1 ile yapılır.

.EXAMPLE
    # Admin PowerShell'de:
    Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process
    .\setup-build-env.ps1
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Step($n, $msg) { Write-Host "`n[$n] $msg" -ForegroundColor Yellow }
function Write-Ok($msg)       { Write-Host "  OK: $msg" -ForegroundColor Green }
function Write-Skip($msg)     { Write-Host "  SKIP: $msg" -ForegroundColor DarkGray }

# ── 1. winget (package manager) ──────────────────────────────────────────────
Write-Step "1/5" "Checking winget..."
if (Get-Command winget -ErrorAction SilentlyContinue) {
    Write-Ok "winget found"
} else {
    throw "winget not found. Install App Installer from Microsoft Store first."
}

# ── 2. Visual Studio Build Tools (MSVC + Windows SDK) ────────────────────────
Write-Step "2/5" "Visual Studio Build Tools..."
$vsWhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$hasMSVC = $false
if (Test-Path $vsWhere) {
    $instances = & $vsWhere -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -format json 2>$null | ConvertFrom-Json
    if ($instances.Count -gt 0) { $hasMSVC = $true }
}
if ($hasMSVC) {
    Write-Ok "MSVC toolchain found"
} else {
    Write-Host "  Installing Visual Studio Build Tools (MSVC + Windows SDK)..." -ForegroundColor Cyan
    winget install Microsoft.VisualStudio.2022.BuildTools `
        --override "--quiet --add Microsoft.VisualStudio.Workload.VCTools --add Microsoft.VisualStudio.Component.Windows11SDK.22621 --includeRecommended" `
        --accept-source-agreements --accept-package-agreements
    Write-Ok "Build Tools installed — you may need to restart your shell"
}

# ── 3. Rust + target ──────────────────────────────────────────────────────────
Write-Step "3/5" "Rust toolchain..."
if (Get-Command rustup -ErrorAction SilentlyContinue) {
    Write-Ok "rustup found: $(rustc --version)"
    rustup target add x86_64-pc-windows-msvc 2>&1 | Out-Null
    Write-Ok "target x86_64-pc-windows-msvc ready"
} else {
    Write-Host "  Installing Rust via rustup..." -ForegroundColor Cyan
    $rustupInit = "$env:TEMP\rustup-init.exe"
    Invoke-WebRequest -Uri "https://win.rustup.rs/x86_64" -OutFile $rustupInit
    & $rustupInit -y --default-toolchain stable --target x86_64-pc-windows-msvc
    $env:PATH = "$env:USERPROFILE\.cargo\bin;$env:PATH"
    Write-Ok "Rust installed: $(rustc --version)"
}

# ── 4. WiX 4 (.NET tool) ─────────────────────────────────────────────────────
Write-Step "4/5" "WiX Toolset v4..."
if (Get-Command wix -ErrorAction SilentlyContinue) {
    Write-Ok "wix found: $(wix --version)"
} else {
    # WiX 4 is distributed as a .NET global tool
    if (-not (Get-Command dotnet -ErrorAction SilentlyContinue)) {
        Write-Host "  Installing .NET SDK (required for WiX 4)..." -ForegroundColor Cyan
        winget install Microsoft.DotNet.SDK.8 --accept-source-agreements --accept-package-agreements
        $env:PATH = "$env:ProgramFiles\dotnet;$env:PATH"
    }
    dotnet tool install --global wix --version 4.0.5
    Write-Ok "WiX 4 installed"
}

# ── 5. OpenSSL (for SQLCipher bundled build) ──────────────────────────────────
Write-Step "5/5" "OpenSSL development headers..."
$opensslDir = "$env:ProgramFiles\OpenSSL-Win64"
if (Test-Path "$opensslDir\include\openssl\crypto.h") {
    Write-Ok "OpenSSL headers found at $opensslDir"
} else {
    Write-Host "  Installing OpenSSL via winget..." -ForegroundColor Cyan
    winget install ShiningLight.OpenSSL --accept-source-agreements --accept-package-agreements
    # Set env vars for cc-rs / rusqlite build
    if (-not $env:OPENSSL_DIR) {
        [System.Environment]::SetEnvironmentVariable("OPENSSL_DIR", $opensslDir, "User")
        $env:OPENSSL_DIR = $opensslDir
    }
    Write-Ok "OpenSSL installed + OPENSSL_DIR set"
}

# Ensure OPENSSL_DIR is set for this session
if (-not $env:OPENSSL_DIR -and (Test-Path "$opensslDir\include")) {
    $env:OPENSSL_DIR = $opensslDir
    [System.Environment]::SetEnvironmentVariable("OPENSSL_DIR", $opensslDir, "User")
}

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Build environment ready!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host @"

Next steps:
  cd apps\agent\installer
  .\build-msi.ps1

Or with a baked-in gateway:
  .\build-msi.ps1 -GatewayUrl "https://personel-gw.acme.com:8443"

MSI will be at: apps\agent\installer\dist\personel-agent.msi
"@ -ForegroundColor Cyan
