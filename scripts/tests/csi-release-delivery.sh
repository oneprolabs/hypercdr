#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime="${HCDR_RUNTIME_ROOT:-$(dirname "$root")/hypercdr-runtime}"
mkdir -p "$runtime/validation"
work="$(mktemp -d "$runtime/validation/csi-delivery.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/source/third_party/velero/pkg" "$work/bin"
git -C "$work/source" init -q
printf baseline > "$work/source/third_party/velero/pkg/source.go"
git -C "$work/source" add .
source "$root/scripts/release/dependency-fingerprint.sh"
ROOT_DIR="$work/source"
initial="$(default_velero_image_tag)"
[[ "$initial" == "$(default_velero_image_tag)" && ${#initial} -le 128 ]]
printf changed >> "$work/source/third_party/velero/pkg/source.go"
changed="$(default_velero_image_tag)"
[[ "$initial" != "$changed" ]]
printf new > "$work/source/third_party/velero/pkg/new.go"
untracked="$(default_velero_image_tag)"
[[ "$untracked" != "$changed" ]]
git -C "$work/source" add .
[[ "$untracked" == "$(default_velero_image_tag)" ]]
rm "$work/source/third_party/velero/pkg/new.go"
[[ "$untracked" != "$(default_velero_image_tag)" ]]
ROOT_DIR="$work"
if default_velero_image_tag > "$work/invalid-source.log" 2>&1; then exit 1; fi
export MOCK_ROLE="$work/role.json" MOCK_CALLS="$work/calls"
cat > "$MOCK_ROLE" <<'EOF'
{"metadata":{"name":"hypercdr-agent","resourceVersion":"1"},"rules":[{"apiGroups":["*"],"resources":["*"],"verbs":["get","list"]},{"apiGroups":["apps"],"resources":["deployments"],"verbs":["get","patch"]}]}
EOF
cp "$MOCK_ROLE" "$work/original.json"
cat > "$work/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "$MOCK_CALLS"
if [[ "$*" == *'get clusterrole '* ]]; then cat "$MOCK_ROLE"; exit 0; fi
if [[ "$*" != *'patch clusterrole '* ]]; then exit 90; fi
if [[ "${MOCK_DENIED:-false}" == true ]]; then echo Forbidden >&2; exit 1; fi
while [[ $# -gt 0 ]]; do
  if [[ "$1" == -p ]]; then patch="$2"; break; fi
  shift
done
jq -e '.[0].op == "test" and .[0].path == "/metadata/resourceVersion" and .[1].path == "/rules"' <<< "$patch" >/dev/null
if [[ "$*" != *'--dry-run=server'* ]]; then
  jq --argjson patch "$patch" '.rules=$patch[1].value' "$MOCK_ROLE" > "$MOCK_ROLE.next"
  mv "$MOCK_ROLE.next" "$MOCK_ROLE"
fi
EOF
chmod +x "$work/bin/kubectl"
export PATH="$work/bin:$PATH"
bash "$root/scripts/release/upgrade-cluster-csi-rbac.sh" > "$work/preview.log"
cmp "$MOCK_ROLE" "$work/original.json"
bash "$root/scripts/release/upgrade-cluster-csi-rbac.sh" --apply > "$work/apply.log"
jq -e '.rules | length == 6 and .[0].verbs == ["get","list"] and .[1].resources == ["deployments"]' "$MOCK_ROLE" >/dev/null
cp "$MOCK_ROLE" "$work/repaired.json"
bash "$root/scripts/release/upgrade-cluster-csi-rbac.sh" --apply > "$work/repeat.log"
cmp "$MOCK_ROLE" "$work/repaired.json"
grep -q 'already current' "$work/repeat.log"
cp "$work/original.json" "$MOCK_ROLE"
export MOCK_DENIED=true
if bash "$root/scripts/release/upgrade-cluster-csi-rbac.sh" --apply > "$work/denied.log" 2>&1; then exit 1; fi
cmp "$MOCK_ROLE" "$work/original.json"
unset MOCK_DENIED
bash "$root/scripts/release/upgrade-cluster-csi-rbac.sh" --namespace enterprise-agent --apply > "$work/enterprise.log"
expected="hypercdr-agent-$(printf enterprise-agent | sha256sum | cut -c1-8)"
grep -q "get clusterrole $expected" "$MOCK_CALLS"
echo 'CSI release source identity and administrator-only RBAC migration passed'
