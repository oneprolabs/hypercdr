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
FRONTEND_SOURCE_DIR="${ROOT_DIR}/platform/frontend"
FRONTEND_DIST_DIR="${WORK_DIR}/platform-frontend/dist"
FRONTEND_DEPS_DIR="${WORK_DIR}/frontend-deps"
FRONTEND_ORIGINAL_NODE_MODULES="${WORK_DIR}/frontend-node-modules.original"
FRONTEND_NODE_MODULES_STASHED="false"
REGISTRY="${REGISTRY%/}"

restore_frontend_node_modules() {
  local source_node_modules="${FRONTEND_SOURCE_DIR}/node_modules"
  [[ "${FRONTEND_SOURCE_DIR}" != "/" && -n "${FRONTEND_SOURCE_DIR}" ]] || return 1
  if [[ -L "${source_node_modules}" || -d "${source_node_modules}" ]]; then
    rm -rf "${source_node_modules}"
  fi
  if [[ "${FRONTEND_NODE_MODULES_STASHED}" == "true" && ( -e "${FRONTEND_ORIGINAL_NODE_MODULES}" || -L "${FRONTEND_ORIGINAL_NODE_MODULES}" ) ]]; then
    mv "${FRONTEND_ORIGINAL_NODE_MODULES}" "${source_node_modules}"
  fi
}

PLATFORM_API_IMAGE="$(image_ref "${REGISTRY}" platform-api "${VERSION}")"
PLATFORM_FRONTEND_IMAGE="$(image_ref "${REGISTRY}" platform-frontend "${VERSION}")"
COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" comm-agent "${VERSION}")"
PLATFORM_UPGRADER_IMAGE="$(image_ref "${REGISTRY}" platform-upgrader "${VERSION}")"

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
  "${WORK_DIR}/platform-upgrader" \
  "${FRONTEND_DEPS_DIR}" \
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
  BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  GIT_COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  VERSION_LDFLAGS="-s -w -X hypercdr-platform/platform/backend/internal/buildinfo.Version=${VERSION} -X hypercdr-platform/platform/backend/internal/buildinfo.GitCommit=${GIT_COMMIT} -X hypercdr-platform/platform/backend/internal/buildinfo.BuildTime=${BUILD_TIME}"
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="${VERSION_LDFLAGS}" -o "${WORK_DIR}/platform-api/platform-api" ./cmd/platform-api
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/platform-api/platform-migrate" ./cmd/platform-migrate
  PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="${VERSION_LDFLAGS}" -o "${WORK_DIR}/platform-upgrader/platform-upgrader" ./cmd/platform-upgrader
)

log "Building frontend dist"
if [[ -e "${FRONTEND_SOURCE_DIR}/node_modules" || -L "${FRONTEND_SOURCE_DIR}/node_modules" ]]; then
  mv "${FRONTEND_SOURCE_DIR}/node_modules" "${FRONTEND_ORIGINAL_NODE_MODULES}"
  FRONTEND_NODE_MODULES_STASHED="true"
fi
trap restore_frontend_node_modules EXIT
cp "${FRONTEND_SOURCE_DIR}/package.json" "${FRONTEND_SOURCE_DIR}/package-lock.json" "${FRONTEND_DEPS_DIR}/"
(
  cd "${FRONTEND_DEPS_DIR}"
  npm ci --registry="${NPM_REGISTRY}" --cache="${NPM_CACHE}"
)
ln -s "${FRONTEND_DEPS_DIR}/node_modules" "${FRONTEND_SOURCE_DIR}/node_modules"
(
  cd "${FRONTEND_SOURCE_DIR}"
  VITE_HCDR_RELEASE_VERSION="${VERSION}" npm run build -- --outDir "${FRONTEND_DIST_DIR}" --emptyOutDir
)
restore_frontend_node_modules
trap - EXIT

FRONTEND_CSS="$(find "${FRONTEND_DIST_DIR}/assets" -maxdepth 1 -type f -name '*.css' -print -quit)"
[[ -n "${FRONTEND_CSS}" && -s "${FRONTEND_CSS}" ]] || die "frontend CSS artifact is missing"
for required_utility in '.grid{' '.flex{' '.items-center{' '.px-5{' '.py-4{'; do
  grep -Fq "${required_utility}" "${FRONTEND_CSS}" || die "frontend CSS is missing required Tailwind utility ${required_utility}; refusing to publish"
done
log "Verified Tailwind utilities in $(basename "${FRONTEND_CSS}")"

cat >"${WORK_DIR}/release-manifest.json" <<EOF
{"version":"${VERSION}","apiImage":"${PLATFORM_API_IMAGE}","frontendImage":"${PLATFORM_FRONTEND_IMAGE}","databaseSchemaVersion":"000019","minimumAgentVersion":"v20260721.4","rollbackSupported":true}
EOF

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

cp -a "${ROOT_DIR}/deployments/docker/nginx/." "${WORK_DIR}/platform-frontend/nginx/"
cp "${ROOT_DIR}/deployments/docker/platform-frontend.Dockerfile" "${WORK_DIR}/platform-frontend/Dockerfile"

cp "${ROOT_DIR}/deployments/docker/comm-agent.local.Dockerfile" "${WORK_DIR}/comm-agent/Dockerfile"
cp "${ROOT_DIR}/deployments/docker/platform-upgrader.Dockerfile" "${WORK_DIR}/platform-upgrader/Dockerfile"

log "Building image ${PLATFORM_API_IMAGE}"
docker build -t "${PLATFORM_API_IMAGE}" "${WORK_DIR}/platform-api"

log "Building image ${PLATFORM_FRONTEND_IMAGE}"
docker build --build-arg NGINX_IMAGE="${NGINX_IMAGE}" -t "${PLATFORM_FRONTEND_IMAGE}" "${WORK_DIR}/platform-frontend"

log "Building image ${COMM_AGENT_IMAGE}"
docker build -t "${COMM_AGENT_IMAGE}" "${WORK_DIR}/comm-agent"

log "Building image ${PLATFORM_UPGRADER_IMAGE}"
docker build -t "${PLATFORM_UPGRADER_IMAGE}" "${WORK_DIR}/platform-upgrader"

if [[ "${PUSH}" == "true" ]]; then
  log "Pushing images"
  docker push "${PLATFORM_API_IMAGE}"
  docker push "${PLATFORM_FRONTEND_IMAGE}"
  docker push "${COMM_AGENT_IMAGE}"
  docker push "${PLATFORM_UPGRADER_IMAGE}"
fi

cat <<EOF

Built images:
  ${PLATFORM_API_IMAGE}
  ${PLATFORM_FRONTEND_IMAGE}
  ${COMM_AGENT_IMAGE}
  ${PLATFORM_UPGRADER_IMAGE}

Next:
  ${SCRIPT_DIR}/push-release.sh ${VERSION} --registry ${REGISTRY}
  Install or upgrade from the bootstrap page or platform UI.
EOF
