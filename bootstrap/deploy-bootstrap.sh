#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-$(cd "${ROOT_DIR}/.." && pwd)/hypercdr-runtime}"
SOURCE_DIR="${HCDR_BOOTSTRAP_PORTAL_SOURCE_DIR:-${RUNTIME_ROOT}/services/bootstrap-portal/source}"
DATA_DIR="${HCDR_BOOTSTRAP_DATA_DIR:-${RUNTIME_ROOT}/services/bootstrap-portal/data}"
PORT="${HCDR_BOOTSTRAP_PORT:-8080}"
TLS_HOST="${HCDR_BOOTSTRAP_TLS_HOST:-}"
TLS_CERT_FILE="${HCDR_BOOTSTRAP_TLS_CERT_FILE:-}"
TLS_KEY_FILE="${HCDR_BOOTSTRAP_TLS_KEY_FILE:-}"
MODE="${HCDR_BOOTSTRAP_PORTAL_MODE:-docker}"
EXECUTE="false"

usage() {
  cat <<'USAGE'
Deploy the HyperCDR bootstrap download portal.

Usage:
  ./bootstrap/deploy-bootstrap.sh [options]

Options:
  --source-dir PATH  Generated portal source.
  --data-dir PATH    Portal runtime data directory.
  --port PORT        HTTP port, default: 8080.
  --tls-host HOST    IP address or DNS name included in an auto-generated certificate.
  --tls-cert-file    Existing PEM certificate. Must be used with --tls-key-file.
  --tls-key-file     Existing PEM private key. Must be used with --tls-cert-file.
  --mode MODE        docker or python, default: docker.
  --execute          Deploy or update the portal.
  -h, --help         Show help.

The portal uses the Registry profile embedded in each release package. It does
not ask users to configure a Registry or install a Registry CA certificate.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:?missing value for --source-dir}"; shift 2 ;;
    --data-dir) DATA_DIR="${2:?missing value for --data-dir}"; shift 2 ;;
    --port) PORT="${2:?missing value for --port}"; shift 2 ;;
    --tls-host) TLS_HOST="${2:?missing value for --tls-host}"; shift 2 ;;
    --tls-cert-file) TLS_CERT_FILE="${2:?missing value for --tls-cert-file}"; shift 2 ;;
    --tls-key-file) TLS_KEY_FILE="${2:?missing value for --tls-key-file}"; shift 2 ;;
    --mode) MODE="${2:?missing value for --mode}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

args=(--source-dir "${SOURCE_DIR}" --data-dir "${DATA_DIR}" --port "${PORT}" --mode "${MODE}")
[[ -n "${TLS_HOST}" ]] && args+=(--tls-host "${TLS_HOST}")
[[ -n "${TLS_CERT_FILE}" ]] && args+=(--tls-cert-file "${TLS_CERT_FILE}")
[[ -n "${TLS_KEY_FILE}" ]] && args+=(--tls-key-file "${TLS_KEY_FILE}")
[[ "${EXECUTE}" == "true" ]] && args+=(--execute)
exec "${SCRIPT_DIR}/portal/install-bootstrap-portal.sh" "${args[@]}"
