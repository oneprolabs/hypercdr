#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
test -s "$root/packaging/dependencies/lock.json"
jq -e '.schemaVersion == 1 and .runtime.postgres.version == "16" and .runtime.velero.version and .runtime.plugins.version and .oadpLock' "$root/packaging/dependencies/lock.json" >/dev/null
bash -n "$root/scripts/release/dependency-reuse.sh" "$root/scripts/release/publish-runtime-images.sh" "$root/scripts/release/sync-velero-plugins.sh"
echo 'Dependency lock and reuse scripts passed'
