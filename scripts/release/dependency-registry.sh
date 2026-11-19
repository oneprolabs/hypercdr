#!/usr/bin/env bash

dependency_remote_digest() {
  local reference="$1" output status digest
  local limit="${HCDR_MANIFEST_TIMEOUT_SECONDS:-60}"
  [[ "$limit" =~ ^[1-9][0-9]*$ ]] || { echo 'invalid manifest timeout' >&2; return 2; }
  if output="$(timeout --kill-after=10 "$limit" docker buildx imagetools inspect "$reference" --format '{{json .Manifest}}' 2>&1)"; then
    digest="$(printf '%s' "$output" | jq -er '.digest | select(test("^sha256:[0-9a-f]{64}$"))')" || {
      echo "invalid manifest response for $reference" >&2
      return 2
    }
    printf '%s\n' "$digest"
  else
    status=$?
    if [[ "$status" != 124 && "$status" != 137 ]] &&
       printf '%s' "$output" | grep -Eiq 'manifest unknown|MANIFEST_UNKNOWN|not found|name unknown'; then
      return 3
    fi
    printf 'registry lookup failed for %s (status %s): %s\n' "$reference" "$status" "$output" >&2
    return 2
  fi
}

dependency_verify_reference() {
  local reference="$1" expected="$2" actual
  actual="$(dependency_remote_digest "$reference")" || return "$?"
  [[ "$actual" == "$expected" ]] || {
    echo "dependency digest mismatch: $reference (expected $expected, received $actual)" >&2
    return 2
  }
}
