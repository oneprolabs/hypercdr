#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${FEISHU_WEBHOOK:-}" ]]; then
  echo "::warning::FEISHU_WEBHOOK is not configured; skipping notification."
  exit 0
fi

REVIEW=""
if [[ "${PR_AGENT_OUTCOME:-}" == "success" ]]; then
  comments_url="${GITHUB_API_URL}/repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments?per_page=100"
  if comments="$(curl --fail --silent --show-error --retry 3 --max-time 20 \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${comments_url}")"; then
    REVIEW="$(jq -r '[.[] | select(.user.login == "github-actions[bot]") | select(.body | test("PR Reviewer Guide|PR Review"; "i"))] | last | .body // ""' <<<"${comments}")"
  else
    echo "::warning::Unable to read the PR-Agent review comment."
  fi
fi

requirements="$(awk '
  /Compliant requirements:/ { capture = 1; next }
  capture && /^- / {
    requirement = $0
    sub(/^- /, "", requirement)
    print requirement
    found = 1
    next
  }
  capture && found && NF { exit }
' <<<"${REVIEW}")"

if [[ -n "${requirements}" ]]; then
  requirements_json="$(jq -Rsc 'split("\n") | map(select(length > 0)) | map({requirement: ., status: "✅ Covered"})' <<<"${requirements}")"
elif [[ "${PR_AGENT_OUTCOME:-}" == "success" && -n "${REVIEW}" ]]; then
  requirements_json='[{"requirement":"Review requirement coverage details","status":"ℹ️ See full review"}]'
elif [[ "${PR_AGENT_OUTCOME:-}" == "success" ]]; then
  requirements_json='[{"requirement":"PR-Agent review","status":"⚠️ Review unavailable"}]'
else
  requirements_json='[{"requirement":"PR-Agent review","status":"❌ Failed"}]'
fi

if [[ "${PR_AGENT_OUTCOME:-}" != "success" ]]; then
  card_template="red"
  status_color="red"
  status_icon="❌"
  issues="PR-Agent review did not complete. Check the GitHub Actions logs."
  tests="Review failed; test coverage was not analyzed."
  merge_status="Not ready"
  merge_advice="Fix the AI Review workflow before manual verification."
elif [[ -z "${REVIEW}" ]]; then
  card_template="orange"
  status_color="orange"
  status_icon="⚠️"
  issues="No PR-Agent review comment was found."
  tests="Test and verification analysis is unavailable."
  merge_status="Needs attention"
  merge_advice="Check the AI Review workflow before merging."
elif grep -qi "No major issues detected" <<<"${REVIEW}"; then
  card_template="green"
  status_color="green"
  status_icon="✅"
  issues="No major issues detected."
  tests="PR-Agent completed its test and verification review. See the full review for details."
  merge_status="Ready after verification"
  merge_advice="No blocking issues were found. Complete human verification before merging."
else
  card_template="orange"
  status_color="orange"
  status_icon="⚠️"
  issues="$(jq -nr --arg review "${REVIEW}" '$review[0:3000]')"
  tests="PR-Agent completed its test and verification review. See the full review for details."
  merge_status="Needs human review"
  merge_advice="Review the findings before merging."
fi

report_date="$(TZ=Asia/Shanghai date +%F)"
pr_state="$(printf '%s' "${PR_STATE}" | tr '[:lower:]' '[:upper:]')"
status_highlight="${status_icon} <font color='${status_color}'>**${merge_status}**</font>"
footer="<font color='grey'>Generated automatically by PR-Agent. Complete human review before merging.</font>"

payload="$(jq -cn \
  --arg project "${PROJECT_NAME:-HyperCDR}" \
  --arg repository "${GITHUB_REPOSITORY}" \
  --arg report_date "${report_date}" \
  --arg pr_number "${PR_NUMBER}" \
  --arg pr_title "${PR_TITLE}" \
  --arg pr_author "${PR_AUTHOR}" \
  --arg pr_state "${pr_state}" \
  --arg head_ref "${PR_HEAD_REF}" \
  --arg base_ref "${PR_BASE_REF}" \
  --arg changed_files "${PR_CHANGED_FILES}" \
  --arg additions "${PR_ADDITIONS}" \
  --arg deletions "${PR_DELETIONS}" \
  --arg card_template "${card_template}" \
  --arg status_highlight "${status_highlight}" \
  --arg issues "${issues}" \
  --arg tests "${tests}" \
  --arg merge_advice "${merge_advice}" \
  --arg footer "${footer}" \
  --arg pr_url "${PR_URL}" \
  --argjson requirements "${requirements_json}" '
  def markdown($content): {tag: "markdown", content: $content};
  def column($content; $weight): {
    tag: "column", width: "weighted", weight: $weight,
    vertical_align: "center", elements: [markdown($content)]
  };
  def column_set($background; $columns): {
    tag: "column_set", flex_mode: "stretch",
    background_style: $background, columns: $columns
  };
  def requirement_row($item; $index):
    column_set((if ($index % 2) == 0 then "grey" else "default" end); [
      column($item.requirement; 4), column($item.status; 2)
    ]);
  {
    msg_type: "interactive",
    card: {
      schema: "2.0",
      config: {wide_screen_mode: true},
      header: {
        title: {tag: "plain_text", content: ("[Code Review] " + $project + " PR #" + $pr_number)},
        template: $card_template
      },
      body: {elements: ([
        markdown($status_highlight + "\n" + $merge_advice),
        markdown("**Repository**: " + $repository + "\n**Author**: " + $pr_author + "\n**Date**: " + $report_date + "\n**PR**: #" + $pr_number + " — " + $pr_title),
        {tag: "hr"},
        column_set("grey"; [
          column("**Status**\n" + $pr_state; 1),
          column("**Files Changed**\n" + $changed_files + " files | +" + $additions + " / -" + $deletions; 1)
        ]),
        column_set("default"; [
          column("**Source Branch**\n" + $head_ref; 1),
          column("**Target Branch**\n" + $base_ref; 1)
        ]),
        {tag: "hr"},
        markdown("**📋 Requirement Coverage**"),
        column_set("blue"; [column("**Requirement**"; 4), column("**Status**"; 2)])
      ] + [range(0; $requirements | length) as $index | requirement_row($requirements[$index]; $index)] + [
        {tag: "hr"},
        markdown("**⚠️ Issues Found**\n" + $issues),
        markdown("**🧪 Tests and Verification**\n" + $tests),
        {tag: "hr"},
        markdown("🔗 [View Full Review](" + $pr_url + ")"),
        markdown($footer)
      ])}
    }
  }')"

response="$(curl --fail --silent --show-error --retry 3 --max-time 20 \
  -X POST -H "Content-Type: application/json" --data "${payload}" "${FEISHU_WEBHOOK}")"
jq -e '(.code? == 0) or (.StatusCode? == 0)' <<<"${response}" >/dev/null || {
  echo "::error::Feishu rejected the notification payload."
  exit 1
}
