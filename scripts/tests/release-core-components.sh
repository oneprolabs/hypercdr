#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/go-root/bin" "$tmp/runtime"

cat >"$tmp/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG}"
EOF
cat >"$tmp/go-root/bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == "env GOROOT" ]]; then
  printf '%s\n' "${FAKE_GO_ROOT}"
  exit 0
fi
printf '%s\n' "$*" >>"${FAKE_GO_LOG}"
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then
    mkdir -p "$(dirname "$2")"
    : >"$2"
    exit 0
  fi
  shift
done
EOF
cat >"$tmp/bin/sha256sum" <<'EOF'
#!/usr/bin/env bash
printf '%s  %s\n' "${FAKE_SHA256}" "$1"
EOF
cat >"$tmp/bin/find" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *backend/internal/migrations/sql* ]]; then
  echo 000001_initial.sql
else
  exec /usr/bin/find "$@"
fi
EOF
cat >"$tmp/bin/cp" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == "/etc/ssl/certs/ca-certificates.crt" ]]; then
  : >"$2"
else
  exec /bin/cp "$@"
fi
EOF
chmod +x "$tmp/bin/cp" "$tmp/bin/docker" "$tmp/bin/find" "$tmp/bin/sha256sum" "$tmp/go-root/bin/go"

export FAKE_DOCKER_LOG="$tmp/docker.log"
export FAKE_GO_LOG="$tmp/go.log"

assert_pushes() {
  local component="$1"
  shift
  : >"$FAKE_DOCKER_LOG"
  if [[ "$component" == default ]]; then
    PATH="$tmp/bin:$PATH" "$root/scripts/release/push-release.sh" 1.0.0.20260922 \
      --registry registry.example.com/hypercdr >/dev/null
  else
    PATH="$tmp/bin:$PATH" "$root/scripts/release/push-release.sh" 1.0.0.20260922 \
      --registry registry.example.com/hypercdr --component "$component" >/dev/null
  fi
  [[ "$(wc -l <"$FAKE_DOCKER_LOG" | tr -d ' ')" == "$#" ]]
  for image in "$@"; do
    grep -Fxq "push registry.example.com/hypercdr/${image}:1.0.0.20260922" "$FAKE_DOCKER_LOG"
  done
}

assert_pushes platform platform-api platform-frontend
assert_pushes agents comm-agent oadp-comm-agent
assert_pushes executor cluster-registration-executor
assert_pushes default platform-api platform-frontend cluster-registration-executor comm-agent oadp-comm-agent

if PATH="$tmp/bin:$PATH" "$root/scripts/release/push-release.sh" 1.0.0.20260922 \
  --registry registry.example.com/hypercdr --component unknown >/dev/null 2>&1; then
  echo "unknown release component was accepted" >&2
  exit 1
fi

: >"$FAKE_DOCKER_LOG"
export FAKE_GO_ROOT="$tmp/go-root"
PATH="$tmp/bin:$PATH" HCDR_GO_BIN="$tmp/go-root/bin/go" \
  HCDR_RUNTIME_ROOT="$tmp/runtime" HCDR_BUILDX_CACHE=gha \
  "$root/scripts/release/build-release.sh" 1.0.0.20260922 --skip-tests \
  --registry registry.example.com/hypercdr --component agents >/dev/null

[[ "$(wc -l <"$FAKE_DOCKER_LOG" | tr -d ' ')" == 2 ]]
grep -Fq 'buildx build --load --cache-from type=gha,scope=comm-agent --cache-to type=gha,mode=max,scope=comm-agent' "$FAKE_DOCKER_LOG"
grep -Fq 'buildx build --load --cache-from type=gha,scope=oadp-comm-agent --cache-to type=gha,mode=max,scope=oadp-comm-agent' "$FAKE_DOCKER_LOG"

: >"$FAKE_DOCKER_LOG"
: >"$FAKE_GO_LOG"
touch "$tmp/kubectl"
export FAKE_SHA256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
PATH="$tmp/bin:$PATH" HCDR_GO_BIN="$tmp/go-root/bin/go" \
  HCDR_RUNTIME_ROOT="$tmp/runtime" HCDR_BUILDX_CACHE=gha \
  HCDR_REGISTRATION_KUBECTL_BINARY="$tmp/kubectl" \
  HCDR_REGISTRATION_KUBECTL_SHA256="$FAKE_SHA256" \
  "$root/scripts/release/build-release.sh" 1.0.0.20260922 \
  --registry registry.example.com/hypercdr --component executor >/dev/null

grep -Fxq 'test ./...' "$FAKE_GO_LOG"
[[ "$(wc -l <"$FAKE_DOCKER_LOG" | tr -d ' ')" == 1 ]]
grep -Fq 'buildx build --load --cache-from type=gha,scope=cluster-registration-executor' "$FAKE_DOCKER_LOG"

echo "release core component selection passed"
