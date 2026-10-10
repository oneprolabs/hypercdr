#!/usr/bin/env bash
# Rebuild a running host development environment without recreating its database.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"
component="${1:-all}"
case "$component" in api|frontend|all) ;; *) dev_die "usage: update-dev.sh [api|frontend|all]" ;; esac
for command in python3 systemctl flock; do require_command "$command"; done
mkdir -p "${HCDR_DEV_DIR}/bin"
exec 9>"${HCDR_DEV_DIR}/.update.lock"
flock -n 9 || dev_die "another development update is running"
# Fixed service names must never update a different runtime's files.
for service in api frontend; do
  [[ "$component" == all || "$component" == "$service" ]] || continue
  systemctl is-active --quiet "hypercdr-dev-${service}.service" || dev_die "${service} is not active; use start-dev.sh"
  expected="${HCDR_DEV_DIR}/run-${service}.sh"
  actual="$(systemctl show "hypercdr-dev-${service}.service" -p ExecStart --value)"
  [[ "$actual" == *"path=${expected} ;"* ]] || dev_die "${service} uses a different or unknown runtime; refusing to update"
done
if [[ "$component" == api || "$component" == all ]]; then
  GO_BIN="${HCDR_GO_BIN:-/usr/local/go/bin/go}"
  capture="${HCDR_DEV_DIR}/bin/.update-source.json"
  candidate="${HCDR_DEV_DIR}/bin/platform-api.next"
  python3 "${SCRIPT_DIR}/provenance.py" capture --source "${HCDR_SOURCE_DIR}" --component api --output "$capture"
  (cd "${HCDR_SOURCE_DIR}/backend"; "$GO_BIN" build -trimpath -o "$candidate" ./cmd/platform-api)
  python3 "${SCRIPT_DIR}/provenance.py" record --capture "$capture" --artifact "$candidate" --output "${candidate}.provenance.json"
  mv "$candidate" "${HCDR_DEV_DIR}/bin/platform-api"
  mv "${candidate}.provenance.json" "${HCDR_DEV_DIR}/bin/platform-api.provenance.json"
  systemctl restart hypercdr-dev-api.service
fi
if [[ "$component" == frontend || "$component" == all ]]; then
  HCDR_FRONTEND_OUT_DIR="${HCDR_DEV_DIR}/frontend/dist.next" "${HCDR_SOURCE_DIR}/scripts/build-frontend.sh"
  rm -rf "${HCDR_DEV_DIR}/frontend/dist.previous"
  mv "${HCDR_DEV_DIR}/frontend/dist" "${HCDR_DEV_DIR}/frontend/dist.previous"
  mv "${HCDR_DEV_DIR}/frontend/dist.next" "${HCDR_DEV_DIR}/frontend/dist"
  mv "${HCDR_DEV_DIR}/frontend/dist.next.provenance.json" "${HCDR_DEV_DIR}/frontend/dist.provenance.json"
  systemctl restart hypercdr-dev-frontend.service
fi
for service in api frontend; do
  [[ "$component" == all || "$component" == "$service" ]] || continue
  systemctl is-active --quiet "hypercdr-dev-${service}.service" || dev_die "${service} failed to restart"
done
"${SCRIPT_DIR}/status-dev.sh"
