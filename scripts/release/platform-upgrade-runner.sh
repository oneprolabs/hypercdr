#!/usr/bin/env bash
set -Eeuo pipefail

INSTALL_DIR="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"
ENV_FILE="${INSTALL_DIR}/.env"
API_URL="${HCDR_UPGRADE_RUNNER_URL:-https://127.0.0.1:12443}"
[[ -r "$ENV_FILE" ]] || { echo "missing ${ENV_FILE}" >&2; exit 1; }
set -a; # shellcheck disable=SC1090
source "$ENV_FILE"; set +a
TOKEN="${HCDR_RELEASE_TOKEN:-}"
[[ -n "$TOKEN" ]] || { echo "HCDR_RELEASE_TOKEN is required" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

api() { curl -ksSf --connect-timeout 5 --max-time 30 -H "X-HyperCDR-Release-Token: ${TOKEN}" "$@"; }
update_job() {
  local id="$1" status="$2" step="$3" progress="$4" message="${5:-}"
  curl -ksSf --connect-timeout 5 --max-time 30 -X POST \
    -H "X-HyperCDR-Release-Token: ${TOKEN}" -H 'Content-Type: application/json' \
    --data "$(jq -nc --arg s "$status" --arg t "$step" --arg m "$message" --argjson p "$progress" --arg e "$(hostname):$$" --argjson started "$([[ "$status" == running ]] && echo true || echo false)" --argjson done "$([[ "$status" == succeeded || "$status" == failed ]] && echo true || echo false)" '{status:$s,step:$t,progress:$p,errorMessage:$m,executorId:$e,markStarted:$started,markDone:$done}')" \
    "${API_URL}/api/v1/platform/upgrades/${id}/status" >/dev/null
}

run_once() {
  local job version id
  job="$(api "${API_URL}/api/v1/platform/upgrades" | jq -c '[.items[] | select(.status == "queued")] | .[0] // empty')"
  [[ -n "$job" ]] || return 0
  id="$(jq -r .id <<<"$job")"; version="$(jq -r .targetVersion <<<"$job")"
  update_job "$id" running preparing 5
  if [[ ! -s "${INSTALL_DIR}/releases/${version}/release-manifest.json" ]]; then
    update_job "$id" failed failed 100 "release manifest is missing for ${version}"
    return 1
  fi
  if HCDR_INSTALL_DIR="$INSTALL_DIR" HCDR_COMPOSE_FILE="${INSTALL_DIR}/docker-compose.yaml" "${INSTALL_DIR}/deploy-blue-green.sh" "$version"; then
    update_job "$id" succeeded completed 100
  else
    update_job "$id" failed failed 100 "blue-green deployment failed"
    return 1
  fi
}

while true; do
  run_once || true
  sleep "${HCDR_UPGRADE_RUNNER_INTERVAL:-15}"
done
