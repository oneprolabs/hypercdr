#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

VERSION="${1:-}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
PLATFORM_URL="${HCDR_PLATFORM_URL:-https://${DEFAULT_HOST}:3002}"
RELEASE_TOKEN_FILE="${HCDR_RELEASE_TOKEN_FILE:-/var/lib/hypercdr/release-token}"

require_version "${VERSION}"
require_registry "${REGISTRY}"
require_cmd curl

REGISTRY="${REGISTRY%/}"
IMAGE="${REGISTRY}/comm-agent:${VERSION}"
[[ -r "${RELEASE_TOKEN_FILE}" ]] || die "release token file is not readable: ${RELEASE_TOKEN_FILE}"
RELEASE_TOKEN="$(tr -d '\r\n' < "${RELEASE_TOKEN_FILE}")"
[[ -n "${RELEASE_TOKEN}" ]] || die "release token file is empty: ${RELEASE_TOKEN_FILE}"

log "Publishing comm-agent candidate ${VERSION}"
log "Image: ${IMAGE}"
curl_args=(
  -fsS --max-time 30 -X POST
  "${PLATFORM_URL%/}/api/v1/component-releases"
  -H "Content-Type: application/json"
  -H "X-HyperCDR-Release-Token: ${RELEASE_TOKEN}"
)
if [[ -n "${HCDR_PLATFORM_CA_FILE:-}" ]]; then
  curl_args+=(--cacert "${HCDR_PLATFORM_CA_FILE}")
else
  curl_args+=(--insecure)
fi
curl "${curl_args[@]}" --data "{\"component\":\"comm-agent\",\"version\":\"${VERSION}\",\"image\":\"${IMAGE}\",\"releaseNotes\":\"Adds remote diagnostic log collection and failure context reporting.\"}" >/dev/null

log "Comm-agent candidate registered: ${VERSION}"
