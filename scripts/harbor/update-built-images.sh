#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET_REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
TAG="${HCDR_IMAGE_TAG:-}"
GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"
NPM_REGISTRY="${HCDR_BUILD_NPM_REGISTRY:-https://registry.npmmirror.com}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Build updated HyperCDR platform and agent images, then push them to Harbor.

Usage:
  ./update-built-images.sh --registry HOST:PORT/hypercdr [options]

Options:
  --registry PREFIX     Target Harbor project prefix, for example 192.168.8.149:5001/hypercdr.
  --tag TAG             Release tag in vYYYYMMDD.N format. Required.
  --goproxy URL         Go module proxy, default: https://goproxy.cn,direct.
  --npm-registry URL    npm registry, default: https://registry.npmmirror.com.
  --execute             Build and push. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) TARGET_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --tag) TAG="${2:?missing value for --tag}"; shift 2 ;;
    --goproxy) GOPROXY="${2:?missing value for --goproxy}"; shift 2 ;;
    --npm-registry) NPM_REGISTRY="${2:?missing value for --npm-registry}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${TARGET_REGISTRY}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
if [[ ! "${TAG}" =~ ^v[0-9]{8}\.[0-9]+$ ]]; then
  echo "--tag is required and must match vYYYYMMDD.N" >&2
  exit 2
fi

TARGET_REGISTRY="${TARGET_REGISTRY%/}"

cat <<EOF
HyperCDR image update plan

Target registry: ${TARGET_REGISTRY}
Tag:             ${TAG}
Go proxy:        ${GOPROXY}
NPM registry:    ${NPM_REGISTRY}
Images:
  ${TARGET_REGISTRY}/platform-api:${TAG}
  ${TARGET_REGISTRY}/platform-frontend:${TAG}
  ${TARGET_REGISTRY}/comm-agent:${TAG}
  ${TARGET_REGISTRY}/platform-upgrader:${TAG}
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  echo
  echo "Dry-run mode. Add --execute to build and push images."
  exit 0
fi

"${ROOT_DIR}/scripts/release/build-release.sh" "${TAG}" \
  --registry "${TARGET_REGISTRY}" \
  --goproxy "${GOPROXY}" \
  --npm-registry "${NPM_REGISTRY}" \
  --push
