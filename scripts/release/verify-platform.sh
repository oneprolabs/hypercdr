#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

HOST="${HCDR_PLATFORM_HOST:-${DEFAULT_HOST}}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
DEPLOY_DIR="${HCDR_DEPLOY_DIR:-/var/lib/hypercdr}"
FRONTEND_URL="http://${HOST}:3002"

usage() {
  cat <<'USAGE'
Verify the standard Docker Compose platform deployment.

Usage:
  verify-platform.sh [--host HOST] [--registry REGISTRY] [--deploy-dir DIR]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="${2:?missing value for --host}"; FRONTEND_URL="http://${HOST}:3002"; shift 2 ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --deploy-dir) DEPLOY_DIR="${2:?missing value for --deploy-dir}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

require_cmd curl
require_cmd docker

if [[ -z "${REGISTRY}" && -f "${DEPLOY_DIR}/.env" ]]; then
  REGISTRY="$(grep -E '^HCDR_IMAGE_REGISTRY=' "${DEPLOY_DIR}/.env" | tail -n1 | cut -d= -f2- || true)"
fi
require_registry "${REGISTRY}"
REGISTRY="${REGISTRY%/}"
REGISTRY_HOST="${REGISTRY%%/*}"

log "Docker Compose services"
(cd "${DEPLOY_DIR}" && docker compose ps)
(cd "${DEPLOY_DIR}" && docker compose ps --services --status running | grep -q '^hypercdr-platform-upgrader$')

log "Checking frontend"
curl -fsS "${FRONTEND_URL}/" >/dev/null

log "Checking install.sh"
install_script="$(curl -fsS "${FRONTEND_URL}/install.sh")"
echo "${install_script}" | grep -q "ws://${HOST}:3002/ws/agent"
echo "${install_script}" | grep -q "AGENT_IMAGE=\"${REGISTRY}/comm-agent:"
echo "${install_script}" | grep -q "VELERO_IMAGE=\"${REGISTRY}/velero:"
echo "${install_script}" | grep -q "${HOST}:3002/assets/registry/ca.crt"

log "Checking prepare-node.sh"
prepare_script="$(curl -fsS "${FRONTEND_URL}/prepare-node.sh")"
echo "${prepare_script}" | grep -q "REGISTRY_HOST=\"${REGISTRY_HOST}\""
echo "${prepare_script}" | grep -q "REGISTRY_CA_URL=\"http://${HOST}:3002/assets/registry/ca.crt\""

log "OK"
