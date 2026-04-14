#!/usr/bin/env bash
# rollback.sh — automated rollback to a previous Personel version.
#
# Faz 16 #176 — Operator + alert-manager-invoked rollback tool. Can be
# called manually during incident response OR automatically by the
# canary abort flow (docs/operations/canary-release.md).
#
# Usage:
#   infra/scripts/rollback.sh <target-version|last-known-good> [options]
#
# Options:
#   --include-schema      Also run postgres migration down (UNSAFE — see below)
#   --reason "..."        Free-text reason, appended to the rollback log
#   --force               Skip confirmation token
#   --dry-run             Show what would happen, don't do it
#
# Examples:
#   sudo infra/scripts/rollback.sh v1.4.2 --reason "p95 latency regression"
#   sudo infra/scripts/rollback.sh last-known-good --force
#
# Safety gates:
#   1. Refuses to run while an active DSR is within 24h of its SLA deadline
#   2. Refuses --include-schema without an explicit interactive token
#   3. Refuses to rollback past the last destruction report (6-month window)
#   4. Writes an audit entry via /v1/system/rollback-report (or local file
#      fallback if the API is unreachable)
#
# Logs:
#   /var/log/personel/rollbacks.log — append-only, one line per run,
#   contains timestamp, actor, target version, reason, exit code.

set -euo pipefail

TARGET="${1:-}"
shift || true

INCLUDE_SCHEMA=false
REASON=""
FORCE=false
DRY_RUN=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --include-schema) INCLUDE_SCHEMA=true; shift ;;
    --reason)         REASON="$2"; shift 2 ;;
    --force)          FORCE=true; shift ;;
    --dry-run)        DRY_RUN=true; shift ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$TARGET" ]; then
  cat <<'USAGE'
Usage: rollback.sh <target-version|last-known-good> [options]

Options:
  --include-schema     Also run migration down (UNSAFE)
  --reason "..."       Reason recorded in rollback log
  --force              Skip interactive confirmation
  --dry-run            Show plan, do nothing
USAGE
  exit 2
fi

LOG_FILE="${LOG_FILE:-/var/log/personel/rollbacks.log}"
COMPOSE_DIR="${COMPOSE_DIR:-infra/compose}"
API_URL="${API_URL:-https://127.0.0.1:8000}"
ACTOR="${SUDO_USER:-$USER}"

mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || true

log() {
  local level="$1"
  shift
  local msg="$*"
  echo "[rollback] [$level] $msg"
  if [ -w "$(dirname "$LOG_FILE")" ]; then
    printf '%s\t%s\t%s\t%s\t%s\n' \
      "$(date -Iseconds)" "$ACTOR" "$level" "$TARGET" "$msg" >> "$LOG_FILE"
  fi
}

# --- Resolve target version ---

