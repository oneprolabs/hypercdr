#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

RESOLVED_LOCK="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"
WORK_DIR="${HCDR_OADP_BUILD_DIR:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/build/oadp}"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
OPM_IMAGE="${HCDR_OPM_IMAGE:-quay.io/operator-framework/opm@sha256:b32d3891616662620da08d7f0ec42c2e69fa2de43427dc975d35b12f7a969a0f}"
POLICY_FILE="${HCDR_CONTAINER_POLICY:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/containers-policy.json}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --lock) RESOLVED_LOCK="${2:?missing value for --lock}"; shift 2 ;;
    --work-dir) WORK_DIR="${2:?missing value for --work-dir}"; shift 2 ;;
    *) echo "Usage: $0 --registry HOST/NAMESPACE [--lock FILE] [--work-dir DIR]" >&2; exit 2 ;;
  esac
done

REGISTRY="${REGISTRY%/}"
[[ "$REGISTRY" == */* && -r "$RESOLVED_LOCK" ]] || { echo "registry or resolved lock is invalid" >&2; exit 2; }
[[ -r "$POLICY_FILE" ]] || { echo "containers policy file is missing: $POLICY_FILE" >&2; exit 2; }
release="$(jq -er .release "$RESOLVED_LOCK")"
bundle_image="$(jq -er .bundle.image "$RESOLVED_LOCK")"
bundle_digest="$(jq -er .bundle.imageDigest "$RESOLVED_LOCK")"
[[ "$bundle_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "bundle digest is invalid" >&2; exit 1; }
bundle_reference="${bundle_image%:*}@${bundle_digest}"
catalog_dir="${WORK_DIR}/${release}/catalog"
mkdir -p "$catalog_dir/configs"
chmod a+rwx "$catalog_dir" "$catalog_dir/configs"

opm() {
  docker run --rm --network host \
    -v "$POLICY_FILE:/etc/containers/policy.json:ro" \
    -v /root/.docker/config.json:/root/.docker/config.json:ro \
    -v "$catalog_dir:/workspace" -w /workspace "$OPM_IMAGE" "$@"
}

opm render "$bundle_reference" --output yaml >"$catalog_dir/configs/bundle.yaml"
cat >"$catalog_dir/configs/package.yaml" <<'YAML'
schema: olm.package
name: oadp-operator
defaultChannel: stable-1.3
YAML
cat >"$catalog_dir/configs/channel.yaml" <<'YAML'
schema: olm.channel
package: oadp-operator
name: stable-1.3
entries:
  - name: oadp-operator.v1.3.10
YAML

opm validate /workspace/configs
[[ ! -e "$catalog_dir/configs.Dockerfile" ]] || unlink "$catalog_dir/configs.Dockerfile"
opm generate dockerfile /workspace/configs --builder-image "$OPM_IMAGE" --base-image "$OPM_IMAGE"
catalog_image="$(image_ref "${REGISTRY}" "oadp-catalog" "${release}")"
docker build --platform linux/amd64 -f "$catalog_dir/configs.Dockerfile" -t "$catalog_image" "$catalog_dir"
docker_push_with_retry "$catalog_image"
docker_pull_with_retry "$catalog_image" >/dev/null
catalog_digest="$(image_digest "$catalog_image")"
[[ "$catalog_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "catalog digest is unavailable" >&2; exit 1; }
docker_manifest_verify_or_warn "${catalog_image%@*}@${catalog_digest}"

rendered="${catalog_dir}/rendered-catalog.yaml"
opm render "${catalog_image%:*}@${catalog_digest}" --output yaml >"$rendered"
grep -q 'schema: olm.package' "$rendered"
grep -q 'schema: olm.channel' "$rendered"
grep -q 'schema: olm.bundle' "$rendered"
if grep -E 'quay\.io/konveyor|registry\.redhat\.io' "$rendered"; then
  echo "catalog contains an external OADP runtime image reference" >&2
  exit 1
fi

jq '.catalog={component:"oadp-catalog",version:$version,image:$image,imageDigest:$digest}' \
  --arg version "$release" --arg image "$catalog_image" --arg digest "$catalog_digest" \
  "$RESOLVED_LOCK" >"${RESOLVED_LOCK}.next"
mv "${RESOLVED_LOCK}.next" "$RESOLVED_LOCK"
echo "OADP catalog verified: ${catalog_image}@${catalog_digest}"
