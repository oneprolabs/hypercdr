#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-$(cd "${ROOT_DIR}/.." && pwd)/hypercdr-runtime}"
SITE_SOURCE_DIR="${SCRIPT_DIR}/site"
VERSION="${1:-}"
BUILD_ROOT="${HCDR_BOOTSTRAP_BUILD_ROOT:-${RUNTIME_ROOT}/build/bootstrap/community}"
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

for required in sed tar date sha256sum; do
  command -v "${required}" >/dev/null 2>&1 || { echo "missing required command: ${required}" >&2; exit 1; }
done

for path in "${WORK_DIR}" "${PUBLISH_DIR}"; do
  if [[ -z "${path}" || "${path}" == "/" || "${path}" == "${SCRIPT_DIR}" ]]; then
    echo "refusing to replace unsafe path: ${path}" >&2
    exit 1
  fi
done

[[ ! -e "${RELEASE_DIR}" ]] || { echo "release already exists: ${RELEASE_DIR}; use a new version or HCDR_RELEASE_ROOT for validation" >&2; exit 1; }
mkdir -p "${WORK_DIR}/hypercdr-bootstrap" "${RELEASE_DIR}"

package_dir="${WORK_DIR}/hypercdr-bootstrap"
cp "${SCRIPT_DIR}/scripts/install-platform.sh" "${package_dir}/install-platform.sh"
cp "${SCRIPT_DIR}/install.sh" "${package_dir}/install.sh"
cp "${SCRIPT_DIR}/templates/install-config.sh" "${package_dir}/install-config.sh"
cp "${SCRIPT_DIR}/scripts/uninstall-platform.sh" "${package_dir}/uninstall-platform.sh"
cp "${SCRIPT_DIR}/prepare-docker-registry.sh" "${package_dir}/prepare-docker-registry.sh"
cp "${SCRIPT_DIR}/check-harbor.sh" "${package_dir}/check-harbor.sh"
cp "${SCRIPT_DIR}/scripts/start-platform.sh" "${package_dir}/start-platform.sh"
cp "${SCRIPT_DIR}/scripts/stop-platform.sh" "${package_dir}/stop-platform.sh"
cp "${SCRIPT_DIR}/scripts/restart-platform.sh" "${package_dir}/restart-platform.sh"
mkdir -p "${package_dir}/templates"
cp "${SCRIPT_DIR}/templates/hypercdr.service" "${package_dir}/templates/hypercdr.service"
mkdir -p "${package_dir}/config" "${package_dir}/scripts/lib"
cp "${ROOT_DIR}/config/registries.conf" "${package_dir}/config/registries.conf"
cp "${ROOT_DIR}/scripts/lib/registry-config.sh" "${package_dir}/scripts/lib/registry-config.sh"
cp "${ROOT_DIR}/docker-compose.yml" "${package_dir}/compose.yaml"
[[ -s "${RELEASE_MANIFEST}" ]] || { echo "complete release manifest is required: ${RELEASE_MANIFEST}" >&2; exit 1; }
manifest_version="$(sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "${RELEASE_MANIFEST}" | head -1)"
[[ "${manifest_version}" == "${VERSION}" ]] || {
  echo "release manifest version ${manifest_version:-unknown} does not match package version ${VERSION}" >&2
  exit 1
}
cp "${RELEASE_MANIFEST}" "${package_dir}/release-manifest.json"
cp -R "${ROOT_DIR}/charts" "${package_dir}/charts"
chmod +x "${package_dir}"/*.sh

sed -i -E "s/(v[0-9]{8}\.[0-9]+|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8})/${VERSION}/g" \
  "${package_dir}/install-platform.sh" \
  "${package_dir}/install-config.sh" \
  "${package_dir}/check-harbor.sh" \
  "${package_dir}/compose.yaml" \
  "${package_dir}/charts/hypercdr-platform/values.yaml"

# The portal command extracts into a directory it creates first. Archive the
# package contents at the root so extraction does not create a duplicated
# hypercdr-bootstrap/hypercdr-bootstrap nesting level.
tar -C "${package_dir}" -czf "${RELEASE_DIR}/hypercdr-bootstrap.tar.gz" .
tar -C "${WORK_DIR}" --transform="s,^hypercdr-bootstrap,hypercdr-installer-${VERSION}," -czf "${RELEASE_DIR}/hypercdr-installer-${VERSION}.tar.gz" hypercdr-bootstrap
cp "${RELEASE_MANIFEST}" "${RELEASE_DIR}/release-manifest.json"
cp "${package_dir}/install-platform.sh" "${RELEASE_DIR}/install-platform.sh"
cp "${package_dir}/uninstall-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh"
cp "${package_dir}/compose.yaml" "${RELEASE_DIR}/compose.yaml"
chmod +x "${RELEASE_DIR}/install-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh"
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

Deploy the portal with:
  bash ${SCRIPT_DIR}/scripts/publish-release.sh --version ${VERSION}
  ${SCRIPT_DIR}/deploy-bootstrap.sh --source-dir ${PUBLISH_DIR} --execute
EOF
