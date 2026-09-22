#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
VERSION=""
COMPONENT="${HCDR_CORE_COMPONENT:-all}"

usage() {
  cat <<'USAGE'
Push HyperCDR release images.

Usage:
  push-release.sh <version> --registry REGISTRY [--component all|platform|agents|executor]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --component) COMPONENT="${2:?missing value for --component}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *)
      if [[ -z "${VERSION}" ]]; then VERSION="$1"; shift; else die "unknown argument: $1"; fi
      ;;
  esac
done

require_version "${VERSION}"
require_registry "${REGISTRY}"
require_cmd docker
REGISTRY="${REGISTRY%/}"

case "${COMPONENT}" in
  all) images=(platform-api platform-frontend cluster-registration-executor comm-agent oadp-comm-agent) ;;
  platform) images=(platform-api platform-frontend) ;;
  agents) images=(comm-agent oadp-comm-agent) ;;
  executor) images=(cluster-registration-executor) ;;
  *) die "component must be all, platform, agents, or executor" ;;
esac

for name in "${images[@]}"; do
  image="$(image_ref "${REGISTRY}" "${name}" "${VERSION}")"
  log "Pushing ${image}"
  docker push "${image}"
done
