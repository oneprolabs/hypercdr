#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEST_ROOT}"' EXIT
REAL_DOCKER="$(type -P docker || true)"
# Stage a package without writing fixtures into the source tree or host units.
cp -a "${SCRIPT_DIR}/../scripts/release" "${TEST_ROOT}/package"
mkdir -p "${TEST_ROOT}/package/scripts/lib"
cp "${SCRIPT_DIR}/../scripts/lib/registry-config.sh" "${TEST_ROOT}/package/scripts/lib/registry-config.sh"
cp "${SCRIPT_DIR}/../scripts/lib/registry-config.sh" "${TEST_ROOT}/package/registry-config.sh"
cp "${SCRIPT_DIR}/../docker-compose.yml" "${TEST_ROOT}/package/compose.yaml"
mkdir -p "${TEST_ROOT}/package/nginx"
cp "${SCRIPT_DIR}/../docker/nginx/edge.conf" "${TEST_ROOT}/package/nginx/edge.conf"
cp "${SCRIPT_DIR}/../docker/nginx/upstream.conf.default" "${TEST_ROOT}/package/nginx/upstream.conf.default"
SCRIPT_DIR="${TEST_ROOT}/package"
export HCDR_SYSTEMD_UNIT_DIR="${TEST_ROOT}/systemd"
export HCDR_NPM_UPSTREAM_READY=true
file_mode() {
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"
}
MANIFEST_FIXTURE="${SCRIPT_DIR}/release-manifest.json"
printf '{"version":"1.0.23.20260714","componentManifest":{}}\n' >"${MANIFEST_FIXTURE}"

mkdir -p "${TEST_ROOT}/bin"
TRUE_BIN="$(type -P true)"
cp "${TRUE_BIN}" "${TEST_ROOT}/bin/ss"
cp "${TRUE_BIN}" "${TEST_ROOT}/bin/systemctl"
cat > "${TEST_ROOT}/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  manifest) exit 0 ;;
  inspect)
    if [[ "$*" == *State.Running* ]]; then printf 'false\n'; else printf 'healthy\n'; fi
    exit 0 ;;
  ps) exit 0 ;;
  compose) exit 0 ;;
  *) exit 0 ;;
esac
EOF
chmod +x "${TEST_ROOT}/bin/docker"
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
grep -qx 'RELEASE_VERSION=1.0.23.20260714' "${PRIVATE_DATA}/.env"
grep -qx 'PLATFORM_API_BLUE_IMAGE=registry.example.test/hypercdr/platform-api:1.0.23.20260714' "${PRIVATE_DATA}/.env"
grep -qx 'PLATFORM_FRONTEND_GREEN_IMAGE=registry.example.test/hypercdr/platform-frontend:1.0.23.20260714' "${PRIVATE_DATA}/.env"
grep -qx 'REGISTRATION_EXECUTOR_IMAGE=registry.example.test/hypercdr/cluster-registration-executor:1.0.23.20260714' "${PRIVATE_DATA}/.env"
test "$(file_mode "${PRIVATE_DATA}/.env")" = "600"
test "$(file_mode "${PRIVATE_DATA}/release-token")" = "600"
grep -q '^HCDR_RELEASE_TOKEN=' "${PRIVATE_DATA}/.env"
test -s "${PRIVATE_DATA}/release-token"
test "$(file_mode "${PRIVATE_DATA}/registration-executor-token")" = "600"
grep -q '^HCDR_REGISTRATION_EXECUTOR_TOKEN=' "${PRIVATE_DATA}/.env"
test -s "${PRIVATE_DATA}/registration-executor-token"
grep -q '^  hypercdr-cluster-registration-executor:' "${PRIVATE_DATA}/docker-compose.yaml"
test -x "${PRIVATE_DATA}/deploy-blue-green.sh"
test -f "${PRIVATE_DATA}/nginx/conf.d/default.conf"
grep -qx 'blue' "${PRIVATE_DATA}/.active_color"
grep -qx 'HCDR_NPM_UPSTREAM_READY=true' "${PRIVATE_DATA}/.env"
! grep -q 'HCDR_POSTGRES_PORT' "${PRIVATE_DATA}/docker-compose.yaml"
"${REAL_DOCKER:-docker}" compose --project-directory "${PRIVATE_DATA}" --env-file "${PRIVATE_DATA}/.env" -f "${PRIVATE_DATA}/docker-compose.yaml" config --quiet

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
test "$(file_mode "${PUBLIC_DATA}/.env")" = "600"

echo "registry CA bootstrap flow: ok"
