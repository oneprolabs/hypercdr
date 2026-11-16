#!/usr/bin/env bash

dependency_cache_key() {
  local component="$1" extra="${2:-}"
  {
    jq -cS 'del(.resolvedAt,.bundle,.catalog)' "$RESOLVED_LOCK"
    sha256sum "$ROOT_DIR/scripts/release/build-community-oadp-${component}.sh" "$ROOT_DIR/scripts/release/dependency-cache.sh"
    printf '%s\n' "$extra"
  } | sha256sum | cut -d' ' -f1
}

restore_dependency_cache() {
  local component="$1" target="$2" cache_image="$3" digest status
  if digest="$(docker_manifest_digest "$cache_image")"; then
    mirror_release_image "$cache_image" "$target" || return 1
    jq --arg component "$component" --arg version "$release" --arg image "$target" --arg digest "$digest" \
      '.[$component]={component:("oadp-"+$component),version:$version,image:$image,imageDigest:$digest}' \
      "$RESOLVED_LOCK" >"${RESOLVED_LOCK}.next" || return 1
    mv "${RESOLVED_LOCK}.next" "$RESOLVED_LOCK"
    log "Reusing verified OADP $component: $target@$digest"
    return 0
  else
    status=$?
    return "$status"
  fi
}
