#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_REGISTRY="${HCDR_SOURCE_IMAGE_REGISTRY:-}"
TARGET_REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
IMAGE_LIST="${HCDR_HARBOR_IMAGE_LIST:-${SCRIPT_DIR}/image-list.txt}"
USERNAME="${HCDR_HARBOR_USERNAME:-}"
PASSWORD="${HCDR_HARBOR_PASSWORD:-}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Sync required HyperCDR images from an existing registry to a target Harbor.

Usage:
  ./sync-required-images.sh --source-registry OLD/hypercdr --registry NEW:PORT/hypercdr [options]

Options:
  --source-registry PREFIX   Existing registry prefix, for example OLD-HOST:PORT/hypercdr.
  --registry PREFIX          Target Harbor project prefix, for example 192.168.8.149:5001/hypercdr.
  --username USER            Target Harbor username.
  --password PASSWORD        Target Harbor password.
  --execute                  Run docker pull/tag/push. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-registry) SOURCE_REGISTRY="${2:?missing value for --source-registry}"; shift 2 ;;
    --registry) TARGET_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --username) USERNAME="${2:?missing value for --username}"; shift 2 ;;
    --password) PASSWORD="${2:?missing value for --password}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${SOURCE_REGISTRY}" ]]; then
  echo "--source-registry is required" >&2
  exit 2
fi
if [[ -z "${TARGET_REGISTRY}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
if [[ ! -f "${IMAGE_LIST}" ]]; then
  echo "image list not found: ${IMAGE_LIST}" >&2
  exit 1
fi

SOURCE_REGISTRY="${SOURCE_REGISTRY%/}"
TARGET_REGISTRY="${TARGET_REGISTRY%/}"
TARGET_HOST="${TARGET_REGISTRY%%/*}"

cat <<EOF
Required HyperCDR image sync plan

Source registry: ${SOURCE_REGISTRY}
Target registry: ${TARGET_REGISTRY}
Image list:      ${IMAGE_LIST}
Docker login:    $([[ -n "${USERNAME}" && -n "${PASSWORD}" ]] && echo "yes (${USERNAME}@${TARGET_HOST})" || echo "no")
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  echo
  echo "Dry-run mode. Add --execute to sync images."
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

  if [[ "${source_image}" == hypercdr/* ]]; then
    source_ref="${SOURCE_REGISTRY}/${source_image#hypercdr/}"
  else
    source_ref="${source_image}"
  fi
  target_ref="${TARGET_REGISTRY}/${target_repo}:${target_tag}"

  echo "Sync: ${source_ref} -> ${target_ref}"
  docker pull "${source_ref}"
  docker tag "${source_ref}" "${target_ref}"
  docker push "${target_ref}"
done < "${IMAGE_LIST}"

echo "Required image sync completed."
