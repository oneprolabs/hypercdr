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
RELEASE_CENTER_URL="${HCDR_RELEASE_CENTER_URL:-}"
RELEASE_CENTER_TOKEN_FILE="${HCDR_RELEASE_CENTER_TOKEN_FILE:-}"

usage() {
  cat <<'USAGE'
Build and push a HyperCDR release.

Usage:
  release-all.sh --config PATH

Options:
  --config PATH       Release config file. Defaults to ./release.conf.
  All release settings, including RELEASE_VERSION, are read from PATH.
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
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (use --help)" ;;
  esac
done

if [[ -f "${CONFIG_FILE}" ]]; then
  # shellcheck disable=SC1090
  source "${CONFIG_FILE}"
elif [[ "${CONFIG_FILE}" != "${SCRIPT_DIR}/release.conf" ]]; then
  die "config file not found: ${CONFIG_FILE}"
fi

VERSION="${RELEASE_VERSION:-${VERSION}}"

# shellcheck source=../lib/registry-config.sh
RELEASE_CENTER_URL="${RELEASE_CENTER_URL:-${HCDR_RELEASE_CENTER_URL:-}}"
RELEASE_CENTER_TOKEN_FILE="${RELEASE_CENTER_TOKEN_FILE:-${HCDR_RELEASE_CENTER_TOKEN_FILE:-}}"
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
require_cmd jq
require_cmd timeout

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
  $(image_ref "${REGISTRY}" "platform-api" "${VERSION}")
  $(image_ref "${REGISTRY}" "platform-frontend" "${VERSION}")
  $(image_ref "${REGISTRY}" "cluster-registration-executor" "${VERSION}")
  $(image_ref "${REGISTRY}" "comm-agent" "${VERSION}")
  $(image_ref "${REGISTRY}" "oadp-comm-agent" "${VERSION}")
  $(image_ref "${REGISTRY}" "oadp-catalog" "1.3.10-hcdr.1")
  $(image_ref "${REGISTRY}" "postgres" "16")
  $(image_ref "${REGISTRY}" "velero" "${HCDR_VELERO_IMAGE_TAG:-v1.18.2-hcdr.4}")
  $(image_ref "${REGISTRY}" "velero-plugin-for-aws" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")
  $(image_ref "${REGISTRY}" "velero-plugin-for-microsoft-azure" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")
  $(image_ref "${REGISTRY}" "velero-plugin-for-gcp" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")
Dry-run complete; no login, build, push, or registration was performed.
EOF
  exit 0
fi

login_registry

if [[ "${RESUME}" == "true" ]]; then
  log "Resume mode: verifying previously pushed core images"
  for name in platform-api platform-frontend cluster-registration-executor comm-agent oadp-comm-agent; do
    image="$(image_ref "${REGISTRY}" "${name}" "${VERSION}")"
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

log "Mirroring the pinned OADP image closure"
"${SCRIPT_DIR}/mirror-community-oadp-images.sh" --registry "${REGISTRY}"
log "Building the pinned OADP bundle"
"${SCRIPT_DIR}/build-community-oadp-bundle.sh" --registry "${REGISTRY}"
log "Building the self-contained OADP catalog"
"${SCRIPT_DIR}/build-community-oadp-catalog.sh" --registry "${REGISTRY}"
OADP_RESOLVED_LOCK="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"

log "Verifying pushed image pulls"
release_images=( \
  "$(image_ref "${REGISTRY}" "platform-api" "${VERSION}")" \
  "$(image_ref "${REGISTRY}" "platform-frontend" "${VERSION}")" \
  "$(image_ref "${REGISTRY}" "cluster-registration-executor" "${VERSION}")" \
  "$(image_ref "${REGISTRY}" "comm-agent" "${VERSION}")" \
  "$(image_ref "${REGISTRY}" "oadp-comm-agent" "${VERSION}")" \
  "$(image_ref "${REGISTRY}" "postgres" "16")" \
  "$(image_ref "${REGISTRY}" "velero" "${HCDR_VELERO_IMAGE_TAG:-v1.18.2-hcdr.4}")" \
  "$(image_ref "${REGISTRY}" "velero-plugin-for-aws" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")" \
  "$(image_ref "${REGISTRY}" "velero-plugin-for-microsoft-azure" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")" \
  "$(image_ref "${REGISTRY}" "velero-plugin-for-gcp" "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")" )
while IFS= read -r image; do release_images+=("${image}"); done < <(jq -r '.images[].target,.bundle.image,.catalog.image' "${OADP_RESOLVED_LOCK}")
for image in "${release_images[@]}"; do
  docker pull "${image}" >/dev/null
  log "Pull OK: ${image}"
done

remote_digest() {
  local image="$1" digest
  digest="$(image_digest "${image}")"
  [[ -n "${digest}" ]] || die "remote digest is unavailable after pulling ${image}"
  printf '%s' "${digest}"
}

PLATFORM_API_IMAGE="$(image_ref "${REGISTRY}" "platform-api" "${VERSION}")"
PLATFORM_FRONTEND_IMAGE="$(image_ref "${REGISTRY}" "platform-frontend" "${VERSION}")"
REGISTRATION_EXECUTOR_IMAGE="$(image_ref "${REGISTRY}" "cluster-registration-executor" "${VERSION}")"
COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" "comm-agent" "${VERSION}")"
OADP_COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" "oadp-comm-agent" "${VERSION}")"
OADP_RELEASE="$(jq -er .release "${OADP_RESOLVED_LOCK}")"
oadp_image() { jq -er --arg component "$1" '.images[]|select(.component==$component)|.target' "${OADP_RESOLVED_LOCK}"; }
OADP_OPERATOR_IMAGE="$(oadp_image oadp-operator)"
OADP_VELERO_IMAGE="$(oadp_image oadp-velero)"
OADP_OPENSHIFT_PLUGIN_IMAGE="$(oadp_image oadp-openshift-plugin)"
OADP_AWS_PLUGIN_IMAGE="$(oadp_image oadp-aws-plugin)"
OADP_RESTORE_HELPER_IMAGE="$(oadp_image oadp-restore-helper)"
OADP_BUNDLE_IMAGE="$(jq -er .bundle.image "${OADP_RESOLVED_LOCK}")"
OADP_CATALOG_IMAGE="$(jq -er .catalog.image "${OADP_RESOLVED_LOCK}")"
VELERO_VERSION="${HCDR_VELERO_IMAGE_TAG:-v1.18.2-hcdr.4}"
VELERO_IMAGE="$(image_ref "${REGISTRY}" "velero" "${VELERO_VERSION}")"
PLUGIN_VERSION="${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}"
AWS_PLUGIN_IMAGE="$(image_ref "${REGISTRY}" "velero-plugin-for-aws" "${PLUGIN_VERSION}")"
AZURE_PLUGIN_IMAGE="$(image_ref "${REGISTRY}" "velero-plugin-for-microsoft-azure" "${PLUGIN_VERSION}")"
GCP_PLUGIN_IMAGE="$(image_ref "${REGISTRY}" "velero-plugin-for-gcp" "${PLUGIN_VERSION}")"

RELEASE_MANIFEST="$(release_work_dir "${VERSION}")/release-manifest.json"
cat >"${RELEASE_MANIFEST}" <<EOF
{
  "version": "${VERSION}",
  "databaseSchemaVersion": "$(find "${ROOT_DIR}/backend/internal/migrations/sql" -maxdepth 1 -type f -name '*.sql' -printf '%f\n' | sort | tail -n 1 | cut -d_ -f1)",
  "rollbackSupported": true,
  "componentManifest": {
    "platform-api": {"version":"${VERSION}","image":"${PLATFORM_API_IMAGE}","imageDigest":"$(remote_digest "${PLATFORM_API_IMAGE}")"},
    "platform-frontend": {"version":"${VERSION}","image":"${PLATFORM_FRONTEND_IMAGE}","imageDigest":"$(remote_digest "${PLATFORM_FRONTEND_IMAGE}")"},
    "cluster-registration-executor": {"version":"${VERSION}","image":"${REGISTRATION_EXECUTOR_IMAGE}","imageDigest":"$(remote_digest "${REGISTRATION_EXECUTOR_IMAGE}")"},
    "comm-agent": {"version":"${VERSION}","image":"${COMM_AGENT_IMAGE}","imageDigest":"$(remote_digest "${COMM_AGENT_IMAGE}")"},
    "oadp-comm-agent": {"version":"${VERSION}","image":"${OADP_COMM_AGENT_IMAGE}","imageDigest":"$(remote_digest "${OADP_COMM_AGENT_IMAGE}")"},
    "oadp-operator": {"version":"1.3.10","image":"${OADP_OPERATOR_IMAGE}","imageDigest":"$(remote_digest "${OADP_OPERATOR_IMAGE}")"},
    "oadp-velero": {"version":"${OADP_RELEASE}","image":"${OADP_VELERO_IMAGE}","imageDigest":"$(remote_digest "${OADP_VELERO_IMAGE}")"},
    "oadp-openshift-plugin": {"version":"${OADP_RELEASE}","image":"${OADP_OPENSHIFT_PLUGIN_IMAGE}","imageDigest":"$(remote_digest "${OADP_OPENSHIFT_PLUGIN_IMAGE}")"},
    "oadp-aws-plugin": {"version":"${OADP_RELEASE}","image":"${OADP_AWS_PLUGIN_IMAGE}","imageDigest":"$(remote_digest "${OADP_AWS_PLUGIN_IMAGE}")"},
    "oadp-restore-helper": {"version":"${OADP_RELEASE}","image":"${OADP_RESTORE_HELPER_IMAGE}","imageDigest":"$(remote_digest "${OADP_RESTORE_HELPER_IMAGE}")"},
    "oadp-bundle": {"version":"${OADP_RELEASE}","image":"${OADP_BUNDLE_IMAGE}","imageDigest":"$(remote_digest "${OADP_BUNDLE_IMAGE}")"},
    "oadp-catalog": {"version":"${OADP_RELEASE}","image":"${OADP_CATALOG_IMAGE}","imageDigest":"$(remote_digest "${OADP_CATALOG_IMAGE}")"},
    "velero": {"version":"${VELERO_VERSION}","image":"${VELERO_IMAGE}","imageDigest":"$(remote_digest "${VELERO_IMAGE}")"},
    "velero-plugin-for-aws": {"version":"${PLUGIN_VERSION}","image":"${AWS_PLUGIN_IMAGE}","imageDigest":"$(remote_digest "${AWS_PLUGIN_IMAGE}")"},
    "velero-plugin-for-microsoft-azure": {"version":"${PLUGIN_VERSION}","image":"${AZURE_PLUGIN_IMAGE}","imageDigest":"$(remote_digest "${AZURE_PLUGIN_IMAGE}")"},
    "velero-plugin-for-gcp": {"version":"${PLUGIN_VERSION}","image":"${GCP_PLUGIN_IMAGE}","imageDigest":"$(remote_digest "${GCP_PLUGIN_IMAGE}")"}
  }
}
EOF
log "Complete release manifest generated: ${RELEASE_MANIFEST}"

log "Generating versioned installer package"
HCDR_RELEASE_MANIFEST="${RELEASE_MANIFEST}" "${ROOT_DIR}/scripts/release/package-release.sh" "${VERSION}"

if [[ "${SKIP_REGISTER}" == "true" || -z "${RELEASE_CENTER_URL}" ]]; then
  log "Release Center registration disabled (no URL configured)"
else
  [[ -r "${RELEASE_CENTER_TOKEN_FILE:-}" ]] || die "Release Center token file is not readable: ${RELEASE_CENTER_TOKEN_FILE}"
  RELEASE_CENTER_TOKEN="$(tr -d '\r\n' < "${RELEASE_CENTER_TOKEN_FILE}")"
  INSTALLER_ARCHIVE="${HCDR_RELEASE_ROOT:-${RUNTIME_ROOT}/releases/community}/${VERSION}/hypercdr-installer-${VERSION}.tar.gz"
  [[ -r "${INSTALLER_ARCHIVE}" ]] || die "release installer archive is missing: ${INSTALLER_ARCHIVE}"
  publish_payload="$(mktemp)"
  trap 'rm -f "${publish_payload}"' EXIT
  jq '.' "${RELEASE_MANIFEST}" >"${publish_payload}"
  curl_args=(-fsS --max-time 30 -X POST "${RELEASE_CENTER_URL%/}/api/v1/releases" -H "Content-Type: application/json" -H "Authorization: Bearer ${RELEASE_CENTER_TOKEN}")
  if [[ -n "${HCDR_RELEASE_CENTER_CA_FILE:-}" ]]; then
    curl_args+=(--cacert "${HCDR_RELEASE_CENTER_CA_FILE}")
  fi
  curl "${curl_args[@]}" --data-binary "@${publish_payload}" >/dev/null
  curl_args=(-fsS --max-time 120 -X POST "${RELEASE_CENTER_URL%/}/api/v1/releases/${VERSION}/installer" -H "Content-Type: application/gzip" -H "Authorization: Bearer ${RELEASE_CENTER_TOKEN}")
  if [[ -n "${HCDR_RELEASE_CENTER_CA_FILE:-}" ]]; then curl_args+=(--cacert "${HCDR_RELEASE_CENTER_CA_FILE}"); fi
  curl "${curl_args[@]}" --data-binary "@${INSTALLER_ARCHIVE}" >/dev/null
  log "Release registered with Release Center: ${VERSION}"
fi

cat <<EOF

SUCCESS: HyperCDR release ${VERSION} completed.
Registry:
  ${REGISTRY}
Installer:
  ${HCDR_RELEASE_ROOT:-${RUNTIME_ROOT}/releases/community}/${VERSION}/hypercdr-installer-${VERSION}.tar.gz

Next:
  Install from the bootstrap package; upgrade through the blue/green release pipeline.
EOF
