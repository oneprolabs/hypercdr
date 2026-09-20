#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install_dir="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"
[[ ! -f "${script_dir}/docker-compose.yaml" ]] || install_dir="${script_dir}"
compose_file=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) install_dir="${2:?missing value}"; shift 2;;
    --compose-file) compose_file="${2:?missing value}"; shift 2;;
    -h|--help) echo "Usage: $0 [--install-dir PATH] [--compose-file PATH]"; exit 0;;
    *) echo "Unknown option: $1" >&2; exit 2;;
  esac
done
compose_file="${compose_file:-${install_dir}/docker-compose.yaml}"
command -v docker >/dev/null || { echo "Docker is required" >&2; exit 1; }
[[ -f "$compose_file" ]] || { echo "Compose file not found: $compose_file" >&2; exit 1; }
if [[ -x "${install_dir}/deploy-blue-green.sh" ]]; then
  HCDR_INSTALL_DIR="${install_dir}" HCDR_COMPOSE_FILE="${compose_file}" \
    "${install_dir}/deploy-blue-green.sh" --start-current
  exit $?
fi
cd "$(dirname "$compose_file")"
docker compose --env-file "${install_dir}/.env" -f "$compose_file" --project-name hypercdr up -d
# Resolve API DNS again after Docker/network recovery, even if Docker already
# auto-started the frontend before Compose processed dependencies.
docker compose --env-file "${install_dir}/.env" -f "$compose_file" --project-name hypercdr restart hypercdr-platform-frontend
port="$(sed -n 's/^HCDR_FRONTEND_PORT=//p' "${install_dir}/.env" | tail -1)"
for ((attempt=0; attempt<90; attempt++)); do
  if curl -kfsS --connect-timeout 2 --max-time 3 "https://127.0.0.1:${port:-12443}/readyz" >/dev/null 2>&1; then
    echo "HyperCDR is ready."; exit 0
  fi
  sleep 2
done
echo "HyperCDR did not become ready. Check docker compose logs in ${install_dir}." >&2
exit 1
