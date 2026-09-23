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
# OpenShift 4.14 and 4.15 use Kubernetes 1.27 and 1.28. A 1.28 client is
# inside the supported kubectl minor-version skew for both server versions.
KUBECTL_VERSION="${HCDR_REGISTRATION_KUBECTL_VERSION:-v1.28.15}"
KUBECTL_BINARY="${HCDR_REGISTRATION_KUBECTL_BINARY:-}"
KUBECTL_SHA256="${HCDR_REGISTRATION_KUBECTL_SHA256:-}"
KUBECTL_DOWNLOAD_MAX_TIME="${HCDR_REGISTRATION_KUBECTL_DOWNLOAD_MAX_TIME:-900}"
COMPONENT="${HCDR_RELEASE_COMPONENT:-all}"

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
require_cmd curl
require_cmd sha256sum
case "${COMPONENT}" in
  all|api|frontend|agents|executor) ;;
  *) die "HCDR_RELEASE_COMPONENT must be all, api, frontend, agents, or executor" ;;
esac
if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then require_cmd npm; fi

GO_BIN=""
if [[ "${COMPONENT}" != frontend ]]; then GO_BIN="$(go_bin)"; fi
WORK_DIR="$(release_work_dir "${VERSION}")"
CACHE_ROOT="${HCDR_CACHE_ROOT:-${DEFAULT_CACHE_ROOT}}"
GO_BUILD_CACHE="${HCDR_GO_BUILD_CACHE:-${CACHE_ROOT}/go-build}"
GO_MOD_CACHE="${HCDR_GO_MOD_CACHE:-${CACHE_ROOT}/go-mod}"
NPM_CACHE="${HCDR_NPM_CACHE:-${CACHE_ROOT}/npm}"
FRONTEND_SOURCE_DIR="${ROOT_DIR}/frontend"
FRONTEND_DIST_DIR="${WORK_DIR}/platform-frontend/dist"
FRONTEND_DEPS_DIR="${WORK_DIR}/frontend-deps"
FRONTEND_ORIGINAL_NODE_MODULES="${WORK_DIR}/frontend-node-modules.original"
FRONTEND_NODE_MODULES_STASHED="false"
LATEST_MIGRATION="$(find "${ROOT_DIR}/backend/internal/migrations/sql" -maxdepth 1 -type f -name '[0-9]*_*.sql' -printf '%f\n' | sort | tail -n 1)"
[[ -n "${LATEST_MIGRATION}" ]] || die "no database migrations found"
DATABASE_SCHEMA_VERSION="${LATEST_MIGRATION%%_*}"
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
OADP_COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" oadp-comm-agent "${VERSION}")"
REGISTRATION_EXECUTOR_IMAGE="$(image_ref "${REGISTRY}" cluster-registration-executor "${VERSION}")"

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
  "${WORK_DIR}/oadp-comm-agent" \
  "${WORK_DIR}/cluster-registration-executor" \
  "${FRONTEND_DEPS_DIR}" \
  "${GO_BUILD_CACHE}" \
  "${GO_MOD_CACHE}" \
  "${NPM_CACHE}"

if [[ "${SKIP_TESTS}" != "true" && ( "${COMPONENT}" == all || "${COMPONENT}" == api || "${COMPONENT}" == executor ) ]]; then
  log "Testing backend"
  (
    cd "${ROOT_DIR}/backend"
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" "${GO_BIN}" test ./...
  )

fi

if [[ "${SKIP_TESTS}" != "true" && ( "${COMPONENT}" == all || "${COMPONENT}" == agents ) ]]; then
  log "Testing comm-agent"
  (
    cd "${ROOT_DIR}/agent/comm-agent"
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" "${GO_BIN}" test ./...
  )
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == api || "${COMPONENT}" == executor ]]; then
log "Building backend binaries"
(
  cd "${ROOT_DIR}/backend"
  BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  GIT_COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  VERSION_LDFLAGS="-s -w -X hypercdr-platform/platform/backend/internal/buildinfo.Version=${VERSION} -X hypercdr-platform/platform/backend/internal/buildinfo.GitCommit=${GIT_COMMIT} -X hypercdr-platform/platform/backend/internal/buildinfo.BuildTime=${BUILD_TIME}"
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      "${GO_BIN}" build -trimpath -ldflags="${VERSION_LDFLAGS}" -o "${WORK_DIR}/platform-api/platform-api" ./cmd/platform-api
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/platform-api/platform-migrate" ./cmd/platform-migrate
  fi
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == executor ]]; then
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      "${GO_BIN}" build -trimpath -ldflags="${VERSION_LDFLAGS}" -o "${WORK_DIR}/cluster-registration-executor/cluster-registration-executor" ./cmd/cluster-registration-executor
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/cluster-registration-executor/curl" ./cmd/registration-curl
  fi
)
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then
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
  VITE_HCDR_RELEASE_VERSION="${VERSION}" \
    VITE_HCDR_RELEASE_DATE="$(date -u +%Y/%m/%d)" \
    npm run build -- --outDir "${FRONTEND_DIST_DIR}" --emptyOutDir
)
restore_frontend_node_modules
trap - EXIT

