#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-$(cd "${ROOT_DIR}/.." && pwd)/hypercdr-runtime}"
VERSION="${1:-}"
BUILD_ROOT="${HCDR_PACKAGE_BUILD_ROOT:-${RUNTIME_ROOT}/build/installer/community}"
RELEASE_ROOT="${HCDR_RELEASE_ROOT:-${RUNTIME_ROOT}/releases/community}"
PUBLISH_DIR="${HCDR_BOOTSTRAP_PUBLISH_DIR:-${RUNTIME_ROOT}/services/bootstrap-portal/source}"
if [[ -n "${HCDR_RELEASE_MANIFEST:-}" ]]; then
  RELEASE_MANIFEST="${HCDR_RELEASE_MANIFEST}"
elif [[ -s "${RUNTIME_ROOT}/build/platform/${VERSION}/release-manifest.json" ]]; then
  RELEASE_MANIFEST="${RUNTIME_ROOT}/build/platform/${VERSION}/release-manifest.json"
else
  RELEASE_MANIFEST="${RUNTIME_ROOT}/build/releases/${VERSION}/release-manifest.json"
fi
WORK_DIR="${BUILD_ROOT}/${VERSION:-unknown}"
RELEASE_DIR="${RELEASE_ROOT}/${VERSION:-unknown}"
PORTAL_RELEASE_DIR="${PUBLISH_DIR}/releases/community/${VERSION:-unknown}"
LEGACY_RELEASE_DIR="${PUBLISH_DIR}/releases/dev"

usage() {
  cat <<'USAGE'
Build the standalone HyperCDR Bootstrap release package.

Usage:
  ./release-bootstrap.sh <version>

Environment:
  HCDR_BOOTSTRAP_BUILD_ROOT   External work root, default /data/hypercdr-runtime/build/bootstrap.
  HCDR_BOOTSTRAP_PUBLISH_DIR  External portal source, default /data/hypercdr-runtime/services/bootstrap-portal/source.

The source directory is not modified. The script updates version references in
the external package and portal copy only. It does not start the portal or
install the HyperCDR control plane.
USAGE
}

if [[ -z "${VERSION}" || "${VERSION}" == "-h" || "${VERSION}" == "--help" ]]; then
  usage
  [[ -n "${VERSION}" ]] && exit 0
  exit 2
fi

if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ && ! "${VERSION}" =~ ^v[0-9]{8}\.[0-9]+$ ]]; then
  echo "version must match MAJOR.MINOR.PATCH.YYYYMMDD, got ${VERSION}" >&2
  exit 2
fi

for required in sed tar date sha256sum jq; do
  command -v "${required}" >/dev/null 2>&1 || { echo "missing required command: ${required}" >&2; exit 1; }
done

for path in "${WORK_DIR}" "${PUBLISH_DIR}"; do
  if [[ -z "${path}" || "${path}" == "/" || "${path}" == "${SCRIPT_DIR}" ]]; then
    echo "refusing to replace unsafe path: ${path}" >&2
    exit 1
  fi
done

[[ ! -e "${RELEASE_DIR}" ]] || { echo "release already exists: ${RELEASE_DIR}; use a new version or HCDR_RELEASE_ROOT for validation" >&2; exit 1; }
[[ ! -e "${WORK_DIR}" ]] || { echo "package work directory already exists: ${WORK_DIR}; use a new version or HCDR_PACKAGE_BUILD_ROOT for validation" >&2; exit 1; }
mkdir -p "${WORK_DIR}/hypercdr-bootstrap" "${RELEASE_DIR}"

