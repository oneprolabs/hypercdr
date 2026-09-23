#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/registry-config.sh
source "${ROOT_DIR}/scripts/lib/registry-config.sh"

[[ "$(image_ref crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr platform-api 1.0.39.20260916)" == "crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr:platform-api-1.0.39.20260916" ]]
[[ "$(image_ref docker.io/oneprolabs/hypercdr postgres 16)" == "docker.io/oneprolabs/hypercdr:postgres-16" ]]
[[ "$(image_ref 192.168.8.149:5001/hypercdr platform-api 1.0.39.20260916)" == "192.168.8.149:5001/hypercdr/platform-api:1.0.39.20260916" ]]

load_registry_profile "${ROOT_DIR}/config/registries.conf"
[[ "${HCDR_SELECTED_REGISTRY}" == "aliyun_acr" ]]
[[ "${HCDR_IMAGE_REGISTRY}" == "crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr" ]]
[[ "${HCDR_REGISTRY_TRUST}" == "system" ]]
[[ "$(bash -c 'printf %s "$HCDR_POSTGRES_SOURCE_IMAGE"')" == "postgres:16" ]]
[[ "$(bash -c 'printf %s "$HCDR_VELERO_PLUGIN_SOURCE_REGISTRY"')" == "docker.io/velero" ]]

load_registry_profile "${ROOT_DIR}/config/registries.conf" dockerhub
[[ "${HCDR_SELECTED_REGISTRY}" == "dockerhub" ]]
[[ "${HCDR_IMAGE_REGISTRY}" == "docker.io/oneprolabs/hypercdr" ]]
[[ "${HCDR_REGISTRY_SERVER}" == "docker.io" ]]

load_registry_profile "${ROOT_DIR}/config/registries.conf" harbor_149
[[ "${HCDR_SELECTED_REGISTRY}" == "harbor_149" ]]
[[ "${HCDR_IMAGE_REGISTRY}" == "192.168.8.149:5001/hypercdr" ]]
[[ "${HCDR_REGISTRY_TRUST}" == "private-ca" ]]

HCDR_POSTGRES_SOURCE_IMAGE_OVERRIDE=registry.local/postgres:16
HCDR_VELERO_PLUGIN_SOURCE_REGISTRY_OVERRIDE=registry.local/velero
load_registry_profile "${ROOT_DIR}/config/registries.conf" aliyun_acr
[[ "${HCDR_POSTGRES_SOURCE_IMAGE}" == "registry.local/postgres:16" ]]
[[ "${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY}" == "registry.local/velero" ]]
[[ "${HCDR_REGISTRY_TRUST}" == "system" ]]

if load_registry_profile "${ROOT_DIR}/config/registries.conf" missing >/dev/null 2>&1; then
  echo "unknown profile unexpectedly succeeded" >&2
  exit 1
fi

echo "registry profile selection: ok"
