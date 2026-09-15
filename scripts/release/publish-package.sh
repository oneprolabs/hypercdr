#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  echo 'Usage: publish-package.sh --version VERSION'
  exit 0
fi
[[ $# == 2 && $1 == --version && $2 =~ ^([0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}|v[0-9]{8}\.[0-9]+)$ ]] || { echo 'Usage: publish-release.sh --version VERSION' >&2; exit 2; }
version="$2"
release="${HCDR_RELEASE_ROOT:-$runtime/releases/community}/$version"
portal="${HCDR_BOOTSTRAP_PUBLISH_DIR:-$runtime/services/bootstrap-portal/source}"
[[ -s "$release/manifest.json" && -s "$release/release-manifest.json" ]] || { echo "Incomplete release: $release" >&2; exit 1; }
(cd "$release" && sha256sum -c "hypercdr-installer-$version.sha256")
mkdir -p "$portal/releases/community/$version" "$portal/releases/dev"
cp -R "$root/bootstrap/site/." "$portal/"
cp -R "$release/." "$portal/releases/community/$version/"
# Compatibility aliases reference the same bytes as the versioned package.
cp -R "$release/." "$portal/releases/community/"
cp -R "$release/." "$portal/releases/dev/"
python3 - "$portal/releases/community/index.json" "$portal/releases/community" "$version" <<'PY'
import json, pathlib, sys
output, root, version = map(pathlib.Path, sys.argv[1:])
items=[]
for manifest in sorted(root.glob('*/release-manifest.json'), reverse=True):
    items.append(json.loads(manifest.read_text()))
output.write_text(json.dumps({'items': items}, indent=2) + '\n')
PY
printf 'Published Community %s to %s\n' "$version" "$portal"
