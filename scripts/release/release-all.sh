#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

CONFIG_FILE="${SCRIPT_DIR}/release.conf"
REGISTRY_CONFIG_FILE="${HCDR_REGISTRY_CONFIG:-${ROOT_DIR}/config/registries.conf}"
REGISTRY_PROFILE="${HCDR_REGISTRY_PROFILE:-}"
VERSION=""
SKIP_TESTS="${HCDR_RELEASE_SKIP_TESTS:-false}"
LOGIN="true"
CLI_REGISTRY=""
CLI_SKIP_TESTS=""
SKIP_REGISTER="false"
DRY_RUN="false"
RESUME="false"
PLATFORM_URL="${HCDR_PLATFORM_URL:-https://${DEFAULT_HOST}:3002}"
RELEASE_TOKEN_FILE="${HCDR_RELEASE_TOKEN_FILE:-/var/lib/hypercdr/release-token}"

usage() {
  cat <<'USAGE'
Build and push a HyperCDR release.

Usage:
  release-all.sh <version> [options]

Options:
  --config PATH       Release config file, default ./release.conf.
  --registry-config PATH
                      Registry profiles file, default config/registries.conf.
  --registry-profile NAME
                      Override HCDR_ACTIVE_REGISTRY for this release.
  --registry PREFIX   Override HCDR_IMAGE_REGISTRY.
  --skip-tests        Skip Go tests during build.
  --no-login          Skip docker login.
  --platform-url URL  Platform API URL, default https://192.168.8.149:3002.
  --release-token-file PATH
                      Release token file, default /var/lib/hypercdr/release-token.
  --skip-register     Build/push only when no platform exists yet.
  --dry-run           Resolve configuration and print the plan without changes.
  --resume            Continue a failed release after verifying all core images exist remotely.
  -h, --help          Show help.

Required config:
  HCDR_IMAGE_REGISTRY=REGISTRY_HOST/NAMESPACE_OR_PROJECT

Optional registry login config:
  HCDR_REGISTRY_SERVER=registry.example.com
  HCDR_REGISTRY_USERNAME=<username>
  HCDR_REGISTRY_PASSWORD_FILE=/secure/path/to/password
  # or HCDR_REGISTRY_PASSWORD=<password>

If no username is configured, the script uses credentials already stored by
`docker login`. Legacy HCDR_HARBOR_* names remain supported.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) CONFIG_FILE="${2:?missing value for --config}"; shift 2 ;;
    --registry-config) REGISTRY_CONFIG_FILE="${2:?missing value for --registry-config}"; shift 2 ;;
    --registry-profile) REGISTRY_PROFILE="${2:?missing value for --registry-profile}"; shift 2 ;;
    --registry) CLI_REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --skip-tests) CLI_SKIP_TESTS="true"; shift ;;
    --no-login) LOGIN="false"; shift ;;
    --platform-url) PLATFORM_URL="${2:?missing value for --platform-url}"; shift 2 ;;
    --release-token-file) RELEASE_TOKEN_FILE="${2:?missing value for --release-token-file}"; shift 2 ;;
    --skip-register) SKIP_REGISTER="true"; shift ;;
    --dry-run) DRY_RUN="true"; shift ;;
    --resume) RESUME="true"; shift ;;
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

# shellcheck source=../lib/registry-config.sh
source "${ROOT_DIR}/scripts/lib/registry-config.sh"
load_registry_profile "${REGISTRY_CONFIG_FILE}" "${REGISTRY_PROFILE}"

SKIP_TESTS="${HCDR_RELEASE_SKIP_TESTS:-${SKIP_TESTS}}"
if [[ -n "${CLI_REGISTRY}" ]]; then
  HCDR_IMAGE_REGISTRY="${CLI_REGISTRY}"
  HCDR_REGISTRY_SERVER="${CLI_REGISTRY%%/*}"
fi
if [[ -n "${CLI_SKIP_TESTS}" ]]; then
  SKIP_TESTS="${CLI_SKIP_TESTS}"
fi

