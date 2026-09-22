#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
COMPOSE_FILE="${INSTALL_DIR}/docker-compose.yaml"
PURGE_DATA="false"
REMOVE_IMAGES="false"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Uninstall the HyperCDR control plane from a standalone Docker host.

Usage:
  ./uninstall.sh [options]

Options:
  --purge-data          Delete this installation directory after containers are removed.
  --remove-images       Remove HyperCDR platform images used by stopped containers.
  --execute             Apply changes. Without this flag, prints the plan only.
  -h, --help            Show help.

Run the script installed beside docker-compose.yaml. Its own location determines
the installation directory, regardless of your current working directory.
Running from the source tree or an extracted installer is not supported.

Examples:
  /var/lib/hypercdr/uninstall.sh
  /srv/hypercdr/uninstall.sh --execute
  /srv/hypercdr/uninstall.sh --purge-data --remove-images --execute

This script removes only HyperCDR control plane containers:
  hypercdr-edge
  hypercdr-platform-frontend-{blue,green}
  hypercdr-platform-api-{blue,green}
  hypercdr-cluster-registration-executor
  hypercdr-postgres

It does not uninstall Harbor and does not stop the bootstrap portal.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge-data) PURGE_DATA="true"; shift ;;
    --remove-images) REMOVE_IMAGES="true"; shift ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

container_image() {
  docker inspect "$1" --format '{{.Config.Image}}' 2>/dev/null || true
}

[[ -f "${COMPOSE_FILE}" && -f "${INSTALL_DIR}/.env" ]] || {
  echo "No installed platform found beside this script: ${INSTALL_DIR}" >&2
  echo "Run uninstall.sh from the actual installation directory." >&2
  exit 1
}

cat <<EOF
HyperCDR control plane uninstall plan

Install directory:        ${INSTALL_DIR}
Compose file:          ${COMPOSE_FILE:-"(not found; fixed containers fallback)"}
Purge data:            ${PURGE_DATA}
Remove images:         ${REMOVE_IMAGES}
Execute changes:       ${EXECUTE}

Target containers:
  hypercdr-edge
  hypercdr-platform-frontend-{blue,green}
  hypercdr-platform-api-{blue,green}
  hypercdr-cluster-registration-executor
  hypercdr-postgres
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  cat <<'EOF'

Dry-run mode. Add --execute to uninstall.
EOF
  exit 0
fi

require_command docker

# Disable boot recovery before removing containers so uninstall is permanent.
if command -v systemctl >/dev/null 2>&1 && [[ -f /etc/systemd/system/hypercdr.service ]]; then
  systemctl disable --now hypercdr.service
  rm /etc/systemd/system/hypercdr.service
  systemctl daemon-reload
fi
if command -v systemctl >/dev/null 2>&1 && [[ -f /etc/systemd/system/hypercdr-platform.service ]]; then
  systemctl disable --now hypercdr-platform.service >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/hypercdr-platform.service
  systemctl daemon-reload
fi

images=()
for name in hypercdr-edge hypercdr-platform-frontend-blue hypercdr-platform-frontend-green \
  hypercdr-platform-api-blue hypercdr-platform-api-green hypercdr-cluster-registration-executor hypercdr-postgres; do
  image="$(container_image "${name}")"
  if [[ -n "${image}" ]]; then
    images+=("${image}")
  fi
done

if [[ -n "${COMPOSE_FILE}" && -f "${COMPOSE_FILE}" ]]; then
  compose_dir="$(cd "$(dirname "${COMPOSE_FILE}")" && pwd)"
  compose_name="$(basename "${COMPOSE_FILE}")"
  (
    cd "${compose_dir}"
    compose_args=(--project-name hypercdr -f "${compose_name}")
    [[ -x "${INSTALL_DIR}/deploy-blue-green.sh" ]] && compose_args+=(--profile blue --profile green)
    down_args=(down --remove-orphans)
    [[ "${PURGE_DATA}" == "true" ]] && down_args+=(--volumes)
    docker compose "${compose_args[@]}" "${down_args[@]}"
  )
else
  docker rm -f hypercdr-edge hypercdr-platform-frontend-blue hypercdr-platform-frontend-green \
    hypercdr-platform-api-blue hypercdr-platform-api-green hypercdr-cluster-registration-executor hypercdr-postgres >/dev/null 2>&1 || true
fi

if [[ "${REMOVE_IMAGES}" == "true" ]]; then
  for image in "${images[@]}"; do
    docker image rm "${image}" >/dev/null 2>&1 || true
  done
fi

if [[ "${PURGE_DATA}" == "true" ]]; then
  if [[ -z "${INSTALL_DIR}" || "${INSTALL_DIR}" == "/" ]]; then
    echo "refusing to purge unsafe install directory: ${INSTALL_DIR}" >&2
    exit 1
  fi
  rm -rf "${INSTALL_DIR}"
fi

cat <<'EOF'

HyperCDR control plane uninstalled.
EOF
