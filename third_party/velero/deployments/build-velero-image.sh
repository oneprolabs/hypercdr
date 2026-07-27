#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
IMAGE_TAG="${HCDR_VELERO_IMAGE_TAG:-v1.17.1-hcdr.1-20260716}"
VELERO_VERSION="${HCDR_VELERO_VERSION:-v1.17.1-hcdr.1}"
UPSTREAM_COMMIT="${HCDR_VELERO_UPSTREAM_COMMIT:-94f64639cee09c5caaa65b65ab5f42175f41c101}"
GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"
RESTIC_VERSION="${HCDR_RESTIC_VERSION:-0.15.0}"
RESTIC_MODULE_PROXY="${HCDR_RESTIC_MODULE_PROXY:-https://goproxy.cn}"
PLATFORM="${HCDR_BUILD_PLATFORM:-linux/amd64}"
BUILD_DIR="${HCDR_VELERO_BUILD_DIR:-/tmp/hypercdr-velero-build/${IMAGE_TAG}}"
PUSH="false"

usage() {
  cat <<'USAGE'
Build the HyperCDR Velero image with the upstream Velero Dockerfile.

Usage:
  build-velero-image.sh [options]

Options:
  --registry REGISTRY       Target registry prefix.
  --tag TAG                 Immutable image tag.
  --version VERSION         Version embedded in the Velero binaries.
  --platform PLATFORM       Target platform, default linux/amd64.
  --build-dir PATH          External metadata/temp directory.
  --restic-module-proxy URL Go module proxy used for restic source.
  --push                    Push after the local image passes validation.
  -h, --help                Show help.
USAGE
}

die() { echo "error: $*" >&2; exit 1; }
log() { echo "==> $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --tag) IMAGE_TAG="${2:?missing value for --tag}"; shift 2 ;;
    --version) VELERO_VERSION="${2:?missing value for --version}"; shift 2 ;;
    --platform) PLATFORM="${2:?missing value for --platform}"; shift 2 ;;
    --build-dir) BUILD_DIR="${2:?missing value for --build-dir}"; shift 2 ;;
    --restic-module-proxy) RESTIC_MODULE_PROXY="${2:?missing value for --restic-module-proxy}"; shift 2 ;;
    --push) PUSH="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

command -v docker >/dev/null 2>&1 || die "docker is required"
[[ -n "${REGISTRY}" ]] || die "--registry is required"
docker buildx version >/dev/null 2>&1 || die "docker buildx is required"
[[ -f "${ROOT_DIR}/Dockerfile" ]] || die "upstream Dockerfile not found"
[[ -f "${ROOT_DIR}/UPSTREAM_BASELINE" ]] || die "UPSTREAM_BASELINE not found"
grep -qx "${UPSTREAM_COMMIT}" "${ROOT_DIR}/UPSTREAM_BASELINE" || die "unexpected upstream baseline"
[[ "${IMAGE_TAG}" != *official* ]] || die "custom builds must not use an official tag"

REGISTRY="${REGISTRY%/}"
IMAGE="${REGISTRY}/velero:${IMAGE_TAG}"
MONOREPO_COMMIT="$(git -C "${ROOT_DIR}" rev-parse HEAD 2>/dev/null || true)"
[[ -n "${MONOREPO_COMMIT}" ]] || die "unable to determine source commit"
mkdir -p "${BUILD_DIR}"
RESTIC_ZIP="${BUILD_DIR}/restic-v${RESTIC_VERSION}.zip"
RESTIC_SOURCE_DIR="${BUILD_DIR}/restic-source"
if [[ ! -f "${RESTIC_SOURCE_DIR}/build.go" ]]; then
  command -v curl >/dev/null 2>&1 || die "curl is required"
  command -v unzip >/dev/null 2>&1 || die "unzip is required"
  log "Downloading restic v${RESTIC_VERSION} source outside the repository"
  curl --fail --location --retry 3 --retry-all-errors \
    "${RESTIC_MODULE_PROXY%/}/github.com/restic/restic/@v/v${RESTIC_VERSION}.zip" \
    --output "${RESTIC_ZIP}"
  RESTIC_UNPACK_DIR="${BUILD_DIR}/restic-unpack"
  rm -rf "${RESTIC_SOURCE_DIR}" "${RESTIC_UNPACK_DIR}"
  mkdir -p "${RESTIC_SOURCE_DIR}"
  unzip -q "${RESTIC_ZIP}" -d "${RESTIC_UNPACK_DIR}"
  RESTIC_MODULE_DIR="$(find "${RESTIC_UNPACK_DIR}" -type f -name build.go -printf '%h\n' -quit)"
  [[ -n "${RESTIC_MODULE_DIR}" ]] || die "restic source archive is invalid"
  cp -a "${RESTIC_MODULE_DIR}/." "${RESTIC_SOURCE_DIR}/"
fi

log "Building ${IMAGE} from upstream baseline ${UPSTREAM_COMMIT}"
log "Build metadata and temporary exports: ${BUILD_DIR}"
docker buildx build --pull --load \
  --build-context "restic-source=${RESTIC_SOURCE_DIR}" \
  --platform "${PLATFORM}" \
  --provenance=false --sbom=false \
  --build-arg "GOPROXY=${GOPROXY}" \
  --build-arg "PKG=github.com/vmware-tanzu/velero" \
  --build-arg "BIN=velero" \
  --build-arg "VERSION=${VELERO_VERSION}" \
  --build-arg "GIT_SHA=${MONOREPO_COMMIT}" \
  --build-arg "GIT_TREE_STATE=clean" \
  --build-arg "REGISTRY=${REGISTRY}" \
  --build-arg "RESTIC_VERSION=${RESTIC_VERSION}" \
  --metadata-file "${BUILD_DIR}/build-metadata.json" \
  -t "${IMAGE}" -f "${ROOT_DIR}/Dockerfile" "${ROOT_DIR}"

log "Validating image contents"
VALIDATION_CONTAINER="$(docker create "${IMAGE}" /velero version --client-only)"
trap 'docker rm -f "${VALIDATION_CONTAINER}" >/dev/null 2>&1 || true' EXIT
IMAGE_FILES="$(docker export "${VALIDATION_CONTAINER}" | tar -tf -)"
for binary in velero velero-helper velero-restore-helper usr/bin/restic; do
  grep -qx "${binary}" <<<"${IMAGE_FILES}" || die "missing executable /${binary}"
done
docker run --rm --entrypoint /velero "${IMAGE}" version --client-only
docker run --rm --entrypoint /usr/bin/restic "${IMAGE}" version
[[ "$(docker image inspect "${IMAGE}" --format '{{.Config.User}}')" == "cnb:cnb" ]] \
  || die "unexpected runtime user"

if [[ "${PUSH}" == "true" ]]; then
  log "Pushing ${IMAGE}"
  docker push "${IMAGE}"
fi

docker image inspect "${IMAGE}" --format 'image={{index .RepoTags 0}} id={{.Id}} user={{.Config.User}}'
