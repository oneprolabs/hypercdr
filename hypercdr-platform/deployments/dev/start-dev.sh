#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

for command in docker npm openssl systemctl systemd-run; do require_command "${command}"; done
GO_BIN="${HCDR_GO_BIN:-/usr/local/go/bin/go}"
[[ -x "${GO_BIN}" ]] || dev_die "Go binary is not executable: ${GO_BIN}"
[[ -r "${HCDR_DEV_TLS_CERT_FILE}" ]] || dev_die "TLS certificate is not readable: ${HCDR_DEV_TLS_CERT_FILE}"
[[ -r "${HCDR_DEV_TLS_KEY_FILE}" ]] || dev_die "TLS private key is not readable: ${HCDR_DEV_TLS_KEY_FILE}"

BIN_DIR="${HCDR_DEV_DIR}/bin"
LOG_DIR="${HCDR_DEV_DIR}/logs"
PID_DIR="${HCDR_DEV_DIR}/pids"
FRONTEND_DIR="${HCDR_DEV_DIR}/frontend"
FRONTEND_SOURCE_DIR="${HCDR_SOURCE_DIR}/platform/frontend"
SECRET_FILE="${HCDR_DEV_DIR}/platform-secret-key"
GO_BUILD_CACHE="${HCDR_CACHE_ROOT}/go-build"
GO_MOD_CACHE="${HCDR_CACHE_ROOT}/go-mod"
NPM_CACHE="${HCDR_CACHE_ROOT}/npm"

mkdir -p "${BIN_DIR}" "${LOG_DIR}" "${PID_DIR}" "${FRONTEND_DIR}" \
  "${HCDR_DEV_DIR}/data/postgres" "${GO_BUILD_CACHE}" "${GO_MOD_CACHE}" "${NPM_CACHE}"

if [[ ! -s "${SECRET_FILE}" ]]; then
  openssl rand -hex 32 > "${SECRET_FILE}"
  chmod 600 "${SECRET_FILE}"
fi

if systemctl is-active --quiet hypercdr-dev-api.service || systemctl is-active --quiet hypercdr-dev-frontend.service; then
  dev_die "development services are already running; use ./stop-dev.sh first"
fi

dev_log "Starting development PostgreSQL"
HCDR_DEV_DIR="${HCDR_DEV_DIR}" \
HCDR_DEV_POSTGRES_PORT="${HCDR_DEV_POSTGRES_PORT}" \
HCDR_IMAGE_REGISTRY="${HCDR_IMAGE_REGISTRY}" \
  docker compose -f "${SCRIPT_DIR}/compose.yaml" up -d

dev_log "Waiting for development PostgreSQL"
for _ in {1..60}; do
  if docker exec hypercdr-dev-postgres pg_isready -U hypercdr -d hypercdr >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec hypercdr-dev-postgres pg_isready -U hypercdr -d hypercdr >/dev/null 2>&1 || dev_die "PostgreSQL is not ready"

dev_log "Building backend binaries outside the source tree"
(
  cd "${HCDR_SOURCE_DIR}/platform/backend"
  PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local \
    GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}" \
    GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    "${GO_BIN}" build -trimpath -o "${BIN_DIR}/platform-migrate" ./cmd/platform-migrate
  PATH="$(dirname "${GO_BIN}"):${PATH}" GOTOOLCHAIN=local \
    GOPROXY="${HCDR_BUILD_GOPROXY:-https://goproxy.cn,direct}" \
    GOCACHE="${GO_BUILD_CACHE}" GOMODCACHE="${GO_MOD_CACHE}" \
    "${GO_BIN}" build -trimpath -o "${BIN_DIR}/platform-api" ./cmd/platform-api
)

export HCDR_DATABASE_URL="postgres://hypercdr:hypercdr@127.0.0.1:${HCDR_DEV_POSTGRES_PORT}/hypercdr?sslmode=disable"
export HCDR_HTTP_ADDR="0.0.0.0:${HCDR_DEV_API_PORT}"
export HCDR_PUBLIC_BASE_URL="https://${HCDR_DEV_HOST}:${HCDR_DEV_FRONTEND_PORT}"
export HCDR_AGENT_WS_ENDPOINT="wss://${HCDR_DEV_HOST}:${HCDR_DEV_FRONTEND_PORT}/ws/agent"
export HCDR_TLS_ENABLED="true"
export HCDR_TLS_CERT_FILE="${HCDR_DEV_TLS_CERT_FILE}"
export HCDR_TLS_KEY_FILE="${HCDR_DEV_TLS_KEY_FILE}"
export HCDR_SECRET_KEY="$(cat "${SECRET_FILE}")"
export HCDR_LOG_LEVEL="${HCDR_LOG_LEVEL:-debug}"

