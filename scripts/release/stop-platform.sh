#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install_dir="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"; compose_file=
[[ ! -f "${script_dir}/docker-compose.yaml" ]] || install_dir="${script_dir}"
while [[ $# -gt 0 ]]; do
 case "$1" in --install-dir) install_dir="${2:?missing value}"; shift 2;; --compose-file) compose_file="${2:?missing value}"; shift 2;; -h|--help) echo "Usage: $0 [--install-dir PATH] [--compose-file PATH]"; exit 0;; *) echo "Unknown option: $1" >&2; exit 2;; esac
done
compose_file="${compose_file:-${install_dir}/docker-compose.yaml}"
command -v docker >/dev/null || { echo "Docker is required" >&2; exit 1; }; [[ -f "$compose_file" ]] || { echo "Compose file not found: $compose_file" >&2; exit 1; }
cd "$(dirname "$compose_file")"
if [[ -x "${install_dir}/deploy-blue-green.sh" ]]; then
  docker compose --env-file "${install_dir}/.env" -f "$compose_file" --project-name hypercdr \
    --profile blue --profile green stop \
    hypercdr-platform-api-blue hypercdr-platform-frontend-blue \
    hypercdr-platform-api-green hypercdr-platform-frontend-green \
    hypercdr-edge hypercdr-cluster-registration-executor hypercdr-postgres
  echo "HyperCDR blue/green services stopped. Data is preserved."
  exit 0
fi
docker compose --env-file "${install_dir}/.env" -f "$compose_file" --project-name hypercdr stop
echo "HyperCDR stopped. Data is preserved. Host reboot will start it again."
