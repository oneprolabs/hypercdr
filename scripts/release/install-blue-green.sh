#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -r "${SCRIPT_DIR}/scripts/lib/registry-config.sh" ]]; then
  ROOT_DIR="${SCRIPT_DIR}"
  REGISTRY_HELPER="${SCRIPT_DIR}/scripts/lib/registry-config.sh"
  COMPOSE_TEMPLATE="${SCRIPT_DIR}/compose.yaml"
  EDGE_CONFIG="${SCRIPT_DIR}/nginx/edge.conf"
  UPSTREAM_CONFIG="${SCRIPT_DIR}/nginx/upstream.conf.default"
  DEPLOY_SCRIPT="${SCRIPT_DIR}/deploy-blue-green.sh"
  START_SCRIPT="${SCRIPT_DIR}/start-platform.sh"
  STOP_SCRIPT="${SCRIPT_DIR}/stop-platform.sh"
  RESTART_SCRIPT="${SCRIPT_DIR}/restart-platform.sh"
  SERVICE_TEMPLATE="${SCRIPT_DIR}/templates/hypercdr.service"
else
  ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  REGISTRY_HELPER="${ROOT_DIR}/scripts/lib/registry-config.sh"
  COMPOSE_TEMPLATE="${ROOT_DIR}/docker-compose.yml"
  EDGE_CONFIG="${ROOT_DIR}/docker/nginx/edge.conf"
  UPSTREAM_CONFIG="${ROOT_DIR}/docker/nginx/upstream.conf.default"
  DEPLOY_SCRIPT="${ROOT_DIR}/scripts/release/deploy-blue-green.sh"
  START_SCRIPT="${ROOT_DIR}/scripts/release/start-platform.sh"
  STOP_SCRIPT="${ROOT_DIR}/scripts/release/stop-platform.sh"
  RESTART_SCRIPT="${ROOT_DIR}/scripts/release/restart-platform.sh"
  SERVICE_TEMPLATE="${ROOT_DIR}/scripts/release/templates/hypercdr.service"
fi
source "$REGISTRY_HELPER"
VERSION=""
BASE_URL=""
REGISTRY=""
INSTALL_DIR="/var/lib/hypercdr"
DOMAIN="hypercdr.com"
EXECUTE=false
LEGACY_MIGRATION=false
LEGACY_BACKUP_DIR=""
LEGACY_EDGE_NAME=""
LEGACY_CONTAINERS=(
  hypercdr-platform-api
  hypercdr-platform-frontend
  hypercdr-platform-upgrader
)

usage() {
  cat <<'USAGE'
Usage: install-blue-green.sh VERSION --base-url https://hypercdr.com --registry REGISTRY \
  [--install-dir PATH] [--execute]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url) BASE_URL="${2:?missing value for --base-url}"; shift 2 ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; shift 2 ;;
    --domain) DOMAIN="${2:?missing value for --domain}"; shift 2 ;;
    --execute) EXECUTE=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) [[ -z "$VERSION" ]] || { echo "unknown argument: $1" >&2; exit 2; }; VERSION="${1#v}"; shift ;;
  esac
done

