#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_REGISTRY="${HCDR_BASE_IMAGE_REGISTRY:-}"
IMAGE_LIST="${HCDR_BASE_IMAGE_LIST:-${SCRIPT_DIR}/base-image-list.txt}"
USERNAME="${HCDR_HARBOR_USERNAME:-}"
PASSWORD="${HCDR_HARBOR_PASSWORD:-}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Sync build base images to a Harbor project.

Usage:
  ./sync-base-images.sh --registry HOST:PORT/base-images [options]

Options:
  --registry PREFIX     Target Harbor project prefix, for example 192.168.8.149:5001/base-images.
  --image-list PATH     Base image list file, default: ./base-image-list.txt.
  --username USER       Target Harbor username.
  --password PASSWORD   Target Harbor password.
  --execute             Run docker pull/tag/push. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) TARGET_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --image-list) IMAGE_LIST="${2:?missing value for --image-list}"; shift 2 ;;
    --username) USERNAME="${2:?missing value for --username}"; shift 2 ;;
    --password) PASSWORD="${2:?missing value for --password}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${TARGET_REGISTRY}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
if [[ ! -f "${IMAGE_LIST}" ]]; then
  echo "image list not found: ${IMAGE_LIST}" >&2
  exit 1
fi

TARGET_REGISTRY="${TARGET_REGISTRY%/}"
TARGET_HOST="${TARGET_REGISTRY%%/*}"

cat <<EOF
Base image sync plan

Target registry: ${TARGET_REGISTRY}
Image list:      ${IMAGE_LIST}
Docker login:    $([[ -n "${USERNAME}" && -n "${PASSWORD}" ]] && echo "yes (${USERNAME}@${TARGET_HOST})" || echo "no")
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  echo
  echo "Dry-run mode. Add --execute to sync base images."
  exit 0
fi

if [[ -n "${USERNAME}" && -n "${PASSWORD}" ]]; then
  printf '%s' "${PASSWORD}" | docker login "${TARGET_HOST}" -u "${USERNAME}" --password-stdin
fi

while read -r source_image target_repo target_tag rest; do
  [[ -n "${source_image:-}" ]] || continue
  [[ "${source_image}" != \#* ]] || continue
  if [[ -z "${target_repo:-}" || -z "${target_tag:-}" || -n "${rest:-}" ]]; then
    echo "invalid image list line: ${source_image} ${target_repo:-} ${target_tag:-} ${rest:-}" >&2
    exit 1
  fi

  target_ref="${TARGET_REGISTRY}/${target_repo}:${target_tag}"
  echo "Sync base image: ${source_image} -> ${target_ref}"
  docker pull "${source_image}"
  docker tag "${source_image}" "${target_ref}"
  docker push "${target_ref}"
done < "${IMAGE_LIST}"

echo "Base image sync completed."
