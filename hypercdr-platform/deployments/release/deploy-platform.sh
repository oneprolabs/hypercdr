#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
HOST="${HCDR_PLATFORM_HOST:-${DEFAULT_HOST}}"
DEPLOY_DIR="${HCDR_DEPLOY_DIR:-/data/hypercdr/deploy}"
VERSION=""
EXECUTE="false"
POSTGRES_IMAGE="${HCDR_POSTGRES_IMAGE:-postgres:16}"
VELERO_IMAGE="${HCDR_VELERO_IMAGE:-}"
VELERO_AWS_PLUGIN_IMAGE="${HCDR_VELERO_AWS_PLUGIN_IMAGE:-}"

usage() {
  cat <<'USAGE'
Render or apply the standard Docker Compose deployment.

Usage:
  deploy-platform.sh <version> [options]

Options:
  --registry REGISTRY     Image registry prefix. Required unless HCDR_IMAGE_REGISTRY is set.
  --host HOST             Public platform host, default 192.168.8.149.
  --deploy-dir DIR        Deploy directory, default /data/hypercdr/deploy.
  --velero-image IMAGE    Velero image to expose through install.sh.
  --execute               Run docker compose pull/up after rendering files.
  -h, --help              Show help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --host) HOST="${2:?missing value for --host}"; shift 2 ;;
    --deploy-dir) DEPLOY_DIR="${2:?missing value for --deploy-dir}"; shift 2 ;;
    --velero-image) VELERO_IMAGE="${2:?missing value for --velero-image}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      if [[ -z "${VERSION}" ]]; then VERSION="$1"; shift; else die "unknown argument: $1"; fi
      ;;
  esac
done

require_version "${VERSION}"
require_registry "${REGISTRY}"
REGISTRY="${REGISTRY%/}"
if [[ -z "${VELERO_IMAGE}" ]]; then
  VELERO_IMAGE="${REGISTRY}/velero:v1.17.1-hcdr.1-20260716"
fi
if [[ -z "${VELERO_AWS_PLUGIN_IMAGE}" ]]; then
  VELERO_AWS_PLUGIN_IMAGE="${REGISTRY}/velero-plugin-for-aws:v1.13.0"
fi
if [[ "${POSTGRES_IMAGE}" == "postgres:16" ]]; then
  POSTGRES_IMAGE="${REGISTRY}/postgres:16"
fi

mkdir -p "${DEPLOY_DIR}/certs" "${DEPLOY_DIR}/data/postgres" "${DEPLOY_DIR}/logs"

SECRET_KEY_FILE="${DEPLOY_DIR}/secret_key"
if [[ ! -s "${SECRET_KEY_FILE}" ]]; then
  openssl rand -hex 32 > "${SECRET_KEY_FILE}"
  chmod 600 "${SECRET_KEY_FILE}"
fi
SECRET_KEY="$(cat "${SECRET_KEY_FILE}")"

PLATFORM_API_IMAGE="$(image_ref "${REGISTRY}" platform-api "${VERSION}")"
PLATFORM_FRONTEND_IMAGE="$(image_ref "${REGISTRY}" platform-frontend "${VERSION}")"
COMM_AGENT_IMAGE="$(image_ref "${REGISTRY}" comm-agent "${VERSION}")"

log "Rendering ${DEPLOY_DIR}/.env"
cat >"${DEPLOY_DIR}/.env" <<EOF
RELEASE_VERSION=${VERSION}
REGISTRY=${REGISTRY}
PLATFORM_HOST=${HOST}

PLATFORM_API_IMAGE=${PLATFORM_API_IMAGE}
PLATFORM_FRONTEND_IMAGE=${PLATFORM_FRONTEND_IMAGE}
POSTGRES_IMAGE=${POSTGRES_IMAGE}

