#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
VERSION=""

usage() {
  cat <<'USAGE'
Push HyperCDR release images.

Usage:
  push-release.sh <version> --registry REGISTRY
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
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

for name in platform-api platform-frontend comm-agent; do
  image="$(image_ref "${REGISTRY}" "${name}" "${VERSION}")"
  log "Pushing ${image}"
  docker push "${image}"
done
