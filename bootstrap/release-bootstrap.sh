#!/usr/bin/env bash
# Historical command builds and publishes; new callers can run the steps separately.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$root/scripts/build-release.sh" --version "${1:?version required}"
exec bash "$root/scripts/publish-release.sh" --version "$1"
