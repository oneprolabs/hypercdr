#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${HCDR_BOOTSTRAP_INSTALL_DIR:-/opt/hypercdr-bootstrap}"
PORT="${HCDR_BOOTSTRAP_PORT:-8080}"
NGINX_IMAGE="${HCDR_BOOTSTRAP_NGINX_IMAGE:-nginx:1.27-alpine}"
SOURCE_DIR="${HCDR_BOOTSTRAP_PORTAL_SOURCE_DIR:-}"
PORTAL_DIR="${HCDR_BOOTSTRAP_PORTAL_DIR:-${INSTALL_DIR}/portal}"
MODE="${HCDR_BOOTSTRAP_PORTAL_MODE:-docker}"
TLS_HOST="${HCDR_BOOTSTRAP_TLS_HOST:-localhost}"
TLS_CERT_FILE="${HCDR_BOOTSTRAP_TLS_CERT_FILE:-}"
TLS_KEY_FILE="${HCDR_BOOTSTRAP_TLS_KEY_FILE:-}"
RELEASE_CENTER_URL="${HCDR_RELEASE_CENTER_URL:-http://172.17.0.1:8090}"
RELEASE_CENTER_TOKEN_FILE="${HCDR_RELEASE_CENTER_TOKEN_FILE:-}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
HyperCDR bootstrap portal installer

Usage:
  ./install-bootstrap-portal.sh [options]

Options:
  --source-dir PATH   Generated portal directory. Defaults to current package root when index.html exists.
  --install-dir PATH     Bootstrap persistent installation directory, default: /opt/hypercdr-bootstrap.
  --port PORT         Portal HTTPS port, default: 8080.
  --tls-host HOST     IP address or DNS name for an auto-generated certificate.
  --tls-cert-file     Existing PEM certificate. Must be used with --tls-key-file.
  --tls-key-file      Existing PEM private key. Must be used with --tls-cert-file.
  --mode MODE         docker or python, default: docker.
  --release-center-url URL  Release Center URL for the server-side catalog proxy.
  --release-center-token-file PATH  Token file used only by the proxy.
  --execute           Install/start portal. Without this flag, prints the plan only.
  -h, --help          Show help.

This script only serves the bootstrap download page and release artifacts. It
does not install the registry and does not install the HyperCDR control plane.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:?missing value for --source-dir}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; PORTAL_DIR="${INSTALL_DIR}/portal"; shift 2 ;;
    --port) PORT="${2:?missing value for --port}"; shift 2 ;;
    --tls-host) TLS_HOST="${2:?missing value for --tls-host}"; shift 2 ;;
    --tls-cert-file) TLS_CERT_FILE="${2:?missing value for --tls-cert-file}"; shift 2 ;;
    --tls-key-file) TLS_KEY_FILE="${2:?missing value for --tls-key-file}"; shift 2 ;;
    --mode) MODE="${2:?missing value for --mode}"; shift 2 ;;
    --release-center-url) RELEASE_CENTER_URL="${2:?missing value for --release-center-url}"; shift 2 ;;
    --release-center-token-file) RELEASE_CENTER_TOKEN_FILE="${2:?missing value for --release-center-token-file}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${SOURCE_DIR}" ]]; then
  if [[ -f "${SCRIPT_DIR}/../../index.html" ]]; then
    SOURCE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  elif [[ -f "${SCRIPT_DIR}/index.html" ]]; then
    SOURCE_DIR="${SCRIPT_DIR}"
  else
    echo "--source-dir is required when the script is not inside a generated portal package" >&2
    exit 2
  fi
fi

if [[ -n "${TLS_CERT_FILE}" || -n "${TLS_KEY_FILE}" ]]; then
  [[ -n "${TLS_CERT_FILE}" && -n "${TLS_KEY_FILE}" ]] || { echo "--tls-cert-file and --tls-key-file must be supplied together" >&2; exit 2; }
  [[ -r "${TLS_CERT_FILE}" ]] || { echo "TLS certificate is not readable: ${TLS_CERT_FILE}" >&2; exit 1; }
  [[ -r "${TLS_KEY_FILE}" ]] || { echo "TLS private key is not readable: ${TLS_KEY_FILE}" >&2; exit 1; }
fi

if [[ ! -f "${SOURCE_DIR}/index.html" ]]; then
  echo "portal index.html not found in source dir: ${SOURCE_DIR}" >&2
  exit 1
fi

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

