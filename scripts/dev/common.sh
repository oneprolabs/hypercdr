#!/usr/bin/env bash
set -euo pipefail

DEV_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HCDR_SOURCE_DIR="$(cd "${DEV_SCRIPT_DIR}/../.." && pwd)"
HCDR_SOURCE_PARENT="$(cd "${HCDR_SOURCE_DIR}/.." && pwd)"
HCDR_RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-${HCDR_SOURCE_PARENT}/hypercdr-runtime}"
HCDR_DEV_CONFIG="${HCDR_DEV_CONFIG:-${HCDR_RUNTIME_ROOT}/dev/dev.conf}"

if [[ ! -f "${HCDR_DEV_CONFIG}" ]]; then
  mkdir -p "$(dirname "${HCDR_DEV_CONFIG}")"
  cp "${DEV_SCRIPT_DIR}/dev.conf.example" "${HCDR_DEV_CONFIG}"
  chmod 600 "${HCDR_DEV_CONFIG}"
fi

# shellcheck disable=SC1090
source "${HCDR_DEV_CONFIG}"

HCDR_DEV_HOST="${HCDR_DEV_HOST:-192.168.8.149}"
HCDR_DEV_DIR="${HCDR_DEV_DIR:-${HCDR_RUNTIME_ROOT}/dev}"
HCDR_DEV_FRONTEND_PORT="${HCDR_DEV_FRONTEND_PORT:-3002}"
HCDR_DEV_API_PORT="${HCDR_DEV_API_PORT:-18080}"
HCDR_DEV_POSTGRES_PORT="${HCDR_DEV_POSTGRES_PORT:-15432}"
HCDR_DEV_REUSE_PLATFORM_POSTGRES="${HCDR_DEV_REUSE_PLATFORM_POSTGRES:-true}"
HCDR_PLATFORM_DEPLOY_ENV="${HCDR_PLATFORM_DEPLOY_ENV:-/var/lib/hypercdr/.env}"
HCDR_CACHE_ROOT="${HCDR_CACHE_ROOT:-${HCDR_RUNTIME_ROOT}/cache}"
HCDR_DEV_TLS_CERT_FILE="${HCDR_DEV_TLS_CERT_FILE:-${HCDR_RUNTIME_ROOT}/certs/platform.crt}"
HCDR_DEV_TLS_KEY_FILE="${HCDR_DEV_TLS_KEY_FILE:-${HCDR_RUNTIME_ROOT}/certs/platform.key}"

dev_log() { echo "==> $*"; }
dev_die() { echo "error: $*" >&2; exit 1; }
require_command() { command -v "$1" >/dev/null 2>&1 || dev_die "$1 is required"; }

pid_running() {
  local file="$1"
  [[ -f "${file}" ]] && kill -0 "$(cat "${file}")" 2>/dev/null
}

stop_pid() {
  local file="$1"
  local label="$2"
  if pid_running "${file}"; then
    local pid
    pid="$(cat "${file}")"
    dev_log "Stopping ${label} (pid ${pid})"
    kill "${pid}"
    for _ in {1..20}; do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 0.25
    done
    kill -0 "${pid}" 2>/dev/null && kill -9 "${pid}" || true
  fi
  rm -f "${file}"
}
