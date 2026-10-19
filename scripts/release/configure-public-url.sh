#!/usr/bin/env bash
set -Eeuo pipefail
install_dir="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"
compose_file="${HCDR_COMPOSE_FILE:-${install_dir}/docker-compose.yaml}"
[[ $# -eq 1 ]] || { echo "Usage: $0 https://app.example.com" >&2; exit 2; }
[[ -f "${install_dir}/.active_color" ]] || { echo "No active HyperCDR release found." >&2; exit 1; }
color="$(tr -d '[:space:]' < "${install_dir}/.active_color")"
case "$color" in blue|green) ;; *) echo "Invalid active release color: $color" >&2; exit 1;; esac
command -v docker >/dev/null || { echo "Docker is required." >&2; exit 1; }
docker compose --env-file "${install_dir}/.env" -f "${compose_file}" --profile "$color" run --rm --no-deps --entrypoint platform-configure-url "hypercdr-platform-api-${color}" "$1"
