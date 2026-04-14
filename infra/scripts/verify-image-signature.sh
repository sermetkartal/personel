#!/usr/bin/env bash
# verify-image-signature.sh — local cosign verification for Personel images.
#
# Faz 16 #170 — operator tool for verifying that a container image pulled
# from ghcr.io was produced by the Personel CI publish-images workflow
# and has not been tampered with since.
#
# Usage:
#   infra/scripts/verify-image-signature.sh <service> [tag-or-digest]
#
# Examples:
#   infra/scripts/verify-image-signature.sh api
#   infra/scripts/verify-image-signature.sh gateway sha-abc1234
#   infra/scripts/verify-image-signature.sh api "@sha256:deadbeef..."
#
# Requires: cosign v2.x on PATH. Install:
#   curl -sSLo cosign https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64
#   chmod +x cosign && sudo mv cosign /usr/local/bin/
#
# Exit 0 on valid signature, 1 on any failure.

set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "Usage: $0 <service> [tag-or-digest]" >&2
  echo "  service: api | gateway | enricher | console | portal | ml-classifier | ocr-service | uba-detector" >&2
  exit 2
fi

SERVICE="$1"
TAG="${2:-latest}"

REGISTRY="${REGISTRY:-ghcr.io}"
REPO_OWNER="${REPO_OWNER:-sermetkartal}"
IMAGE_PREFIX="${REPO_OWNER}/personel"

# Identity regex MUST match the real workflow path on main. Update when
# the workflow is renamed.
IDENTITY_REGEX="${IDENTITY_REGEX:-^https://github.com/${REPO_OWNER}/personel/.github/workflows/publish-images.yml@}"
ISSUER="${ISSUER:-https://token.actions.githubusercontent.com}"

if [[ "$TAG" == @* ]]; then
  REF="${REGISTRY}/${IMAGE_PREFIX}-${SERVICE}${TAG}"
else
  REF="${REGISTRY}/${IMAGE_PREFIX}-${SERVICE}:${TAG}"
fi

echo "[verify-image] reference: $REF"
echo "[verify-image] identity regex: $IDENTITY_REGEX"
echo "[verify-image] issuer: $ISSUER"

if ! command -v cosign >/dev/null 2>&1; then
  echo "[verify-image] ERROR: cosign not found on PATH" >&2
  exit 1
fi

export COSIGN_EXPERIMENTAL=1

echo "[verify-image] Step 1/2 — verify keyless signature..."
cosign verify \
  --certificate-identity-regexp "$IDENTITY_REGEX" \
  --certificate-oidc-issuer "$ISSUER" \
  "$REF" > /dev/null

echo "[verify-image] Step 2/2 — verify SBOM attestation..."
cosign verify-attestation \
  --type spdxjson \
  --certificate-identity-regexp "$IDENTITY_REGEX" \
  --certificate-oidc-issuer "$ISSUER" \
  "$REF" > /dev/null

echo "[verify-image] OK — $REF is signed + attested by the Personel CI."