HCDR_DATABASE_URL=postgres://hypercdr:hypercdr@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_HTTP_ADDR=0.0.0.0:18080
HCDR_PUBLIC_BASE_URL=http://${HOST}:3002
HCDR_AGENT_WS_ENDPOINT=ws://${HOST}:3002/ws/agent
HCDR_IMAGE_REGISTRY=${REGISTRY}
HCDR_AGENT_IMAGE=${COMM_AGENT_IMAGE}
HCDR_VELERO_IMAGE=${VELERO_IMAGE}
HCDR_VELERO_AWS_PLUGIN_IMAGE=${VELERO_AWS_PLUGIN_IMAGE}
HCDR_REGISTRY_CA_PATH=/etc/hypercdr/registry-ca.crt
HCDR_SECRET_KEY=${SECRET_KEY}
HCDR_TLS_ENABLED=false
HCDR_LOG_LEVEL=info
EOF

log "Rendering ${DEPLOY_DIR}/docker-compose.yaml"
cat >"${DEPLOY_DIR}/docker-compose.yaml" <<'EOF'
services:
  hypercdr-postgres:
    image: ${POSTGRES_IMAGE}
    container_name: hypercdr-postgres
    environment:
      POSTGRES_DB: hypercdr
      POSTGRES_USER: hypercdr
      POSTGRES_PASSWORD: hypercdr
    ports:
      - "15432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U hypercdr -d hypercdr"]
      interval: 5s
      timeout: 3s
      retries: 20
    volumes:
      - ./data/postgres:/var/lib/postgresql/data

  hypercdr-platform-api:
    image: ${PLATFORM_API_IMAGE}
    container_name: hypercdr-platform-api
    depends_on:
      hypercdr-postgres:
        condition: service_healthy
    environment:
      HCDR_DATABASE_URL: ${HCDR_DATABASE_URL}
      HCDR_HTTP_ADDR: ${HCDR_HTTP_ADDR}
      HCDR_PUBLIC_BASE_URL: ${HCDR_PUBLIC_BASE_URL}
      HCDR_AGENT_WS_ENDPOINT: ${HCDR_AGENT_WS_ENDPOINT}
      HCDR_IMAGE_REGISTRY: ${HCDR_IMAGE_REGISTRY}
      HCDR_AGENT_IMAGE: ${HCDR_AGENT_IMAGE}
      HCDR_VELERO_IMAGE: ${HCDR_VELERO_IMAGE}
      HCDR_VELERO_AWS_PLUGIN_IMAGE: ${HCDR_VELERO_AWS_PLUGIN_IMAGE}
      HCDR_REGISTRY_CA_PATH: ${HCDR_REGISTRY_CA_PATH}
      HCDR_SECRET_KEY: ${HCDR_SECRET_KEY}
      HCDR_TLS_ENABLED: ${HCDR_TLS_ENABLED}
      HCDR_LOG_LEVEL: ${HCDR_LOG_LEVEL}
    command: ["/bin/sh", "-c", "/usr/local/bin/platform-migrate && exec /usr/local/bin/platform-api"]
    ports:
      - "18080:18080"
    volumes:
      - /data/harbor/cert/hypercdr-ca.crt:/etc/hypercdr/registry-ca.crt:ro
    restart: unless-stopped

  hypercdr-platform-frontend:
    image: ${PLATFORM_FRONTEND_IMAGE}
    container_name: hypercdr-platform-frontend
    depends_on:
      - hypercdr-platform-api
    ports:
      - "3002:3002"
    restart: unless-stopped
EOF

cat <<EOF

Rendered deployment:
  ${DEPLOY_DIR}/.env
  ${DEPLOY_DIR}/docker-compose.yaml

Images:
  ${PLATFORM_API_IMAGE}
  ${PLATFORM_FRONTEND_IMAGE}
  ${COMM_AGENT_IMAGE}
  ${VELERO_IMAGE}
  ${VELERO_AWS_PLUGIN_IMAGE}
EOF

if [[ "${EXECUTE}" == "true" ]]; then
  require_cmd docker
  log "Applying Docker Compose deployment"
  (
    cd "${DEPLOY_DIR}"
    docker compose pull
    docker compose up -d
  )
fi
