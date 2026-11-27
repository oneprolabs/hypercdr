#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
jq -e '.schemaVersion == 1 and (.runtime.postgres.targetDigest|test("^sha256:[0-9a-f]{64}$")) and .runtime.velero.buildFromSource and .runtime.velero.sourceAddressed and (.runtime.plugins.targets|length == 3) and .oadpLock' "$root/packaging/dependencies/lock.json" >/dev/null
bash -n "$root/scripts/release/common.sh" "$root/scripts/release/publish-runtime-images.sh" "$root/scripts/release/sync-velero-plugins.sh"
echo 'Dependency lock and reuse checks passed'
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
mkdir -p "$runtime/validation"
work="$(mktemp -d "$runtime/validation/reuse-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
if [[ "${MOCK_MODE}" == locked ]]; then
  printf '%s\n' "$*" >>"$MOCK_CALLS"
  [[ "$1 $2 $3" == 'manifest inspect --verbose' ]] || exit 77
  case "$4" in
    *postgres-16) query='.runtime.postgres.targetDigest' ;;
    *velero-v1.18.2-hcdr.9-build.*)
      printf '{"Descriptor":{"digest":"sha256:%064d"}}\n' 1
      exit 0 ;;
    *velero-plugin-for-aws-v1.13.0) query='.runtime.plugins.targets["velero-plugin-for-aws"]' ;;
    *velero-plugin-for-microsoft-azure-v1.13.0) query='.runtime.plugins.targets["velero-plugin-for-microsoft-azure"]' ;;
    *velero-plugin-for-gcp-v1.13.0) query='.runtime.plugins.targets["velero-plugin-for-gcp"]' ;;
    *) exit 78 ;;
  esac
  digest="$(jq -er "$query" "$MOCK_LOCK")" || exit 79
  jq -n --arg digest "$digest" '{Descriptor:{digest:$digest}}'
  exit 0
fi
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
source "$root/scripts/release/dependency-fingerprint.sh"
[[ "$(velero_release_version)" == v1.18.2-hcdr.9 ]]
[[ "$(default_velero_image_tag)" =~ ^v1\.18\.2-hcdr\.9-build\.[0-9a-f]{40}-backup-volume-policy-v1-isolation-cache-v1$ ]]
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
export MOCK_MODE=locked MOCK_CALLS="$work/calls.log" MOCK_LOCK="$root/packaging/dependencies/lock.json"
bash "$root/scripts/release/publish-runtime-images.sh" --registry registry.cn-beijing.aliyuncs.com/example/hypercdr
bash "$root/scripts/release/sync-velero-plugins.sh" --registry registry.cn-beijing.aliyuncs.com/example/hypercdr
[[ "$(wc -l <"$MOCK_CALLS")" == 5 ]]
! grep -Eq '^(pull|build|push|tag) ' "$MOCK_CALLS"
echo 'Actual runtime and plugin entry points: five target metadata queries, zero layer transfers or builds'
export HCDR_VELERO_IMAGE_TAG=v1.18.2-hcdr.7
if bash "$root/scripts/release/publish-runtime-images.sh" --registry registry.cn-beijing.aliyuncs.com/example/hypercdr >"$work/stale.log" 2>&1; then exit 1; fi
grep -q 'refusing a stale or mutable tag' "$work/stale.log"
unset HCDR_VELERO_IMAGE_TAG
if bash "$root/scripts/release/publish-runtime-images.sh" --registry registry.cn-beijing.aliyuncs.com/example/hypercdr --force-velero-build >"$work/force.log" 2>&1; then exit 1; fi
grep -q 'refusing to overwrite immutable' "$work/force.log"
