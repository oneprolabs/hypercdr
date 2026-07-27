#!/usr/bin/env bash
set -euo pipefail

HARBOR_URL="${HCDR_HARBOR_URL:-}"
PROJECT="${HCDR_HARBOR_PROJECT:-hypercdr}"
USERNAME="${HCDR_HARBOR_USERNAME:-admin}"
PASSWORD="${HCDR_HARBOR_PASSWORD:-Harbor12345}"
INSECURE="true"
EXECUTE="false"
HCDR_CACHE_ROOT="${HCDR_CACHE_ROOT:-/data/tmp/hypercdr}"
TMPDIR="${TMPDIR:-${HCDR_CACHE_ROOT}/tmp}"

usage() {
  cat <<'USAGE'
Initialize HyperCDR Harbor project

Usage:
  ./init-project.sh --harbor-url https://HOST [options]

Options:
  --harbor-url URL      Harbor URL, for example https://192.168.8.149:5001.
  --project NAME        Project name, default: hypercdr.
  --username USER       Harbor username, default: admin.
  --password PASSWORD   Harbor password.
  --secure              Verify Harbor TLS certificate. Default is insecure for self-signed lab certs.
  --execute             Apply changes. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --harbor-url) HARBOR_URL="${2:?missing value for --harbor-url}"; shift 2 ;;
    --project) PROJECT="${2:?missing value for --project}"; shift 2 ;;
    --username) USERNAME="${2:?missing value for --username}"; shift 2 ;;
    --password) PASSWORD="${2:?missing value for --password}"; shift 2 ;;
    --secure) INSECURE="false"; shift ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${HARBOR_URL}" ]]; then
  echo "--harbor-url is required" >&2
  exit 2
fi

CURL_TLS=()
if [[ "${INSECURE}" == "true" ]]; then
  CURL_TLS=(-k)
fi

echo "Harbor project init plan"
echo "Harbor URL:      ${HARBOR_URL}"
echo "Project:         ${PROJECT}"
echo "Username:        ${USERNAME}"
echo "TLS verification: $([[ "${INSECURE}" == "true" ]] && echo "disabled" || echo "enabled")"
echo "Execute changes: ${EXECUTE}"

if [[ "${EXECUTE}" != "true" ]]; then
  echo "Dry-run mode. Add --execute to create the project."
  exit 0
fi

encoded_project="$(printf '%s' "${PROJECT}" | sed 's|/|%2F|g')"
mkdir -p "${TMPDIR}"
project_response="${TMPDIR}/hcdr-harbor-project.json"
create_response="${TMPDIR}/hcdr-harbor-create-project.json"
status="$(curl "${CURL_TLS[@]}" -sS -u "${USERNAME}:${PASSWORD}" -o "${project_response}" -w '%{http_code}' "${HARBOR_URL%/}/api/v2.0/projects/${encoded_project}")"
if [[ "${status}" == "200" ]]; then
  echo "Project already exists: ${PROJECT}"
  exit 0
fi
if [[ "${status}" != "404" ]]; then
  echo "failed to query Harbor project, HTTP ${status}" >&2
  cat "${project_response}" >&2 || true
  exit 1
fi

payload="$(printf '{"project_name":"%s","public":false,"metadata":{"public":"false"}}' "${PROJECT}")"
create_status="$(curl "${CURL_TLS[@]}" -sS -u "${USERNAME}:${PASSWORD}" -H 'Content-Type: application/json' -o "${create_response}" -w '%{http_code}' -d "${payload}" "${HARBOR_URL%/}/api/v2.0/projects")"
if [[ "${create_status}" != "201" ]]; then
  echo "failed to create Harbor project, HTTP ${create_status}" >&2
  cat "${create_response}" >&2 || true
  exit 1
fi

echo "Project created: ${PROJECT}"
