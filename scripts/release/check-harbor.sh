#!/usr/bin/env bash
set -euo pipefail

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
IMAGE_TAG="${HCDR_IMAGE_TAG:-v20260714.5}"

usage() {
  cat <<'USAGE'
Check Harbor availability for HyperCDR deployment.

Usage:
  ./check-harbor.sh --registry HOST:PORT/hypercdr

Options:
  --registry PREFIX   Harbor project prefix, for example 192.168.7.128/hypercdr.
  --image-tag TAG     Platform image tag to verify, default v20260714.5.
  -h, --help          Show help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --image-tag) IMAGE_TAG="${2:?missing value for --image-tag}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${REGISTRY}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "missing required command: curl" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "missing required command: docker" >&2
  exit 1
fi

REGISTRY="${REGISTRY#http://}"
REGISTRY="${REGISTRY#https://}"
REGISTRY_HOST="${REGISTRY%%/*}"

echo "Check Harbor availability"
echo "Registry host: ${REGISTRY_HOST}"

echo
echo "[1/2] Harbor API"
for attempt in $(seq 1 30); do
  if curl -k -fsSL "https://${REGISTRY_HOST}/api/v2.0/systeminfo" >/dev/null 2>&1; then
    echo "[OK] Harbor API is reachable."
    break
  fi
  if [[ "${attempt}" == "30" ]]; then
    echo "[FAILED] Harbor API is not reachable: https://${REGISTRY_HOST}" >&2
    exit 1
  fi
  echo "Harbor API is not ready; retrying in 2s (${attempt}/30)..."
  sleep 2
done

echo
echo "[2/2] Docker image pull"
for image in \
  "${REGISTRY}/platform-api:${IMAGE_TAG}" \
  "${REGISTRY}/platform-frontend:${IMAGE_TAG}" \
  "${REGISTRY}/comm-agent:${IMAGE_TAG}" \
  "${REGISTRY}/postgres:16"; do
  if docker pull "${image}" >/dev/null; then
    echo "[OK] Docker can pull ${image}."
  else
    echo "[FAILED] Docker cannot pull ${image}." >&2
    echo "Run prepare-docker-registry.sh first, then check Harbor again." >&2
    exit 1
  fi
done

echo
echo "SUCCESS: Harbor is ready for HyperCDR deployment."
