#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEST_ROOT}"' EXIT

mkdir -p "${TEST_ROOT}/bin"
cp /bin/true "${TEST_ROOT}/bin/docker"
cat > "${TEST_ROOT}/bin/curl" <<'EOF'
#!/usr/bin/env bash
printf '200'
EOF
chmod +x "${TEST_ROOT}/bin/curl" "${TEST_ROOT}/bin/docker"

PRIVATE_DATA="${TEST_ROOT}/private"
PRIVATE_CA="${TEST_ROOT}/customer-registry-ca.crt"
cp /data/harbor/cert/hypercdr-ca.crt "${PRIVATE_CA}"
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --public-base-url https://platform.example.test:3002 \
  --data-dir "${PRIVATE_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust private-ca \
  --registry-ca-file "${PRIVATE_CA}" \
  --execute >/dev/null

cmp "${PRIVATE_CA}" "${PRIVATE_DATA}/certs/registry-ca.crt"
grep -qx 'HCDR_REGISTRY_CA_PATH=/etc/hypercdr/registry/ca.crt' "${PRIVATE_DATA}/.env"
grep -qx "HCDR_REGISTRY_CA_FILE=${PRIVATE_DATA}/certs/registry-ca.crt" "${PRIVATE_DATA}/.env"
grep -q '^HCDR_POSTGRES_PASSWORD=' "${PRIVATE_DATA}/.env"
! grep -qx 'HCDR_POSTGRES_PASSWORD=hypercdr' "${PRIVATE_DATA}/.env"
PRIVATE_DB_PASSWORD="$(sed -n 's/^HCDR_POSTGRES_PASSWORD=//p' "${PRIVATE_DATA}/.env")"
grep -qx 'RELEASE_VERSION=v20260714.5' "${PRIVATE_DATA}/.env"
grep -qx 'PLATFORM_UPGRADER_IMAGE=registry.example.test/hypercdr/platform-upgrader:v20260714.5' "${PRIVATE_DATA}/.env"
test "$(stat -c '%a' "${PRIVATE_DATA}/.env")" = "600"
test "$(stat -c '%a' "${PRIVATE_DATA}/release-token")" = "600"
grep -q '^HCDR_RELEASE_TOKEN=' "${PRIVATE_DATA}/.env"
test -s "${PRIVATE_DATA}/release-token"
grep -q '^  hypercdr-platform-upgrader:' "${PRIVATE_DATA}/docker-compose.yaml"
! grep -q 'HCDR_POSTGRES_PORT' "${PRIVATE_DATA}/docker-compose.yaml"
grep -q '/var/run/docker.sock:/var/run/docker.sock' "${PRIVATE_DATA}/docker-compose.yaml"
/usr/bin/docker compose --project-directory "${PRIVATE_DATA}" --env-file "${PRIVATE_DATA}/.env" -f "${PRIVATE_DATA}/docker-compose.yaml" config --quiet

# Re-running the installer must retain the initialized database password.
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --public-base-url https://platform.example.test:3002 \
  --data-dir "${PRIVATE_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust private-ca \
  --registry-ca-file "${PRIVATE_CA}" \
  --execute >/dev/null
grep -qx "HCDR_POSTGRES_PASSWORD=${PRIVATE_DB_PASSWORD}" "${PRIVATE_DATA}/.env"

PUBLIC_DATA="${TEST_ROOT}/public"
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --public-base-url https://platform.example.test:3002 \
  --data-dir "${PUBLIC_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust system \
  --execute >/dev/null

test ! -e "${PUBLIC_DATA}/certs/registry-ca.crt"
grep -qx 'HCDR_REGISTRY_CA_PATH=' "${PUBLIC_DATA}/.env"
grep -qx 'HCDR_REGISTRY_CA_FILE=/dev/null' "${PUBLIC_DATA}/.env"
test "$(stat -c '%a' "${PUBLIC_DATA}/.env")" = "600"

echo "registry CA bootstrap flow: ok"
