#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WORKSPACE_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-${WORKSPACE_DIR}/hypercdr-runtime}"

DEFAULT_HOST="192.168.8.149"
DEFAULT_GO="/usr/local/go/bin/go"
DEFAULT_GOPROXY="https://goproxy.cn,direct"
DEFAULT_NPM_REGISTRY="https://registry.npmmirror.com"
DEFAULT_BUILD_ROOT="${RUNTIME_ROOT}/build/platform"
DEFAULT_CACHE_ROOT="${RUNTIME_ROOT}/cache"

die() {
  echo "error: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

docker_push_with_retry() {
  local image="$1" attempt=1 max_attempts="${HCDR_DOCKER_PUSH_ATTEMPTS:-5}"
  while ! docker push "${image}"; do
    if (( attempt >= max_attempts )); then
      echo "error: failed to push ${image} after ${max_attempts} attempts" >&2
      return 1
    fi
    local delay=$((5 * attempt))
    log "Push failed for ${image}; retrying in ${delay}s (${attempt}/${max_attempts})"
    sleep "${delay}"
    ((attempt++))
  done
}

docker_pull_with_retry() {
  local image="$1" attempt=1 max_attempts="${HCDR_DOCKER_PUSH_ATTEMPTS:-5}"
  while ! docker pull --platform linux/amd64 "${image}"; do
    if (( attempt >= max_attempts )); then return 1; fi
    local delay=$((5 * attempt)); log "Pull verification failed for ${image}; retrying in ${delay}s (${attempt}/${max_attempts})"; sleep "${delay}"; ((attempt++))
  done
}

docker_manifest_inspect_with_retry() {
  local image="$1" attempt=1 max_attempts="${HCDR_DOCKER_PUSH_ATTEMPTS:-5}"
  while ! docker manifest inspect "${image}" >/dev/null 2>&1; do
    if (( attempt >= max_attempts )); then return 1; fi
    local delay=$((5 * attempt)); log "Manifest verification failed for ${image}; retrying in ${delay}s (${attempt}/${max_attempts})"; sleep "${delay}"; ((attempt++))
  done
}

require_version() {
  local version="${1:-}"
  [[ -n "${version}" ]] || die "version is required, for example 1.0.0.20260901"
  [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ || "${version}" =~ ^v[0-9]{8}\.[0-9]+$ ]] || die "version must match MAJOR.MINOR.PATCH.YYYYMMDD, got ${version}"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

require_registry() {
  local registry="${1:-}"
  [[ -n "${registry}" ]] || die "registry is required, pass --registry HOST:PORT/hypercdr or set HCDR_IMAGE_REGISTRY"
}

go_bin() {
  local go="${HCDR_GO_BIN:-${DEFAULT_GO}}"
  if [[ "${go}" != */* ]]; then
    go="$(command -v "${go}" || true)"
  fi
  [[ -n "${go}" && -x "${go}" ]] || die "Go binary not found or not executable: ${HCDR_GO_BIN:-${DEFAULT_GO}}"
  # Respect the toolchain selected by go.mod/go.work. The system go command may
  # be a bootstrap toolchain (for example 1.24) while `go env GOROOT` points at
  # an already installed, module-qualified toolchain (for example 1.25.13).
  # Build commands intentionally use GOTOOLCHAIN=local after this resolution so
  # a formal release never downloads a compiler halfway through a build.
  local selected_goroot selected_go
  selected_goroot="$("${go}" env GOROOT 2>/dev/null || true)"
  selected_go="${selected_goroot}/bin/go"
  if [[ -n "${selected_goroot}" && -x "${selected_go}" ]]; then
    go="${selected_go}"
  fi
  echo "${go}"
}

source "${ROOT_DIR}/scripts/lib/registry-config.sh"

release_work_dir() {
  local version="$1"
  echo "${HCDR_BUILD_ROOT:-${DEFAULT_BUILD_ROOT}}/${version}"
}
