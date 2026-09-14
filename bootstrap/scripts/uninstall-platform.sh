#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"
COMPOSE_FILE="${HCDR_COMPOSE_FILE:-}"
PURGE_DATA="false"
REMOVE_IMAGES="false"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Uninstall the HyperCDR control plane from a standalone Docker host.

Usage:
  ./uninstall-platform.sh [options]

Options:
  --install-dir PATH       HyperCDR data/deploy directory, default: /var/lib/hypercdr.
  --compose-file PATH   Docker Compose file. Defaults to ./compose.yaml when present,
                        or <install-dir>/docker-compose.yaml when present.
  --purge-data          Delete <install-dir> after containers are removed.
  --remove-images       Remove HyperCDR platform images used by stopped containers.
  --execute             Apply changes. Without this flag, prints the plan only.
  -h, --help            Show help.

This script removes only HyperCDR control plane containers:
  hypercdr-platform-frontend
  hypercdr-platform-api
  hypercdr-platform-upgrader
  hypercdr-cluster-registration-executor
  hypercdr-postgres

It does not uninstall Harbor and does not stop the bootstrap portal.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; shift 2 ;;
    --compose-file) COMPOSE_FILE="${2:?missing value for --compose-file}"; shift 2 ;;
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

if [[ -z "${COMPOSE_FILE}" ]]; then
  if [[ -f "${INSTALL_DIR}/docker-compose.yaml" ]]; then
    COMPOSE_FILE="${INSTALL_DIR}/docker-compose.yaml"
  elif [[ -f "${INSTALL_DIR}/compose.yaml" ]]; then
    COMPOSE_FILE="${INSTALL_DIR}/compose.yaml"
  elif [[ -f ./compose.yaml ]]; then
    COMPOSE_FILE="$(pwd)/compose.yaml"
  fi
fi

cat <<EOF
HyperCDR control plane uninstall plan

Install directory:        ${INSTALL_DIR}
Compose file:          ${COMPOSE_FILE:-"(not found; fixed containers fallback)"}
Purge data:            ${PURGE_DATA}
Remove images:         ${REMOVE_IMAGES}
Execute changes:       ${EXECUTE}

Target containers:
  hypercdr-platform-frontend
  hypercdr-platform-api
  hypercdr-platform-upgrader
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
if command -v systemctl >/dev/null 2>&1 && [[ -f /etc/systemd/system/hypercdr-platform.service ]]; then
  systemctl disable --now hypercdr-platform.service
  rm /etc/systemd/system/hypercdr-platform.service
  systemctl daemon-reload
fi

images=()
for name in hypercdr-platform-frontend hypercdr-platform-api hypercdr-platform-upgrader hypercdr-cluster-registration-executor hypercdr-postgres; do
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
    down_args=(down --remove-orphans)
    [[ "${PURGE_DATA}" == "true" ]] && down_args+=(--volumes)
    docker compose --project-name hypercdr -f "${compose_name}" "${down_args[@]}"
  )
else
  docker rm -f hypercdr-platform-frontend hypercdr-platform-api hypercdr-platform-upgrader hypercdr-cluster-registration-executor hypercdr-postgres >/dev/null 2>&1 || true
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
