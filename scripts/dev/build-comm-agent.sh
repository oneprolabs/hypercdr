#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_BIN="${HCDR_GO_BIN:-$(command -v go)}"
OUTPUT_DIR="${HCDR_AGENT_BUILD_DIR:-${HCDR_RUNTIME_ROOT:-${ROOT_DIR}/../hypercdr-runtime}/validation/message-structure-agent}"
OUTPUT_FILE="${OUTPUT_DIR}/comm-agent"

[[ -x "${GO_BIN}" ]] || {
  echo "Go binary is not executable: ${GO_BIN}" >&2
  exit 1
}

mkdir -p "${OUTPUT_DIR}"
go_version="$(GOTOOLCHAIN=auto "${GO_BIN}" version)"
if [[ ! "${go_version}" =~ 'go1.25' ]]; then
  echo "Go 1.25.13 or compatible Go 1.25 toolchain is required: ${go_version}" >&2
  exit 1
fi

(
  cd "${ROOT_DIR}/agent/comm-agent"
  GOTOOLCHAIN=auto "${GO_BIN}" test ./pkg/protocol ./internal/wsclient
  GOTOOLCHAIN=auto "${GO_BIN}" build -trimpath -o "${OUTPUT_FILE}" ./cmd/comm-agent
)

sha256sum "${OUTPUT_FILE}" | tee "${OUTPUT_FILE}.sha256"
printf 'Built %s\n' "${OUTPUT_FILE}"
