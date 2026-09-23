#!/usr/bin/env bash
set -euo pipefail

# No file editing required:
# ./install.sh --base-url https://hypercdr.example.com --install-dir /data/hypercdr/deploy
usage() {
  printf '%s\n' \
    'Usage: ./install.sh [docker] --base-url HTTPS_URL [--public-base-url URL] [--install-dir PATH] [--check]' \
    'Deployment mode: install.sh installs the control plane on this host using Docker Compose.' \
    'The underlying install-platform.sh command uses the explicit mode: ./install-platform.sh docker ...' \
    'Command-line values override install-config.sh; other settings use that file.' \
    'Example: ./install.sh --base-url https://hypercdr.example.com --install-dir /data/hypercdr/deploy' \
    'Add --check to validate prerequisites without installing. Installation requires interactive YES confirmation.'
}
base_override=""
install_override=""
public_override=""
check_only=false
# Accept the explicit Docker mode for compatibility with install-platform.sh
# and older package documentation. This entry point only supports Docker.
if [[ "${1:-}" == "docker" ]]; then
  shift
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url|--public-base-url|--install-dir)
      [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || { echo "Missing value for $1" >&2; exit 2; }
      if [[ "$1" == --base-url ]]; then base_override="$2"; elif [[ "$1" == --public-base-url ]]; then public_override="$2"; else install_override="$2"; fi
      shift 2 ;;
    --check) check_only=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${HCDR_INSTALL_CONFIG:-${SCRIPT_DIR}/install-config.sh}"

if [[ ! -r "${CONFIG_FILE}" ]]; then
  echo "installation config is not readable: ${CONFIG_FILE}" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "${CONFIG_FILE}"

if [[ -n "$base_override" ]]; then
  HCDR_BASE_URL="${base_override%/}"
fi
[[ "$HCDR_BASE_URL" =~ ^https://([A-Za-z0-9.-]+)(:([0-9]+))?/?$ ]] || { echo '--base-url must be an HTTPS host URL, optionally with a port' >&2; exit 2; }
if [[ -n "${BASH_REMATCH[3]:-}" ]]; then
  url_port="${BASH_REMATCH[3]}"
  [[ ${#url_port} -le 5 ]] && (( 10#${url_port} >= 1 && 10#${url_port} <= 65535 )) || { echo 'Port must be between 1 and 65535' >&2; exit 2; }
fi
if [[ -n "$install_override" ]]; then
  [[ "$install_override" == /* && "$install_override" != / && "$install_override" != *$'\n'* ]] || { echo '--install-dir must be an absolute, non-root directory' >&2; exit 2; }
  HCDR_INSTALL_DIR="$install_override"
fi
if [[ -n "$public_override" ]]; then
  [[ "$public_override" =~ ^https?://[^/]+/?$ ]] || { echo '--public-base-url must be an http(s) origin without a path' >&2; exit 2; }
  HCDR_PUBLIC_BASE_URL="${public_override%/}"
fi

required_vars=(HCDR_BASE_URL HCDR_REGISTRY HCDR_IMAGE_TAG HCDR_INSTALL_DIR)
for name in "${required_vars[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "required setting is empty in ${CONFIG_FILE}: ${name}" >&2
    exit 2
  fi
done

# install-platform.sh runs as a child process. Export authentication settings
# loaded from install-config.sh so the selected challenge reaches runtime.
export HCDR_AUTH_CHALLENGE_MODE
export HCDR_TURNSTILE_SITE_KEY
export HCDR_TURNSTILE_SECRET_KEY
export HCDR_TURNSTILE_VERIFY_URL
export HCDR_PROXY_NETWORK
export HCDR_NPM_UPSTREAM_READY

if [[ "${HCDR_BASE_URL}" == *"hypercdr.example.com"* ]]; then
  echo "Supply the public HTTPS URL or edit install-config.sh before installation" >&2
  exit 2
fi

args=(
  docker
  --base-url "${HCDR_BASE_URL}"
  --agent-private-endpoint "${HCDR_BASE_URL/https:/wss:}/ws/agent"
  --registry "${HCDR_REGISTRY}"
  --image-tag "${HCDR_IMAGE_TAG}"
  --install-dir "${HCDR_INSTALL_DIR}"
)

if [[ -n "${HCDR_PUBLIC_BASE_URL:-}" ]]; then
  args+=(--public-base-url "${HCDR_PUBLIC_BASE_URL}")
fi

echo "HyperCDR installation configuration:"
echo "  URL:      ${HCDR_BASE_URL}"
echo "  Registry: ${HCDR_REGISTRY}"
echo "  Version:  ${HCDR_IMAGE_TAG}"
echo "  Install:  ${HCDR_INSTALL_DIR}"
echo

if [[ "$check_only" == true ]]; then
  echo "Running host prerequisite checks. No services will be installed."
  exec "${SCRIPT_DIR}/install-platform.sh" "${args[@]}"
fi

cat <<'NOTICE'
Required software:
  - Docker Engine (daemon running and accessible)
  - Docker Compose V2 ('docker compose')
  - bash, curl, and openssl

The installer will verify these requirements again before making changes.
NOTICE
if [[ ! -t 0 ]]; then
  echo "Interactive confirmation is required. Run this installer from a terminal." >&2
  exit 2
fi
read -r -p "Confirm that the required software is installed, then type YES to continue: " confirmation
[[ "${confirmation}" == "YES" ]] || { echo "Installation canceled."; exit 2; }

exec "${SCRIPT_DIR}/install-platform.sh" "${args[@]}" --confirm-prerequisites --execute
