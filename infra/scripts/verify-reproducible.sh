#!/usr/bin/env bash
# Faz 12 #130 — Reproducible build verification.
#
# Usage:
#   infra/scripts/verify-reproducible.sh                # all components
#   infra/scripts/verify-reproducible.sh go-api         # single component
#
# Components:
#   go-api, go-gateway, go-qa, rust-agent, node-console, node-portal

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
cd "$REPO_ROOT"

TARGET="${1:-all}"
TMP_DIR=$(mktemp -d -t personel-repro-XXXXXX)
trap 'rm -rf "$TMP_DIR"' EXIT

say() { printf '\033[1;34m[repro]\033[0m %s\n' "$*" >&2; }
ok()  { printf '\033[1;32m[repro ✓]\033[0m %s\n' "$*" >&2; }
fail(){ printf '\033[1;31m[repro ✗]\033[0m %s\n' "$*" >&2; exit 1; }

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

export SOURCE_DATE_EPOCH=$(git log -1 --format=%ct 2>/dev/null || echo 1700000000)
say "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH"

verify_go() {
  local proj=$1 name=$2
  say "Go build x2: $proj"

  local out1="$TMP_DIR/${name}.1"
  local out2="$TMP_DIR/${name}.2"

  (cd "$proj" && \
    CGO_ENABLED=0 GOFLAGS=-trimpath \
    go build -trimpath -buildvcs=false \
      -ldflags='-s -w -buildid=' \
      -o "$out1" ./cmd/"$name" 2>&1 | tail -5)

  (cd "$proj" && \
    CGO_ENABLED=0 GOFLAGS=-trimpath \
    go build -trimpath -buildvcs=false \
      -ldflags='-s -w -buildid=' \
      -o "$out2" ./cmd/"$name" 2>&1 | tail -5)

  local h1=$(hash_file "$out1")
  local h2=$(hash_file "$out2")

  if [ "$h1" = "$h2" ]; then
    ok "$name: $h1"
  else
    fail "$name: MISMATCH — $h1 vs $h2"
  fi
}

verify_rust() {
  say "Rust agent build x2"
  export RUSTFLAGS='--remap-path-prefix=/=.'

  (cd apps/agent && \
    cargo clean -p personel-agent 2>&1 | tail -3 && \
    cargo build --release --locked -p personel-agent 2>&1 | tail -5)

  local out1="$TMP_DIR/agent.1"
  cp apps/agent/target/release/personel-agent* "$out1" 2>/dev/null || \
    cp apps/agent/target/release/personel-agent "$out1"

  (cd apps/agent && \
    cargo clean -p personel-agent 2>&1 | tail -3 && \
    cargo build --release --locked -p personel-agent 2>&1 | tail -5)

  local out2="$TMP_DIR/agent.2"
  cp apps/agent/target/release/personel-agent* "$out2" 2>/dev/null || \
    cp apps/agent/target/release/personel-agent "$out2"

  local h1=$(hash_file "$out1")
  local h2=$(hash_file "$out2")

  if [ "$h1" = "$h2" ]; then
    ok "rust-agent: $h1"
  else
    fail "rust-agent: MISMATCH — $h1 vs $h2"
  fi
}

case "$TARGET" in
  all)
    verify_go apps/api api
    verify_go apps/gateway gateway
    verify_go apps/gateway enricher
    verify_rust
    ;;
  go-api)       verify_go apps/api api ;;
  go-gateway)   verify_go apps/gateway gateway ;;
  go-enricher)  verify_go apps/gateway enricher ;;
  go-qa)        verify_go apps/qa simulator ;;
  rust-agent)   verify_rust ;;
  node-console|node-portal)
    say "Node reproducibility verification is currently best-effort; see docs/security/reproducible-builds.md §5"
    ;;
  *)
    fail "Unknown target: $TARGET. Valid: all, go-api, go-gateway, go-enricher, go-qa, rust-agent"
    ;;
esac

ok "All checks passed."
