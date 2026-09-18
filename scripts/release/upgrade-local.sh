#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
usage() { cat <<'USAGE'
Upgrade an existing HyperCDR Docker installation from this local package.
Usage: ./upgrade-local.sh --base-url HTTPS_URL --image-tag VERSION [options]
USAGE
}
base_url=""; image_tag=""; install_dir="/var/lib/hypercdr"; registry=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url) base_url="${2:?missing value}"; shift 2;;
    --image-tag) image_tag="${2:?missing value}"; shift 2;;
    --install-dir) install_dir="${2:?missing value}"; shift 2;;
    --registry) registry="${2:?missing value}"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2;;
  esac
done
[[ -n "$base_url" && -n "$image_tag" ]] || { usage >&2; exit 2; }
[[ -f "$install_dir/.env" ]] || { echo "existing installation not found: $install_dir" >&2; exit 1; }
CONFIG_FILE="${HCDR_INSTALL_CONFIG:-${SCRIPT_DIR}/install-config.sh}"
[[ -r "${CONFIG_FILE}" ]] || { echo "installation config is not readable: ${CONFIG_FILE}" >&2; exit 1; }
# shellcheck disable=SC1090
source "${CONFIG_FILE}"
export HCDR_AUTH_CHALLENGE_MODE
export HCDR_TURNSTILE_SITE_KEY
export HCDR_TURNSTILE_SECRET_KEY
export HCDR_TURNSTILE_VERIFY_URL
args=(docker --base-url "$base_url" --image-tag "$image_tag" --install-dir "$install_dir" --confirm-prerequisites --execute)
[[ -z "$registry" ]] || args+=(--registry "$registry")
exec "$SCRIPT_DIR/install-platform.sh" "${args[@]}"
