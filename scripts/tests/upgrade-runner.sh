#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
printf 'HCDR_RELEASE_TOKEN=test\n' > "$tmp/.env"
export HCDR_INSTALL_DIR="$tmp"
source "$root/scripts/release/platform-upgrade-runner.sh"
cat > "$tmp/deploy-blue-green.sh" <<'DEPLOY'
#!/usr/bin/env bash
touch "$HCDR_INSTALL_DIR/deployed"
DEPLOY
chmod +x "$tmp/deploy-blue-green.sh"
api() {
 if [[ "$1" == */upgrades ]]; then
  printf '%s' '{"items":[{"id":"job","status":"queued","releaseId":"release","targetVersion":"1.0.99.20260924"}]}'
 elif [[ "$scenario" == fetch_failure ]]; then
  return 22
 elif [[ "$scenario" == wrong_version ]]; then
  printf '%s' '{"version":"wrong","componentManifest":{"platform-api":{}}}'
 else
  printf '%s' '{"version":"1.0.99.20260924","componentManifest":{"platform-api":{"version":"test","image":"test","imageDigest":"test"}}}'
 fi
}
update_job() { [[ "$scenario" != status_failure ]]; }
for scenario in fetch_failure wrong_version status_failure; do
 if run_once; then echo "unexpected success: $scenario"; exit 1; fi
 [[ ! -e "$tmp/deployed" ]]
 [[ ! -e "$tmp/releases/1.0.99.20260924/release-manifest.json" ]]
done
scenario=success
run_once
[[ -e "$tmp/deployed" ]]
jq -e '.version == "1.0.99.20260924" and .componentManifest["platform-api"].image == "test"' "$tmp/releases/1.0.99.20260924/release-manifest.json" >/dev/null
echo 'upgrade runner failure handling passed'
