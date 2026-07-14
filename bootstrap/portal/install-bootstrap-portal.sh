#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${HCDR_BOOTSTRAP_DATA_DIR:-/opt/hypercdr-bootstrap}"
PORT="${HCDR_BOOTSTRAP_PORT:-8080}"
SOURCE_DIR="${HCDR_BOOTSTRAP_PORTAL_SOURCE_DIR:-}"
PORTAL_DIR="${HCDR_BOOTSTRAP_PORTAL_DIR:-${DATA_DIR}/portal}"
MODE="${HCDR_BOOTSTRAP_PORTAL_MODE:-docker}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
HyperCDR bootstrap portal installer

Usage:
  ./install-bootstrap-portal.sh [options]

Options:
  --source-dir PATH   Generated portal directory. Defaults to current package root when index.html exists.
  --data-dir PATH     Bootstrap persistent data directory, default: /opt/hypercdr-bootstrap.
  --port PORT         Portal HTTP port, default: 8080.
  --mode MODE         docker or python, default: docker.
  --execute           Install/start portal. Without this flag, prints the plan only.
  -h, --help          Show help.

This script only serves the bootstrap download page and release artifacts. It
does not install the registry and does not install the HyperCDR control plane.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:?missing value for --source-dir}"; shift 2 ;;
    --data-dir) DATA_DIR="${2:?missing value for --data-dir}"; PORTAL_DIR="${DATA_DIR}/portal"; shift 2 ;;
    --port) PORT="${2:?missing value for --port}"; shift 2 ;;
    --mode) MODE="${2:?missing value for --mode}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${SOURCE_DIR}" ]]; then
  if [[ -f "${SCRIPT_DIR}/../../index.html" ]]; then
    SOURCE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  elif [[ -f "${SCRIPT_DIR}/index.html" ]]; then
    SOURCE_DIR="${SCRIPT_DIR}"
  else
    echo "--source-dir is required when the script is not inside a generated portal package" >&2
    exit 2
  fi
fi

if [[ ! -f "${SOURCE_DIR}/index.html" ]]; then
  echo "portal index.html not found in source dir: ${SOURCE_DIR}" >&2
  exit 1
fi

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

cat <<EOF
HyperCDR bootstrap portal plan

Mode:            ${MODE}
Source dir:      ${SOURCE_DIR}
Install dir:     ${PORTAL_DIR}
HTTP port:       ${PORT}
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  cat <<EOF

Dry-run mode. Add --execute to install/start the portal.
EOF
  exit 0
fi

mkdir -p "${PORTAL_DIR}"
rm -rf "${PORTAL_DIR:?}/"*
cp -R "${SOURCE_DIR}/." "${PORTAL_DIR}/"

case "${MODE}" in
  docker)
    require_command docker
    HCDR_BOOTSTRAP_PORT="${PORT}" \
    HCDR_BOOTSTRAP_PORTAL_DIR="${PORTAL_DIR}" \
    docker compose -f "${SCRIPT_DIR}/portal-compose.yaml" up -d
    ;;
  python)
    require_command python3
    cat <<EOF

Starting foreground Python HTTP server.
Press Ctrl+C to stop it.
EOF
    exec python3 -m http.server "${PORT}" --directory "${PORTAL_DIR}"
    ;;
  *)
    echo "unknown mode: ${MODE}" >&2
    exit 2
    ;;
esac

cat <<EOF

HyperCDR bootstrap portal is running.

URL:
  http://0.0.0.0:${PORT}

Installed files:
  ${PORTAL_DIR}
EOF
