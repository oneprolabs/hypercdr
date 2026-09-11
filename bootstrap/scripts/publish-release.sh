#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
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
printf 'Published Community %s to %s\n' "$version" "$portal"
