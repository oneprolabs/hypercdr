#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
PLUGIN_VERSION="${HCDR_VELERO_PLUGIN_VERSION:-v1.13.0}"
SOURCE_REGISTRY="${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY:-docker.io/velero}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --version) PLUGIN_VERSION="${2:?missing value for --version}"; shift 2 ;;
    --source-registry) SOURCE_REGISTRY="${2:?missing value for --source-registry}"; shift 2 ;;
    -h|--help)
      echo "Usage: sync-velero-plugins.sh --registry HOST:PORT/PROJECT [--version v1.13.0] [--source-registry REGISTRY/PROJECT]"
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
done

require_registry "${REGISTRY}"
require_cmd docker
REGISTRY="${REGISTRY%/}"
SOURCE_REGISTRY="${SOURCE_REGISTRY%/}"

plugins=(
  "velero-plugin-for-aws|${SOURCE_REGISTRY}/velero-plugin-for-aws:${PLUGIN_VERSION}"
  "velero-plugin-for-microsoft-azure|${SOURCE_REGISTRY}/velero-plugin-for-microsoft-azure:${PLUGIN_VERSION}"
  "velero-plugin-for-gcp|${SOURCE_REGISTRY}/velero-plugin-for-gcp:${PLUGIN_VERSION}"
)

for entry in "${plugins[@]}"; do
  name="${entry%%|*}"
  source_image="${entry#*|}"
  target_image="${REGISTRY}/${name}:${PLUGIN_VERSION}"
  log "Mirroring ${source_image} to ${target_image}"
  docker pull "${source_image}"
  docker tag "${source_image}" "${target_image}"
  docker push "${target_image}"
done
