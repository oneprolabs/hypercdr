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
SKIP_REGISTER="false"
DRY_RUN="false"
RESUME="false"
PHASE="${HCDR_RELEASE_PHASE:-all}"
COMPONENT="${HCDR_RELEASE_COMPONENT:-all}"

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
  RELEASE_VERSION=MAJOR.MINOR.PATCH.YYYYMMDD
  HCDR_IMAGE_REGISTRY=REGISTRY_HOST/NAMESPACE_OR_PROJECT
  HCDR_RELEASE_SECRETS_FILE=./release.secrets.conf

Required secrets file values:
  HCDR_REGISTRY_SERVER=registry.example.com
  HCDR_REGISTRY_USERNAME=<username>
  HCDR_REGISTRY_PASSWORD_FILE=/secure/path/to/password
  # or HCDR_REGISTRY_PASSWORD=<password>
  HCDR_TURNSTILE_SECRET_KEY=<matching-secret-key>

Relative secret-file paths are resolved from the directory containing the
release config. The populated secrets file is local-only. The resulting private
installer contains the Turnstile deployment secret and must be protected.
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
  CONFIG_FILE="$(cd "$(dirname "${CONFIG_FILE}")" && pwd)/$(basename "${CONFIG_FILE}")"
  # shellcheck disable=SC1090
  source "${CONFIG_FILE}"
elif [[ "${CONFIG_FILE}" != "${SCRIPT_DIR}/release.conf" ]]; then
  die "config file not found: ${CONFIG_FILE}"
fi

