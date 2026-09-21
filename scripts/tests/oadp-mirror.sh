#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/runtime"

cat >"$tmp/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG}"
if [[ "$1 $2 $3" == "manifest inspect --verbose" ]]; then
  case "$4" in
    *oadp-operator*) digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ;;
    *oadp-velero*) digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ;;
    *) exit 1 ;;
  esac
  jq -cn --arg digest "$digest" '{Descriptor:{digest:$digest}}'
fi
EOF
chmod +x "$tmp/bin/docker"

cat >"$tmp/lock.json" <<'EOF'
{
  "release": "1.3.10-hcdr.1",
  "platform": "linux/amd64",
  "images": [
    {
      "component": "oadp-operator",
      "source": "example.invalid/oadp-operator:1.3",
      "sourceDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "required": true
    },
    {
      "component": "oadp-velero",
      "source": "example.invalid/oadp-velero:1.3",
      "sourceDigest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "required": true
    }
  ]
}
EOF

export FAKE_DOCKER_LOG="$tmp/docker.log"
PATH="$tmp/bin:$PATH" HCDR_RUNTIME_ROOT="$tmp/runtime" HCDR_OADP_PARALLELISM=2 \
  "$root/scripts/release/mirror-community-oadp-images.sh" \
  --registry registry.example.com/hypercdr \
  --lock "$tmp/lock.json" \
  --output "$tmp/resolved.json"

[[ "$(jq '[.images[] | select(.required)] | length' "$tmp/resolved.json")" == 2 ]]
[[ "$(jq -r '.images[] | select(.component == "oadp-operator") | .targetDigest' "$tmp/resolved.json")" == sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ]]
! grep -Eq '^(pull|push) ' "$tmp/docker.log"

echo "OADP mirror reuse and parallel aggregation passed"
