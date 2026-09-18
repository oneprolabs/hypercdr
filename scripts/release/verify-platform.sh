#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

HOST="${HCDR_PLATFORM_HOST:-${DEFAULT_HOST}}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
DEPLOY_DIR="${HCDR_DEPLOY_DIR:-/var/lib/hypercdr}"
PORT="${HCDR_PLATFORM_PORT:-12443}"
FRONTEND_URL="https://${HOST}:${PORT}"
ADDRESS_OVERRIDE=false

usage() {
  cat <<'USAGE'
Verify the standard Docker Compose platform deployment.

Usage:
  verify-platform.sh [--host HOST] [--port PORT] [--registry REGISTRY] [--deploy-dir DIR]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="${2:?missing value for --host}"; ADDRESS_OVERRIDE=true; shift 2 ;;
    --port) PORT="${2:?missing value for --port}"; ADDRESS_OVERRIDE=true; shift 2 ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --deploy-dir) DEPLOY_DIR="${2:?missing value for --deploy-dir}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

require_cmd curl
require_cmd docker

FRONTEND_URL="https://${HOST}:${PORT}"
if [[ "$ADDRESS_OVERRIDE" == false && -f "${DEPLOY_DIR}/.env" ]]; then
  configured_url="$(sed -n 's/^HCDR_BASE_URL=//p' "${DEPLOY_DIR}/.env" | tail -1)"
  FRONTEND_URL="${configured_url:-$FRONTEND_URL}"
fi
ADVERTISED_URL="$FRONTEND_URL"
if [[ -f "${DEPLOY_DIR}/.env" ]]; then
  configured_public_url="$(sed -n 's/^HCDR_PUBLIC_BASE_URL=//p' "${DEPLOY_DIR}/.env" | tail -1)"
  ADVERTISED_URL="${configured_public_url:-$FRONTEND_URL}"
fi

if [[ -z "${REGISTRY}" && -f "${DEPLOY_DIR}/.env" ]]; then
  REGISTRY="$(grep -E '^HCDR_IMAGE_REGISTRY=' "${DEPLOY_DIR}/.env" | tail -n1 | cut -d= -f2- || true)"
fi
require_registry "${REGISTRY}"
REGISTRY="${REGISTRY%/}"
REGISTRY_HOST="${REGISTRY%%/*}"

log "Docker Compose services"
(cd "${DEPLOY_DIR}" && docker compose ps)
(cd "${DEPLOY_DIR}" && docker compose ps --services --status running | grep -q '^hypercdr-platform-upgrader$')
(cd "${DEPLOY_DIR}" && docker compose ps --services --status running | grep -q '^hypercdr-cluster-registration-executor$')

log "Checking frontend"
curl -kfsS "${FRONTEND_URL}/readyz" >/dev/null

log "Checking install.sh"
install_script="$(curl -kfsS "${FRONTEND_URL}/install.sh")"
grep -Fq "${ADVERTISED_URL}/api/v1/agent-tokens/validate" <<< "$install_script"
echo "${install_script}" | grep -q "AGENT_IMAGE=\"${REGISTRY}/comm-agent:"
echo "${install_script}" | grep -q "VELERO_IMAGE=\"${REGISTRY}/velero:"
grep -Fq "${ADVERTISED_URL}/assets/registry/ca.crt" <<< "$install_script"

if [[ -f "${DEPLOY_DIR}/.env" ]] && grep -Eq '^HCDR_REGISTRY_CA_PATH=.+$' "${DEPLOY_DIR}/.env"; then
  log "Checking prepare-node.sh"
  prepare_script="$(curl -kfsS "${FRONTEND_URL}/prepare-node.sh")"
  grep -Fq "REGISTRY_HOST=\"${REGISTRY_HOST}\"" <<< "$prepare_script"
  grep -Fq "${ADVERTISED_URL}/assets/registry/ca.crt" <<< "$prepare_script"
fi

log "OK"
