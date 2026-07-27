#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

for service in api frontend; do
  unit="hypercdr-dev-${service}.service"
  echo "${service}: $(systemctl is-active "${unit}" 2>/dev/null || true)"
done

docker ps --filter name=hypercdr-dev-postgres --format 'postgres: {{.Status}}'