package_dir="${WORK_DIR}/hypercdr-bootstrap"
RELEASE_SCRIPTS_DIR="${ROOT_DIR}/scripts/release"
cp "${RELEASE_SCRIPTS_DIR}/install-platform.sh" "${package_dir}/install-platform.sh"
cp "${SCRIPT_DIR}/install.sh" "${package_dir}/install.sh"
cp "${SCRIPT_DIR}/install-config.sh" "${package_dir}/install-config.sh"
cp "${RELEASE_SCRIPTS_DIR}/uninstall-platform.sh" "${package_dir}/uninstall-platform.sh"
cp "${RELEASE_SCRIPTS_DIR}/uninstall.sh" "${package_dir}/uninstall.sh"
cp "${RELEASE_SCRIPTS_DIR}/upgrade-local.sh" "${package_dir}/upgrade-local.sh"
cp "${SCRIPT_DIR}/prepare-docker-registry.sh" "${package_dir}/prepare-docker-registry.sh"
cp "${SCRIPT_DIR}/check-harbor.sh" "${package_dir}/check-harbor.sh"
cp "${RELEASE_SCRIPTS_DIR}/start-platform.sh" "${package_dir}/start-platform.sh"
cp "${RELEASE_SCRIPTS_DIR}/stop-platform.sh" "${package_dir}/stop-platform.sh"
cp "${RELEASE_SCRIPTS_DIR}/restart-platform.sh" "${package_dir}/restart-platform.sh"
cp "${RELEASE_SCRIPTS_DIR}/uninstall.sh" "${package_dir}/uninstall.sh"
cp "${RELEASE_SCRIPTS_DIR}/PLATFORM-LIFECYCLE.md" "${package_dir}/PLATFORM-LIFECYCLE.md"
cp "${RELEASE_SCRIPTS_DIR}/LOCAL-INSTALLATION.md" "${package_dir}/README.md"
mkdir -p "${package_dir}/templates"
cp "${RELEASE_SCRIPTS_DIR}/templates/hypercdr.service" "${package_dir}/templates/hypercdr.service"
mkdir -p "${package_dir}/config" "${package_dir}/scripts/lib"
cp "${ROOT_DIR}/config/registries.conf" "${package_dir}/config/registries.conf"
cp "${ROOT_DIR}/scripts/lib/registry-config.sh" "${package_dir}/scripts/lib/registry-config.sh"
cp "${ROOT_DIR}/scripts/lib/registry-config.sh" "${package_dir}/registry-config.sh"
cp "${ROOT_DIR}/docker-compose.yml" "${package_dir}/compose.yaml"
cp "${RELEASE_SCRIPTS_DIR}/deploy-blue-green.sh" "${package_dir}/deploy-blue-green.sh"
cp "${RELEASE_SCRIPTS_DIR}/install-blue-green.sh" "${package_dir}/install-blue-green.sh"
mkdir -p "${package_dir}/nginx"
cp "${ROOT_DIR}/docker/nginx/edge.conf" "${package_dir}/nginx/edge.conf"
cp "${ROOT_DIR}/docker/nginx/upstream.conf.default" "${package_dir}/nginx/upstream.conf.default"
[[ -s "${RELEASE_MANIFEST}" ]] || { echo "complete release manifest is required: ${RELEASE_MANIFEST}" >&2; exit 1; }
manifest_version="$(jq -er '.version' "${RELEASE_MANIFEST}")"
[[ "${manifest_version}" == "${VERSION}" ]] || {
  echo "release manifest version ${manifest_version:-unknown} does not match package version ${VERSION}" >&2
  exit 1
}
jq -e '.componentManifest as $m | ["platform-api", "platform-frontend", "cluster-registration-executor", "comm-agent", "velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp", "oadp-comm-agent", "oadp-operator", "oadp-velero", "oadp-openshift-plugin", "oadp-aws-plugin", "oadp-restore-helper", "oadp-bundle", "oadp-catalog"] | all(.[]; . as $name | ($m[$name].version | type == "string" and length > 0) and ($m[$name].image | type == "string" and length > 0))' "${RELEASE_MANIFEST}" >/dev/null || {
  echo "release manifest must contain all required platform and agent components" >&2
  exit 1
}
cp "${RELEASE_MANIFEST}" "${package_dir}/release-manifest.json"
cp -R "${ROOT_DIR}/charts" "${package_dir}/charts"
chmod +x "${package_dir}"/*.sh

auth_challenge_mode="${HCDR_AUTH_CHALLENGE_MODE:-image}"
case "${auth_challenge_mode}" in
  image) ;;
  turnstile)
    [[ -n "${HCDR_TURNSTILE_SITE_KEY:-}" ]] || { echo "HCDR_TURNSTILE_SITE_KEY is required for a Turnstile installer" >&2; exit 1; }
    [[ -n "${HCDR_TURNSTILE_SECRET_KEY:-}" ]] || { echo "HCDR_TURNSTILE_SECRET_KEY is required for a Turnstile installer" >&2; exit 1; }
    ;;
  *) echo "HCDR_AUTH_CHALLENGE_MODE must be image or turnstile" >&2; exit 1 ;;
esac
{
  printf '\n# Authentication settings injected by the release pipeline.\n'
  printf 'HCDR_AUTH_CHALLENGE_MODE=%q\n' "${auth_challenge_mode}"
  printf 'HCDR_TURNSTILE_SITE_KEY=%q\n' "${HCDR_TURNSTILE_SITE_KEY:-}"
  printf 'HCDR_TURNSTILE_SECRET_KEY=%q\n' "${HCDR_TURNSTILE_SECRET_KEY:-}"
  printf 'HCDR_TURNSTILE_VERIFY_URL=%q\n' "${HCDR_TURNSTILE_VERIFY_URL:-https://challenges.cloudflare.com/turnstile/v0/siteverify}"
} >> "${package_dir}/install-config.sh"
chmod 600 "${package_dir}/install-config.sh"

sed -i -E "s/(v[0-9]{8}\.[0-9]+|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8})/${VERSION}/g" \
  "${package_dir}/install-platform.sh" \
  "${package_dir}/install-config.sh" \
  "${package_dir}/check-harbor.sh" \
  "${package_dir}/README.md" \
  "${package_dir}/compose.yaml" \
  "${package_dir}/charts/hypercdr-platform/values.yaml"

# The portal command extracts into a directory it creates first. Archive the
# package contents at the root so extraction does not create a duplicated
# hypercdr-bootstrap/hypercdr-bootstrap nesting level.
tar -C "${package_dir}" -czf "${RELEASE_DIR}/hypercdr-bootstrap.tar.gz" .
tar -C "${WORK_DIR}" --transform="s,^hypercdr-bootstrap,hypercdr-installer-${VERSION}," -czf "${RELEASE_DIR}/hypercdr-installer-${VERSION}.tar.gz" hypercdr-bootstrap
chmod 600 "${RELEASE_DIR}/hypercdr-bootstrap.tar.gz" "${RELEASE_DIR}/hypercdr-installer-${VERSION}.tar.gz"
cp "${RELEASE_MANIFEST}" "${RELEASE_DIR}/release-manifest.json"
cp "${package_dir}/install-platform.sh" "${RELEASE_DIR}/install-platform.sh"
cp "${package_dir}/uninstall-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh"
cp "${package_dir}/uninstall.sh" "${RELEASE_DIR}/uninstall.sh"
cp "${package_dir}/compose.yaml" "${RELEASE_DIR}/compose.yaml"
chmod +x "${RELEASE_DIR}/install-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh" "${RELEASE_DIR}/uninstall.sh"
(
  cd "${RELEASE_DIR}"
  sha256sum "hypercdr-installer-${VERSION}.tar.gz" > "hypercdr-installer-${VERSION}.sha256"
)

cat > "${RELEASE_DIR}/manifest.json" <<EOF
{
  "edition": "community",
  "name": "HyperCDR Community",
  "version": "${VERSION}",
  "buildTime": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "artifacts": [
    {"name":"Installer package","file":"hypercdr-installer-${VERSION}.tar.gz","checksum":"hypercdr-installer-${VERSION}.sha256","description":"Versioned installer scripts, Docker Compose template, and Helm chart assets."},
    {"name":"Bootstrap compatibility package","file":"hypercdr-bootstrap.tar.gz","description":"Compatibility alias used by the development download portal."},
    {"name":"Control plane installer","file":"install-platform.sh","description":"Standalone installer for Kubernetes or Docker Compose."},
    {"name":"Docker Compose template","file":"compose.yaml","description":"Standalone host Docker Compose template."},
    {"name":"Control plane uninstaller","file":"uninstall-platform.sh","description":"Docker Compose uninstaller for the control plane."}
  ]
}
EOF


# Keep the historical /releases/dev URL available for existing bookmarks and
# older portal copies while the UI uses the edition-specific path.

cat <<EOF
Bootstrap release ${VERSION} created without modifying source files.
Work directory:
  ${WORK_DIR}
Release directory:
  ${RELEASE_DIR}
Portal source:
  ${PUBLISH_DIR}

Install from the extracted package:
  bash install.sh --base-url https://HOST:12443
EOF
