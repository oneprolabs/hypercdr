#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UPSTREAM_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_ROOT="${1:?usage: prepare-patched-source.sh OUTPUT_DIR}"
PATCH_ROOT="${UPSTREAM_ROOT}/patches"
KOPIA_MODULE="github.com/project-velero/kopia"
KOPIA_VERSION="v0.0.0-20251230033609-d946b1e75197"
GOPROXY_VALUE="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"

command -v git >/dev/null 2>&1
command -v go >/dev/null 2>&1
command -v jq >/dev/null 2>&1
[[ -f "${PATCH_ROOT}/series" ]]
[[ -f "${UPSTREAM_ROOT}/UPSTREAM_BASELINE" ]]
[[ -n "${OUTPUT_ROOT}" && "${OUTPUT_ROOT}" != "/" ]]

if [[ -e "${OUTPUT_ROOT}" ]]; then
  echo "error: patched source output already exists: ${OUTPUT_ROOT}" >&2
  exit 1
fi
mkdir -p "${OUTPUT_ROOT}"
cp -a "${UPSTREAM_ROOT}/." "${OUTPUT_ROOT}/"
git -C "${OUTPUT_ROOT}" init -q

VELERO_PATCH="${PATCH_ROOT}/velero/0001-forward-s3-bucket-lookup-to-kopia.patch"
git -C "${OUTPUT_ROOT}" apply --check "${VELERO_PATCH}"
git -C "${OUTPUT_ROOT}" apply "${VELERO_PATCH}"

KOPIA_JSON="$(cd "${UPSTREAM_ROOT}" && GOWORK=off GOPROXY="${GOPROXY_VALUE}" go mod download -json "${KOPIA_MODULE}@${KOPIA_VERSION}")"
KOPIA_SOURCE="$(jq -r '.Dir // empty' <<<"${KOPIA_JSON}")"
[[ -n "${KOPIA_SOURCE}" && -d "${KOPIA_SOURCE}" ]] || {
  echo "error: unable to resolve pinned Kopia source" >&2
  exit 1
}

KOPIA_TARGET="${OUTPUT_ROOT}/third_party/project-velero-kopia"
mkdir -p "${KOPIA_TARGET}"
cp -a "${KOPIA_SOURCE}/." "${KOPIA_TARGET}/"
chmod -R u+w "${KOPIA_TARGET}"
git -C "${KOPIA_TARGET}" init -q
KOPIA_PATCH="${PATCH_ROOT}/kopia/0001-support-s3-bucket-lookup.patch"
git -C "${KOPIA_TARGET}" apply --check "${KOPIA_PATCH}"
git -C "${KOPIA_TARGET}" apply "${KOPIA_PATCH}"

(
  cd "${OUTPUT_ROOT}"
  GOWORK=off go mod edit -replace="github.com/kopia/kopia=./third_party/project-velero-kopia"
)

echo "${OUTPUT_ROOT}"
