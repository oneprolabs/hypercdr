#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-}"
ALLOWED_REGISTRY="${2:-}"
[[ -n "$IMAGE" && -n "$ALLOWED_REGISTRY" ]] || { echo "usage: verify-oadp-catalog.sh IMAGE ALLOWED_REGISTRY" >&2; exit 2; }
ALLOWED_REGISTRY="${ALLOWED_REGISTRY%/}"
case "$IMAGE" in
  "${ALLOWED_REGISTRY}/"*) ;;
  *) echo "catalog image itself is outside ${ALLOWED_REGISTRY}: ${IMAGE}" >&2; exit 1 ;;
esac
command -v docker >/dev/null 2>&1 || { echo "docker is required to inspect the OADP catalog" >&2; exit 2; }
OPM_IMAGE="${HCDR_OPM_IMAGE:-quay.io/operator-framework/opm@sha256:b32d3891616662620da08d7f0ec42c2e69fa2de43427dc975d35b12f7a969a0f}"
POLICY_FILE="${HCDR_CONTAINER_POLICY:-/data/hypercdr-runtime/containers-policy.json}"
[[ -r "$POLICY_FILE" ]] || { echo "containers policy file is missing: $POLICY_FILE" >&2; exit 2; }

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
docker run --rm -v "$POLICY_FILE:/etc/containers/policy.json:ro" \
  -v /root/.docker/config.json:/root/.docker/config.json:ro "$OPM_IMAGE" render "$IMAGE" --output yaml >"$rendered"
grep -q 'schema: olm.package' "$rendered" && grep -q 'name: oadp-operator' "$rendered" || {
  echo "catalog does not contain oadp-operator" >&2
  exit 1
}
grep -q 'schema: olm.channel' "$rendered" && grep -q 'name: stable-1.3' "$rendered" || {
  echo "catalog does not contain the qualified stable-1.3 channel" >&2
  exit 1
}

mapfile -t external_images < <(grep -E '^[[:space:]]*(- )?image:' "$rendered" | sed -E 's/^[[:space:]-]*image:[[:space:]]*//' | sort -u | awk -v prefix="${ALLOWED_REGISTRY}/" 'index($0,prefix) != 1')
if (( ${#external_images[@]} > 0 )); then
  echo "catalog contains images outside ${ALLOWED_REGISTRY}:" >&2
  printf '  %s\n' "${external_images[@]}" >&2
  exit 1
fi
mapfile -t mutable_images < <(grep -E '^[[:space:]]*(- )?image:' "$rendered" | sed -E 's/^[[:space:]-]*image:[[:space:]]*//' | sort -u | awk -v prefix="${ALLOWED_REGISTRY}/" 'index($0,prefix) == 1 && $0 !~ /@sha256:[0-9a-f]{64}$/')
if (( ${#mutable_images[@]} > 0 )); then
  echo "catalog contains mutable runtime image references:" >&2
  printf '  %s\n' "${mutable_images[@]}" >&2
  exit 1
fi
echo "OADP catalog is self-contained in ${ALLOWED_REGISTRY}: ${IMAGE}"
