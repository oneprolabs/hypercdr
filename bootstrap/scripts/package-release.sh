#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
version="${1:?Usage: package-release.sh VERSION}"
[[ "$version" =~ ^([0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}|v[0-9]{8}\.[0-9]+)$ ]] || exit 2
release="${HCDR_RELEASE_ROOT:-$runtime/releases/community}/$version"
archive="hypercdr-installer-$version.tar.gz"
[[ -s "$release/$archive" && -s "$release/$archive.sha256" ]] || {
  # Platform checksum names omit .tar.gz.
  [[ -s "$release/$archive" && -s "$release/hypercdr-installer-$version.sha256" ]] || {
    echo "Platform installer missing: $release. Run scripts/release/release-all.sh first." >&2
    exit 1
  }
}
(cd "$release" && sha256sum -c "hypercdr-installer-$version.sha256")
output="${HCDR_BOOTSTRAP_RELEASE_ROOT:-$runtime/releases/bootstrap}/$version"
[[ ! -e "$output" ]] || { echo "Bootstrap release already exists: $output" >&2; exit 1; }
mkdir -p "$output"
stage="$(mktemp -d "$output/stage.XXXXXX")"
cp -R "$root/bootstrap/portal" "$stage/portal"
cp "$root/bootstrap/deploy-bootstrap.sh" "$stage/deploy-bootstrap.sh"
HCDR_BOOTSTRAP_PUBLISH_DIR="$stage/source" bash "$root/bootstrap/scripts/publish-release.sh" --version "$version"
tar -C "$stage" -czf "$output/hypercdr-bootstrap-portal-$version.tar.gz" .
(cd "$output" && sha256sum "hypercdr-bootstrap-portal-$version.tar.gz" > "hypercdr-bootstrap-portal-$version.sha256")
echo "Bootstrap package: $output/hypercdr-bootstrap-portal-$version.tar.gz"
