#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REMOTE_NAME="github"
EXPECTED_REMOTE="git@github.com:oneprolabs/hypercdr.git"
VERSION=""
COMMIT_MESSAGE=""

usage() {
  cat <<'USAGE'
Commit and publish a HyperCDR community release to GitHub.

Usage:
  scripts/release/publish-github.sh --version ce-vX.Y.Z [--message MESSAGE]

The VERSION file must already contain the exact requested version. This
script never pushes origin, overwrites tags, or changes an existing tag.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:?missing value for --version}"; shift 2 ;;
    --message) COMMIT_MESSAGE="${2:?missing value for --message}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "${VERSION}" ]] || { echo "error: --version is required" >&2; exit 2; }
[[ "${VERSION}" =~ ^ce-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "error: invalid community version: ${VERSION}" >&2
  exit 2
}

cd "${ROOT_DIR}"
[[ "$(git branch --show-current)" == "main" ]] || { echo "error: release must run on main" >&2; exit 2; }
actual_remote="$(git remote get-url "${REMOTE_NAME}")"
[[ "${actual_remote}" == "${EXPECTED_REMOTE}" ]] || {
  echo "error: ${REMOTE_NAME} is not the required SSH remote: ${actual_remote}" >&2
  exit 2
}
[[ "$(cat VERSION)" == "${VERSION}" ]] || {
  echo "error: VERSION ($(cat VERSION)) does not match ${VERSION}" >&2
  exit 2
}
git diff --check

if [[ -z "${COMMIT_MESSAGE}" ]]; then
  COMMIT_MESSAGE="发布 ${VERSION}"
fi

git add -A
if ! git diff --cached --quiet; then
  git commit -m "${COMMIT_MESSAGE}"
fi

[[ "$(git describe --exact-match --tags HEAD 2>/dev/null || true)" == "" ]] || {
  echo "error: HEAD already has a tag; refusing to create another release tag" >&2
  exit 2
}
if git show-ref --verify --quiet "refs/tags/${VERSION}"; then
  echo "error: local tag already exists: ${VERSION}" >&2
  exit 2
fi
if git ls-remote --exit-code --tags "${REMOTE_NAME}" "refs/tags/${VERSION}" >/dev/null 2>&1; then
  echo "error: remote tag already exists: ${VERSION}" >&2
  exit 2
fi

git tag -a "${VERSION}" -m "${VERSION}"
git push "${REMOTE_NAME}" main
git push "${REMOTE_NAME}" "${VERSION}"

remote_commit="$(git ls-remote "${REMOTE_NAME}" refs/heads/main | awk '{print $1}')"
local_commit="$(git rev-parse HEAD)"
[[ "${remote_commit}" == "${local_commit}" ]] || { echo "error: remote main verification failed" >&2; exit 1; }
remote_tag="$(git ls-remote "${REMOTE_NAME}" "refs/tags/${VERSION}^{}" | awk '{print $1}')"
[[ "${remote_tag}" == "${local_commit}" ]] || { echo "error: remote tag verification failed" >&2; exit 1; }
echo "published ${VERSION} at ${local_commit} via ${REMOTE_NAME} SSH"
