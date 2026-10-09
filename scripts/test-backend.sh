#!/usr/bin/env bash
# Run backend tests against PostgreSQL without touching any platform database.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ $# -eq 0 ]]; then set -- ./...; fi
if [[ -n "${HCDR_TEST_DATABASE_URL:-}" ]]; then
  cd "${ROOT_DIR}/backend"
  exec go test "$@"
fi
command -v docker >/dev/null || { echo "Docker is required for isolated PostgreSQL tests" >&2; exit 1; }
test_container="hypercdr-test-postgres-$$-${RANDOM}"
cleanup() { docker rm -f "${test_container}" >/dev/null 2>&1 || true; }
trap cleanup EXIT
image="${HCDR_TEST_POSTGRES_IMAGE:-postgres:16-alpine}"
docker run -d --name "${test_container}" --label hypercdr.purpose=tests \
  -e POSTGRES_USER=hypercdr_test -e POSTGRES_PASSWORD=hypercdr_test \
  -e POSTGRES_DB=hypercdr_test_admin -p 127.0.0.1::5432 \
  --tmpfs /var/lib/postgresql/data:rw,size=1024m "${image}" >/dev/null
ready=false
for attempt in {1..60}; do
  if docker exec "${test_container}" pg_isready -U hypercdr_test -d hypercdr_test_admin >/dev/null 2>&1; then ready=true; break; fi
  sleep 0.5
done
[[ "${ready}" == true ]] || { echo "Test PostgreSQL did not become ready" >&2; exit 1; }
port="$(docker port "${test_container}" 5432/tcp | sed -n 's/^127\.0\.0\.1://p')"
[[ "${port}" =~ ^[0-9]+$ ]] || { echo "Test PostgreSQL port unavailable" >&2; exit 1; }
export HCDR_TEST_DATABASE_URL="postgres://hypercdr_test:hypercdr_test@127.0.0.1:${port}/hypercdr_test_admin?sslmode=disable"
cd "${ROOT_DIR}/backend"
go test "$@"
