#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

REGISTRY="${HCDR_IMAGE_REGISTRY:-192.168.8.149/hypercdr}"
IMAGE_TAG="${HCDR_VELERO_IMAGE_TAG:-v1.17.1-helperfix}"
VELERO_VERSION="${HCDR_VELERO_VERSION:-v1.17.1}"
GO_BIN="${HCDR_GO_BIN:-/usr/local/go/bin/go}"
GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}"
RUNTIME_IMAGE="${HCDR_VELERO_RUNTIME_IMAGE:-docker.m.daocloud.io/paketobuildpacks/run-jammy-tiny:0.2.78}"
WORK_DIR="${HCDR_VELERO_BUILD_DIR:-/tmp/hypercdr-image-build/velero}"
PUSH="false"

usage() {
  cat <<'USAGE'
Build the HyperCDR Velero image from local Velero source.

Usage:
  build-velero-image.sh [options]

Options:
  --registry REGISTRY       Target registry prefix, default 192.168.8.149/hypercdr.
  --tag TAG                 Image tag, default v1.17.1-helperfix.
  --version VERSION         Velero build version, default v1.17.1.
  --runtime-image IMAGE     Runtime base image.
  --push                    Push image after build.
  -h, --help                Show help.
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --tag) IMAGE_TAG="${2:?missing value for --tag}"; shift 2 ;;
    --version) VELERO_VERSION="${2:?missing value for --version}"; shift 2 ;;
    --runtime-image) RUNTIME_IMAGE="${2:?missing value for --runtime-image}"; shift 2 ;;
    --push) PUSH="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -x "${GO_BIN}" ]] || die "Go binary not found or not executable: ${GO_BIN}"
command -v docker >/dev/null 2>&1 || die "docker is required"

REGISTRY="${REGISTRY%/}"
IMAGE="${REGISTRY}/velero:${IMAGE_TAG}"
PKG="github.com/vmware-tanzu/velero"
LDFLAGS="-s -w -X ${PKG}/pkg/buildinfo.Version=${VELERO_VERSION} -X ${PKG}/pkg/buildinfo.GitSHA=hypercdr-source -X ${PKG}/pkg/buildinfo.GitTreeState=dirty -X ${PKG}/pkg/buildinfo.ImageRegistry=${REGISTRY}"

log "Building Velero binaries from ${ROOT_DIR}"
rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}/rootfs/usr/bin"

(
  cd "${ROOT_DIR}"
  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="${LDFLAGS}" -o "${WORK_DIR}/rootfs/velero" ./cmd/velero

  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="${LDFLAGS}" -o "${WORK_DIR}/rootfs/velero-helper" ./cmd/velero-helper

  PATH="$(dirname "${GO_BIN}"):${PATH}" \
    GOTOOLCHAIN=local GOPROXY="${GOPROXY}" \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "${GO_BIN}" build -trimpath -ldflags="${LDFLAGS}" -o "${WORK_DIR}/rootfs/velero-restore-helper" ./cmd/velero-restore-helper
)

cp "${WORK_DIR}/rootfs/velero-helper" "${WORK_DIR}/rootfs/usr/bin/velero-helper"
cp "${WORK_DIR}/rootfs/velero-restore-helper" "${WORK_DIR}/rootfs/usr/bin/velero-restore-helper"

cat >"${WORK_DIR}/Dockerfile" <<EOF
FROM ${RUNTIME_IMAGE}

COPY rootfs/ /

USER cnb:cnb
ENTRYPOINT ["/velero"]
EOF

log "Building image ${IMAGE}"
docker build -t "${IMAGE}" "${WORK_DIR}"

log "Checking Velero image"
docker run --rm --entrypoint /velero "${IMAGE}" version --client-only

if [[ "${PUSH}" == "true" ]]; then
  log "Pushing ${IMAGE}"
  docker push "${IMAGE}"
fi

cat <<EOF

Built image:
  ${IMAGE}
EOF
