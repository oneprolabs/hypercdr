#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

python3 "${SCRIPT_DIR}/provenance.py" status --source "${HCDR_SOURCE_DIR}"

docker ps --filter name=hypercdr-dev-postgres --format 'postgres: {{.Status}}'
