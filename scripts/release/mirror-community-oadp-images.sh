#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT_DIR/scripts/lib/registry-config.sh"
LOCK_FILE="${ROOT_DIR}/packaging/oadp/image-lock.json"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
OUTPUT="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"

usage() {
  echo "Usage: mirror-community-oadp-images.sh --registry HOST/NAMESPACE [--lock FILE] [--output FILE]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --lock) LOCK_FILE="${2:?missing value for --lock}"; shift 2 ;;
    --output) OUTPUT="${2:?missing value for --output}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

REGISTRY="${REGISTRY%/}"
[[ "$REGISTRY" == */* ]] || { echo "registry must include host and namespace" >&2; exit 2; }
[[ -r "$LOCK_FILE" ]] || { echo "lock file is not readable: $LOCK_FILE" >&2; exit 2; }
command -v docker >/dev/null
command -v jq >/dev/null
mkdir -p "$(dirname "$OUTPUT")"

release="$(jq -er '.release' "$LOCK_FILE")"
platform="$(jq -er '.platform' "$LOCK_FILE")"
tmp="$(mktemp "${OUTPUT}.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
jq '. + {resolvedAt:(now|todateiso8601), registry:$registry, images:[]}' \
  --arg registry "$REGISTRY" "$LOCK_FILE" >"$tmp"

while IFS=$'\t' read -r component source source_digest required; do
  target="$(image_ref "${REGISTRY}" "${component}" "${release}")"
  [[ "$source_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "locked source digest is invalid: $component" >&2; exit 1; }
  echo "==> Pulling locked ${source%@*}@${source_digest} for ${platform}"
  timeout 300 docker pull --platform "$platform" "${source%@*}@${source_digest}"
  docker tag "${source%@*}@${source_digest}" "$target"
  timeout 300 docker push "$target"
  timeout 300 docker pull --platform "$platform" "$target" >/dev/null
  target_digest="$(docker image inspect "$target" --format '{{join .RepoDigests "\n"}}' | awk -F@ -v repo="${target%:*}" '$1==repo && $2 ~ /^sha256:[0-9a-f]{64}$/ {print $2; exit}')"
  [[ "$target_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "target digest unavailable: $target" >&2; exit 1; }
  docker manifest inspect "${target%@*}@${target_digest}" >/dev/null

  next="$(mktemp "${OUTPUT}.XXXXXX")"
  jq '.images += [{component:$component,source:$source,sourceDigest:$sourceDigest,target:$target,targetDigest:$targetDigest,required:($required=="true")}]' \
    --arg component "$component" --arg source "$source" --arg sourceDigest "$source_digest" \
    --arg target "$target" --arg targetDigest "$target_digest" --arg required "$required" "$tmp" >"$next"
  mv "$next" "$tmp"
done < <(jq -r '.images[] | [.component,.source,.sourceDigest,(.required|tostring)] | @tsv' "$LOCK_FILE")

required_count="$(jq '[.images[]|select(.required)]|length' "$LOCK_FILE")"
resolved_required_count="$(jq '[.images[]|select(.required and (.targetDigest|test("^sha256:[0-9a-f]{64}$")))]|length' "$tmp")"
[[ "$required_count" == "$resolved_required_count" ]] || { echo "required OADP image set is incomplete" >&2; exit 1; }
mv "$tmp" "$OUTPUT"
trap - EXIT
echo "OADP image mirror verified: $OUTPUT"
