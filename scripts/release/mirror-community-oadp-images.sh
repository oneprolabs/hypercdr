#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT_DIR/scripts/lib/registry-config.sh"
LOCK_FILE="${ROOT_DIR}/packaging/oadp/image-lock.json"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
OUTPUT="${HCDR_OADP_RESOLVED_LOCK:-${HCDR_RUNTIME_ROOT:-/data/hypercdr-runtime}/oadp-mirror/resolved-image-lock.json}"
IMAGE_TIMEOUT="${HCDR_OADP_IMAGE_TIMEOUT_SECONDS:-3600}"
PARALLELISM="${HCDR_OADP_PARALLELISM:-3}"

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
[[ "$IMAGE_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || { echo "HCDR_OADP_IMAGE_TIMEOUT_SECONDS must be a positive integer" >&2; exit 2; }
[[ "$PARALLELISM" =~ ^[1-9][0-9]*$ ]] || { echo "HCDR_OADP_PARALLELISM must be a positive integer" >&2; exit 2; }
[[ -r "$LOCK_FILE" ]] || { echo "lock file is not readable: $LOCK_FILE" >&2; exit 2; }
command -v docker >/dev/null
command -v jq >/dev/null
mkdir -p "$(dirname "$OUTPUT")"

release="$(jq -er '.release' "$LOCK_FILE")"
platform="$(jq -er '.platform' "$LOCK_FILE")"
tmp="$(mktemp "${OUTPUT}.XXXXXX")"
parts_dir="$(mktemp -d "$(dirname "$OUTPUT")/oadp-parts.XXXXXX")"
trap 'rm -f "$tmp"; rm -rf "$parts_dir"' EXIT
jq '. + {resolvedAt:(now|todateiso8601), registry:$registry, images:[]}' \
  --arg registry "$REGISTRY" "$LOCK_FILE" >"$tmp"

manifest_digest() {
  docker manifest inspect --verbose "$1" 2>/dev/null \
    | jq -r 'if type == "array" then .[0].Descriptor.digest else .Descriptor.digest end // empty'
}

retry_image_command() {
  local label="$1" attempt status
  shift
  for attempt in 1 2 3; do
    if timeout "$IMAGE_TIMEOUT" "$@"; then
      return 0
    else
      status=$?
    fi
    echo "${label} failed (attempt ${attempt}/3, status ${status})" >&2
    (( attempt == 3 )) || sleep $((attempt * 5))
  done
  return "$status"
}

mirror_image() {
  local component="$1" source="$2" source_digest="$3" required="$4"
  local target target_digest target_repository part
  target="$(image_ref "${REGISTRY}" "${component}" "${release}")"
  [[ "$source_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "locked source digest is invalid: $component" >&2; exit 1; }

  target_digest="$(manifest_digest "$target" || true)"
  if [[ "$target_digest" == "$source_digest" ]]; then
    echo "==> Reusing verified OADP image ${target}@${target_digest}"
  else
    echo "==> Mirroring locked ${source%@*}@${source_digest} for ${platform}"
    retry_image_command "pull ${component}" docker pull --platform "$platform" "${source%@*}@${source_digest}"
    docker tag "${source%@*}@${source_digest}" "$target"
    retry_image_command "push ${component}" docker push "$target"
    target_digest="$(manifest_digest "$target")"
  fi

  [[ "$target_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "target digest unavailable: $target" >&2; exit 1; }
  target_repository="${target%:*}"
  docker manifest inspect "${target_repository}@${target_digest}" >/dev/null

  part="${parts_dir}/${component}.json"
  jq -n '{component:$component,source:$source,sourceDigest:$sourceDigest,target:$target,targetDigest:$targetDigest,required:($required=="true")}' \
    --arg component "$component" --arg source "$source" --arg sourceDigest "$source_digest" \
    --arg target "$target" --arg targetDigest "$target_digest" --arg required "$required" >"${part}.tmp"
  mv "${part}.tmp" "$part"
}

export REGISTRY release platform IMAGE_TIMEOUT parts_dir
export -f image_ref manifest_digest retry_image_command mirror_image
jq -r '.images[] | [.component,.source,.sourceDigest,(.required|tostring)] | @tsv' "$LOCK_FILE" \
  | xargs -P "$PARALLELISM" -n 4 bash -c 'mirror_image "$@"' _

jq -s '.[0] + {images:(.[1:] | sort_by(.component))}' "$tmp" "$parts_dir"/*.json >"${tmp}.resolved"
mv "${tmp}.resolved" "$tmp"

required_count="$(jq '[.images[]|select(.required)]|length' "$LOCK_FILE")"
resolved_required_count="$(jq '[.images[]|select(.required and (.targetDigest|test("^sha256:[0-9a-f]{64}$")))]|length' "$tmp")"
[[ "$required_count" == "$resolved_required_count" ]] || { echo "required OADP image set is incomplete" >&2; exit 1; }
mv "$tmp" "$OUTPUT"
trap - EXIT
rm -rf "$parts_dir"
echo "OADP image mirror verified: $OUTPUT"
