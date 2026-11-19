#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
jq -e '.schemaVersion == 1 and (.runtime.postgres.targetDigest|test("^sha256:[0-9a-f]{64}$")) and (.runtime.velero.targetDigest|test("^sha256:[0-9a-f]{64}$")) and (.runtime.plugins.targets|length == 3) and .oadpLock' "$root/packaging/dependencies/lock.json" >/dev/null
bash -n "$root/scripts/release/common.sh" "$root/scripts/release/publish-runtime-images.sh" "$root/scripts/release/sync-velero-plugins.sh"
echo 'Dependency lock and reuse checks passed'
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
mkdir -p "$runtime/validation"
work="$(mktemp -d "$runtime/validation/reuse-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "$MOCK_MODE" in
  present) printf '{"Descriptor":{"digest":"sha256:%064d"}}\n' 0 ;;
  missing) echo 'no such manifest' >&2; exit 1 ;;
  denied) echo 'unauthorized' >&2; exit 1 ;;
  malformed) echo '{}' ;;
  slow) sleep 5 ;;
esac
EOF
chmod +x "$work/bin/docker"
export PATH="$work/bin:$PATH"
source "$root/scripts/release/common.sh"
expected="$(printf 'sha256:%064d' 0)"
export MOCK_MODE=present
dependency_target_matches_digest example.invalid/image:fixed "$expected"
if dependency_target_matches_digest example.invalid/image:fixed "$(printf 'sha256:%064d' 1)"; then exit 1; else [[ "$?" == 2 ]]; fi
for mode in missing denied malformed slow; do
  export MOCK_MODE="$mode" HCDR_DOCKER_OPERATION_TIMEOUT_SECONDS=1
  expected_status=2
  [[ "$mode" != missing ]] || expected_status=1
  if dependency_target_matches_digest example.invalid/image:fixed "$expected"; then
    exit 1
  else
    [[ "$?" == "$expected_status" ]]
  fi
done
echo 'Reuse, mismatch, absence, auth, malformed response and timeout tests passed'
