#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${RELEASE_CENTER_DATA:-/var/lib/release-center}"
TOKEN_FILE="${RELEASE_CENTER_TOKEN_FILE:-}"
IMAGE="${RELEASE_CENTER_IMAGE:-hypercdr/release-center:latest}"
[[ -n "${RELEASE_CENTER_TOKEN:-}" || -r "${TOKEN_FILE}" ]] || { echo "RELEASE_CENTER_TOKEN or RELEASE_CENTER_TOKEN_FILE is required" >&2; exit 2; }
token="${RELEASE_CENTER_TOKEN:-}"; [[ -n "$token" ]] || token="$(tr -d '\r\n' < "$TOKEN_FILE")"
mkdir -p "${DATA_DIR}"
RELEASE_CENTER_TOKEN="$token" RELEASE_CENTER_DATA="$DATA_DIR" RELEASE_CENTER_IMAGE="$IMAGE" docker compose -p release-center -f "${ROOT_DIR}/compose.yaml" up -d
echo "Release Center deployed with data directory: ${DATA_DIR}"
