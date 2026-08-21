#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WORKSPACE_DIR="$(cd "${ROOT_DIR}/.." && pwd)"

DEFAULT_HOST="192.168.8.149"
DEFAULT_GO="/usr/local/go/bin/go"
DEFAULT_GOPROXY="https://goproxy.cn,direct"
DEFAULT_NPM_REGISTRY="https://registry.npmmirror.com"
DEFAULT_BUILD_ROOT="${WORKSPACE_DIR}/.build/platform"
DEFAULT_CACHE_ROOT="${WORKSPACE_DIR}/.cache"

die() {
  echo "error: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

require_version() {
  local version="${1:-}"
  [[ -n "${version}" ]] || die "version is required, for example v20260714.1"
  [[ "${version}" =~ ^v[0-9]{8}\.[0-9]+$ ]] || die "version must match vYYYYMMDD.N, got ${version}"
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
  [[ -x "${go}" ]] || die "Go binary not found or not executable: ${go}"
  echo "${go}"
}

image_ref() {
  local registry="${1%/}"
  local name="$2"
  local version="$3"
  echo "${registry}/${name}:${version}"
}

local_image_digest() {
  local image="$1"
  local digest
  digest="$(docker image inspect "${image}" --format '{{range .RepoDigests}}{{println .}}{{end}}' | awk -F@ -v prefix="${image%:*}@" '$0 ~ "^" prefix {print $2; exit}')"
  if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    digest="$(docker image inspect "${image}" --format '{{range .RepoDigests}}{{println .}}{{end}}' | awk -F@ 'NF == 2 && $2 ~ /^sha256:[0-9a-f]{64}$/ {print $2; exit}')"
  fi
  [[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || die "verified image digest is unavailable for ${image}"
  echo "${digest}"
}

release_work_dir() {
  local version="$1"
  echo "${HCDR_BUILD_ROOT:-${DEFAULT_BUILD_ROOT}}/${version}"
}
