#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
jq -e '.schemaVersion == 1 and (.runtime.postgres.targetDigest|test("^sha256:[0-9a-f]{64}$")) and (.runtime.velero.targetDigest|test("^sha256:[0-9a-f]{64}$")) and (.runtime.plugins.targets|length == 3) and .oadpLock' "$root/packaging/dependencies/lock.json" >/dev/null
bash -n "$root/scripts/release/common.sh" "$root/scripts/release/publish-runtime-images.sh" "$root/scripts/release/sync-velero-plugins.sh"
echo 'Dependency lock and reuse checks passed'
