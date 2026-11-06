#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fixture="$tmp/repository"
mkdir -p "$fixture/scripts/release" "$fixture/scripts/lib" "$fixture/third_party/velero/deployments"
cp "$root/scripts/release/build-velero.sh" "$root/scripts/release/common.sh" "$fixture/scripts/release/"
cp "$root/scripts/lib/registry-config.sh" "$fixture/scripts/lib/"
git -C "$fixture" init -q
git -C "$fixture" -c user.name=Test -c user.email=test@example.invalid commit --allow-empty -qm fixture
export EXPECTED_SOURCE_COMMIT="$(git -C "$fixture" rev-parse HEAD)"

cat >"$fixture/third_party/velero/deployments/prepare-patched-source.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$1/deployments"
cat >"$1/deployments/build-velero-image.sh" <<'BUILD'
#!/usr/bin/env bash
set -euo pipefail
[[ "$HCDR_VELERO_TARGET_IMAGE" == registry.example.com/hypercdr/velero:runtime-test ]]
[[ "$HCDR_VELERO_SOURCE_COMMIT" == "$EXPECTED_SOURCE_COMMIT" ]]
printf '%s\n' "$@" >"$BUILD_ARGS_LOG"
BUILD
chmod +x "$1/deployments/build-velero-image.sh"
EOF
chmod +x "$fixture/third_party/velero/deployments/prepare-patched-source.sh"

export BUILD_ARGS_LOG="$tmp/arguments"
for mode in default override; do
  runtime="$tmp/hypercdr-runtime"
  if [[ "$mode" == override ]]; then
    runtime="$tmp/custom/runtime"
    export HCDR_RUNTIME_ROOT="$runtime"
  else
    unset HCDR_RUNTIME_ROOT
  fi
  [[ ! -e "$runtime" ]]
  bash "$fixture/scripts/release/build-velero.sh" --registry registry.example.com/hypercdr --tag runtime-test --push
  [[ -d "$runtime/build" ]]
  grep -Fxq "$runtime/build/velero/runtime-test" "$BUILD_ARGS_LOG"
  grep -Fxq -- '--push' "$BUILD_ARGS_LOG"
done
echo "Velero build initializes absent default and custom runtime directories"