[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]] || { echo "invalid version: $VERSION" >&2; exit 2; }
[[ "$BASE_URL" == https://* ]] || { echo "--base-url must use https://" >&2; exit 2; }
[[ -n "$REGISTRY" ]] || { echo "--registry is required" >&2; exit 2; }
[[ "$INSTALL_DIR" == /* && "$INSTALL_DIR" != / ]] || { echo "--install-dir must be an absolute non-root path" >&2; exit 2; }
if [[ "$EXECUTE" != true ]]; then
  printf 'Dry run: version=%s registry=%s install=%s domain=%s\n' "$VERSION" "$REGISTRY" "$INSTALL_DIR" "$DOMAIN"
  exit 0
fi

for command_name in docker openssl install; do
  command -v "$command_name" >/dev/null 2>&1 || { echo "missing required command: $command_name" >&2; exit 1; }
done
docker compose version >/dev/null 2>&1 || { echo "Docker Compose V2 is required" >&2; exit 1; }

is_legacy_install() {
  [[ -f "${INSTALL_DIR}/.env" ]] || return 1
  ! grep -q '^PLATFORM_API_BLUE_IMAGE=' "${INSTALL_DIR}/.env" ||
    ! grep -q '^PLATFORM_API_GREEN_IMAGE=' "${INSTALL_DIR}/.env"
}

legacy_container_exists() {
  docker inspect "$1" >/dev/null 2>&1
}

prepare_legacy_migration() {
  LEGACY_MIGRATION=true
  LEGACY_BACKUP_DIR="${INSTALL_DIR}/backups/legacy-migration-$(date -u +%Y%m%dT%H%M%SZ)"
  mkdir -p "${LEGACY_BACKUP_DIR}"
  [[ -f "${INSTALL_DIR}/.env" ]] && install -m 0600 "${INSTALL_DIR}/.env" "${LEGACY_BACKUP_DIR}/.env"
  [[ -f "${INSTALL_DIR}/docker-compose.yaml" ]] && install -m 0644 "${INSTALL_DIR}/docker-compose.yaml" "${LEGACY_BACKUP_DIR}/docker-compose.yaml"
  [[ -d "${INSTALL_DIR}/nginx" ]] && cp -a "${INSTALL_DIR}/nginx" "${LEGACY_BACKUP_DIR}/nginx"
  [[ -d "${INSTALL_DIR}/tls" ]] && cp -a "${INSTALL_DIR}/tls" "${LEGACY_BACKUP_DIR}/tls"
  [[ -f "${INSTALL_DIR}/tls.crt" ]] && install -m 0644 "${INSTALL_DIR}/tls.crt" "${LEGACY_BACKUP_DIR}/tls.crt"
  [[ -f "${INSTALL_DIR}/tls.key" ]] && install -m 0600 "${INSTALL_DIR}/tls.key" "${LEGACY_BACKUP_DIR}/tls.key"
  docker ps -a --format '{{.Names}} {{.Image}} {{.Status}}' >"${LEGACY_BACKUP_DIR}/containers.txt"

  for container in "${LEGACY_CONTAINERS[@]}"; do
    if legacy_container_exists "$container"; then
      docker update --restart=no "$container" >/dev/null 2>&1 || true
      docker stop "$container" >/dev/null 2>&1 || true
    fi
  done

  # The new edge uses the same public ports. Rename, rather than remove, an
  # existing edge so it can be restored if the migration fails.
  if legacy_container_exists hypercdr-edge; then
    LEGACY_EDGE_NAME="hypercdr-edge-legacy-$(date -u +%Y%m%d%H%M%S)"
    docker rename hypercdr-edge "$LEGACY_EDGE_NAME"
    docker update --restart=no "$LEGACY_EDGE_NAME" >/dev/null 2>&1 || true
    docker stop "$LEGACY_EDGE_NAME" >/dev/null 2>&1 || true
  fi
}

rollback_legacy_migration() {
  [[ "$LEGACY_MIGRATION" == true ]] || return 0
  printf 'Legacy migration failed; restoring the previous application containers.\n' >&2
  docker rm -f \
    hypercdr-edge \
    hypercdr-platform-api-blue \
    hypercdr-platform-api-green \
    hypercdr-platform-frontend-blue \
    hypercdr-platform-frontend-green \
    hypercdr-cluster-registration-executor \
    >/dev/null 2>&1 || true
  if [[ -n "$LEGACY_EDGE_NAME" ]] && legacy_container_exists "$LEGACY_EDGE_NAME"; then
    docker rename "$LEGACY_EDGE_NAME" hypercdr-edge >/dev/null 2>&1 || true
    docker start hypercdr-edge >/dev/null 2>&1 || true
  fi
  for container in "${LEGACY_CONTAINERS[@]}"; do
    docker start "$container" >/dev/null 2>&1 || true
  done
  if [[ -n "$LEGACY_BACKUP_DIR" ]]; then
    [[ -f "${LEGACY_BACKUP_DIR}/.env" ]] && install -m 0600 "${LEGACY_BACKUP_DIR}/.env" "${INSTALL_DIR}/.env"
    [[ -f "${LEGACY_BACKUP_DIR}/docker-compose.yaml" ]] && install -m 0644 "${LEGACY_BACKUP_DIR}/docker-compose.yaml" "${INSTALL_DIR}/docker-compose.yaml"
    if [[ -d "${LEGACY_BACKUP_DIR}/nginx" ]]; then
      rm -rf "${INSTALL_DIR}/nginx"
      cp -a "${LEGACY_BACKUP_DIR}/nginx" "${INSTALL_DIR}/nginx"
    fi
    if [[ -d "${LEGACY_BACKUP_DIR}/tls" ]]; then
      rm -rf "${INSTALL_DIR}/tls"
      cp -a "${LEGACY_BACKUP_DIR}/tls" "${INSTALL_DIR}/tls"
    fi
    [[ -f "${LEGACY_BACKUP_DIR}/tls.crt" ]] && install -m 0644 "${LEGACY_BACKUP_DIR}/tls.crt" "${INSTALL_DIR}/tls.crt"
    [[ -f "${LEGACY_BACKUP_DIR}/tls.key" ]] && install -m 0600 "${LEGACY_BACKUP_DIR}/tls.key" "${INSTALL_DIR}/tls.key"
  fi
}

mkdir -p "$INSTALL_DIR/data/postgres" "$INSTALL_DIR/registration-sessions" "$INSTALL_DIR/nginx/conf.d" "$INSTALL_DIR/backups"
if is_legacy_install; then
  prepare_legacy_migration
  trap 'rollback_legacy_migration' ERR
fi
install -m 0644 "$COMPOSE_TEMPLATE" "$INSTALL_DIR/docker-compose.yaml"
install -m 0644 "$EDGE_CONFIG" "$INSTALL_DIR/nginx/conf.d/default.conf"
install -m 0644 "$UPSTREAM_CONFIG" "$INSTALL_DIR/nginx/conf.d/upstream.conf"
install -m 0755 "$DEPLOY_SCRIPT" "$INSTALL_DIR/deploy-blue-green.sh"
install -m 0644 "$REGISTRY_HELPER" "$INSTALL_DIR/registry-config.sh"
install -m 0755 "$START_SCRIPT" "$INSTALL_DIR/start-platform.sh"
install -m 0755 "$STOP_SCRIPT" "$INSTALL_DIR/stop-platform.sh"
install -m 0755 "$RESTART_SCRIPT" "$INSTALL_DIR/restart-platform.sh"
install -m 0644 "$SERVICE_TEMPLATE" "$INSTALL_DIR/hypercdr.service.template"
# Keep the package manifest in the installation directory. The API uses this
# immutable, environment-local file as the cluster component source.
if [[ -s "${SCRIPT_DIR}/release-manifest.json" ]]; then
  mkdir -p "${INSTALL_DIR}/releases/${VERSION}"
  install -m 0644 "${SCRIPT_DIR}/release-manifest.json" "${INSTALL_DIR}/releases/${VERSION}/release-manifest.json"
  install -m 0644 "${SCRIPT_DIR}/release-manifest.json" "${INSTALL_DIR}/current-release.json.tmp"
  mv "${INSTALL_DIR}/current-release.json.tmp" "${INSTALL_DIR}/current-release.json"
fi
read_env() {
  local key="$1" file="$INSTALL_DIR/.env"
  [[ -f "$file" ]] || return 0
  sed -n "s/^${key}=//p" "$file" | head -1
}
POSTGRES_PASSWORD="$(read_env HCDR_POSTGRES_PASSWORD)"; [[ -n "$POSTGRES_PASSWORD" ]] || POSTGRES_PASSWORD="$(openssl rand -hex 24)"
SECRET_KEY="$(read_env HCDR_SECRET_KEY)"; [[ -n "$SECRET_KEY" ]] || SECRET_KEY="$(openssl rand -hex 32)"
RELEASE_TOKEN="$(read_env HCDR_RELEASE_TOKEN)"; [[ -n "$RELEASE_TOKEN" ]] || RELEASE_TOKEN="$(openssl rand -hex 32)"
EXECUTOR_TOKEN="$(read_env HCDR_REGISTRATION_EXECUTOR_TOKEN)"; [[ -n "$EXECUTOR_TOKEN" ]] || EXECUTOR_TOKEN="$(openssl rand -hex 32)"
AUTH_MODE="$(read_env HCDR_AUTH_CHALLENGE_MODE)"; AUTH_MODE="${AUTH_MODE:-turnstile}"
TURNSTILE_SITE_KEY="$(read_env HCDR_TURNSTILE_SITE_KEY)"
TURNSTILE_SECRET_KEY="$(read_env HCDR_TURNSTILE_SECRET_KEY)"
PROXY_NETWORK="${HCDR_PROXY_NETWORK:-$(read_env HCDR_PROXY_NETWORK)}"
PROXY_NETWORK="${PROXY_NETWORK:-nginx-proxy-manager_default}"
NPM_UPSTREAM_READY="${HCDR_NPM_UPSTREAM_READY:-$(read_env HCDR_NPM_UPSTREAM_READY)}"
NPM_UPSTREAM_READY="${NPM_UPSTREAM_READY:-false}"

cat >"${INSTALL_DIR}/.env.tmp" <<EOF
HCDR_DOMAIN=${DOMAIN}
HCDR_BASE_URL=${BASE_URL}
HCDR_PUBLIC_BASE_URL=${BASE_URL}
HCDR_AGENT_WS_ENDPOINT=${BASE_URL/https:/wss:}/ws/agent
HCDR_IMAGE_REGISTRY=${REGISTRY%/}
HCDR_IMAGE_TAG=${VERSION}
RELEASE_VERSION=${VERSION}
PLATFORM_API_BLUE_IMAGE=$(image_ref "${REGISTRY}" "platform-api" "${VERSION}")
PLATFORM_FRONTEND_BLUE_IMAGE=$(image_ref "${REGISTRY}" "platform-frontend" "${VERSION}")
PLATFORM_API_GREEN_IMAGE=$(image_ref "${REGISTRY}" "platform-api" "${VERSION}")
PLATFORM_FRONTEND_GREEN_IMAGE=$(image_ref "${REGISTRY}" "platform-frontend" "${VERSION}")
REGISTRATION_EXECUTOR_IMAGE=$(image_ref "${REGISTRY}" "cluster-registration-executor" "${VERSION}")
POSTGRES_IMAGE=$(image_ref "${REGISTRY}" "postgres" "16")
HCDR_POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
HCDR_DATABASE_URL=postgres://hypercdr:${POSTGRES_PASSWORD}@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_INSTALL_DIR=${INSTALL_DIR}
HCDR_PROXY_NETWORK=${PROXY_NETWORK}
HCDR_NPM_UPSTREAM_READY=${NPM_UPSTREAM_READY}
HCDR_NGINX_CONFIG_DIR=${INSTALL_DIR}/nginx/conf.d
HCDR_HTTPS_PORT=12443
HCDR_TLS_CERT_FILE=${INSTALL_DIR}/tls.crt
HCDR_TLS_KEY_FILE=${INSTALL_DIR}/tls.key
HCDR_SECRET_KEY=${SECRET_KEY}
HCDR_RELEASE_TOKEN=${RELEASE_TOKEN}
HCDR_RELEASE_MANIFEST_PATH=${INSTALL_DIR}/current-release.json
HCDR_REGISTRATION_EXECUTOR_TOKEN=${EXECUTOR_TOKEN}
HCDR_DEPLOY_MODE=docker-compose
HCDR_TLS_ENABLED=false
# Supported values: image (built-in image CAPTCHA) or turnstile (Cloudflare Turnstile).
HCDR_AUTH_CHALLENGE_MODE=${AUTH_MODE}
HCDR_TURNSTILE_SITE_KEY=${TURNSTILE_SITE_KEY}
HCDR_TURNSTILE_SECRET_KEY=${TURNSTILE_SECRET_KEY}
EOF
chmod 600 "${INSTALL_DIR}/.env.tmp"
mv "${INSTALL_DIR}/.env.tmp" "${INSTALL_DIR}/.env"

if [[ ! -f "${INSTALL_DIR}/.active_color" ]]; then printf 'blue\n' >"${INSTALL_DIR}/.active_color"; fi
if [[ "$EXECUTE" == true ]]; then
  if ! HCDR_INSTALL_DIR="$INSTALL_DIR" "$INSTALL_DIR/deploy-blue-green.sh" "$VERSION"; then
    rollback_legacy_migration
    exit 1
  fi
  trap - ERR
  if [[ "$LEGACY_MIGRATION" == true ]]; then
    printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${INSTALL_DIR}/.migration_complete"
    chmod 600 "${INSTALL_DIR}/.migration_complete"
  fi
  if command -v systemctl >/dev/null 2>&1; then
    sed "s|__INSTALL_DIR__|${INSTALL_DIR}|g" "$INSTALL_DIR/hypercdr.service.template" > /etc/systemd/system/hypercdr.service
    systemctl daemon-reload
    systemctl enable docker.service hypercdr.service >/dev/null
  fi
fi

echo "Blue/green runtime prepared at ${INSTALL_DIR}"