[[ -n "${HCDR_RELEASE_SECRETS_FILE:-}" ]] || die "HCDR_RELEASE_SECRETS_FILE is required in ${CONFIG_FILE}"
if [[ "${HCDR_RELEASE_SECRETS_FILE}" = /* ]]; then
  RELEASE_SECRETS_FILE="${HCDR_RELEASE_SECRETS_FILE}"
else
  RELEASE_SECRETS_FILE="$(dirname "${CONFIG_FILE}")/${HCDR_RELEASE_SECRETS_FILE}"
fi
[[ -r "${RELEASE_SECRETS_FILE}" ]] || die "release secrets file is not readable: ${RELEASE_SECRETS_FILE} (copy release.secrets.conf.example and populate it)"
# shellcheck disable=SC1090
source "${RELEASE_SECRETS_FILE}"

VERSION="${RELEASE_VERSION:-${VERSION}}"

# shellcheck source=../lib/registry-config.sh
source "${ROOT_DIR}/scripts/lib/registry-config.sh"
load_registry_profile "${REGISTRY_CONFIG_FILE}" "${REGISTRY_PROFILE}"

SKIP_TESTS="${HCDR_RELEASE_SKIP_TESTS:-${SKIP_TESTS}}"

case "${PHASE}" in
  all|core|dependencies|finalize) ;;
  *) die "HCDR_RELEASE_PHASE must be all, core, dependencies, or finalize" ;;
esac
case "${COMPONENT}" in
  all|api|frontend|agents|executor) ;;
  *) die "HCDR_RELEASE_COMPONENT must be all, api, frontend, agents, or executor" ;;
esac

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
AUTH_CHALLENGE_MODE="${HCDR_AUTH_CHALLENGE_MODE:-turnstile}"
TURNSTILE_SITE_KEY="${HCDR_TURNSTILE_SITE_KEY:-}"
TURNSTILE_SECRET_KEY="${HCDR_TURNSTILE_SECRET_KEY:-}"
TURNSTILE_VERIFY_URL="${HCDR_TURNSTILE_VERIFY_URL:-https://challenges.cloudflare.com/turnstile/v0/siteverify}"

case "${AUTH_CHALLENGE_MODE}" in
  image) ;;
  turnstile)
    [[ -n "${TURNSTILE_SITE_KEY}" ]] || die "HCDR_TURNSTILE_SITE_KEY is required when Turnstile is enabled"
    [[ -n "${TURNSTILE_SECRET_KEY}" ]] || die "HCDR_TURNSTILE_SECRET_KEY is required in ${RELEASE_SECRETS_FILE} when Turnstile is enabled"
    [[ "${TURNSTILE_VERIFY_URL}" == https://* ]] || die "HCDR_TURNSTILE_VERIFY_URL must use HTTPS"
    ;;
  *) die "HCDR_AUTH_CHALLENGE_MODE must be image or turnstile" ;;
esac

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
Login challenge: ${AUTH_CHALLENGE_MODE}
Skip tests:     ${SKIP_TESTS}
Phase:          ${PHASE}
Core component: ${COMPONENT}
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

if [[ "${PHASE}" == "all" || "${PHASE}" == "core" ]]; then
  case "${COMPONENT}" in
    all) core_images=(platform-api platform-frontend cluster-registration-executor comm-agent oadp-comm-agent) ;;
    api) core_images=(platform-api) ;;
    frontend) core_images=(platform-frontend) ;;
    agents) core_images=(comm-agent oadp-comm-agent) ;;
    executor) core_images=(cluster-registration-executor) ;;
  esac
  if [[ "${RESUME}" == "true" ]]; then
    log "Resume mode: verifying previously pushed core images"
    for name in "${core_images[@]}"; do
      image="$(image_ref "${REGISTRY}" "${name}" "${VERSION}")"
      docker manifest inspect "${image}" >/dev/null 2>&1 || die "cannot resume: core image is unavailable: ${image}"
      log "Resume prerequisite OK: ${image}"
    done
  else
    log "Building release images"
    "${SCRIPT_DIR}/build-release.sh" "${build_args[@]}"

    log "Pushing release images"
    "${SCRIPT_DIR}/push-release.sh" "${VERSION}" --registry "${REGISTRY}" --component "${COMPONENT}"
  fi
fi

if [[ "${PHASE}" == "core" ]]; then
  log "Core release images published"
  exit 0
fi

if [[ "${PHASE}" == "all" || "${PHASE}" == "dependencies" ]]; then
  DEPENDENCY_STEP_TIMEOUT="${HCDR_DEPENDENCY_STEP_TIMEOUT_SECONDS:-3600}"
  [[ "${DEPENDENCY_STEP_TIMEOUT}" =~ ^[1-9][0-9]*$ ]] || die "HCDR_DEPENDENCY_STEP_TIMEOUT_SECONDS must be a positive integer"
  log "Publishing required runtime images"
  timeout "${DEPENDENCY_STEP_TIMEOUT}" "${SCRIPT_DIR}/publish-runtime-images.sh" --registry "${REGISTRY}"

  log "Mirroring Velero object-storage plugins"
  plugin_sync_args=(--registry "${REGISTRY}" --version "${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}")
  if [[ -n "${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY:-}" ]]; then
    plugin_sync_args+=(--source-registry "${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY}")
  fi
  timeout "${DEPENDENCY_STEP_TIMEOUT}" "${SCRIPT_DIR}/sync-velero-plugins.sh" "${plugin_sync_args[@]}"

  log "Mirroring the pinned OADP image closure"
  timeout "${DEPENDENCY_STEP_TIMEOUT}" "${SCRIPT_DIR}/mirror-community-oadp-images.sh" --registry "${REGISTRY}"
  log "Building the pinned OADP bundle"
  timeout "${DEPENDENCY_STEP_TIMEOUT}" "${SCRIPT_DIR}/build-community-oadp-bundle.sh" --registry "${REGISTRY}"
  log "Building the self-contained OADP catalog"
  timeout "${DEPENDENCY_STEP_TIMEOUT}" "${SCRIPT_DIR}/build-community-oadp-catalog.sh" --registry "${REGISTRY}"
fi

OADP_RESOLVED_LOCK="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"

if [[ "${PHASE}" == "dependencies" ]]; then
  [[ -r "${OADP_RESOLVED_LOCK}" ]] || die "resolved OADP image lock is missing: ${OADP_RESOLVED_LOCK}"
  log "Release dependencies published: ${OADP_RESOLVED_LOCK}"
  exit 0
fi

[[ -r "${OADP_RESOLVED_LOCK}" ]] || die "resolved OADP image lock is missing: ${OADP_RESOLVED_LOCK}"

log "Verifying pushed image pulls"
PULL_PARALLELISM="${HCDR_RELEASE_PULL_PARALLELISM:-4}"
PULL_TIMEOUT="${HCDR_RELEASE_PULL_TIMEOUT_SECONDS:-3600}"
[[ "${PULL_PARALLELISM}" =~ ^[1-9][0-9]*$ ]] || die "HCDR_RELEASE_PULL_PARALLELISM must be a positive integer"
[[ "${PULL_TIMEOUT}" =~ ^[1-9][0-9]*$ ]] || die "HCDR_RELEASE_PULL_TIMEOUT_SECONDS must be a positive integer"
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

pull_release_image() {
  local image="$1" attempt status
  for attempt in 1 2 3; do
    if timeout "${PULL_TIMEOUT}" docker pull "${image}" >/dev/null; then
      echo "==> Pull OK: ${image}"
      return 0
    else
      status=$?
    fi
    echo "pull ${image} failed (attempt ${attempt}/3, status ${status})" >&2
    (( attempt == 3 )) || sleep $((attempt * 5))
  done
  return "${status}"
}
export PULL_TIMEOUT
export -f pull_release_image
printf '%s\0' "${release_images[@]}" \
  | xargs -0 -P "${PULL_PARALLELISM}" -n 1 bash -c 'pull_release_image "$1"' _

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
mkdir -p "$(dirname "${RELEASE_MANIFEST}")"
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
HCDR_RELEASE_MANIFEST="${RELEASE_MANIFEST}" \
HCDR_AUTH_CHALLENGE_MODE="${AUTH_CHALLENGE_MODE}" \
HCDR_TURNSTILE_SITE_KEY="${TURNSTILE_SITE_KEY}" \
HCDR_TURNSTILE_SECRET_KEY="${TURNSTILE_SECRET_KEY}" \
HCDR_TURNSTILE_VERIFY_URL="${TURNSTILE_VERIFY_URL}" \
  "${ROOT_DIR}/scripts/release/package-release.sh" "${VERSION}"

log "GitHub Actions publishes the manifest and installer assets."

cat <<EOF

SUCCESS: HyperCDR release ${VERSION} completed.
Registry:
  ${REGISTRY}
Installer:
  ${HCDR_RELEASE_ROOT:-${RUNTIME_ROOT}/releases/community}/${VERSION}/hypercdr-installer-${VERSION}.tar.gz

Next:
  Install from deploy/online/install.sh; upgrade through the blue/green release pipeline.
EOF
