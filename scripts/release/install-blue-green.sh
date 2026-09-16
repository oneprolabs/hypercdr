#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
VERSION=""
BASE_URL=""
REGISTRY=""
INSTALL_DIR="/var/lib/hypercdr"
DOMAIN="hypercdr.com"
TLS_CERT_FILE=""
TLS_KEY_FILE=""
EXECUTE=false

usage() {
  cat <<'USAGE'
Usage: install-blue-green.sh VERSION --base-url https://hypercdr.com --registry REGISTRY \
  [--tls-cert-file PATH --tls-key-file PATH] [--install-dir PATH] [--execute]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url) BASE_URL="${2:?missing value for --base-url}"; shift 2 ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; shift 2 ;;
    --domain) DOMAIN="${2:?missing value for --domain}"; shift 2 ;;
    --tls-cert-file) TLS_CERT_FILE="${2:?missing value for --tls-cert-file}"; shift 2 ;;
    --tls-key-file) TLS_KEY_FILE="${2:?missing value for --tls-key-file}"; shift 2 ;;
    --execute) EXECUTE=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) [[ -z "$VERSION" ]] || { echo "unknown argument: $1" >&2; exit 2; }; VERSION="${1#v}"; shift ;;
  esac
done

[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]] || { echo "invalid version: $VERSION" >&2; exit 2; }
[[ "$BASE_URL" == https://* ]] || { echo "--base-url must use https://" >&2; exit 2; }
[[ -n "$REGISTRY" ]] || { echo "--registry is required" >&2; exit 2; }
[[ "$INSTALL_DIR" == /* && "$INSTALL_DIR" != / ]] || { echo "--install-dir must be an absolute non-root path" >&2; exit 2; }
if [[ -n "$TLS_CERT_FILE" || -n "$TLS_KEY_FILE" ]]; then
  [[ -n "$TLS_CERT_FILE" && -n "$TLS_KEY_FILE" ]] || { echo "TLS certificate and key must be configured together" >&2; exit 2; }
  [[ -r "$TLS_CERT_FILE" && -r "$TLS_KEY_FILE" ]] || { echo "TLS certificate and key must be readable" >&2; exit 1; }
fi

if [[ "$EXECUTE" != true ]]; then
  printf 'Dry run: version=%s registry=%s install=%s domain=%s\n' "$VERSION" "$REGISTRY" "$INSTALL_DIR" "$DOMAIN"
  exit 0
fi

for command_name in docker openssl install; do
  command -v "$command_name" >/dev/null 2>&1 || { echo "missing required command: $command_name" >&2; exit 1; }
done
docker compose version >/dev/null 2>&1 || { echo "Docker Compose V2 is required" >&2; exit 1; }

mkdir -p "$INSTALL_DIR/data/postgres" "$INSTALL_DIR/registration-sessions" "$INSTALL_DIR/nginx/conf.d" "$INSTALL_DIR/nginx/acme" "$INSTALL_DIR/backups"
install -m 0644 "$ROOT_DIR/docker-compose.yml" "$INSTALL_DIR/docker-compose.yaml"
install -m 0644 "$ROOT_DIR/docker/nginx/edge.conf" "$INSTALL_DIR/nginx/conf.d/default.conf"
install -m 0644 "$ROOT_DIR/docker/nginx/upstream.conf.default" "$INSTALL_DIR/nginx/conf.d/upstream.conf"
install -m 0755 "$ROOT_DIR/scripts/release/deploy-blue-green.sh" "$INSTALL_DIR/deploy-blue-green.sh"
install -m 0755 "$ROOT_DIR/scripts/release/start-platform.sh" "$INSTALL_DIR/start-platform.sh"
install -m 0755 "$ROOT_DIR/scripts/release/stop-platform.sh" "$INSTALL_DIR/stop-platform.sh"
install -m 0755 "$ROOT_DIR/scripts/release/restart-platform.sh" "$INSTALL_DIR/restart-platform.sh"
install -m 0644 "$ROOT_DIR/scripts/release/templates/hypercdr.service" "$INSTALL_DIR/hypercdr.service.template"
if [[ -n "$TLS_CERT_FILE" ]]; then
  install -m 0644 "$TLS_CERT_FILE" "$INSTALL_DIR/tls.crt"
  install -m 0600 "$TLS_KEY_FILE" "$INSTALL_DIR/tls.key"
else
  openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout "$INSTALL_DIR/tls.key" -out "$INSTALL_DIR/tls.crt" \
    -subj "/CN=${DOMAIN}" \
    -addext "subjectAltName=DNS:${DOMAIN},DNS:*.${DOMAIN}" >/dev/null 2>&1
  chmod 0644 "$INSTALL_DIR/tls.crt"
  chmod 0600 "$INSTALL_DIR/tls.key"
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
AUTH_MODE="$(read_env HCDR_AUTH_CHALLENGE_MODE)"; AUTH_MODE="${AUTH_MODE:-image}"
TURNSTILE_SITE_KEY="$(read_env HCDR_TURNSTILE_SITE_KEY)"
TURNSTILE_SECRET_KEY="$(read_env HCDR_TURNSTILE_SECRET_KEY)"

cat >"${INSTALL_DIR}/.env.tmp" <<EOF
HCDR_DOMAIN=${DOMAIN}
HCDR_BASE_URL=${BASE_URL}
HCDR_AGENT_WS_ENDPOINT=${BASE_URL/https:/wss:}/ws/agent
HCDR_IMAGE_REGISTRY=${REGISTRY%/}
HCDR_IMAGE_TAG=${VERSION}
RELEASE_VERSION=${VERSION}
PLATFORM_API_BLUE_IMAGE=${REGISTRY%/}/platform-api:${VERSION}
PLATFORM_FRONTEND_BLUE_IMAGE=${REGISTRY%/}/platform-frontend:${VERSION}
PLATFORM_API_GREEN_IMAGE=${REGISTRY%/}/platform-api:${VERSION}
PLATFORM_FRONTEND_GREEN_IMAGE=${REGISTRY%/}/platform-frontend:${VERSION}
REGISTRATION_EXECUTOR_IMAGE=${REGISTRY%/}/cluster-registration-executor:${VERSION}
POSTGRES_IMAGE=${REGISTRY%/}/postgres:16
HCDR_POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
HCDR_DATABASE_URL=postgres://hypercdr:${POSTGRES_PASSWORD}@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_INSTALL_DIR=${INSTALL_DIR}
HCDR_NGINX_CONFIG_DIR=${INSTALL_DIR}/nginx/conf.d
HCDR_ACME_WEBROOT=${INSTALL_DIR}/nginx/acme
HCDR_TLS_CERT_FILE=${INSTALL_DIR}/tls.crt
HCDR_TLS_KEY_FILE=${INSTALL_DIR}/tls.key
HCDR_SECRET_KEY=${SECRET_KEY}
HCDR_RELEASE_TOKEN=${RELEASE_TOKEN}
HCDR_REGISTRATION_EXECUTOR_TOKEN=${EXECUTOR_TOKEN}
HCDR_DEPLOY_MODE=docker-compose
HCDR_TLS_ENABLED=false
HCDR_AUTH_CHALLENGE_MODE=${AUTH_MODE}
HCDR_TURNSTILE_SITE_KEY=${TURNSTILE_SITE_KEY}
HCDR_TURNSTILE_SECRET_KEY=${TURNSTILE_SECRET_KEY}
EOF
chmod 600 "${INSTALL_DIR}/.env.tmp"
mv "${INSTALL_DIR}/.env.tmp" "${INSTALL_DIR}/.env"

if [[ ! -f "${INSTALL_DIR}/.active_color" ]]; then printf 'blue\n' >"${INSTALL_DIR}/.active_color"; fi
if [[ "$EXECUTE" == true ]]; then
  HCDR_INSTALL_DIR="$INSTALL_DIR" "$INSTALL_DIR/deploy-blue-green.sh" "$VERSION"
  if command -v systemctl >/dev/null 2>&1; then
    sed "s|__INSTALL_DIR__|${INSTALL_DIR}|g" "$INSTALL_DIR/hypercdr.service.template" > /etc/systemd/system/hypercdr.service
    systemctl daemon-reload
    systemctl enable docker.service hypercdr.service >/dev/null
  fi
fi

echo "Blue/green runtime prepared at ${INSTALL_DIR}"