FRONTEND_CSS="$(find "${FRONTEND_DIST_DIR}/assets" -maxdepth 1 -type f -name '*.css' -print -quit)"
[[ -n "${FRONTEND_CSS}" && -s "${FRONTEND_CSS}" ]] || die "frontend CSS artifact is missing"
for required_utility in '.grid{' '.flex{' '.items-center{' '.px-5{' '.py-4{'; do
  grep -Fq "${required_utility}" "${FRONTEND_CSS}" || die "frontend CSS is missing required Tailwind utility ${required_utility}; refusing to publish"
done
log "Verified Tailwind utilities in $(basename "${FRONTEND_CSS}")"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then
cat >"${WORK_DIR}/release-manifest.json" <<EOF
{"version":"${VERSION}","apiImage":"${PLATFORM_API_IMAGE}","frontendImage":"${PLATFORM_FRONTEND_IMAGE}","databaseSchemaVersion":"${DATABASE_SCHEMA_VERSION}","minimumAgentVersion":"v20260721.4","rollbackSupported":true}
EOF
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
log "Building comm-agent binary"
(
  cd "${ROOT_DIR}/agent/comm-agent"
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/comm-agent/comm-agent" ./cmd/comm-agent
)
cp "${WORK_DIR}/comm-agent/comm-agent" "${WORK_DIR}/oadp-comm-agent/oadp-comm-agent"
fi

log "Preparing Docker contexts"
if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then
cp /etc/ssl/certs/ca-certificates.crt "${WORK_DIR}/platform-api/ca-certificates.crt"
cp "${ROOT_DIR}/backend/Dockerfile" "${WORK_DIR}/platform-api/Dockerfile"
sed -i "s#^FROM debian:bookworm-slim#FROM ${DEBIAN_IMAGE}#" "${WORK_DIR}/platform-api/Dockerfile"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then
cp -a "${ROOT_DIR}/docker/nginx/." "${WORK_DIR}/platform-frontend/nginx/"
cp "${ROOT_DIR}/frontend/Dockerfile" "${WORK_DIR}/platform-frontend/Dockerfile"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
cp /etc/ssl/certs/ca-certificates.crt "${WORK_DIR}/comm-agent/ca-certificates.crt"
cp /etc/ssl/certs/ca-certificates.crt "${WORK_DIR}/oadp-comm-agent/ca-certificates.crt"
cp "${ROOT_DIR}/agent/comm-agent/Dockerfile" "${WORK_DIR}/comm-agent/Dockerfile"
cp "${ROOT_DIR}/agent/comm-agent/oadp.Dockerfile" "${WORK_DIR}/oadp-comm-agent/Dockerfile"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == executor ]]; then
cp /etc/ssl/certs/ca-certificates.crt "${WORK_DIR}/cluster-registration-executor/ca-certificates.crt"
cp "${ROOT_DIR}/backend/cluster-registration-executor.Dockerfile" "${WORK_DIR}/cluster-registration-executor/Dockerfile"
if [[ -n "${KUBECTL_BINARY}" ]]; then
  [[ -f "${KUBECTL_BINARY}" && -r "${KUBECTL_BINARY}" ]] || die "configured kubectl binary is not readable: ${KUBECTL_BINARY}"
  [[ "${KUBECTL_SHA256}" =~ ^[0-9a-f]{64}$ ]] || die "HCDR_REGISTRATION_KUBECTL_SHA256 is required with a local kubectl binary"
  log "Using local kubectl ${KUBECTL_VERSION} candidate for registration executor"
  cp "${KUBECTL_BINARY}" "${WORK_DIR}/cluster-registration-executor/kubectl"
