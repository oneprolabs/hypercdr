#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${HCDR_DATA_DIR:-/var/lib/hypercdr}"
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
  --data-dir PATH       HyperCDR data/deploy directory, default: /var/lib/hypercdr.
  --compose-file PATH   Docker Compose file. Defaults to ./compose.yaml when present,
                        or <data-dir>/docker-compose.yaml when present.
  --purge-data          Delete <data-dir> after containers are removed.
  --remove-images       Remove HyperCDR platform images used by stopped containers.
  --execute             Apply changes. Without this flag, prints the plan only.
  -h, --help            Show help.

This script removes only HyperCDR control plane containers:
  hypercdr-platform-frontend
  hypercdr-platform-api
  hypercdr-platform-upgrader
  hypercdr-postgres

It does not uninstall Harbor and does not stop the bootstrap portal.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --data-dir) DATA_DIR="${2:?missing value for --data-dir}"; shift 2 ;;
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
  if [[ -f "${DATA_DIR}/docker-compose.yaml" ]]; then
    COMPOSE_FILE="${DATA_DIR}/docker-compose.yaml"
  elif [[ -f "${DATA_DIR}/compose.yaml" ]]; then
    COMPOSE_FILE="${DATA_DIR}/compose.yaml"
  elif [[ -f ./compose.yaml ]]; then
    COMPOSE_FILE="$(pwd)/compose.yaml"
  fi
fi

cat <<EOF
HyperCDR control plane uninstall plan

Data directory:        ${DATA_DIR}
Compose file:          ${COMPOSE_FILE:-"(not found; fixed containers fallback)"}
Purge data:            ${PURGE_DATA}
Remove images:         ${REMOVE_IMAGES}
Execute changes:       ${EXECUTE}

Target containers:
  hypercdr-platform-frontend
  hypercdr-platform-api
  hypercdr-platform-upgrader
  hypercdr-postgres
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  cat <<'EOF'

Dry-run mode. Add --execute to uninstall.
EOF
  exit 0
fi

require_command docker

images=()
for name in hypercdr-platform-frontend hypercdr-platform-api hypercdr-platform-upgrader hypercdr-postgres; do
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
    docker compose -f "${compose_name}" down --remove-orphans
  )
else
  docker rm -f hypercdr-platform-frontend hypercdr-platform-api hypercdr-platform-upgrader hypercdr-postgres >/dev/null 2>&1 || true
fi

if [[ "${REMOVE_IMAGES}" == "true" ]]; then
  for image in "${images[@]}"; do
    docker image rm "${image}" >/dev/null 2>&1 || true
  done
fi

if [[ "${PURGE_DATA}" == "true" ]]; then
  if [[ -z "${DATA_DIR}" || "${DATA_DIR}" == "/" ]]; then
    echo "refusing to purge unsafe data dir: ${DATA_DIR}" >&2
    exit 1
  fi
  rm -rf "${DATA_DIR}"
fi

cat <<'EOF'

HyperCDR control plane uninstalled.
EOF
