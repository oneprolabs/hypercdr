#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
mkdir -p "${ROOT_DIR}/../hypercdr-runtime/validation"
WORK="$(mktemp -d "${ROOT_DIR}/../hypercdr-runtime/validation/installer-package.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
VERSION=1.0.0.20260924
jq -n --arg version "$VERSION" '
  {version: $version, componentManifest: (
    ["platform-api", "platform-frontend", "cluster-registration-executor", "comm-agent",
     "velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure",
     "velero-plugin-for-gcp", "oadp-comm-agent", "oadp-operator", "oadp-velero",
     "oadp-openshift-plugin", "oadp-aws-plugin", "oadp-restore-helper", "oadp-bundle",
     "oadp-catalog"] | map({key: ., value: {version: $version, image: ("example.invalid/test:" + .)}}) | from_entries)}
' > "$WORK/manifest.json"
HCDR_PACKAGE_BUILD_ROOT="$WORK/build" HCDR_RELEASE_ROOT="$WORK/releases" \
HCDR_RELEASE_MANIFEST="$WORK/manifest.json" HCDR_AUTH_CHALLENGE_MODE=image \
  bash "$ROOT_DIR/scripts/release/package-release.sh" "$VERSION" > "$WORK/package.log"
cd "$WORK/releases/$VERSION"
sha256sum -c "hypercdr-installer-$VERSION.sha256"
mkdir "$WORK/extracted"
tar -xzf "hypercdr-installer-$VERSION.tar.gz" -C "$WORK/extracted"
PACKAGE="$WORK/extracted/hypercdr-installer-$VERSION"
for script in install-blue-green.sh deploy-blue-green.sh platform-upgrade-runner.sh uninstall.sh uninstall-platform.sh; do
  test -x "$PACKAGE/$script"
  bash -n "$PACKAGE/$script"
done
cmp "$WORK/manifest.json" "$PACKAGE/release-manifest.json"
for service in hypercdr.service hypercdr-upgrade-runner.service; do
  test -s "$PACKAGE/templates/$service"
done
# Exercise argument parsing from the extracted package without host mutations.
bash "$PACKAGE/install-blue-green.sh" "$VERSION" \
  --base-url https://127.0.0.1:12443 --domain 127.0.0.1 \
  --registry example.invalid/test \
  --install-dir "$WORK/installed" > "$WORK/install.log"
grep -Fq 'Dry run:' "$WORK/install.log"
echo 'installer package contents and dry run passed'
