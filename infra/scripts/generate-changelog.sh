#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Changelog Generator
# TR: Git commit geçmişinden Conventional Commits'e dayalı CHANGELOG üretir.
# EN: Generates a CHANGELOG from git commit history using Conventional Commits.
#
# Usage:
#   ./generate-changelog.sh                          # produces CHANGELOG.md from repo root
#   ./generate-changelog.sh --version 1.0.0          # generate for specific version tag
#   ./generate-changelog.sh --from v0.9.0 --to v1.0.0  # range
#   ./generate-changelog.sh --since "2026-01-01"     # date range
#   ./generate-changelog.sh --release 1.0.0          # full release cut: appends to CHANGELOG.md +
#                                                     # writes docs/releases/v1.0.0.md
#
# Conventional Commits format expected:
#   feat(scope): description     → Added
#   fix(scope): description      → Fixed
#   docs(scope): description     → Documentation
#   security(scope): description → Security
#   refactor(scope): description → Changed
#   perf(scope): description     → Performance
#   BREAKING CHANGE: ...         → Breaking Changes
#
# Anything else goes into "Other" section or is skipped depending on --strict.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "$REPO_ROOT"

VERSION=""
FROM_REF=""
TO_REF="HEAD"
SINCE=""
RELEASE_MODE=0
STRICT=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)  VERSION="$2"; shift 2 ;;
        --from)     FROM_REF="$2"; shift 2 ;;
        --to)       TO_REF="$2"; shift 2 ;;
        --since)    SINCE="$2"; shift 2 ;;
        --release)  RELEASE_MODE=1; VERSION="$2"; shift 2 ;;
        --strict)   STRICT=1; shift ;;
        --help|-h)
            sed -n '2,22p' "$0"
            exit 0 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

# Determine range
RANGE_ARG=""
if [[ -n "$FROM_REF" ]]; then
    RANGE_ARG="${FROM_REF}..${TO_REF}"
elif [[ -n "$SINCE" ]]; then
    RANGE_ARG="--since=${SINCE}"
else
    # Default: from most recent tag to HEAD
    LAST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    if [[ -n "$LAST_TAG" ]]; then
        RANGE_ARG="${LAST_TAG}..HEAD"
    fi
fi

# Extract commits
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if [[ -n "$RANGE_ARG" ]]; then
    git log ${RANGE_ARG} --pretty=format:'%H|%s|%b---END---' --no-merges > "$TMP"
else
    git log --pretty=format:'%H|%s|%b---END---' --no-merges > "$TMP"
fi

# Parse into categorized arrays (initialize empty so `set -u` is safe)
FEAT=()
FIX=()
DOCS=()
SECURITY=()
REFACTOR=()
PERF=()
BREAKING=()
OTHER=()

# Process commits — split by ---END--- sentinel
while IFS= read -r line; do
    if [[ -z "$line" || "$line" == "---END---" ]]; then
        continue
    fi
    # Only process the first line of each entry (subject)
    if [[ "$line" =~ ^([a-f0-9]{40})\|(.+)\|(.*) ]]; then
        HASH="${BASH_REMATCH[1]}"
        SUBJECT="${BASH_REMATCH[2]}"
        BODY="${BASH_REMATCH[3]:-}"
        SHORT_HASH="${HASH:0:7}"

        # BREAKING CHANGE in body or subject with "!"
        if [[ "$SUBJECT" == *"!:"* ]] || [[ "$BODY" == *"BREAKING CHANGE:"* ]]; then
            BREAKING+=("${SUBJECT} (${SHORT_HASH})")
            continue
        fi

        case "$SUBJECT" in
            feat*)     FEAT+=("${SUBJECT} (${SHORT_HASH})") ;;
            fix*)      FIX+=("${SUBJECT} (${SHORT_HASH})") ;;
            docs*)     DOCS+=("${SUBJECT} (${SHORT_HASH})") ;;
            security*) SECURITY+=("${SUBJECT} (${SHORT_HASH})") ;;
            refactor*) REFACTOR+=("${SUBJECT} (${SHORT_HASH})") ;;
            perf*)     PERF+=("${SUBJECT} (${SHORT_HASH})") ;;
            *)
                if [[ $STRICT -eq 0 ]]; then
                    OTHER+=("${SUBJECT} (${SHORT_HASH})")
                fi
                ;;
        esac
    fi
