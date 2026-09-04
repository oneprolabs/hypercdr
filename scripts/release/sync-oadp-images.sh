#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
SOURCE_CATALOG="${HCDR_OADP_SOURCE_CATALOG:-registry.redhat.io/redhat/redhat-operator-index:v4.15}"
CHANNEL="${HCDR_OADP_CHANNEL:-stable-1.3}"
WORKSPACE="${HCDR_OADP_MIRROR_WORKSPACE:-/data/hypercdr-runtime/oadp-mirror}"
DRY_RUN=false

usage() {
  cat <<'USAGE'
Mirror the qualified OADP operator and all related images to Alibaba Cloud.

Usage:
  sync-oadp-images.sh --registry REGISTRY [--workspace DIR] [--dry-run]

Prerequisites:
  - oc-mirror and jq
  - container auth containing a valid Red Hat pull secret and Alibaba registry
    push credentials (normally ${XDG_RUNTIME_DIR}/containers/auth.json)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --workspace) WORKSPACE="${2:?missing value for --workspace}"; shift 2 ;;
    --source-catalog) SOURCE_CATALOG="${2:?missing value for --source-catalog}"; shift 2 ;;
    --channel) CHANNEL="${2:?missing value for --channel}"; shift 2 ;;
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

REGISTRY="${REGISTRY%/}"
[[ -n "$REGISTRY" && "$REGISTRY" == */* ]] || { echo "--registry must include the Alibaba registry host and namespace" >&2; exit 2; }
[[ "$WORKSPACE" == /data/hypercdr-runtime/* ]] || { echo "--workspace must be below /data/hypercdr-runtime" >&2; exit 2; }
command -v oc-mirror >/dev/null 2>&1 || { [[ "$DRY_RUN" == true ]] || { echo "oc-mirror is required" >&2; exit 2; }; }

mkdir -p "$WORKSPACE"
config="$WORKSPACE/imageset-config.yaml"
cat >"$config" <<YAML
apiVersion: mirror.openshift.io/v1alpha2
kind: ImageSetConfiguration
storageConfig:
  local:
    path: ${WORKSPACE}/metadata
mirror:
  operators:
    - catalog: ${SOURCE_CATALOG}
      packages:
        - name: redhat-oadp-operator
          channels:
            - name: ${CHANNEL}
YAML

if [[ "$DRY_RUN" == true ]]; then
  printf 'Generated %s\n' "$config"
  printf 'oc-mirror --config %q docker://%q\n' "$config" "$REGISTRY"
  exit 0
fi

oc-mirror --config "$config" "docker://${REGISTRY}" --workspace "file://${WORKSPACE}/workspace"
mapping="$(find "$WORKSPACE" -type f -name mapping.txt -print | sort | tail -n 1)"
[[ -r "$mapping" ]] || { echo "oc-mirror completed without a mapping.txt result" >&2; exit 1; }
catalog_image="$(awk -F= -v source="$SOURCE_CATALOG" '$1 == source {print $2; exit}' "$mapping")"
[[ -n "$catalog_image" ]] || { echo "mirrored catalog image was not found in $mapping" >&2; exit 1; }

"${ROOT_DIR}/scripts/release/verify-oadp-catalog.sh" "$catalog_image" "$REGISTRY"
printf 'OADP mirror completed. Add this immutable result to the release environment:\n'
printf 'HCDR_OADP_CATALOG_IMAGE=%s\n' "$catalog_image"
