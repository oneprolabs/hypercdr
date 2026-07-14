#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
VERSION=""
SKIP_TESTS="false"
PUSH="false"
GOPROXY="${HCDR_BUILD_GOPROXY:-${DEFAULT_GOPROXY}}"
NPM_REGISTRY="${HCDR_BUILD_NPM_REGISTRY:-${DEFAULT_NPM_REGISTRY}}"
NGINX_IMAGE="${HCDR_FRONTEND_NGINX_IMAGE:-nginx:1.27-alpine}"
DEBIAN_IMAGE="${HCDR_API_RUNTIME_IMAGE:-debian:bookworm-slim}"

usage() {
  cat <<'USAGE'
Build HyperCDR release images.

Usage:
  build-release.sh <version> [options]

Options:
  --registry REGISTRY       Target registry prefix. Required unless HCDR_IMAGE_REGISTRY is set.
  --skip-tests              Skip Go tests.
  --push                    Push images after building.
  --goproxy GOPROXY         Go proxy, default https://goproxy.cn,direct.
  --npm-registry URL        npm registry, default https://registry.npmmirror.com.
  -h, --help                Show help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --skip-tests) SKIP_TESTS="true"; shift ;;
    --push) PUSH="true"; shift ;;
    --goproxy) GOPROXY="${2:?missing value for --goproxy}"; shift 2 ;;
    --npm-registry) NPM_REGISTRY="${2:?missing value for --npm-registry}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *)
      if [[ -z "${VERSION}" ]]; then VERSION="$1"; shift; else die "unknown argument: $1"; fi
      ;;
  esac
done

require_version "${VERSION}"
require_registry "${REGISTRY}"
require_cmd docker
require_cmd npm

GO_BIN="$(go_bin)"
WORK_DIR="$(release_work_dir "${VERSION}")"
CACHE_ROOT="${HCDR_CACHE_ROOT:-${DEFAULT_CACHE_ROOT}}"
GO_BUILD_CACHE="${HCDR_GO_BUILD_CACHE:-${CACHE_ROOT}/go-build}"
GO_MOD_CACHE="${HCDR_GO_MOD_CACHE:-${CACHE_ROOT}/go-mod}"
NPM_CACHE="${HCDR_NPM_CACHE:-${CACHE_ROOT}/npm}"
FRONTEND_WORK_DIR="${WORK_DIR}/frontend-src"
REGISTRY="${REGISTRY%/}"

PLATFORM_API_IMAGE="$(image_ref "${REGISTRY}" platform-api "${VERSION}")"
PLATFORM_FRONTEND_IMAGE="$(image_ref "${REGISTRY}" platform-frontend "${VERSION}")"
COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" comm-agent "${VERSION}")"

log "Release version: ${VERSION}"
log "Registry: ${REGISTRY}"
log "Work dir: ${WORK_DIR}"
log "Cache root: ${CACHE_ROOT}"

if [[ -z "${WORK_DIR}" || "${WORK_DIR}" == "/" ]]; then
  die "unsafe build work dir: ${WORK_DIR}"
fi
rm -rf "${WORK_DIR}"
mkdir -p \
  "${WORK_DIR}/platform-api" \
  "${WORK_DIR}/platform-frontend/nginx" \
  "${WORK_DIR}/comm-agent" \
  "${FRONTEND_WORK_DIR}" \
  "${GO_BUILD_CACHE}" \
  "${GO_MOD_CACHE}" \
  "${NPM_CACHE}"

if [[ "${SKIP_TESTS}" != "true" ]]; then
  log "Testing backend"
  (
    cd "${ROOT_DIR}/platform/backend"
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" "${GO_BIN}" test ./...
  )

  log "Testing comm-agent"
  (
    cd "${ROOT_DIR}/agent/comm-agent"
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" "${GO_BIN}" test ./...
  )
fi

log "Building backend binaries"
(
  cd "${ROOT_DIR}/platform/backend"
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/platform-api/platform-api" ./cmd/platform-api
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/platform-api/platform-migrate" ./cmd/platform-migrate
)

log "Building frontend dist"
cp -a "${ROOT_DIR}/platform/frontend/." "${FRONTEND_WORK_DIR}/"
rm -rf "${FRONTEND_WORK_DIR}/node_modules" "${FRONTEND_WORK_DIR}/dist"
(
  cd "${FRONTEND_WORK_DIR}"
  npm ci --registry="${NPM_REGISTRY}" --cache="${NPM_CACHE}"
  npm run build
)

log "Building comm-agent binary"
(
  cd "${ROOT_DIR}/agent/comm-agent"
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/comm-agent/comm-agent" ./cmd/comm-agent
)

log "Preparing Docker contexts"
cp /etc/ssl/certs/ca-certificates.crt "${WORK_DIR}/platform-api/ca-certificates.crt"
cp "${ROOT_DIR}/deployments/docker/platform-api.runtime.Dockerfile" "${WORK_DIR}/platform-api/Dockerfile"
sed -i "s#^FROM debian:bookworm-slim#FROM ${DEBIAN_IMAGE}#" "${WORK_DIR}/platform-api/Dockerfile"

cp -a "${FRONTEND_WORK_DIR}/dist" "${WORK_DIR}/platform-frontend/dist"
cp -a "${ROOT_DIR}/deployments/docker/nginx/." "${WORK_DIR}/platform-frontend/nginx/"
cp "${ROOT_DIR}/deployments/docker/platform-frontend.Dockerfile" "${WORK_DIR}/platform-frontend/Dockerfile"

cp "${ROOT_DIR}/deployments/docker/comm-agent.local.Dockerfile" "${WORK_DIR}/comm-agent/Dockerfile"

log "Building image ${PLATFORM_API_IMAGE}"
docker build -t "${PLATFORM_API_IMAGE}" "${WORK_DIR}/platform-api"

log "Building image ${PLATFORM_FRONTEND_IMAGE}"
docker build --build-arg NGINX_IMAGE="${NGINX_IMAGE}" -t "${PLATFORM_FRONTEND_IMAGE}" "${WORK_DIR}/platform-frontend"

log "Building image ${COMM_AGENT_IMAGE}"
docker build -t "${COMM_AGENT_IMAGE}" "${WORK_DIR}/comm-agent"

if [[ "${PUSH}" == "true" ]]; then
  log "Pushing images"
  docker push "${PLATFORM_API_IMAGE}"
  docker push "${PLATFORM_FRONTEND_IMAGE}"
  docker push "${COMM_AGENT_IMAGE}"
fi

cat <<EOF

Built images:
  ${PLATFORM_API_IMAGE}
  ${PLATFORM_FRONTEND_IMAGE}
  ${COMM_AGENT_IMAGE}

Next:
  ${SCRIPT_DIR}/push-release.sh ${VERSION} --registry ${REGISTRY}
  Install or upgrade from the bootstrap page or platform UI.
EOF
