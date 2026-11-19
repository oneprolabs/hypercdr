#!/usr/bin/env bash

dependency_digest() {
  docker manifest inspect --verbose "$1" 2>/dev/null |
    jq -r 'if type == "array" then .[0].Descriptor.digest else .Descriptor.digest end // empty'
}

dependency_target_matches() {
  local source="$1" target="$2" source_digest target_digest
  source_digest="$(dependency_digest "$source")" || return 1
  target_digest="$(dependency_digest "$target")" || return 1
  [[ "$source_digest" =~ ^sha256:[0-9a-f]{64}$ && "$source_digest" == "$target_digest" ]]
}
