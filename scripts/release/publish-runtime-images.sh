#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
POSTGRES_SOURCE="${HCDR_POSTGRES_SOURCE_IMAGE:-postgres:16}"
VELERO_TAG="${HCDR_VELERO_IMAGE_TAG:-v1.18.2-hcdr.7}"
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
  mirror_release_image "$source" "$target"
}

mirror_image "${POSTGRES_SOURCE}" "$(image_ref "${REGISTRY}" "postgres" "16")"

VELERO_TARGET="$(image_ref "${REGISTRY}" "velero" "${VELERO_TAG}")"
source "${SCRIPT_DIR}/dependency-fingerprint.sh"
fingerprint="$(velero_input_fingerprint)"
cache_image="$(image_ref "$REGISTRY" velero "${VELERO_TAG}-input-${fingerprint}")"
cache_digest=''
if cache_digest="$(docker_manifest_digest "$cache_image")"; then
  :
else
  status=$?
  [[ "$status" == 3 ]] || exit "$status"
fi
if [[ "${FORCE_VELERO_BUILD}" == "true" || -z "$cache_digest" ]]; then
  log "Building pinned HyperCDR Velero image from third_party/velero"
  "${SCRIPT_DIR}/build-velero.sh" \
    --registry "${REGISTRY}" --tag "${VELERO_TAG}"
  docker tag "$VELERO_TARGET" "$cache_image"
  docker_push_with_retry "$cache_image"
  mirror_release_image "$cache_image" "$VELERO_TARGET"
else
  log "Reusing Velero input fingerprint: $fingerprint"
  mirror_release_image "$cache_image" "$VELERO_TARGET"
fi
