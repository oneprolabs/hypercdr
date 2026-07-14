#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

CONFIG_FILE="${SCRIPT_DIR}/release.conf"
VERSION=""
SKIP_TESTS="${HCDR_RELEASE_SKIP_TESTS:-false}"
LOGIN="true"
CLI_REGISTRY=""
CLI_SKIP_TESTS=""

usage() {
  cat <<'USAGE'
Build and push a HyperCDR release.

Usage:
  release-all.sh <version> [options]

Options:
  --config PATH       Release config file, default ./release.conf.
  --registry PREFIX   Override HCDR_IMAGE_REGISTRY.
  --skip-tests        Skip Go tests during build.
  --no-login          Skip docker login.
  -h, --help          Show help.

Required config:
  HCDR_IMAGE_REGISTRY=HOST:PORT/hypercdr

Optional Harbor login config:
  HCDR_HARBOR_SERVER=HOST:PORT
  HCDR_HARBOR_USERNAME=admin
  HCDR_HARBOR_PASSWORD_FILE=/path/to/password
  # or HCDR_HARBOR_PASSWORD=<password>
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) CONFIG_FILE="${2:?missing value for --config}"; shift 2 ;;
    --registry) CLI_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --skip-tests) CLI_SKIP_TESTS="true"; shift ;;
    --no-login) LOGIN="false"; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      if [[ -z "${VERSION}" ]]; then VERSION="$1"; shift; else die "unknown argument: $1"; fi
      ;;
  esac
done

if [[ -f "${CONFIG_FILE}" ]]; then
  # shellcheck disable=SC1090
  source "${CONFIG_FILE}"
elif [[ "${CONFIG_FILE}" != "${SCRIPT_DIR}/release.conf" ]]; then
  die "config file not found: ${CONFIG_FILE}"
fi

SKIP_TESTS="${HCDR_RELEASE_SKIP_TESTS:-${SKIP_TESTS}}"
if [[ -n "${CLI_REGISTRY}" ]]; then
  HCDR_IMAGE_REGISTRY="${CLI_REGISTRY}"
fi
if [[ -n "${CLI_SKIP_TESTS}" ]]; then
  SKIP_TESTS="${CLI_SKIP_TESTS}"
fi

require_version "${VERSION}"
require_registry "${HCDR_IMAGE_REGISTRY:-}"
require_cmd docker

REGISTRY="${HCDR_IMAGE_REGISTRY%/}"
REGISTRY_HOST="${REGISTRY%%/*}"
HARBOR_SERVER="${HCDR_HARBOR_SERVER:-${REGISTRY_HOST}}"

login_harbor() {
  if [[ "${LOGIN}" != "true" ]]; then
    log "Skipping docker login"
    return
  fi

  if [[ -z "${HCDR_HARBOR_USERNAME:-}" ]]; then
    log "Skipping docker login: HCDR_HARBOR_USERNAME is not set"
    return
  fi

  if [[ -n "${HCDR_HARBOR_PASSWORD_FILE:-}" ]]; then
    [[ -r "${HCDR_HARBOR_PASSWORD_FILE}" ]] || die "password file is not readable: ${HCDR_HARBOR_PASSWORD_FILE}"
    log "Logging in to Harbor ${HARBOR_SERVER} as ${HCDR_HARBOR_USERNAME}"
    docker login "${HARBOR_SERVER}" -u "${HCDR_HARBOR_USERNAME}" --password-stdin < "${HCDR_HARBOR_PASSWORD_FILE}"
    return
  fi

  if [[ -n "${HCDR_HARBOR_PASSWORD:-}" ]]; then
    log "Logging in to Harbor ${HARBOR_SERVER} as ${HCDR_HARBOR_USERNAME}"
    printf '%s' "${HCDR_HARBOR_PASSWORD}" | docker login "${HARBOR_SERVER}" -u "${HCDR_HARBOR_USERNAME}" --password-stdin
    return
  fi

  log "Skipping docker login: no Harbor password source configured"
}

build_args=("${VERSION}" --registry "${REGISTRY}")
if [[ "${SKIP_TESTS}" == "true" ]]; then
  build_args+=(--skip-tests)
fi
if [[ -n "${HCDR_BUILD_GOPROXY:-}" ]]; then
  build_args+=(--goproxy "${HCDR_BUILD_GOPROXY}")
fi
if [[ -n "${HCDR_BUILD_NPM_REGISTRY:-}" ]]; then
  build_args+=(--npm-registry "${HCDR_BUILD_NPM_REGISTRY}")
fi

cat <<EOF
HyperCDR release plan

Version:        ${VERSION}
Registry:       ${REGISTRY}
Harbor server:  ${HARBOR_SERVER}
Skip tests:     ${SKIP_TESTS}
EOF

login_harbor

log "Building release images"
"${SCRIPT_DIR}/build-release.sh" "${build_args[@]}"

log "Pushing release images"
"${SCRIPT_DIR}/push-release.sh" "${VERSION}" --registry "${REGISTRY}"

log "Verifying pushed image pulls"
for image in \
  "${REGISTRY}/platform-api:${VERSION}" \
  "${REGISTRY}/platform-frontend:${VERSION}" \
  "${REGISTRY}/comm-agent:${VERSION}"; do
  docker pull "${image}" >/dev/null
  log "Pull OK: ${image}"
done

cat <<EOF

SUCCESS: HyperCDR release ${VERSION} completed.
Registry:
  ${REGISTRY}

Next:
  Install or upgrade the control plane from the bootstrap page or platform UI.
EOF
