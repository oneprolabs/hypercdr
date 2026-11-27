#!/usr/bin/env bash

velero_source_fingerprint() {
  (
    set -o pipefail
    cd "$ROOT_DIR" || return 1
    git ls-files --cached --others --exclude-standard -z -- third_party/velero scripts/release/build-velero.sh scripts/release/common.sh scripts/release/dependency-fingerprint.sh scripts/release/publish-runtime-images.sh scripts/lib/registry-config.sh packaging/dependencies/lock.json \
      | sort -zu | while IFS= read -r -d '' input; do
          [[ ! -f "$input" ]] || sha256sum "$input"
        done || return 1
    printf '%s\n' "platform=${HCDR_BUILD_PLATFORM:-linux/amd64}" "restic=${HCDR_RESTIC_VERSION:-0.15.0}"
  ) | sha256sum | cut -d' ' -f1
}

velero_release_version() {
  local version
  version="$(cat "$ROOT_DIR/third_party/velero/HYPERCDR_VERSION")" || return 1
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-hcdr\.[0-9]+$ ]] || return 1
  printf '%s\n' "$version"
}

default_velero_image_tag() {
  local fingerprint version
  version="$(velero_release_version)" || return 1
  fingerprint="$(velero_source_fingerprint)" || return 1
  [[ "$fingerprint" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf '%s-build.%.40s-backup-volume-policy-v1-isolation-cache-v1\n' "$version" "$fingerprint"
}

velero_input_fingerprint() {
  local input base digest
  input="$(
    cd "$ROOT_DIR"
    { git ls-files -z third_party/velero scripts/release/build-velero.sh scripts/lib/registry-config.sh; printf '%s\0' scripts/release/dependency-fingerprint.sh; } \
      | sort -zu | xargs -0 sha256sum
    printf '%s\n' "tag=$VELERO_TAG" "registry=$REGISTRY" "platform=${HCDR_BUILD_PLATFORM:-linux/amd64}" "restic=${HCDR_RESTIC_VERSION:-0.15.0}"
  )" || return 1
  while read -r base; do
    digest="$(docker_manifest_digest "$base")" || return 1
    input+=$'\n'"$base@$digest"
  done < <(awk 'toupper($1)=="FROM" { for (field=2; field<=NF; field++) if ($field !~ /^--/) { print $field; break } }' "$ROOT_DIR/third_party/velero/Dockerfile" | sort -u)
  printf '%s' "$input" | sha256sum | cut -d' ' -f1
}