else
  log "Downloading checksum-verified kubectl ${KUBECTL_VERSION} for registration executor"
  KUBECTL_CACHE_DIR="${CACHE_ROOT}/downloads/kubectl/${KUBECTL_VERSION}/linux-amd64"
  KUBECTL_CACHE_FILE="${KUBECTL_CACHE_DIR}/kubectl"
  KUBECTL_PART_FILE="${KUBECTL_CACHE_FILE}.part"
  mkdir -p "${KUBECTL_CACHE_DIR}"
  if [[ ! -s "${KUBECTL_CACHE_FILE}" ]]; then
    # Keep a partial file outside WORK_DIR so a failed release can resume the
    # checksum-protected download instead of restarting a large transfer.
    download_attempt=1
    until curl -fL --retry 2 --retry-max-time "${KUBECTL_DOWNLOAD_MAX_TIME}" \
      --connect-timeout 30 --max-time "${KUBECTL_DOWNLOAD_MAX_TIME}" --speed-time 60 --speed-limit 128 \
      --continue-at - "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
      -o "${KUBECTL_PART_FILE}"; do
      (( download_attempt < 5 )) || die "kubectl ${KUBECTL_VERSION} download failed after ${download_attempt} resumable attempts"
      log "Resuming kubectl download after attempt ${download_attempt}"
      download_attempt=$((download_attempt + 1))
    done
  fi
  curl -fsSL --retry 1 --retry-max-time 60 --connect-timeout 10 --max-time 60 --speed-time 20 --speed-limit 32 \
    "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl.sha256" \
    -o "${WORK_DIR}/cluster-registration-executor/kubectl.sha256"
  KUBECTL_SHA256="$(tr -d '[:space:]' < "${WORK_DIR}/cluster-registration-executor/kubectl.sha256")"
  if [[ ! -s "${KUBECTL_CACHE_FILE}" ]]; then
    KUBECTL_PART_SHA256="$(sha256sum "${KUBECTL_PART_FILE}" | awk '{print $1}')"
    [[ "${KUBECTL_SHA256}" =~ ^[0-9a-f]{64}$ && "${KUBECTL_PART_SHA256}" == "${KUBECTL_SHA256}" ]] || die "downloaded kubectl ${KUBECTL_VERSION} checksum verification failed"
    mv "${KUBECTL_PART_FILE}" "${KUBECTL_CACHE_FILE}"
  fi
  cp "${KUBECTL_CACHE_FILE}" "${WORK_DIR}/cluster-registration-executor/kubectl"
fi
KUBECTL_ACTUAL="$(sha256sum "${WORK_DIR}/cluster-registration-executor/kubectl" | awk '{print $1}')"
[[ "${KUBECTL_SHA256}" =~ ^[0-9a-f]{64}$ && "${KUBECTL_ACTUAL}" == "${KUBECTL_SHA256}" ]] || die "kubectl ${KUBECTL_VERSION} checksum verification failed"
chmod 0755 "${WORK_DIR}/cluster-registration-executor/kubectl"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then
log "Building image ${PLATFORM_API_IMAGE}"
docker_build_with_retry "${PLATFORM_API_IMAGE}" "${WORK_DIR}/platform-api"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then
log "Building image ${PLATFORM_FRONTEND_IMAGE}"
docker_build_with_retry "${PLATFORM_FRONTEND_IMAGE}" --build-arg NGINX_IMAGE="${NGINX_IMAGE}" "${WORK_DIR}/platform-frontend"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
log "Building image ${COMM_AGENT_IMAGE}"
docker_build_with_retry "${COMM_AGENT_IMAGE}" "${WORK_DIR}/comm-agent"

log "Building image ${OADP_COMM_AGENT_IMAGE}"
docker_build_with_retry "${OADP_COMM_AGENT_IMAGE}" "${WORK_DIR}/oadp-comm-agent"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == executor ]]; then
log "Building image ${REGISTRATION_EXECUTOR_IMAGE}"
docker_build_with_retry "${REGISTRATION_EXECUTOR_IMAGE}" --build-arg DEBIAN_IMAGE="${DEBIAN_IMAGE}" "${WORK_DIR}/cluster-registration-executor"
fi

if [[ "${PUSH}" == "true" ]]; then
  log "Pushing images"
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then docker push "${PLATFORM_API_IMAGE}"; fi
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then docker push "${PLATFORM_FRONTEND_IMAGE}"; fi
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
    docker push "${COMM_AGENT_IMAGE}"
    docker push "${OADP_COMM_AGENT_IMAGE}"
  fi
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == executor ]]; then docker push "${REGISTRATION_EXECUTOR_IMAGE}"; fi
fi

cat <<EOF

Built images:
  ${PLATFORM_API_IMAGE}
  ${PLATFORM_FRONTEND_IMAGE}
  ${COMM_AGENT_IMAGE}
  ${REGISTRATION_EXECUTOR_IMAGE}

Next:
  ${SCRIPT_DIR}/push-release.sh ${VERSION} --registry ${REGISTRY}
  Install from the bootstrap package; upgrade through the blue/green release pipeline.
EOF