done < <(tr '\0' '\n' < "$TMP" | sed 's/---END---/\n/g')

# Format output
OUT=$(mktemp)
trap 'rm -f "$TMP" "$OUT"' EXIT

if [[ -n "$VERSION" ]]; then
    echo "## [${VERSION}] — $(date +%Y-%m-%d)" > "$OUT"
else
    echo "## Unreleased — $(date +%Y-%m-%d)" > "$OUT"
fi
echo "" >> "$OUT"

append_section() {
    local title="$1"
    shift
    local -a items=("$@")
    if [[ ${#items[@]} -gt 0 ]]; then
        echo "### ${title}" >> "$OUT"
        echo "" >> "$OUT"
        for item in "${items[@]}"; do
            echo "- ${item}" >> "$OUT"
        done
        echo "" >> "$OUT"
    fi
}

# Use ${arr[@]+"${arr[@]}"} expansion pattern to be safe under set -u
# when the array is empty.
[[ ${#BREAKING[@]} -gt 0 ]] && append_section "Breaking Changes" "${BREAKING[@]}"
[[ ${#FEAT[@]} -gt 0 ]]     && append_section "Added" "${FEAT[@]}"
[[ ${#FIX[@]} -gt 0 ]]      && append_section "Fixed" "${FIX[@]}"
[[ ${#SECURITY[@]} -gt 0 ]] && append_section "Security" "${SECURITY[@]}"
[[ ${#PERF[@]} -gt 0 ]]     && append_section "Performance" "${PERF[@]}"
[[ ${#REFACTOR[@]} -gt 0 ]] && append_section "Changed" "${REFACTOR[@]}"
[[ ${#DOCS[@]} -gt 0 ]]     && append_section "Documentation" "${DOCS[@]}"
[[ ${#OTHER[@]} -gt 0 ]]    && append_section "Other" "${OTHER[@]}"

if [[ $RELEASE_MODE -eq 1 && -n "$VERSION" ]]; then
    # Full release cut mode
    # 1. Append to CHANGELOG.md (after the header)
    mkdir -p docs/releases
    if [[ -f CHANGELOG.md ]]; then
        # Insert after the first H1 header
        TMP_CHL=$(mktemp)
        awk -v NEW_FILE="$OUT" '
            NR==1 && /^# / { print; next }
            NR==2 && /^$/  { print; print ""; while ((getline line < NEW_FILE) > 0) print line; next }
            { print }
        ' CHANGELOG.md > "$TMP_CHL"
        mv "$TMP_CHL" CHANGELOG.md
    else
        cat > CHANGELOG.md <<EOF
# Changelog

All notable changes to Personel Platform are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/) and this
project adheres to [Semantic Versioning](https://semver.org/).

EOF
        cat "$OUT" >> CHANGELOG.md
    fi

    # 2. Write dedicated release notes file
    RELEASE_FILE="docs/releases/v${VERSION}.md"
    cat > "$RELEASE_FILE" <<EOF
# Personel v${VERSION} — Release Notes

**Release Date**: $(date +%Y-%m-%d)
**Git tag**: \`v${VERSION}\`

$(cat "$OUT")

---

## Upgrade Instructions

\`\`\`bash
cd /opt/personel
git fetch --tags
git checkout v${VERSION}
sudo ./infra/install.sh --upgrade
\`\`\`

## Verification

After upgrade, verify health:

\`\`\`bash
curl https://<host>/healthz
curl https://<host>/public/status.json | jq '.overall'
\`\`\`

## Rollback

If issues arise, rollback:

\`\`\`bash
git checkout v$(git describe --tags --abbrev=0 HEAD~1)
sudo ./infra/install.sh --upgrade
\`\`\`

## Support

- Email: destek@personel.local
- SLA: See \`docs/customer-success/support-sla.md\`
EOF

    echo "Release notes written:"
    echo "  CHANGELOG.md (top of file)"
    echo "  ${RELEASE_FILE}"
else
    cat "$OUT"
fi
