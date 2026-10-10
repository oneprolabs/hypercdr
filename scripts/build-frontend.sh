#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_ROOT="${HCDR_RUNTIME_ROOT:-$(cd "${ROOT_DIR}/.." && pwd)/hypercdr-runtime}"
WORK_DIR="${HCDR_COMMUNITY_FRONTEND_WORK_DIR:-${RUNTIME_ROOT}/build/community/frontend-workspace}"
OUT_DIR="${HCDR_FRONTEND_OUT_DIR:-${RUNTIME_ROOT}/build/community/frontend}"
NPM_CACHE="${HCDR_NPM_CACHE:-${RUNTIME_ROOT}/cache/npm}"

case "${WORK_DIR}" in
  ""|/|"${ROOT_DIR}") echo "unsafe frontend work directory: ${WORK_DIR}" >&2; exit 1;;
esac

rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}" "${OUT_DIR}" "${NPM_CACHE}"
python3 "${ROOT_DIR}/scripts/dev/provenance.py" capture --source "${ROOT_DIR}" --component frontend --output "${WORK_DIR}/.source-build.json"
cp -a "${ROOT_DIR}/frontend/." "${WORK_DIR}/"
cd "${WORK_DIR}"
npm ci --cache="${NPM_CACHE}"
npm run lint
npm test
HCDR_FRONTEND_OUT_DIR="${OUT_DIR}" npm run build
python3 "${ROOT_DIR}/scripts/dev/provenance.py" record --capture "${WORK_DIR}/.source-build.json" --artifact "${OUT_DIR}" --output "${OUT_DIR}.provenance.json"
printf '%s\n' "${OUT_DIR}"
