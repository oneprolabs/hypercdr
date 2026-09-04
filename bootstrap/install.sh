#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${HCDR_INSTALL_CONFIG:-${SCRIPT_DIR}/install-config.sh}"

if [[ ! -r "${CONFIG_FILE}" ]]; then
  echo "installation config is not readable: ${CONFIG_FILE}" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "${CONFIG_FILE}"

required_vars=(HCDR_PUBLIC_BASE_URL HCDR_REGISTRY HCDR_IMAGE_TAG HCDR_DATA_DIR HCDR_HTTP_PORT HCDR_API_PORT)
for name in "${required_vars[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "required setting is empty in ${CONFIG_FILE}: ${name}" >&2
    exit 2
  fi
done

if [[ "${HCDR_PUBLIC_BASE_URL}" == *"192.0.2.10"* ]]; then
  echo "replace the example IP in HCDR_PUBLIC_BASE_URL before installation" >&2
  exit 2
fi

args=(
  docker
  --public-base-url "${HCDR_PUBLIC_BASE_URL}"
  --agent-private-endpoint "${HCDR_PUBLIC_BASE_URL/https:/wss:}/ws/agent"
  --registry "${HCDR_REGISTRY}"
  --image-tag "${HCDR_IMAGE_TAG}"
  --data-dir "${HCDR_DATA_DIR}"
  --http-port "${HCDR_HTTP_PORT}"
  --api-port "${HCDR_API_PORT}"
)

if [[ -n "${HCDR_AGENT_PUBLIC_URL:-}" ]]; then
  args+=(--agent-public-url "${HCDR_AGENT_PUBLIC_URL}")
fi

if [[ -n "${HCDR_TLS_CERT_FILE:-}" || -n "${HCDR_TLS_KEY_FILE:-}" ]]; then
  if [[ -z "${HCDR_TLS_CERT_FILE:-}" || -z "${HCDR_TLS_KEY_FILE:-}" ]]; then
    echo "HCDR_TLS_CERT_FILE and HCDR_TLS_KEY_FILE must be configured together" >&2
    exit 2
  fi
  args+=(--tls-cert-file "${HCDR_TLS_CERT_FILE}" --tls-key-file "${HCDR_TLS_KEY_FILE}")
fi

echo "HyperCDR installation configuration:"
echo "  URL:      ${HCDR_PUBLIC_BASE_URL}"
echo "  Registry: ${HCDR_REGISTRY}"
echo "  Version:  ${HCDR_IMAGE_TAG}"
echo "  Data:     ${HCDR_DATA_DIR}"
echo

if [[ "${1:-}" == "--check" ]]; then
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
