#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_DIR="$(mktemp -d)"
trap 'rm -rf "${RUNTIME_DIR}"' EXIT
export HCDR_INSTALL_DIR="${RUNTIME_DIR}"
export HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml"

# shellcheck source=../release/deploy-blue-green.sh
source "${ROOT_DIR}/scripts/release/deploy-blue-green.sh"

[[ "$(other_color blue)" == green ]]
[[ "$(other_color green)" == blue ]]
if other_color red >/dev/null 2>&1; then
  echo "invalid color was accepted" >&2
  exit 1
fi

[[ "$(select_deploy_color blue false)" == blue ]]
[[ "$(select_deploy_color blue true)" == green ]]

printf 'PLATFORM_API_BLUE_IMAGE=old\n' > "${RUNTIME_DIR}/.env"
set_env_value "${RUNTIME_DIR}/.env" PLATFORM_API_BLUE_IMAGE registry/platform-api:new
grep -Fxq 'PLATFORM_API_BLUE_IMAGE=registry/platform-api:new' "${RUNTIME_DIR}/.env"
set_env_value "${RUNTIME_DIR}/.env" PLATFORM_FRONTEND_BLUE_IMAGE registry/platform-frontend:new
grep -Fxq 'PLATFORM_FRONTEND_BLUE_IMAGE=registry/platform-frontend:new' "${RUNTIME_DIR}/.env"

render_upstream green "${RUNTIME_DIR}/upstream.conf"
grep -Fq 'map $host $hypercdr_api_active { default hypercdr-platform-api-green:18080; }' "${RUNTIME_DIR}/upstream.conf"
grep -Fq 'map $host $hypercdr_frontend_active { default hypercdr-platform-frontend-green:3002; }' "${RUNTIME_DIR}/upstream.conf"
grep -Fq 'wait_for_http "hypercdr-platform-frontend-${color}" 3002 / https' "${ROOT_DIR}/scripts/release/deploy-blue-green.sh"
grep -Fq 'HCDR_HTTPS_PORT:-12443' "${ROOT_DIR}/scripts/release/deploy-blue-green.sh"

FAKE_BIN="${RUNTIME_DIR}/bin"
mkdir -p "${FAKE_BIN}"
cat >"${FAKE_BIN}/docker" <<'EOF'
#!/usr/bin/env bash
set -e
if [[ "$1" == ps ]]; then
  printf '%s\n' "${FAKE_DOCKER_PS:-}"
  exit 0
fi
if [[ "$1" == inspect ]]; then
  case "$*" in
    *".State.Running"*)
      if [[ -n "${FAKE_RUNNING_FILE:-}" && -f "${FAKE_RUNNING_FILE}" ]]; then echo true; else echo false; fi
      ;;
    *".State.Health"*) echo healthy ;;
  esac
fi
exit 0
EOF
cat >"${FAKE_BIN}/curl" <<'EOF'
#!/usr/bin/env bash
exit "${FAKE_CURL_EXIT:-0}"
EOF
cat >"${FAKE_BIN}/ss" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${FAKE_SS_OUTPUT:-}"
EOF
chmod +x "${FAKE_BIN}/docker" "${FAKE_BIN}/curl" "${FAKE_BIN}/ss"
cat >"${RUNTIME_DIR}/.env" <<'EOF'
HCDR_IMAGE_REGISTRY=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr
HCDR_POSTGRES_PASSWORD=test-password
HCDR_DATABASE_URL=postgres://hypercdr:test-password@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_RELEASE_TOKEN=test-release-token
HCDR_REGISTRATION_EXECUTOR_TOKEN=test-registration-token
HCDR_TLS_CERT_FILE=/tmp/test.crt
HCDR_TLS_KEY_FILE=/tmp/test.key
HCDR_HTTP_PORT=18088
HCDR_HTTPS_PORT=12443
PLATFORM_API_BLUE_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-api-1.0.32.20260915
PLATFORM_FRONTEND_BLUE_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-frontend-1.0.32.20260915
PLATFORM_API_GREEN_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-api-1.0.32.20260915
PLATFORM_FRONTEND_GREEN_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-frontend-1.0.32.20260915
REGISTRATION_EXECUTOR_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:cluster-registration-executor-1.0.32.20260915
EOF
touch "${RUNTIME_DIR}/docker-compose.yaml"
FAKE_DOCKER_PS=$'nginx-proxy-manager\t0.0.0.0:80-81->80-81/tcp, 0.0.0.0:443->443/tcp' \
HCDR_INSTALL_DIR="${RUNTIME_DIR}" \
HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml" \
PATH="${FAKE_BIN}:${PATH}" \
  check_public_ports
HCDR_INSTALL_DIR="${RUNTIME_DIR}" \
HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml" \
HCDR_DOMAIN=hypercdr.com \
HCDR_POST_SWITCH_OBSERVE_SECONDS=0 \
HCDR_HEALTH_INTERVAL=0 \
PATH="${FAKE_BIN}:${PATH}" \
  "${ROOT_DIR}/scripts/release/deploy-blue-green.sh" 1.0.33.20260916
grep -Fxq blue "${RUNTIME_DIR}/.active_color"
grep -Fq 'map $host $hypercdr_api_active { default hypercdr-platform-api-blue:18080; }' "${RUNTIME_DIR}/nginx/conf.d/upstream.conf"

touch "${RUNTIME_DIR}/running"
FAKE_RUNNING_FILE="${RUNTIME_DIR}/running" \
HCDR_INSTALL_DIR="${RUNTIME_DIR}" \
HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml" \
HCDR_POST_SWITCH_OBSERVE_SECONDS=0 \
HCDR_OLD_COLOR_DRAIN_SECONDS=0 \
HCDR_HEALTH_INTERVAL=0 \
PATH="${FAKE_BIN}:${PATH}" \
  "${ROOT_DIR}/scripts/release/deploy-blue-green.sh" 1.0.34.20260916
grep -Fxq green "${RUNTIME_DIR}/.active_color"
grep -Fxq 'PLATFORM_API_GREEN_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-api-1.0.34.20260916' "${RUNTIME_DIR}/.env"
grep -Fq 'map $host $hypercdr_api_active { default hypercdr-platform-api-green:18080; }' "${RUNTIME_DIR}/nginx/conf.d/upstream.conf"
grep -Fxq 1.0.33.20260916 "${RUNTIME_DIR}/.rollback_version"

FAKE_RUNNING_FILE="${RUNTIME_DIR}/running" \
HCDR_INSTALL_DIR="${RUNTIME_DIR}" \
HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml" \
PATH="${FAKE_BIN}:${PATH}" \
  "${ROOT_DIR}/scripts/release/deploy-blue-green.sh" --rollback
grep -Fxq blue "${RUNTIME_DIR}/.active_color"
grep -Fxq 'PLATFORM_API_BLUE_IMAGE=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-api-1.0.33.20260916' "${RUNTIME_DIR}/.env"

if FAKE_CURL_EXIT=1 FAKE_RUNNING_FILE="${RUNTIME_DIR}/running" \
  HCDR_INSTALL_DIR="${RUNTIME_DIR}" \
  HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml" \
  HCDR_POST_SWITCH_OBSERVE_SECONDS=0 HCDR_HEALTH_INTERVAL=0 \
  PATH="${FAKE_BIN}:${PATH}" \
  "${ROOT_DIR}/scripts/release/deploy-blue-green.sh" 1.0.35.20260916; then
  echo "post-switch failure was not reported" >&2
  exit 1
fi
grep -Fxq blue "${RUNTIME_DIR}/.active_color"

echo "blue-green deploy helper tests passed"
