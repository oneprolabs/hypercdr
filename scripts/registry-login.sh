#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REGISTRY_CONFIG="${HCDR_REGISTRY_CONFIG:-${ROOT_DIR}/config/registries.conf}"
REGISTRY_PROFILE="${HCDR_REGISTRY_PROFILE:-}"
USERNAME="${HCDR_REGISTRY_USERNAME:-}"
PASSWORD_FILE="${HCDR_REGISTRY_PASSWORD_FILE:-}"
DRY_RUN="false"

usage() {
  cat <<'USAGE'
Log in Docker to a configured HyperCDR container registry.

Usage:
  ./scripts/registry-login.sh [options]

Options:
  --profile NAME         Registry profile; defaults to HCDR_ACTIVE_REGISTRY.
  --config PATH          Registry profiles file, default config/registries.conf.
  --username USER        Registry username. If omitted, Docker prompts for it.
  --password-file PATH   Read the password from a protected file.
  --dry-run              Print the selected target without logging in.
  -h, --help             Show help.

Examples:
  ./scripts/registry-login.sh
  ./scripts/registry-login.sh --profile aliyun_acr
  ./scripts/registry-login.sh --profile harbor_149 --username admin

Credentials are stored by Docker for the current operating-system user. Never
put a password or access key in config/registries.conf or a command argument.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) REGISTRY_PROFILE="${2:?missing value for --profile}"; shift 2 ;;
    --config) REGISTRY_CONFIG="${2:?missing value for --config}"; shift 2 ;;
    --username) USERNAME="${2:?missing value for --username}"; shift 2 ;;
    --password-file) PASSWORD_FILE="${2:?missing value for --password-file}"; shift 2 ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 1; }
# shellcheck source=lib/registry-config.sh
source "${ROOT_DIR}/scripts/lib/registry-config.sh"
load_registry_profile "${REGISTRY_CONFIG}" "${REGISTRY_PROFILE}"

cat <<EOF
HyperCDR Registry Login
  Profile:  ${HCDR_SELECTED_REGISTRY}
  Provider: ${HCDR_SELECTED_REGISTRY_PROVIDER}
  Server:   ${HCDR_REGISTRY_SERVER}
  Images:   ${HCDR_IMAGE_REGISTRY}/<component>:<version>
EOF

if [[ "${DRY_RUN}" == "true" ]]; then
  echo "Dry-run complete; docker login was not executed."
  exit 0
fi

if [[ -n "${PASSWORD_FILE}" ]]; then
  [[ -n "${USERNAME}" ]] || { echo "error: --username is required with --password-file" >&2; exit 2; }
  [[ -r "${PASSWORD_FILE}" ]] || { echo "error: password file is not readable: ${PASSWORD_FILE}" >&2; exit 1; }
  docker login "${HCDR_REGISTRY_SERVER}" --username "${USERNAME}" --password-stdin < "${PASSWORD_FILE}"
elif [[ -n "${HCDR_REGISTRY_PASSWORD:-}" ]]; then
  [[ -n "${USERNAME}" ]] || { echo "error: HCDR_REGISTRY_USERNAME is required with HCDR_REGISTRY_PASSWORD" >&2; exit 2; }
  printf '%s' "${HCDR_REGISTRY_PASSWORD}" | docker login "${HCDR_REGISTRY_SERVER}" --username "${USERNAME}" --password-stdin
elif [[ -n "${USERNAME}" ]]; then
  docker login "${HCDR_REGISTRY_SERVER}" --username "${USERNAME}"
else
  read -r -p "Registry username only (do not include the server address): " USERNAME
  [[ -n "${USERNAME}" ]] || { echo "error: registry username is required" >&2; exit 2; }
  if [[ "${USERNAME}" == *[[:space:]]* || "${USERNAME}" == *"${HCDR_REGISTRY_SERVER}"* ]]; then
    echo "error: enter only the Registry username, without spaces or the server address" >&2
    exit 2
  fi
  docker login "${HCDR_REGISTRY_SERVER}" --username "${USERNAME}"
fi

echo "SUCCESS: Docker credentials are ready for profile ${HCDR_SELECTED_REGISTRY}."
