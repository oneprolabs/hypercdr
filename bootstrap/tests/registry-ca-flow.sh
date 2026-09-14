#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEST_ROOT}"' EXIT
# Stage a package without writing fixtures into the source tree or host units.
cp -a "${SCRIPT_DIR}/../scripts/release" "${TEST_ROOT}/package"
cp "${SCRIPT_DIR}/../docker-compose.yml" "${TEST_ROOT}/package/compose.yaml"
SCRIPT_DIR="${TEST_ROOT}/package"
export HCDR_SYSTEMD_UNIT_DIR="${TEST_ROOT}/systemd"
MANIFEST_FIXTURE="${SCRIPT_DIR}/release-manifest.json"
printf '{"version":"v20260714.5","componentManifest":{}}\n' >"${MANIFEST_FIXTURE}"

mkdir -p "${TEST_ROOT}/bin"
cp /bin/true "${TEST_ROOT}/bin/docker"
cp /bin/true "${TEST_ROOT}/bin/ss"
cp /bin/true "${TEST_ROOT}/bin/systemctl"
cat > "${TEST_ROOT}/bin/curl" <<'EOF'
#!/usr/bin/env bash
printf '200'
EOF
chmod +x "${TEST_ROOT}/bin/curl" "${TEST_ROOT}/bin/docker"

PRIVATE_DATA="${TEST_ROOT}/private"
PRIVATE_CA="${TEST_ROOT}/customer-registry-ca.crt"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=registry.example.test' \
  -keyout "${TEST_ROOT}/customer-registry-ca.key" \
  -out "${PRIVATE_CA}" >/dev/null 2>&1
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --base-url https://platform.example.test:3002 \
  --public-base-url https://platform.example.test:3002 \
  --install-dir "${PRIVATE_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust private-ca \
  --registry-ca-file "${PRIVATE_CA}" \
  --confirm-prerequisites \
  --execute >/dev/null

cmp "${PRIVATE_CA}" "${PRIVATE_DATA}/certs/registry-ca.crt"
grep -qx 'HCDR_REGISTRY_CA_PATH=/etc/hypercdr/registry/ca.crt' "${PRIVATE_DATA}/.env"
grep -qx "HCDR_REGISTRY_CA_FILE=${PRIVATE_DATA}/certs/registry-ca.crt" "${PRIVATE_DATA}/.env"
grep -q '^HCDR_POSTGRES_PASSWORD=' "${PRIVATE_DATA}/.env"
! grep -qx 'HCDR_POSTGRES_PASSWORD=hypercdr' "${PRIVATE_DATA}/.env"
PRIVATE_DB_PASSWORD="$(sed -n 's/^HCDR_POSTGRES_PASSWORD=//p' "${PRIVATE_DATA}/.env")"
grep -qx 'RELEASE_VERSION=v20260714.5' "${PRIVATE_DATA}/.env"
grep -qx 'PLATFORM_UPGRADER_IMAGE=registry.example.test/hypercdr/platform-upgrader:v20260714.5' "${PRIVATE_DATA}/.env"
grep -qx 'REGISTRATION_EXECUTOR_IMAGE=registry.example.test/hypercdr/cluster-registration-executor:v20260714.5' "${PRIVATE_DATA}/.env"
test "$(stat -c '%a' "${PRIVATE_DATA}/.env")" = "600"
test "$(stat -c '%a' "${PRIVATE_DATA}/release-token")" = "600"
grep -q '^HCDR_RELEASE_TOKEN=' "${PRIVATE_DATA}/.env"
test -s "${PRIVATE_DATA}/release-token"
test "$(stat -c '%a' "${PRIVATE_DATA}/registration-executor-token")" = "600"
grep -q '^HCDR_REGISTRATION_EXECUTOR_TOKEN=' "${PRIVATE_DATA}/.env"
test -s "${PRIVATE_DATA}/registration-executor-token"
grep -q '^  hypercdr-platform-upgrader:' "${PRIVATE_DATA}/docker-compose.yaml"
grep -q '^  hypercdr-cluster-registration-executor:' "${PRIVATE_DATA}/docker-compose.yaml"
! grep -q 'HCDR_POSTGRES_PORT' "${PRIVATE_DATA}/docker-compose.yaml"
grep -q '/var/run/docker.sock:/var/run/docker.sock' "${PRIVATE_DATA}/docker-compose.yaml"
/usr/bin/docker compose --project-directory "${PRIVATE_DATA}" --env-file "${PRIVATE_DATA}/.env" -f "${PRIVATE_DATA}/docker-compose.yaml" config --quiet

# Re-running the installer must retain the initialized database password.
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --base-url https://platform.example.test:3002 \
  --public-base-url https://platform.example.test:3002 \
  --install-dir "${PRIVATE_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust private-ca \
  --registry-ca-file "${PRIVATE_CA}" \
  --confirm-prerequisites \
  --execute >/dev/null
grep -qx "HCDR_POSTGRES_PASSWORD=${PRIVATE_DB_PASSWORD}" "${PRIVATE_DATA}/.env"

PUBLIC_DATA="${TEST_ROOT}/public"
PATH="${TEST_ROOT}/bin:${PATH}" "${SCRIPT_DIR}/install-platform.sh" docker \
  --base-url https://platform.example.test:3002 \
  --public-base-url https://platform.example.test:3002 \
  --install-dir "${PUBLIC_DATA}" \
  --registry registry.example.test/hypercdr \
  --registry-trust system \
  --confirm-prerequisites \
  --execute >/dev/null

test ! -e "${PUBLIC_DATA}/certs/registry-ca.crt"
grep -qx 'HCDR_REGISTRY_CA_PATH=' "${PUBLIC_DATA}/.env"
grep -qx 'HCDR_REGISTRY_CA_FILE=/dev/null' "${PUBLIC_DATA}/.env"
test "$(stat -c '%a' "${PUBLIC_DATA}/.env")" = "600"

echo "registry CA bootstrap flow: ok"
