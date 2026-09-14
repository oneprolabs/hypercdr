#!/usr/bin/env bash
set -euo pipefail
version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --edition) [[ "${2:?edition required}" == community ]] || exit 2; shift 2 ;;
    --version) version="${2:?version required}"; shift 2 ;;
    *) echo 'Usage: build-release.sh --version VERSION [--edition community]' >&2; exit 2 ;;
  esac
done
exec bash "$(dirname "${BASH_SOURCE[0]}")/package-release.sh" "${version}"
