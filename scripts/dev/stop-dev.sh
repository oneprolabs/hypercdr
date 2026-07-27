#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

for unit in hypercdr-dev-frontend.service hypercdr-dev-api.service; do
  if systemctl is-active --quiet "${unit}"; then
    dev_log "Stopping ${unit}"
    systemctl stop "${unit}"
  fi
  systemctl reset-failed "${unit}" >/dev/null 2>&1 || true
done

frontend_node_modules="${HCDR_SOURCE_DIR}/frontend/node_modules"
if [[ -L "${frontend_node_modules}" ]]; then
  rm -f "${frontend_node_modules}"
fi

if docker inspect hypercdr-dev-postgres >/dev/null 2>&1; then
  dev_log "Stopping development PostgreSQL"
  HCDR_DEV_DIR="${HCDR_DEV_DIR}" \
  HCDR_DEV_POSTGRES_PORT="${HCDR_DEV_POSTGRES_PORT}" \
  HCDR_IMAGE_REGISTRY="${HCDR_IMAGE_REGISTRY}" \
    docker compose -f "${HCDR_SOURCE_DIR}/docker-compose.dev.yml" down --remove-orphans
else
  dev_log "Keeping shared platform PostgreSQL running"
fi

dev_log "Development mode stopped; data remains in ${HCDR_DEV_DIR}/data"
