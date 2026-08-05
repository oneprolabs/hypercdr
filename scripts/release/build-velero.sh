#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
REGISTRY_CONFIG="${HCDR_REGISTRY_CONFIG:-${ROOT_DIR}/config/registries.conf}"
REGISTRY_PROFILE="${HCDR_REGISTRY_PROFILE:-}"
IMAGE_TAG="${HCDR_VELERO_IMAGE_TAG:-v1.18.2-hcdr.2}"
PUSH="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --registry-config) REGISTRY_CONFIG="${2:?missing value for --registry-config}"; shift 2 ;;
    --registry-profile) REGISTRY_PROFILE="${2:?missing value for --registry-profile}"; shift 2 ;;
    --tag) IMAGE_TAG="${2:?missing value for --tag}"; shift 2 ;;
    --push) PUSH="true"; shift ;;
    -h|--help) echo "Usage: build-velero.sh [--registry-profile NAME] [--registry PREFIX] [--tag TAG] [--push]"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

if [[ -z "${REGISTRY}" ]]; then
  source "${ROOT_DIR}/scripts/lib/registry-config.sh"
  load_registry_profile "${REGISTRY_CONFIG}" "${REGISTRY_PROFILE}"
  REGISTRY="${HCDR_IMAGE_REGISTRY}"
fi
require_registry "${REGISTRY}"
args=(--registry "${REGISTRY}" --tag "${IMAGE_TAG}" --version "${IMAGE_TAG}" --build-dir "${HCDR_RUNTIME_ROOT:-${RUNTIME_ROOT}}/build/velero/${IMAGE_TAG}")
[[ "${PUSH}" == "true" ]] && args+=(--push)
"${ROOT_DIR}/third_party/velero/deployments/build-velero-image.sh" "${args[@]}"