require_version "${VERSION}"
require_registry "${HCDR_IMAGE_REGISTRY:-}"
require_cmd docker
require_cmd curl

REGISTRY="${HCDR_IMAGE_REGISTRY%/}"
REGISTRY_HOST="${REGISTRY%%/*}"
REGISTRY_SERVER="${HCDR_REGISTRY_SERVER:-${HCDR_HARBOR_SERVER:-${REGISTRY_HOST}}}"
REGISTRY_USERNAME="${HCDR_REGISTRY_USERNAME:-${HCDR_HARBOR_USERNAME:-}}"
REGISTRY_PASSWORD_FILE="${HCDR_REGISTRY_PASSWORD_FILE:-${HCDR_HARBOR_PASSWORD_FILE:-}}"
REGISTRY_PASSWORD="${HCDR_REGISTRY_PASSWORD:-${HCDR_HARBOR_PASSWORD:-}}"

login_registry() {
  if [[ "${LOGIN}" != "true" ]]; then
    log "Skipping docker login"
    return
  fi

  if [[ -z "${REGISTRY_USERNAME}" ]]; then
    log "Using existing Docker credentials for ${REGISTRY_SERVER}"
    return
  fi

  if [[ -n "${REGISTRY_PASSWORD_FILE}" ]]; then
    [[ -r "${REGISTRY_PASSWORD_FILE}" ]] || die "password file is not readable: ${REGISTRY_PASSWORD_FILE}"
    log "Logging in to ${REGISTRY_SERVER} as ${REGISTRY_USERNAME}"
    docker login "${REGISTRY_SERVER}" -u "${REGISTRY_USERNAME}" --password-stdin < "${REGISTRY_PASSWORD_FILE}"
    return
  fi

  if [[ -n "${REGISTRY_PASSWORD}" ]]; then
    log "Logging in to ${REGISTRY_SERVER} as ${REGISTRY_USERNAME}"
    printf '%s' "${REGISTRY_PASSWORD}" | docker login "${REGISTRY_SERVER}" -u "${REGISTRY_USERNAME}" --password-stdin
    return
  fi

  die "registry username is configured but no password source is available"
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
Profile:        ${HCDR_SELECTED_REGISTRY:-command-line}
Registry server: ${REGISTRY_SERVER}
Skip tests:     ${SKIP_TESTS}
EOF

if [[ "${DRY_RUN}" == "true" ]]; then
  cat <<EOF
Images:
  ${REGISTRY}/platform-api:${VERSION}
  ${REGISTRY}/platform-frontend:${VERSION}
  ${REGISTRY}/platform-upgrader:${VERSION}
  ${REGISTRY}/comm-agent:${VERSION}
  ${REGISTRY}/postgres:16
  ${REGISTRY}/velero:${HCDR_VELERO_IMAGE_TAG:-v1.17.1-hcdr.1-20260716}
Dry-run complete; no login, build, push, or registration was performed.
EOF
  exit 0
fi

login_registry

if [[ "${RESUME}" == "true" ]]; then
  log "Resume mode: verifying previously pushed core images"
  for name in platform-api platform-frontend platform-upgrader comm-agent; do
    image="${REGISTRY}/${name}:${VERSION}"
    docker manifest inspect "${image}" >/dev/null 2>&1 || die "cannot resume: core image is unavailable: ${image}"
    log "Resume prerequisite OK: ${image}"
  done
else
  log "Building release images"
  "${SCRIPT_DIR}/build-release.sh" "${build_args[@]}"

  log "Pushing release images"
  "${SCRIPT_DIR}/push-release.sh" "${VERSION}" --registry "${REGISTRY}"
fi

log "Publishing required runtime images"
"${SCRIPT_DIR}/publish-runtime-images.sh" --registry "${REGISTRY}"

log "Mirroring Velero object-storage plugins"
plugin_sync_args=(--registry "${REGISTRY}" --version "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")
if [[ -n "${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY:-}" ]]; then
  plugin_sync_args+=(--source-registry "${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY}")
fi
"${SCRIPT_DIR}/sync-velero-plugins.sh" "${plugin_sync_args[@]}"

log "Verifying pushed image pulls"
for image in \
  "${REGISTRY}/platform-api:${VERSION}" \
  "${REGISTRY}/platform-frontend:${VERSION}" \
  "${REGISTRY}/platform-upgrader:${VERSION}" \
  "${REGISTRY}/comm-agent:${VERSION}" \
  "${REGISTRY}/velero-plugin-for-aws:${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}" \
  "${REGISTRY}/velero-plugin-for-microsoft-azure:${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}" \
  "${REGISTRY}/velero-plugin-for-gcp:${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}"; do
  docker pull "${image}" >/dev/null
  log "Pull OK: ${image}"
done

log "Generating versioned installer package"
"${ROOT_DIR}/bootstrap/release-bootstrap.sh" "${VERSION}"

if [[ "${SKIP_REGISTER}" == "true" ]]; then
  log "Skipping platform release registration"
else
  [[ -r "${RELEASE_TOKEN_FILE}" ]] || die "release token file is not readable: ${RELEASE_TOKEN_FILE}; use --skip-register only for the initial seed release"
  RELEASE_TOKEN="$(tr -d '\r\n' < "${RELEASE_TOKEN_FILE}")"
  [[ -n "${RELEASE_TOKEN}" ]] || die "release token file is empty: ${RELEASE_TOKEN_FILE}"
  log "Registering candidate release ${VERSION} with ${PLATFORM_URL}"
  curl_args=(-fsS --max-time 30 -X POST "${PLATFORM_URL%/}/api/v1/platform/releases" -H "Content-Type: application/json" -H "X-HyperCDR-Release-Token: ${RELEASE_TOKEN}")
  if [[ -n "${HCDR_PLATFORM_CA_FILE:-}" ]]; then
    curl_args+=(--cacert "${HCDR_PLATFORM_CA_FILE}")
  else
    curl_args+=(--insecure)
  fi
  DATABASE_SCHEMA_VERSION="$(find "${ROOT_DIR}/backend/internal/migrations/sql" -maxdepth 1 -type f -name '*.sql' -printf '%f\n' | sort | tail -n 1 | cut -d_ -f1)"
  [[ -n "${DATABASE_SCHEMA_VERSION}" ]] || die "failed to determine database schema version"
  curl "${curl_args[@]}" --data "{\"version\":\"${VERSION}\",\"databaseSchemaVersion\":\"${DATABASE_SCHEMA_VERSION}\",\"minimumAgentVersion\":\"v20260721.4\",\"rollbackSupported\":true,\"releaseNotes\":\"HyperCDR platform ${VERSION}\"}" >/dev/null
  log "Candidate release registered: ${VERSION}"

  log "Registering comm-agent candidate ${VERSION} with ${PLATFORM_URL}"
  component_curl_args=(-fsS --max-time 30 -X POST "${PLATFORM_URL%/}/api/v1/component-releases" -H "Content-Type: application/json" -H "X-HyperCDR-Release-Token: ${RELEASE_TOKEN}")
  if [[ -n "${HCDR_PLATFORM_CA_FILE:-}" ]]; then
    component_curl_args+=(--cacert "${HCDR_PLATFORM_CA_FILE}")
  else
    component_curl_args+=(--insecure)
  fi
  curl "${component_curl_args[@]}" --data "{\"component\":\"comm-agent\",\"version\":\"${VERSION}\",\"image\":\"${REGISTRY}/comm-agent:${VERSION}\",\"releaseNotes\":\"HyperCDR Comm Agent ${VERSION}\"}" >/dev/null
  log "Comm-agent candidate registered: ${VERSION}"
fi

cat <<EOF

SUCCESS: HyperCDR release ${VERSION} completed.
Registry:
  ${REGISTRY}
Installer:
  ${HCDR_BOOTSTRAP_PUBLISH_DIR:-${RUNTIME_ROOT}/bootstrap-portal-source}/releases/dev/hypercdr-installer-${VERSION}.tar.gz

Next:
  Install or upgrade the control plane from the bootstrap page or platform UI.
EOF
