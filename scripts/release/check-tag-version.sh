#!/usr/bin/env bash
set -euo pipefail
root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
tag="${1:?usage: bash scripts/release/check-tag-version.sh ce-vX.Y.Z}"
[[ "$tag" =~ ^ce-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo "Invalid community tag: $tag" >&2; exit 2; }
working_version="$(cat "$root/VERSION")"
committed_version="$(git -C "$root" show HEAD:VERSION)"
if [[ "$working_version" != "$tag" || "$committed_version" != "$tag" ]]; then
  printf 'Release blocked: tag=%s working VERSION=%s HEAD VERSION=%s. Update and commit VERSION before tagging.\n' "$tag" "$working_version" "$committed_version" >&2
  exit 1
fi
printf 'Release version verified: %s\n' "$tag"
