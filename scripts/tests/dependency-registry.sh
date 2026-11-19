#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$root/scripts/release/dependency-registry.sh"
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
mkdir -p "$runtime/validation"
work="$(mktemp -d "$runtime/validation/dependency-registry.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "$FAKE_REGISTRY_MODE" in
  present) printf '{"digest":"sha256:%064d"}\n' 0 ;;
  missing) echo 'manifest unknown' >&2; exit 1 ;;
  missing_acr) echo "ERROR: $4: not found" >&2; exit 1 ;;
  missing_command) echo 'docker: command not found' >&2; exit 127 ;;
  denied) echo 'unauthorized' >&2; exit 1 ;;
  offline) echo 'connection timed out' >&2; exit 1 ;;
  malformed) echo '{}' ;;
  stalled) sleep 20 ;;
esac
EOF
chmod +x "$work/bin/docker"
export PATH="$work/bin:$PATH"
export FAKE_REGISTRY_MODE=present
digest="$(printf 'sha256:%064d' 0)"
dependency_verify_reference example.invalid/image:fixed "$digest"
if dependency_verify_reference example.invalid/image:fixed "sha256:$(printf '%064d' 1)"; then exit 1; fi
for mode in missing missing_acr missing_command denied offline malformed stalled; do
  export FAKE_REGISTRY_MODE="$mode" HCDR_MANIFEST_TIMEOUT_SECONDS=1
  if dependency_remote_digest example.invalid/image:fixed; then exit 1; else status=$?; fi
  expected=2
  [[ "$mode" != missing && "$mode" != missing_acr ]] || expected=3
  [[ "$status" == "$expected" ]]
done
echo 'Dependency registry: digest verification, absence, auth, network, malformed response and timeout passed'
