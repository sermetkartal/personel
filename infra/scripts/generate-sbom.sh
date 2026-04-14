#!/usr/bin/env bash
# Faz 12 #125 — Local SBOM generator.
# Produces CycloneDX JSON SBOMs for every Personel component into ./sbom/.
#
# Usage: infra/scripts/generate-sbom.sh [output_dir]
#
# Requirements:
#   - cyclonedx-gomod (go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.7.0)
#   - cargo-cyclonedx (cargo install cargo-cyclonedx --version 0.5.5 --locked)
#   - cyclonedx-npm     (npm install -g @cyclonedx/cyclonedx-npm@1.19.3)
#   - cyclonedx-py      (pip install cyclonedx-bom==4.5.0)

set -euo pipefail

OUT_DIR="${1:-sbom}"
mkdir -p "$OUT_DIR"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
cd "$REPO_ROOT"

say() { printf '\033[1;34m[sbom]\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33m[sbom]\033[0m %s\n' "$*" >&2; }

# --- Go projects ---
for proj in apps/api apps/gateway apps/qa; do
  if command -v cyclonedx-gomod >/dev/null 2>&1 && [ -f "$proj/go.mod" ]; then
    name=$(echo "$proj" | tr '/' '-')
    say "Go SBOM: $proj"
    cyclonedx-gomod mod -licenses -json \
      -output "$OUT_DIR/${name}.cdx.json" "$proj" || warn "$proj failed"
  else
    warn "Skipping Go SBOM for $proj (cyclonedx-gomod missing or no go.mod)"
  fi
done

# --- Rust workspace ---
if command -v cargo >/dev/null 2>&1 && cargo cyclonedx --help >/dev/null 2>&1; then
  say "Rust SBOM: apps/agent"
  (cd apps/agent && cargo cyclonedx --format json --all)
  find apps/agent -name '*.cdx.json' -exec cp {} "$OUT_DIR/" \;
else
  warn "Skipping Rust SBOM (cargo-cyclonedx missing)"
fi

# --- Node projects ---
if command -v cyclonedx-npm >/dev/null 2>&1; then
  for proj in apps/console apps/portal; do
    if [ -f "$proj/package.json" ]; then
      name=$(basename "$proj")
      say "Node SBOM: $proj"
      (cd "$proj" && cyclonedx-npm --output-format json \
        --output-file "$REPO_ROOT/$OUT_DIR/node-${name}.cdx.json" 2>/dev/null) \
        || warn "$proj failed"
    fi
  done
else
  warn "Skipping Node SBOM (cyclonedx-npm missing)"
fi

# --- Python projects ---
if command -v cyclonedx-py >/dev/null 2>&1; then
  for proj in apps/ml-classifier apps/ocr-service apps/uba-detector; do
    name=$(echo "$proj" | tr '/' '-')
    if [ -f "$proj/requirements.txt" ]; then
      say "Python SBOM: $proj"
      cyclonedx-py requirements "$proj/requirements.txt" \
        --output-format json \
        --output-file "$OUT_DIR/${name}.cdx.json" || warn "$proj failed"
    elif [ -f "$proj/pyproject.toml" ]; then
      say "Python SBOM (poetry): $proj"
      (cd "$proj" && cyclonedx-py poetry --output-format json \
        --output-file "$REPO_ROOT/$OUT_DIR/${name}.cdx.json") || warn "$proj failed"
    else
      warn "Skipping Python SBOM for $proj (no requirements.txt or pyproject.toml)"
    fi
  done
else
  warn "Skipping Python SBOMs (cyclonedx-py missing)"
fi

say "Done. SBOMs in $OUT_DIR/"
ls -la "$OUT_DIR/" 2>/dev/null || true
