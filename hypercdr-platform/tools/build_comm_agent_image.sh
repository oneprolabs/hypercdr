#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
TAG="${HCDR_AGENT_IMAGE_TAG:-dev}"
if [[ -z "${HCDR_AGENT_IMAGE:-}" && -z "${REGISTRY}" ]]; then
  echo "error: registry is required, set HCDR_IMAGE_REGISTRY or HCDR_AGENT_IMAGE" >&2
  exit 1
fi
IMAGE="${HCDR_AGENT_IMAGE:-${REGISTRY%/}/comm-agent:${TAG}}"

docker build \
  -f "${ROOT_DIR}/deployments/docker/comm-agent.Dockerfile" \
  -t "${IMAGE}" \
  "${ROOT_DIR}"

if [[ "${HCDR_PUSH_IMAGE:-false}" == "true" ]]; then
  docker push "${IMAGE}"
fi

echo "${IMAGE}"
