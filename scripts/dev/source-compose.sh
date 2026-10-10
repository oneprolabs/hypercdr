#!/usr/bin/env bash
# Portable container development, isolated from host development and blue/green.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_DIR="${HCDR_SOURCE_RUNTIME:-$(dirname "${ROOT_DIR}")/hypercdr-runtime/environments/source-compose}"
mkdir -p "${RUNTIME_DIR}"
RUNTIME_DIR="$(cd "${RUNTIME_DIR}" && pwd)"
case "${RUNTIME_DIR}/" in "${ROOT_DIR}/"*) echo 'Runtime must be outside the repository' >&2; exit 1;; esac
export HCDR_SOURCE_RUNTIME="${RUNTIME_DIR}"
ENV_FILE="${RUNTIME_DIR}/compose.env"
if [[ ! -s "${ENV_FILE}" ]]; then
  umask 077
  for key in HCDR_SOURCE_DB_PASSWORD HCDR_SOURCE_SECRET_KEY HCDR_SOURCE_RELEASE_TOKEN HCDR_SOURCE_EXECUTOR_TOKEN; do
    printf '%s=%s\n' "${key}" "$(openssl rand -hex 32)"
  done > "${ENV_FILE}"
fi
mkdir -p "${RUNTIME_DIR}/releases"
action="${1:-help}"
shift || true
compose=(docker compose --project-name "${HCDR_SOURCE_PROJECT:-hypercdr-source}" --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.source.yml")
case "${action}" in
  up)
    if [[ "${1:-}" == --manifest ]]; then
      manifest="${2:?provide a release-manifest.json file}"
      python3 - "${manifest}" <<'PY'
import json,sys
x=json.load(open(sys.argv[1]))
if not x.get('version') or not isinstance(x.get('componentManifest'), dict):
    raise ValueError('A published release manifest is required')
required = {'comm-agent', 'velero', 'velero-plugin-for-aws',
            'velero-plugin-for-microsoft-azure', 'velero-plugin-for-gcp'}
if required - x['componentManifest'].keys():
    raise ValueError('Missing cluster installer components')
for item in x['componentManifest'].values():
    if not isinstance(item, dict) or not item.get('image','').startswith('registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:'):
        raise ValueError('Unexpected image registry')
PY
      cp "${manifest}" "${RUNTIME_DIR}/releases/release-manifest.json"
      shift 2
    fi
    [[ $# == 0 ]] || { echo 'Usage: source-compose.sh up [--manifest FILE]' >&2; exit 1; }
    [[ -s "${RUNTIME_DIR}/releases/release-manifest.json" ]] || {
      echo 'Provide a GitHub Release release-manifest.json with up --manifest FILE; cluster installers need its immutable component list.' >&2; exit 1;
    }
    "${compose[@]}" up --build -d --wait --wait-timeout 180
    ;;
  build|config|ps|logs|down) "${compose[@]}" "${action}" "$@" ;;
  *) echo 'Usage: source-compose.sh {up [--manifest FILE]|build|config|ps|logs|down}' ;;
esac
