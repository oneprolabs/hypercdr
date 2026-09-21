#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"

cat >"$tmp/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"/issues/12/comments"* ]]; then
  jq -cn '[{user:{login:"github-actions[bot]"},body:"PR Reviewer Guide\nNo major issues detected\nCompliant requirements:\n- Preserve blue/green deployment\n"}]'
  exit 0
fi
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "--data" ]]; then
    printf '%s' "$2" >"${FAKE_FEISHU_PAYLOAD}"
    break
  fi
  shift
done
printf '%s\n' '{"code":0}'
EOF
chmod +x "$tmp/bin/curl"

export FAKE_FEISHU_PAYLOAD="$tmp/payload.json"
PATH="$tmp/bin:$PATH" \
FEISHU_WEBHOOK=https://example.invalid/webhook \
GITHUB_API_URL=https://api.github.invalid \
GITHUB_REPOSITORY=oneprolabs/hypercdr \
GITHUB_TOKEN=test-token \
PR_AGENT_OUTCOME=success \
PR_NUMBER=12 \
PR_TITLE='Parallelize release publishing' \
PR_AUTHOR=tester \
PR_STATE=open \
PR_HEAD_REF=codex/parallel-release \
PR_BASE_REF=main \
PR_CHANGED_FILES=6 \
PR_ADDITIONS=120 \
PR_DELETIONS=20 \
PR_URL=https://github.invalid/oneprolabs/hypercdr/pull/12 \
  "$root/scripts/ci/notify-pr-review.sh"

jq -e '.card.header.title.content == "[Code Review] HyperCDR PR #12"' "$tmp/payload.json" >/dev/null
jq -e '[.card.body.elements[] | .. | strings] | any(contains("Preserve blue/green deployment"))' "$tmp/payload.json" >/dev/null
jq -e '[.card.body.elements[] | .. | strings] | any(contains("Ready after verification"))' "$tmp/payload.json" >/dev/null

echo "PR review notification payload passed"
