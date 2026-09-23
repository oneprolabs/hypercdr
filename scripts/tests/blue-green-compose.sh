#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_DIR="$(mktemp -d)"
trap 'rm -rf "${RUNTIME_DIR}"' EXIT
ENV_FILE="${RUNTIME_DIR}/.env"
cat >"${ENV_FILE}" <<'EOF'
POSTGRES_IMAGE=postgres:16
HCDR_POSTGRES_PASSWORD=test-password
HCDR_DATABASE_URL=postgres://hypercdr:test-password@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_IMAGE_REGISTRY=registry.example/hypercdr
HCDR_RELEASE_TOKEN=test-release-token
HCDR_REGISTRATION_EXECUTOR_TOKEN=test-registration-token
HCDR_DOMAIN=hypercdr.com
HCDR_PROXY_NETWORK=nginx-proxy-manager_default
HCDR_TLS_DIR=/tmp/tls
PLATFORM_API_BLUE_IMAGE=registry.example/hypercdr/platform-api:test
PLATFORM_FRONTEND_BLUE_IMAGE=registry.example/hypercdr/platform-frontend:test
PLATFORM_API_GREEN_IMAGE=registry.example/hypercdr/platform-api:test
PLATFORM_FRONTEND_GREEN_IMAGE=registry.example/hypercdr/platform-frontend:test
REGISTRATION_EXECUTOR_IMAGE=registry.example/hypercdr/cluster-registration-executor:test
EOF

rendered="${RUNTIME_DIR}/compose.yaml"
docker compose --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.yml" \
  --profile blue --profile green config >"${rendered}"

for service in \
  hypercdr-edge hypercdr-postgres \
  hypercdr-platform-api-blue hypercdr-platform-frontend-blue \
  hypercdr-platform-api-green hypercdr-platform-frontend-green \
  hypercdr-cluster-registration-executor; do
  grep -Fq "  ${service}:" "${rendered}"
done

! grep -Fq 'hypercdr-platform-upgrader:' "${rendered}"
grep -Fq 'profiles:' "${rendered}"
! grep -Fq 'published:' "${rendered}"
grep -Fq 'hypercdr-edge:' "${rendered}"
grep -Fq 'image: registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:nginx-1.27-alpine' "${rendered}"
grep -Fq 'networks: [hypercdr-proxy, hypercdr-blue, hypercdr-green]' "${ROOT_DIR}/docker-compose.yml"
! grep -Eq 'published: "(18080|3002|5432)"' "${rendered}"
grep -Fq 'hypercdr-platform-api-blue:18080' "${ROOT_DIR}/docker/nginx/upstream.conf.default"
grep -Fq 'resolver 127.0.0.11 valid=10s' "${ROOT_DIR}/docker/nginx/edge.conf"
grep -Fq 'proxy_pass http://$hypercdr_api_active' "${ROOT_DIR}/docker/nginx/edge.conf"
grep -Fq 'proxy_pass http://$hypercdr_frontend_active' "${ROOT_DIR}/docker/nginx/edge.conf"
grep -Fq 'external: true' "${ROOT_DIR}/docker-compose.yml"
grep -Fq 'name: ${HCDR_PROXY_NETWORK:-nginx-proxy-manager_default}' "${ROOT_DIR}/docker-compose.yml"
grep -Fq 'listen 80;' "${ROOT_DIR}/docker/nginx/default.conf"
! grep -Fq 'listen 3002 ssl;' "${ROOT_DIR}/docker/nginx/default.conf"

echo "blue-green compose contract passed"