preflight_portal() {

  require_command docker
  require_command openssl
  docker info >/dev/null 2>&1 || { echo "Docker daemon is not available" >&2; return 1; }
  docker compose version >/dev/null 2>&1 || { echo "Docker Compose V2 is required" >&2; return 1; }
  [[ -r "${SOURCE_DIR}/index.html" ]] || { echo "portal source is not readable: ${SOURCE_DIR}" >&2; return 1; }
  if command -v ss >/dev/null 2>&1 && ss -ltn "sport = :${PORT}" | tail -n +2 | grep -q .; then
    echo "portal port ${PORT} is already in use" >&2
    return 1
  fi
}

cat <<EOF
HyperCDR bootstrap portal plan

Mode:            ${MODE}
Source dir:      ${SOURCE_DIR}
Install dir:     ${PORTAL_DIR}
HTTPS port:      ${PORT}
TLS host:        ${TLS_HOST}
Portal image:    ${NGINX_IMAGE}
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  cat <<EOF

Dry-run mode. Add --execute to install/start the portal.
EOF
  exit 0
fi

preflight_portal

mkdir -p "${PORTAL_DIR}"
rm -rf "${PORTAL_DIR:?}/"*
cp -R "${SOURCE_DIR}/." "${PORTAL_DIR}/"
# The portal is served by an unprivileged web-server user. Release generation
# may run with a restrictive umask, so normalize only the static portal copy.
find "${PORTAL_DIR}" -type d -exec chmod 0755 {} +
find "${PORTAL_DIR}" -type f -exec chmod 0644 {} +

TLS_DIR="${INSTALL_DIR}/tls"
if [[ "${MODE}" == "docker" ]]; then
  mkdir -p "${TLS_DIR}"
  [[ -z "${RELEASE_CENTER_TOKEN_FILE}" || -r "${RELEASE_CENTER_TOKEN_FILE}" ]] || { echo "Release Center token file is not readable" >&2; exit 1; }
  token=""; [[ -z "${RELEASE_CENTER_TOKEN_FILE}" ]] || token="$(tr -d '\r\n' < "${RELEASE_CENTER_TOKEN_FILE}")"
  config_file="${INSTALL_DIR}/nginx-tls.conf"
  sed "s|__RELEASE_CENTER_URL__|${RELEASE_CENTER_URL}|; s|__RELEASE_CENTER_TOKEN__|${token}|" "${SCRIPT_DIR}/nginx-tls.conf" > "${config_file}"
  if [[ -n "${TLS_CERT_FILE}" ]]; then
    cp "${TLS_CERT_FILE}" "${TLS_DIR}/portal.crt"
    cp "${TLS_KEY_FILE}" "${TLS_DIR}/portal.key"
  elif [[ ! -s "${TLS_DIR}/portal.crt" || ! -s "${TLS_DIR}/portal.key" ]]; then
    require_command openssl
    san="DNS:${TLS_HOST}"
    [[ "${TLS_HOST}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && san="IP:${TLS_HOST}"
    openssl req -x509 -nodes -newkey rsa:4096 -sha256 -days 7300 \
      -subj "/CN=${TLS_HOST}" -addext "subjectAltName=${san},DNS:localhost,IP:127.0.0.1" \
      -keyout "${TLS_DIR}/portal.key" -out "${TLS_DIR}/portal.crt" >/dev/null 2>&1
  fi
  chmod 0600 "${TLS_DIR}/portal.key"
  chmod 0644 "${TLS_DIR}/portal.crt"
fi

case "${MODE}" in
  docker)
    URL_SCHEME="https"
    require_command docker
    HCDR_BOOTSTRAP_PORT="${PORT}" \
    HCDR_BOOTSTRAP_NGINX_IMAGE="${NGINX_IMAGE}" \
    HCDR_BOOTSTRAP_PORTAL_DIR="${PORTAL_DIR}" \
    HCDR_BOOTSTRAP_TLS_DIR="${TLS_DIR}" \
    HCDR_BOOTSTRAP_NGINX_CONFIG="${config_file}" \
    docker compose -f "${SCRIPT_DIR}/portal-compose.yaml" up -d
    ;;
  python)
    URL_SCHEME="http"
    require_command python3
    cat <<EOF

Starting foreground Python HTTP server.
Press Ctrl+C to stop it.
EOF
    exec python3 -m http.server "${PORT}" --directory "${PORTAL_DIR}"
    ;;
  *)
    echo "unknown mode: ${MODE}" >&2
    exit 2
    ;;
esac

cat <<EOF

HyperCDR bootstrap portal is running.

URL:
  ${URL_SCHEME}://${TLS_HOST}:${PORT}

Installed files:
  ${PORTAL_DIR}
EOF
