#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
POSTGRES_SOURCE="${HCDR_POSTGRES_SOURCE_IMAGE:-postgres:16}"
VELERO_TAG="${HCDR_VELERO_IMAGE_TAG:-v1.17.1-hcdr.1-20260716}"
FORCE_VELERO_BUILD="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --force-velero-build) FORCE_VELERO_BUILD="true"; shift ;;
    -h|--help) echo "Usage: publish-runtime-images.sh --registry HOST/NAMESPACE [--force-velero-build]"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

require_registry "${REGISTRY}"
require_cmd docker
REGISTRY="${REGISTRY%/}"

mirror_image() {
  local source="$1" target="$2"
  log "Mirroring ${source} to ${target}"
  docker pull "${source}"
  docker tag "${source}" "${target}"
  docker push "${target}"
}

mirror_image "${POSTGRES_SOURCE}" "${REGISTRY}/postgres:16"

VELERO_TARGET="${REGISTRY}/velero:${VELERO_TAG}"
if [[ "${FORCE_VELERO_BUILD}" == "true" ]] || ! docker manifest inspect "${VELERO_TARGET}" >/dev/null 2>&1; then
  log "Building pinned HyperCDR Velero image from third_party/velero"
  "${ROOT_DIR}/third_party/velero/deployments/build-velero-image.sh" \
    --registry "${REGISTRY}" --tag "${VELERO_TAG}" --push
else
  log "Velero image already published: ${VELERO_TARGET}"
fi
