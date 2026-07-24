#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SITE_SOURCE_DIR="${SCRIPT_DIR}/site"
VERSION="${1:-}"
BUILD_ROOT="${HCDR_BOOTSTRAP_BUILD_ROOT:-/data/hypercdr/.build/bootstrap}"
PUBLISH_DIR="${HCDR_BOOTSTRAP_PUBLISH_DIR:-/data/hypercdr/bootstrap-publish}"
WORK_DIR="${BUILD_ROOT}/${VERSION:-unknown}"
RELEASE_DIR="${PUBLISH_DIR}/releases/dev"

usage() {
  cat <<'USAGE'
Build the standalone HyperCDR Bootstrap release package.

Usage:
  ./release-bootstrap.sh <version>

Environment:
  HCDR_BOOTSTRAP_BUILD_ROOT   External work root, default /data/hypercdr/.build/bootstrap.
  HCDR_BOOTSTRAP_PUBLISH_DIR  External portal source, default /data/hypercdr/bootstrap-publish.

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

if [[ ! "${VERSION}" =~ ^v[0-9]{8}\.[0-9]+$ ]]; then
  echo "version must match vYYYYMMDD.N, got ${VERSION}" >&2
  exit 2
fi

for required in sed tar date; do
  command -v "${required}" >/dev/null 2>&1 || { echo "missing required command: ${required}" >&2; exit 1; }
done

for path in "${WORK_DIR}" "${PUBLISH_DIR}"; do
  if [[ -z "${path}" || "${path}" == "/" || "${path}" == "${SCRIPT_DIR}" ]]; then
    echo "refusing to replace unsafe path: ${path}" >&2
    exit 1
  fi
done

rm -rf "${WORK_DIR}" "${PUBLISH_DIR}"
mkdir -p "${WORK_DIR}/hypercdr-bootstrap" "${PUBLISH_DIR}"
cp -R "${SITE_SOURCE_DIR}/." "${PUBLISH_DIR}/"
mkdir -p "${RELEASE_DIR}"

package_dir="${WORK_DIR}/hypercdr-bootstrap"
cp "${SCRIPT_DIR}/install-platform.sh" "${package_dir}/install-platform.sh"
cp "${SCRIPT_DIR}/uninstall-platform.sh" "${package_dir}/uninstall-platform.sh"
cp "${SCRIPT_DIR}/prepare-docker-registry.sh" "${package_dir}/prepare-docker-registry.sh"
cp "${SCRIPT_DIR}/check-harbor.sh" "${package_dir}/check-harbor.sh"
cp "${SCRIPT_DIR}/compose.yaml" "${package_dir}/compose.yaml"
cp -R "${SCRIPT_DIR}/charts" "${package_dir}/charts"
chmod +x "${package_dir}"/*.sh

sed -i -E "s/v[0-9]{8}\.[0-9]+/${VERSION}/g" \
  "${package_dir}/install-platform.sh" \
  "${package_dir}/check-harbor.sh" \
  "${package_dir}/compose.yaml" \
  "${package_dir}/charts/hypercdr-platform/values.yaml" \
  "${PUBLISH_DIR}/assets/app.js"

# The portal command extracts into a directory it creates first. Archive the
# package contents at the root so extraction does not create a duplicated
# hypercdr-bootstrap/hypercdr-bootstrap nesting level.
tar -C "${package_dir}" -czf "${RELEASE_DIR}/hypercdr-bootstrap.tar.gz" .
cp "${package_dir}/install-platform.sh" "${RELEASE_DIR}/install-platform.sh"
cp "${package_dir}/uninstall-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh"
cp "${package_dir}/compose.yaml" "${RELEASE_DIR}/compose.yaml"
chmod +x "${RELEASE_DIR}/install-platform.sh" "${RELEASE_DIR}/uninstall-platform.sh"

cat > "${RELEASE_DIR}/manifest.json" <<EOF
{
  "version": "${VERSION}",
  "buildTime": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "artifacts": [
    {"name":"Bootstrap package","file":"hypercdr-bootstrap.tar.gz","description":"Installer scripts, Docker Compose template, and Helm chart assets."},
    {"name":"Control plane installer","file":"install-platform.sh","description":"Standalone installer for Kubernetes or Docker Compose."},
    {"name":"Docker Compose template","file":"compose.yaml","description":"Standalone host Docker Compose template."},
    {"name":"Control plane uninstaller","file":"uninstall-platform.sh","description":"Docker Compose uninstaller for the control plane."}
  ]
}
EOF

cat <<EOF
Bootstrap release ${VERSION} created without modifying source files.
Work directory:
  ${WORK_DIR}
Portal source:
  ${PUBLISH_DIR}

Deploy the portal with:
  ${SCRIPT_DIR}/portal/install-bootstrap-portal.sh --source-dir ${PUBLISH_DIR} --data-dir /data/hypercdr/bootstrap-portal --port 8080 --execute
EOF