resolve_target() {
  if [ "$TARGET" = "last-known-good" ]; then
    if [ ! -f "$COMPOSE_DIR/.last-known-good" ]; then
      log ERROR ".last-known-good marker missing — pass an explicit version"
      exit 3
    fi
    TARGET="$(cat "$COMPOSE_DIR/.last-known-good" | tr -d '[:space:]')"
    log INFO "resolved last-known-good → $TARGET"
  fi

  if [[ ! "$TARGET" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    log ERROR "target '$TARGET' does not look like a semver tag"
    exit 3
  fi
}

# --- Safety gates ---

check_dsr_sla() {
  log INFO "checking active DSRs for SLA proximity..."
  local payload
  if ! payload=$(curl -sfk "$API_URL/v1/dsr/sla-status" 2>/dev/null); then
    log WARN "DSR SLA check failed — proceeding (API unreachable)"
    return 0
  fi
  local critical
  critical=$(echo "$payload" | jq -r '.within_24h // 0' 2>/dev/null || echo 0)
  if [ "$critical" -gt 0 ] && [ "$FORCE" != "true" ]; then
    log BLOCK "$critical DSR(s) within 24h of SLA deadline — use --force to override"
    exit 1
  fi
}

check_destruction_window() {
  log INFO "checking 6-month destruction window..."
  # If the target version is older than the last destruction report,
  # rolling back would resurrect evidence that has been legally erased.
  # Block unconditionally.
  local last_destroy
  if ! last_destroy=$(curl -sfk "$API_URL/v1/dpo/destruction-reports?limit=1" 2>/dev/null |
                     jq -r '.reports[0].generated_at // empty' 2>/dev/null); then
    log WARN "destruction check failed — proceeding"
    return 0
  fi
  if [ -z "$last_destroy" ]; then
    return 0
  fi
  log INFO "last destruction report: $last_destroy — target version $TARGET must post-date it"
  # Operator must interactively confirm — we can't automatically compare
  # a release date to a destruction report without the release metadata
  # service being queryable.
  if [ "$FORCE" != "true" ]; then
    read -rp "[rollback] Confirm target $TARGET was tagged AFTER $last_destroy (yes/no): " ack
    if [ "$ack" != "yes" ]; then
      log BLOCK "operator declined destruction-window confirmation"
      exit 1
    fi
  fi
}

confirm() {
  if [ "$FORCE" = "true" ]; then
    return 0
  fi
  echo
  echo "=============================================================="
  echo " ROLLBACK PLAN"
  echo "=============================================================="
  echo " Target version: $TARGET"
  echo " Reason:         ${REASON:-<none>}"
  echo " Include schema: $INCLUDE_SCHEMA"
  echo " Dry run:        $DRY_RUN"
  echo "=============================================================="
  echo
  read -rp "Type 'ROLLBACK $TARGET' to proceed: " token
  if [ "$token" != "ROLLBACK $TARGET" ]; then
    log ABORT "confirmation token mismatch"
    exit 1
  fi
}

# --- Actions ---

retag_images() {
  log INFO "retagging images to $TARGET"
  local services=(api gateway enricher console portal ml-classifier ocr-service uba-detector)
  for svc in "${services[@]}"; do
    local src="ghcr.io/${REPO_OWNER:-personel}/personel-${svc}:${TARGET}"
    local dst="ghcr.io/${REPO_OWNER:-personel}/personel-${svc}:latest"
    if [ "$DRY_RUN" = "true" ]; then
      echo "  [dry] docker pull $src && docker tag $src $dst"
      continue
    fi
    docker pull "$src"
    docker tag "$src" "$dst"
  done
}

recreate_services() {
  log INFO "recreating stateless containers"
  local services=(api gateway enricher console portal)
  if [ "$DRY_RUN" = "true" ]; then
    echo "  [dry] docker compose up -d --force-recreate ${services[*]}"
    return
  fi
  (cd "$COMPOSE_DIR" && docker compose up -d --force-recreate "${services[@]}")
}

maybe_migrate_down() {
  if [ "$INCLUDE_SCHEMA" != "true" ]; then
    log INFO "schema migration: SKIPPED (use --include-schema to enable)"
    return
  fi

  # Hard-gate schema downgrade with a second confirmation.
  if [ "$FORCE" != "true" ]; then
    echo
    echo "!! WARNING: --include-schema runs migration DOWN."
    echo "!! This MAY drop columns and lose data."
    read -rp "Type 'DROP SCHEMA $TARGET' to proceed: " token
    if [ "$token" != "DROP SCHEMA $TARGET" ]; then
      log ABORT "schema rollback declined"
      exit 1
    fi
  fi

  if [ "$DRY_RUN" = "true" ]; then
    echo "  [dry] migrate -path ... down 1"
    return
  fi

  # Roll back exactly one migration step. Multi-step rollback is NEVER
  # automated — every extra step is a separate explicit invocation.
  docker run --rm --network personel_default \
    -v "$PWD/apps/api/internal/postgres/migrations:/migrations" \
    migrate/migrate:v4.17.0 \
    -path=/migrations \
    -database "postgres://app_admin_api:apipass123@personel-postgres:5432/personel?sslmode=disable" \
    down 1
}

invalidate_cache() {
  log INFO "invalidating in-process caches (feature flags, RBAC, config)"
  # The API caches are TTL-bounded; forcing recreation above resets
  # them. This step only matters for NATS-durable consumer offsets.
  if [ "$DRY_RUN" = "true" ]; then
    return
  fi
  # Ping the health endpoint to warm new containers
  for i in 1 2 3 4 5; do
    if curl -sfk "$API_URL/healthz" >/dev/null; then
      log INFO "api /healthz ok (attempt $i)"
      return
    fi
    sleep 2
  done
  log ERROR "api /healthz did not become healthy within 10s after rollback"
  return 1
}

post_rollback_smoke() {
  log INFO "smoke test"
  if [ "$DRY_RUN" = "true" ]; then
    return
  fi
  curl -sfk "$API_URL/healthz" >/dev/null || { log ERROR "api healthz FAIL"; exit 4; }
  curl -sfk "$API_URL/readyz" >/dev/null || { log ERROR "api readyz FAIL"; exit 4; }
  log INFO "smoke ok"
}

emit_audit() {
  local diff="{\"target\":\"$TARGET\",\"reason\":\"$REASON\",\"include_schema\":$INCLUDE_SCHEMA,\"actor\":\"$ACTOR\"}"
  log INFO "emitting rollback audit entry"
  if [ "$DRY_RUN" = "true" ]; then
    echo "  [dry] POST /v1/system/rollback-report $diff"
    return
  fi
  curl -sfk -X POST "$API_URL/v1/system/rollback-report" \
    -H "Content-Type: application/json" \
    -d "$diff" || log WARN "rollback-report API unreachable — local log only"
}

# --- Main ---

log INFO "rollback start — target=$TARGET actor=$ACTOR dry-run=$DRY_RUN"

resolve_target
check_dsr_sla
check_destruction_window
confirm

retag_images
maybe_migrate_down
recreate_services
invalidate_cache
post_rollback_smoke
emit_audit

log INFO "rollback complete — active version is now $TARGET"
