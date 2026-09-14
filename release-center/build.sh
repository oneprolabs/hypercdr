#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="${RELEASE_CENTER_IMAGE:-hypercdr/release-center:latest}"
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
docker build -t "${IMAGE}" "${ROOT_DIR}"
echo "Built Release Center image: ${IMAGE}"
