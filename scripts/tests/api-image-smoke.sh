#!/usr/bin/env bash
# Exercise the actual built API image with an isolated real PostgreSQL database.
set -euo pipefail
image="${1:?provide the locally built platform API image}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-$(dirname "${ROOT_DIR}")/hypercdr-runtime}"
mkdir -p "${RUNTIME_ROOT}/validation"
runtime="$(mktemp -d "${RUNTIME_ROOT}/validation/api-image-smoke-XXXXXX")"
prefix="hypercdr-image-smoke-$$-${RANDOM}"
cleanup() {
  docker rm -f "${prefix}-api" "${prefix}-postgres" >/dev/null 2>&1 || true
  docker network rm "${prefix}" >/dev/null 2>&1 || true
  rm -rf "${runtime}"
}
trap cleanup EXIT
# Synthetic catalog is only an isolated smoke fixture; no cluster receives it.
python3 - "${runtime}/release-manifest.json" <<'PY'
import json,sys
names=['comm-agent','oadp-comm-agent','oadp-catalog','oadp-bundle','oadp-operator',
       'oadp-velero','oadp-openshift-plugin','oadp-aws-plugin','oadp-restore-helper',
       'velero','velero-plugin-for-aws','velero-plugin-for-microsoft-azure','velero-plugin-for-gcp']
json.dump({'version':'smoke','componentManifest': {name: {'version':'smoke',
    'image':'registry.example.invalid/hypercdr:'+name+'-smoke',
    'imageDigest':'sha256:'+'0'*64} for name in names}},open(sys.argv[1],'w'))
PY
docker network create "${prefix}" >/dev/null
docker run -d --name "${prefix}-postgres" --network "${prefix}" --network-alias postgres \
  -e POSTGRES_USER=hypercdr -e POSTGRES_PASSWORD=smoke -e POSTGRES_DB=hypercdr \
  --tmpfs /var/lib/postgresql/data:rw,size=1024m \
  "${HCDR_TEST_POSTGRES_IMAGE:-postgres:16-alpine}" >/dev/null
ready=false
for attempt in {1..60}; do
  if docker exec "${prefix}-postgres" pg_isready -U hypercdr -d hypercdr >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "${ready}" == true ]] || { echo 'Smoke PostgreSQL did not become ready' >&2; exit 1; }
docker run -d --name "${prefix}-api" --network "${prefix}" -p 127.0.0.1::18080 \
  -e HCDR_HTTP_ADDR=0.0.0.0:18080 \
  -e HCDR_DATABASE_URL='postgres://hypercdr:smoke@postgres:5432/hypercdr?sslmode=disable' \
  -e HCDR_SECRET_KEY=smoke-secret -e HCDR_AUTH_CHALLENGE_MODE=captcha \
  -e HCDR_PUBLIC_BASE_URL=http://localhost:18080 \
  -e HCDR_RELEASE_MANIFEST_PATH=/releases/release-manifest.json \
  -v "${runtime}:/releases:ro" --entrypoint /bin/sh "${image}" \
  -c '/usr/local/bin/platform-migrate && exec /usr/local/bin/platform-api' >/dev/null
port="$(docker port "${prefix}-api" 18080/tcp | sed -n 's/^127\.0\.0\.1://p')"
[[ "${port}" =~ ^[0-9]+$ ]]
base="http://127.0.0.1:${port}"
curl -fsS --retry 30 --retry-all-errors --retry-delay 1 --max-time 3 "${base}/healthz" >/dev/null
curl -fsS --max-time 10 "${base}/api/v1/auth/captcha" >"${runtime}/captcha.json"
python3 - "${runtime}/captcha.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1]));assert x.get('id') and x.get('image')
PY
curl -fsS --max-time 10 "${base}/install.sh" >"${runtime}/install.sh"
bash -n "${runtime}/install.sh"
grep -Fq 'kubeconfig' "${runtime}/install.sh"
! grep -Eq '\{\{[A-Z_]+\}\}' "${runtime}/install.sh"
echo 'Built API image: migrations, health, captcha and assembled installer passed'
