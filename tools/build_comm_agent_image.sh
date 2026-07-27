#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-${WORKSPACE_DIR}/hypercdr-runtime}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
TAG="${HCDR_AGENT_IMAGE_TAG:-dev}"
GO_BIN="${HCDR_GO_BIN:-/usr/local/go/bin/go}"
GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"
BUILD_ROOT="${HCDR_BUILD_ROOT:-${RUNTIME_ROOT}/build/comm-agent}"
CACHE_ROOT="${HCDR_CACHE_ROOT:-${RUNTIME_ROOT}/cache}"
WORK_DIR="${BUILD_ROOT}/${TAG}"
GO_BUILD_CACHE="${HCDR_GO_BUILD_CACHE:-${CACHE_ROOT}/go-build}"
GO_MOD_CACHE="${HCDR_GO_MOD_CACHE:-${CACHE_ROOT}/go-mod}"
if [[ -z "${HCDR_AGENT_IMAGE:-}" && -z "${REGISTRY}" ]]; then
  echo "error: registry is required, set HCDR_IMAGE_REGISTRY or HCDR_AGENT_IMAGE" >&2
  exit 1
fi
IMAGE="${HCDR_AGENT_IMAGE:-${REGISTRY%/}/comm-agent:${TAG}}"

[[ -x "${GO_BIN}" ]] || { echo "error: Go binary not found: ${GO_BIN}" >&2; exit 1; }
[[ -n "${WORK_DIR}" && "${WORK_DIR}" != "/" ]] || { echo "error: unsafe build directory" >&2; exit 1; }

rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}" "${GO_BUILD_CACHE}" "${GO_MOD_CACHE}"

if [[ "${HCDR_SKIP_TESTS:-false}" != "true" ]]; then
  echo "==> Testing comm-agent"
  (
    cd "${ROOT_DIR}/agent/comm-agent"
    PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
      GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" "${GO_BIN}" test ./...
  )
fi

echo "==> Building comm-agent binary outside the source tree"
(
  cd "${ROOT_DIR}/agent/comm-agent"
  PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
    GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${WORK_DIR}/comm-agent" ./cmd/comm-agent
)

cp "${ROOT_DIR}/docker/comm-agent.local.Dockerfile" "${WORK_DIR}/Dockerfile"
echo "==> Building ${IMAGE}"
docker build -t "${IMAGE}" "${WORK_DIR}"

if [[ "${HCDR_PUSH_IMAGE:-false}" == "true" ]]; then
  echo "==> Pushing ${IMAGE}"
  docker push "${IMAGE}"
fi

echo "${IMAGE}"
