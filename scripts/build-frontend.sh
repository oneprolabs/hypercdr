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
cp -a "${ROOT_DIR}/frontend/." "${WORK_DIR}/"
cd "${WORK_DIR}"
npm ci --cache="${NPM_CACHE}"
npm run test:extensions
npm run test:topology
HCDR_FRONTEND_OUT_DIR="${OUT_DIR}" npm run build
printf '%s\n' "${OUT_DIR}"
