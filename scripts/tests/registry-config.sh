#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/registry-config.sh
source "${ROOT_DIR}/scripts/lib/registry-config.sh"

load_registry_profile "${ROOT_DIR}/config/registries.conf"
[[ "${HCDR_SELECTED_REGISTRY}" == "aliyun_acr" ]]
[[ "${HCDR_IMAGE_REGISTRY}" == "crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr" ]]
[[ "${HCDR_REGISTRY_TRUST}" == "system" ]]
[[ "$(bash -c 'printf %s "$HCDR_POSTGRES_SOURCE_IMAGE"')" == "postgres:16" ]]
[[ "$(bash -c 'printf %s "$HCDR_VELERO_PLUGIN_SOURCE_REGISTRY"')" == "docker.io/velero" ]]

load_registry_profile "${ROOT_DIR}/config/registries.conf" harbor_149
[[ "${HCDR_SELECTED_REGISTRY}" == "harbor_149" ]]
[[ "${HCDR_IMAGE_REGISTRY}" == "192.168.8.149:5001/hypercdr" ]]
[[ "${HCDR_REGISTRY_TRUST}" == "private-ca" ]]

if load_registry_profile "${ROOT_DIR}/config/registries.conf" missing >/dev/null 2>&1; then
  echo "unknown profile unexpectedly succeeded" >&2
  exit 1
fi

echo "registry profile selection: ok"
