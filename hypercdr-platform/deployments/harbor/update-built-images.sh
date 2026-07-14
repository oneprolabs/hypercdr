#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET_REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
TAG="${HCDR_IMAGE_TAG:-dev}"
BASE_REGISTRY="${HCDR_BUILD_BASE_REGISTRY:-}"
GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"
GOSUMDB="${HCDR_BUILD_GOSUMDB:-sum.golang.google.cn}"
NPM_REGISTRY="${HCDR_BUILD_NPM_REGISTRY:-https://registry.npmmirror.com}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Build updated HyperCDR platform and agent images, then push them to Harbor.

Usage:
  ./update-built-images.sh --registry HOST:PORT/hypercdr [options]

Options:
  --registry PREFIX     Target Harbor project prefix, for example 192.168.8.149:5001/hypercdr.
  --tag TAG             Image tag, default: dev.
  --base-registry PREFIX
                        Harbor prefix containing build base images. If omitted,
                        defaults to the same Harbor host with /base-images.
  --goproxy URL         Go module proxy, default: https://goproxy.cn,direct.
  --gosumdb VALUE       Go checksum database setting, default: sum.golang.google.cn.
  --npm-registry URL    npm registry, default: https://registry.npmmirror.com.
  --execute             Build and push. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) TARGET_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --tag) TAG="${2:?missing value for --tag}"; shift 2 ;;
    --base-registry) BASE_REGISTRY="${2:?missing value for --base-registry}"; shift 2 ;;
    --goproxy) GOPROXY="${2:?missing value for --goproxy}"; shift 2 ;;
    --gosumdb) GOSUMDB="${2:?missing value for --gosumdb}"; shift 2 ;;
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

TARGET_REGISTRY="${TARGET_REGISTRY%/}"
if [[ -z "${BASE_REGISTRY}" ]]; then
  HARBOR_HOST="${TARGET_REGISTRY%%/*}"
  BASE_REGISTRY="${HARBOR_HOST}/base-images"
fi
BASE_REGISTRY="${BASE_REGISTRY%/}"

cat <<EOF
HyperCDR image update plan

Target registry: ${TARGET_REGISTRY}
Tag:             ${TAG}
Base registry:   ${BASE_REGISTRY}
Go proxy:        ${GOPROXY}
Go sumdb:        ${GOSUMDB}
NPM registry:    ${NPM_REGISTRY}
Images:
  ${TARGET_REGISTRY}/platform-api:${TAG}
  ${TARGET_REGISTRY}/comm-agent:${TAG}
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  echo
  echo "Dry-run mode. Add --execute to build and push images."
  exit 0
fi

HCDR_IMAGE_REGISTRY="${TARGET_REGISTRY}" \
HCDR_IMAGE_TAG="${TAG}" \
HCDR_BUILD_BASE_REGISTRY="${BASE_REGISTRY}" \
HCDR_BUILD_GOPROXY="${GOPROXY}" \
HCDR_BUILD_GOSUMDB="${GOSUMDB}" \
HCDR_BUILD_NPM_REGISTRY="${NPM_REGISTRY}" \
HCDR_PUSH_IMAGE=true \
"${ROOT_DIR}/scripts/build-images.sh"
