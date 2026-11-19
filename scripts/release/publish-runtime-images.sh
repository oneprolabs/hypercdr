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
  log "Mirroring ${source} to ${target}"
  docker pull "${source}"
  docker tag "${source}" "${target}"
  docker_push_with_retry "${target}"
}

POSTGRES_TARGET="$(image_ref "${REGISTRY}" "postgres" "16")"
POSTGRES_DIGEST="$(jq -er '.runtime.postgres.targetDigest' "${ROOT_DIR}/packaging/dependencies/lock.json")"
if dependency_target_matches_digest "${POSTGRES_TARGET}" "${POSTGRES_DIGEST}"; then
  log "Reusing unchanged PostgreSQL dependency: ${POSTGRES_TARGET}"
else
  status=$?
  [[ "$status" == 1 ]] || die "PostgreSQL verification failed; refusing automatic republish"
  mirror_image "${POSTGRES_SOURCE}" "${POSTGRES_TARGET}"
fi

VELERO_TARGET="$(image_ref "${REGISTRY}" "velero" "${VELERO_TAG}")"
VELERO_DIGEST="$(jq -er '.runtime.velero.targetDigest' "${ROOT_DIR}/packaging/dependencies/lock.json")"
if [[ "${FORCE_VELERO_BUILD}" != "true" ]]; then
  if dependency_target_matches_digest "${VELERO_TARGET}" "${VELERO_DIGEST}"; then
    log "Velero image already published: ${VELERO_TARGET}"
    exit 0
  else
    status=$?
    [[ "$status" == 1 ]] || die "Velero verification failed; refusing automatic rebuild"
  fi
fi
  log "Building pinned HyperCDR Velero image from third_party/velero"
  "${SCRIPT_DIR}/build-velero.sh" \
    --registry "${REGISTRY}" --tag "${VELERO_TAG}" --push
