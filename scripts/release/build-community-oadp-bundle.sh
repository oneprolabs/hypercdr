#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT_DIR/scripts/lib/registry-config.sh"
RESOLVED_LOCK="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"
WORK_DIR="${HCDR_OADP_BUILD_DIR:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/build/oadp}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) echo "Usage: $0 --registry HOST/NAMESPACE [--lock FILE] [--work-dir DIR]"; exit 0 ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --lock) RESOLVED_LOCK="${2:?missing value for --lock}"; shift 2 ;;
    --work-dir) WORK_DIR="${2:?missing value for --work-dir}"; shift 2 ;;
    *) echo "Usage: $0 --registry HOST/NAMESPACE [--lock FILE] [--work-dir DIR]" >&2; exit 2 ;;
  esac
done

REGISTRY="${REGISTRY%/}"
[[ "$REGISTRY" == */* && -r "$RESOLVED_LOCK" ]] || { echo "registry or resolved lock is invalid" >&2; exit 2; }
for command in curl jq docker; do command -v "$command" >/dev/null; done
release="$(jq -er .release "$RESOLVED_LOCK")"
commit="$(jq -er .sourceCommit "$RESOLVED_LOCK")"
bundle_dir="${WORK_DIR}/${release}/bundle"
mkdir -p "$bundle_dir/manifests" "$bundle_dir/metadata"

tree_file="${bundle_dir}/source-tree.json"
if [[ "$(jq -r '.sha // empty' "$tree_file" 2>/dev/null || true)" == "$commit" ]] &&
   [[ "$(find "$bundle_dir/manifests" "$bundle_dir/metadata" -type f 2>/dev/null | wc -l)" -gt 0 ]]; then
  echo "Using cached, commit-qualified OADP bundle source: ${commit}"
else
  curl -LfsS --retry 4 --connect-timeout 10 --max-time 120 \
    "https://api.github.com/repos/openshift/oadp-operator/git/trees/${commit}?recursive=1" -o "$tree_file"
fi
jq -e '.truncated == false' "$tree_file" >/dev/null

download_blob() {
  local source_path="$1" target_path="$2" sha
  sha="$(jq -er --arg path "$source_path" '.tree[]|select(.path==$path and .type=="blob")|.sha' "$tree_file")"
  if ! curl -LfsS --retry 2 --connect-timeout 10 --max-time 120 \
      "https://api.github.com/repos/openshift/oadp-operator/git/blobs/${sha}" \
      | jq -er .content | tr -d '\n' | base64 -d >"$target_path"; then
    # GitHub's API is rate limited without a token; the immutable raw commit
    # endpoint remains public and is equivalent for a path pinned by the tree.
    curl -LfsS --retry 4 --connect-timeout 10 --max-time 120 \
      "https://raw.githubusercontent.com/openshift/oadp-operator/${commit}/${source_path}" -o "$target_path"
  fi
  [[ -s "$target_path" ]] || { echo "downloaded empty OADP bundle file: ${source_path}" >&2; exit 1; }
}

while IFS= read -r source_path; do
  target_path="${bundle_dir}/${source_path#bundle/}"
  [[ -s "$target_path" ]] || download_blob "$source_path" "$target_path"
done < <(jq -r '.tree[]|select(.type=="blob" and (.path|startswith("bundle/manifests/") or startswith("bundle/metadata/")))|.path' "$tree_file")
cp "${ROOT_DIR}/packaging/oadp/bundle.Dockerfile" "${bundle_dir}/Dockerfile"

csv="${bundle_dir}/manifests/oadp-operator.clusterserviceversion.yaml"
[[ -s "$csv" ]] || { echo "OADP CSV is missing" >&2; exit 1; }
declare -A replacements=(
  [oadp-operator]='quay.io/konveyor/oadp-operator:oadp-1.3'
  [oadp-velero]='quay.io/konveyor/velero:oadp-1.3'
  [oadp-openshift-plugin]='quay.io/konveyor/openshift-velero-plugin:oadp-1.3'
  [oadp-aws-plugin]='quay.io/konveyor/velero-plugin-for-aws:oadp-1.3'
  [oadp-restore-helper]='quay.io/konveyor/velero-restore-helper:oadp-1.3'
  [oadp-csi-plugin]='quay.io/konveyor/velero-plugin-for-csi:oadp-1.3'
  [oadp-azure-plugin]='quay.io/konveyor/velero-plugin-for-microsoft-azure:oadp-1.3'
  [oadp-gcp-plugin]='quay.io/konveyor/velero-plugin-for-gcp:oadp-1.3'
  [oadp-kubevirt-plugin]='quay.io/konveyor/kubevirt-velero-plugin:v0.6.2'
  [oadp-must-gather]='quay.io/konveyor/oadp-must-gather:oadp-1.3'
  [oadp-cli]='quay.io/konveyor/oadp-cli-binaries:oadp-1.3'
)
for component in "${!replacements[@]}"; do
  target_tag="$(jq -er --arg component "$component" '.images[]|select(.component==$component)|.target' "$RESOLVED_LOCK")"
  target_digest="$(jq -er --arg component "$component" '.images[]|select(.component==$component)|.targetDigest' "$RESOLVED_LOCK")"
  target_repository="${target_tag%:*}"
  target="${target_repository}@${target_digest}"
  sed -i "s#${replacements[$component]}#${target}#g" "$csv"
  sed -i "s#${target_tag}#${target}#g" "$csv"
done

if grep -R -E 'quay\.io/konveyor|registry\.redhat\.io' "$bundle_dir/manifests" "$bundle_dir/metadata"; then
  echo "bundle still contains an external runtime image reference" >&2
  exit 1
fi
grep -q 'version: 1.3.10' "$csv" || { echo "CSV is not OADP 1.3.10" >&2; exit 1; }

bundle_image="$(image_ref "${REGISTRY}" "oadp-bundle" "${release}")"
docker build --platform linux/amd64 -t "$bundle_image" "$bundle_dir"
docker push "$bundle_image"
docker pull --platform linux/amd64 "$bundle_image" >/dev/null
bundle_digest="$(image_digest "$bundle_image")"
[[ "$bundle_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "bundle digest is unavailable" >&2; exit 1; }
docker manifest inspect "${bundle_image%@*}@${bundle_digest}" >/dev/null
jq '.bundle={component:"oadp-bundle",version:$version,image:$image,imageDigest:$digest}' \
  --arg version "$release" --arg image "$bundle_image" --arg digest "$bundle_digest" \
  "$RESOLVED_LOCK" >"${RESOLVED_LOCK}.next"
mv "${RESOLVED_LOCK}.next" "$RESOLVED_LOCK"
echo "OADP bundle verified: ${bundle_image}@${bundle_digest}"
