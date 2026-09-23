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
KUBECTL_VERSION="${HCDR_REGISTRATION_KUBECTL_VERSION:-v1.28.15}"
KUBECTL_DOWNLOAD_MAX_TIME="${HCDR_REGISTRATION_KUBECTL_DOWNLOAD_MAX_TIME:-900}"
KUBECTL_BINARY="${HCDR_REGISTRATION_KUBECTL_BINARY:-}"
KUBECTL_SHA256="${HCDR_REGISTRATION_KUBECTL_SHA256:-}"
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
case "${COMPONENT}" in
  all|api|frontend|agents|executor) ;;
  *) die "HCDR_RELEASE_COMPONENT must be all, api, frontend, agents, or executor" ;;
esac

WORK_DIR="$(release_work_dir "${VERSION}")"
[[ -n "${WORK_DIR}" && "${WORK_DIR}" != / ]] || die "unsafe build work dir: ${WORK_DIR}"
mkdir -p "${WORK_DIR}"
REGISTRY="${REGISTRY%/}"

if [[ "${SKIP_TESTS}" != true && "${COMPONENT}" != frontend ]]; then
  GO_BIN="$(go_bin)"
  CACHE_ROOT="${HCDR_CACHE_ROOT:-${DEFAULT_CACHE_ROOT}}"
  mkdir -p "${CACHE_ROOT}/go-build" "${CACHE_ROOT}/go-mod"
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == api || "${COMPONENT}" == executor ]]; then
    log "Testing backend"
    (cd "${ROOT_DIR}/backend" && PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${CACHE_ROOT}/go-build" GOMODCACHE="${CACHE_ROOT}/go-mod" "${GO_BIN}" test ./...)
  fi
  if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
    log "Testing comm-agent"
    (cd "${ROOT_DIR}/agent/comm-agent" && PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${CACHE_ROOT}/go-build" GOMODCACHE="${CACHE_ROOT}/go-mod" "${GO_BIN}" test ./...)
  fi
fi

PLATFORM_API_IMAGE="$(image_ref "${REGISTRY}" platform-api "${VERSION}")"
PLATFORM_FRONTEND_IMAGE="$(image_ref "${REGISTRY}" platform-frontend "${VERSION}")"
COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" comm-agent "${VERSION}")"
OADP_COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" oadp-comm-agent "${VERSION}")"
REGISTRATION_EXECUTOR_IMAGE="$(image_ref "${REGISTRY}" cluster-registration-executor "${VERSION}")"
GIT_COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ "${COMPONENT}" == all || "${COMPONENT}" == api ]]; then
  log "Building ${PLATFORM_API_IMAGE}"
  docker_build_with_retry "${PLATFORM_API_IMAGE}" --platform linux/amd64 -f "${ROOT_DIR}/backend/Dockerfile" \
    --build-arg "GOPROXY=${GOPROXY}" \
    --build-arg "VERSION=${VERSION}" --build-arg "GIT_COMMIT=${GIT_COMMIT}" \
    --build-arg "BUILD_TIME=${BUILD_TIME}" "${ROOT_DIR}/backend"

  LATEST_MIGRATION="$(find "${ROOT_DIR}/backend/internal/migrations/sql" -maxdepth 1 -type f -name '[0-9]*_*.sql' | sort | tail -n 1)"
  [[ -n "${LATEST_MIGRATION}" ]] || die "no database migrations found"
  DATABASE_SCHEMA_VERSION="$(basename "${LATEST_MIGRATION}")"
  DATABASE_SCHEMA_VERSION="${DATABASE_SCHEMA_VERSION%%_*}"
  cat >"${WORK_DIR}/release-manifest.json" <<EOF
{"version":"${VERSION}","apiImage":"${PLATFORM_API_IMAGE}","frontendImage":"${PLATFORM_FRONTEND_IMAGE}","databaseSchemaVersion":"${DATABASE_SCHEMA_VERSION}","minimumAgentVersion":"v20260721.4","rollbackSupported":true}
EOF
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == frontend ]]; then
  log "Building ${PLATFORM_FRONTEND_IMAGE}"
  docker_build_with_retry "${PLATFORM_FRONTEND_IMAGE}" --platform linux/amd64 -f "${ROOT_DIR}/frontend/Dockerfile" \
    --build-arg "NPM_REGISTRY=${NPM_REGISTRY}" \
    --build-arg "VERSION=${VERSION}" --build-arg "RELEASE_DATE=$(date -u +%Y/%m/%d)" "${ROOT_DIR}"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == agents ]]; then
  log "Building ${COMM_AGENT_IMAGE}"
  docker_build_with_retry "${COMM_AGENT_IMAGE}" --platform linux/amd64 -f "${ROOT_DIR}/agent/comm-agent/Dockerfile" \
    --build-arg "GOPROXY=${GOPROXY}" "${ROOT_DIR}/agent/comm-agent"
  log "Building ${OADP_COMM_AGENT_IMAGE}"
  docker_build_with_retry "${OADP_COMM_AGENT_IMAGE}" --platform linux/amd64 -f "${ROOT_DIR}/agent/comm-agent/oadp.Dockerfile" \
    --build-arg "GOPROXY=${GOPROXY}" "${ROOT_DIR}/agent/comm-agent"
fi

if [[ "${COMPONENT}" == all || "${COMPONENT}" == executor ]]; then
  kubectl_context=()
  if [[ -n "${KUBECTL_BINARY}" ]]; then
    [[ -r "${KUBECTL_BINARY}" && "${KUBECTL_SHA256}" =~ ^[0-9a-f]{64}$ ]] || die "local kubectl requires a readable binary and SHA256"
    [[ "$(sha256sum "${KUBECTL_BINARY}" | awk '{print $1}')" == "${KUBECTL_SHA256}" ]] || die "local kubectl checksum verification failed"
    mkdir -p "${WORK_DIR}/kubectl-context"
    cp "${KUBECTL_BINARY}" "${WORK_DIR}/kubectl-context/kubectl"
    kubectl_context=(--build-context "kubectl-downloader=${WORK_DIR}/kubectl-context")
  fi
  log "Building ${REGISTRATION_EXECUTOR_IMAGE}"
  docker_build_with_retry "${REGISTRATION_EXECUTOR_IMAGE}" --platform linux/amd64 -f "${ROOT_DIR}/backend/cluster-registration-executor.Dockerfile" \
    --build-arg "GOPROXY=${GOPROXY}" \
    --build-arg "KUBECTL_VERSION=${KUBECTL_VERSION}" --build-arg "KUBECTL_DOWNLOAD_MAX_TIME=${KUBECTL_DOWNLOAD_MAX_TIME}" --build-arg "VERSION=${VERSION}" \
    --build-arg "GIT_COMMIT=${GIT_COMMIT}" --build-arg "BUILD_TIME=${BUILD_TIME}" \
    "${kubectl_context[@]}" "${ROOT_DIR}/backend"
fi

if [[ "${PUSH}" == true ]]; then
  "${SCRIPT_DIR}/push-release.sh" "${VERSION}" --registry "${REGISTRY}" --component "${COMPONENT}"
fi

log "Built ${COMPONENT} release image(s) for ${VERSION}"
