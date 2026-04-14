#!/usr/bin/env bash
# sign-msi.sh — local MSI signing for dev workflow.
#
# Faz 16 #171 — operator helper script. Wraps signtool.exe (when run under
# WSL with access to the Windows toolchain) OR AzureSignTool (when run on
# a developer machine with Azure CLI logged in).
#
# Usage:
#   infra/scripts/sign-msi.sh <path-to-msi>
#
# Environment variables (pick one mode):
#
#   Mode A — Azure Key Vault (recommended for pre-EV-cert development):
#     AZURE_KEY_VAULT_URL          — e.g. https://personel-kv.vault.azure.net
#     AZURE_KEY_VAULT_CERT_NAME    — certificate name in the vault
#     (AZURE_CLIENT_ID, AZURE_TENANT_ID optional if az login is active)
#
#   Mode B — PFX file:
#     WINDOWS_CERT_PFX_PATH        — path to .pfx
#     WINDOWS_CERT_PASSWORD        — .pfx password
#
# AWAITING: EV Code Signing Certificate purchase. Until present, this
# script exits 0 with a warning banner so local dev flows are not
# blocked.

set -euo pipefail

MSI="${1:-}"
if [ -z "$MSI" ] || [ ! -f "$MSI" ]; then
  echo "Usage: $0 <path-to-msi>" >&2
  exit 2
fi

echo "[sign-msi] target: $MSI"

# --- Mode detection ---
if [ -n "${AZURE_KEY_VAULT_URL:-}" ] && [ -n "${AZURE_KEY_VAULT_CERT_NAME:-}" ]; then
  MODE="azure"
elif [ -n "${WINDOWS_CERT_PFX_PATH:-}" ] && [ -n "${WINDOWS_CERT_PASSWORD:-}" ]; then
  MODE="pfx"
else
  MODE="unsigned"
fi

echo "[sign-msi] mode: $MODE"

case "$MODE" in
  azure)
    if ! command -v AzureSignTool >/dev/null 2>&1; then
      echo "[sign-msi] ERROR: AzureSignTool not found. Install with:" >&2
      echo "  dotnet tool install --global AzureSignTool" >&2
      exit 1
    fi
    AzureSignTool sign \
      -kvu "$AZURE_KEY_VAULT_URL" \
      -kvc "$AZURE_KEY_VAULT_CERT_NAME" \
      ${AZURE_CLIENT_ID:+-kvi "$AZURE_CLIENT_ID"} \
      ${AZURE_TENANT_ID:+-kvt "$AZURE_TENANT_ID"} \
      -kvm \
      -tr http://timestamp.digicert.com \
      -td sha256 \
      -fd sha256 \
      "$MSI"
    echo "[sign-msi] OK — signed via Azure Key Vault."
    ;;

  pfx)
    if ! command -v signtool.exe >/dev/null 2>&1; then
      echo "[sign-msi] ERROR: signtool.exe not on PATH. Run from WSL with" >&2
      echo "           the Windows SDK x64 bin folder added to PATH, or use" >&2
      echo "           PowerShell instead of bash." >&2
      exit 1
    fi
    signtool.exe sign \
      /f "$WINDOWS_CERT_PFX_PATH" \
      /p "$WINDOWS_CERT_PASSWORD" \
      /tr http://timestamp.digicert.com \
      /td sha256 \
      /fd sha256 \
      "$MSI"
    echo "[sign-msi] OK — signed via PFX file."
    ;;

  unsigned)
    cat >&2 <<'BANNER'
[sign-msi] ================================================================
[sign-msi]   WARNING: MSI LEFT UNSIGNED
[sign-msi]
[sign-msi]   No signing secrets found in environment. The MSI will be
[sign-msi]   delivered unsigned — acceptable for dev/pilot ONLY.
[sign-msi]
[sign-msi]   Production deploy MUST sign. Purchase the EV Code Signing
[sign-msi]   certificate (~$700/year, Sectigo), place it in Azure Key
[sign-msi]   Vault, and export:
[sign-msi]
[sign-msi]     AZURE_KEY_VAULT_URL=...
[sign-msi]     AZURE_KEY_VAULT_CERT_NAME=...
[sign-msi]
[sign-msi]   Then re-run this script.
[sign-msi] ================================================================
BANNER
    exit 0
    ;;
esac
