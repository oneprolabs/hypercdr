#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec bash "$root/backend/internal/httpserver/installers/csi-upgrade-rbac.sh" "$@"