dev_log "Running database migrations"
"${BIN_DIR}/platform-migrate"

dev_log "Preparing external frontend dependencies"
rm -rf "${FRONTEND_DIR}"
mkdir -p "${FRONTEND_DIR}"
cp "${FRONTEND_SOURCE_DIR}/package.json" "${FRONTEND_DIR}/package.json"
cp "${FRONTEND_SOURCE_DIR}/package-lock.json" "${FRONTEND_DIR}/package-lock.json"
(
  cd "${FRONTEND_DIR}"
  npm ci --registry="${HCDR_BUILD_NPM_REGISTRY:-https://registry.npmmirror.com}" --cache="${NPM_CACHE}"
)

if [[ -e "${FRONTEND_SOURCE_DIR}/node_modules" && ! -L "${FRONTEND_SOURCE_DIR}/node_modules" ]]; then
  dev_die "refusing to replace non-symlink ${FRONTEND_SOURCE_DIR}/node_modules"
fi
ln -sfn "${FRONTEND_DIR}/node_modules" "${FRONTEND_SOURCE_DIR}/node_modules"

cat > "${HCDR_DEV_DIR}/run-api.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
set -a
source "${HCDR_DEV_CONFIG}"
set +a
export HCDR_DATABASE_URL="${HCDR_DATABASE_URL}"
export HCDR_HTTP_ADDR="${HCDR_HTTP_ADDR}"
export HCDR_PUBLIC_BASE_URL="${HCDR_PUBLIC_BASE_URL}"
export HCDR_AGENT_WS_ENDPOINT="${HCDR_AGENT_WS_ENDPOINT}"
export HCDR_TLS_ENABLED=false
export HCDR_SECRET_KEY="${HCDR_SECRET_KEY}"
exec "${BIN_DIR}/platform-api" >> "${LOG_DIR}/platform-api.log" 2>&1
EOF

cat > "${HCDR_DEV_DIR}/run-frontend.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
export HCDR_API_PROXY="http://127.0.0.1:${HCDR_DEV_API_PORT}"
export HCDR_DEV_TLS_CERT_FILE="${HCDR_DEV_TLS_CERT_FILE}"
export HCDR_DEV_TLS_KEY_FILE="${HCDR_DEV_TLS_KEY_FILE}"
cd "${FRONTEND_SOURCE_DIR}"
exec "${FRONTEND_DIR}/node_modules/.bin/vite" --host 0.0.0.0 --port "${HCDR_DEV_FRONTEND_PORT}" >> "${LOG_DIR}/platform-frontend.log" 2>&1
EOF
chmod 700 "${HCDR_DEV_DIR}/run-api.sh" "${HCDR_DEV_DIR}/run-frontend.sh"
: > "${LOG_DIR}/platform-api.log"
: > "${LOG_DIR}/platform-frontend.log"

systemctl reset-failed hypercdr-dev-api.service hypercdr-dev-frontend.service >/dev/null 2>&1 || true
dev_log "Starting backend HTTPS transient service"
systemd-run --unit=hypercdr-dev-api --property=Restart=on-failure \
  --property=RestartSec=2s "${HCDR_DEV_DIR}/run-api.sh" >/dev/null

dev_log "Starting Vite HTTPS transient service"
systemd-run --unit=hypercdr-dev-frontend --property=Restart=on-failure \
  --property=RestartSec=2s "${HCDR_DEV_DIR}/run-frontend.sh" >/dev/null

sleep 2
systemctl is-active --quiet hypercdr-dev-api.service || dev_die "backend failed; see ${LOG_DIR}/platform-api.log"
systemctl is-active --quiet hypercdr-dev-frontend.service || dev_die "frontend failed; see ${LOG_DIR}/platform-frontend.log"

cat <<EOF

HyperCDR development mode is running.
Frontend:  https://${HCDR_DEV_HOST}:${HCDR_DEV_FRONTEND_PORT}
Backend:   http://127.0.0.1:${HCDR_DEV_API_PORT} (internal proxy target)
Agent WSS: wss://${HCDR_DEV_HOST}:${HCDR_DEV_FRONTEND_PORT}/ws/agent
Logs:      ${LOG_DIR}
EOF
